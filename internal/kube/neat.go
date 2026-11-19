package kube

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

var runtimeAnnotations = map[string]bool{
	"kubectl.kubernetes.io/last-applied-configuration": true,
	"deployment.kubernetes.io/revision":                true,
	"pv.kubernetes.io/bind-completed":                  true,
	"pv.kubernetes.io/bound-by-controller":             true,
	"volume.beta.kubernetes.io/storage-provisioner":    true,
	"volume.kubernetes.io/storage-provisioner":         true,
	"control-plane.alpha.kubernetes.io/leader":         true,
}

// ObjectToNeatYAML strips server-managed state, runtime fields, and defaulted
// values from a Kubernetes object before rendering it as YAML.
func ObjectToNeatYAML(obj *unstructured.Unstructured) (string, error) {
	neat := NeatObject(obj)
	b, err := yaml.Marshal(neat.Object)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// NeatObject returns a deep-copied object with cluster state and default fields removed.
func NeatObject(obj *unstructured.Unstructured) *unstructured.Unstructured {
	out := obj.DeepCopy()
	m := out.Object

	delete(m, "status")
	cleanMetadata(asMap(m["metadata"]))

	kind, _ := m["kind"].(string)
	spec := asMap(m["spec"])
	if spec == nil {
		return out
	}

	switch kind {
	case "Pod":
		cleanPodSpec(spec, true)
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController":
		cleanWorkloadSpec(kind, spec)
		if tpl := asMap(spec["template"]); tpl != nil {
			cleanTemplateMetadata(asMap(tpl["metadata"]))
			if ps := asMap(tpl["spec"]); ps != nil {
				cleanPodSpec(ps, false)
			}
		}
	case "Job":
		if tpl := asMap(spec["template"]); tpl != nil {
			cleanTemplateMetadata(asMap(tpl["metadata"]))
			if ps := asMap(tpl["spec"]); ps != nil {
				cleanPodSpec(ps, false)
			}
		}
	case "CronJob":
		if jt := asMap(spec["jobTemplate"]); jt != nil {
			cleanTemplateMetadata(asMap(jt["metadata"]))
			if jspec := asMap(jt["spec"]); jspec != nil {
				if tpl := asMap(jspec["template"]); tpl != nil {
					cleanTemplateMetadata(asMap(tpl["metadata"]))
					if ps := asMap(tpl["spec"]); ps != nil {
						cleanPodSpec(ps, false)
					}
				}
			}
		}
	case "Service":
		cleanServiceSpec(spec)
	case "PersistentVolumeClaim":
		delete(spec, "volumeName")
		deleteIfEqual(spec, "volumeMode", "Filesystem")
	}

	pruneEmptyMaps(spec)
	return out
}

func cleanMetadata(meta map[string]any) {
	if meta == nil {
		return
	}
	for _, k := range []string{
		"creationTimestamp",
		"generation",
		"resourceVersion",
		"uid",
		"selfLink",
		"managedFields",
	} {
		delete(meta, k)
	}
	if anns := asMap(meta["annotations"]); anns != nil {
		for k := range runtimeAnnotations {
			delete(anns, k)
		}
		if len(anns) == 0 {
			delete(meta, "annotations")
		}
	}
	for _, listKey := range []string{"labels", "finalizers"} {
		if m := asMap(meta[listKey]); m != nil && len(m) == 0 {
			delete(meta, listKey)
		} else if s, ok := meta[listKey].([]any); ok && len(s) == 0 {
			delete(meta, listKey)
		}
	}
}

func cleanTemplateMetadata(meta map[string]any) {
	if meta == nil {
		return
	}
	delete(meta, "creationTimestamp")
	if anns := asMap(meta["annotations"]); anns != nil && len(anns) == 0 {
		delete(meta, "annotations")
	}
	if len(meta) == 0 {
		return
	}
}

func cleanWorkloadSpec(kind string, spec map[string]any) {
	deleteIfInt(spec, "revisionHistoryLimit", 10)
	deleteIfInt(spec, "progressDeadlineSeconds", 600)
	if kind == "Deployment" {
		if strat := asMap(spec["strategy"]); strat != nil {
			if strat["type"] == "RollingUpdate" {
				if ru := asMap(strat["rollingUpdate"]); ru == nil || (ru["maxSurge"] == "25%" && ru["maxUnavailable"] == "25%") {
					delete(spec, "strategy")
				}
			}
		}
	}
}

func cleanServiceSpec(spec map[string]any) {
	delete(spec, "clusterIP")
	delete(spec, "clusterIPs")
	delete(spec, "ipFamilies")
	deleteIfEqual(spec, "ipFamilyPolicy", "SingleStack")
	deleteIfEqual(spec, "internalTrafficPolicy", "Cluster")
	deleteIfEqual(spec, "sessionAffinity", "None")
}

func cleanPodSpec(spec map[string]any, isPod bool) {
	if isPod {
		delete(spec, "nodeName")
		delete(spec, "nominatedNodeName")
		if tols, ok := spec["tolerations"].([]any); ok {
			kept := make([]any, 0, len(tols))
			for _, item := range tols {
				if !isDefaultNodeToleration(asMap(item)) {
					kept = append(kept, item)
				}
			}
			if len(kept) == 0 {
				delete(spec, "tolerations")
			} else {
				spec["tolerations"] = kept
			}
		}
	}

	deleteIfEqual(spec, "dnsPolicy", "ClusterFirst")
	deleteIfEqual(spec, "restartPolicy", "Always")
	deleteIfEqual(spec, "schedulerName", "default-scheduler")
	deleteIfEqual(spec, "preemptionPolicy", "PreemptLowerPriority")
	deleteIfInt(spec, "terminationGracePeriodSeconds", 30)
	if v, ok := spec["enableServiceLinks"].(bool); ok && v {
		delete(spec, "enableServiceLinks")
	}

	if _, hasNew := spec["serviceAccountName"]; hasNew {
		delete(spec, "serviceAccount")
	}
	deleteIfEqual(spec, "serviceAccountName", "default")
	deleteIfEqual(spec, "serviceAccount", "default")

	if sc := asMap(spec["securityContext"]); sc != nil && len(sc) == 0 {
		delete(spec, "securityContext")
	}

	strippedVols := map[string]bool{}
	if vols, ok := spec["volumes"].([]any); ok {
		kept := make([]any, 0, len(vols))
		for _, item := range vols {
			vm := asMap(item)
			name, _ := vm["name"].(string)
			if isServiceAccountVolume(name, vm) {
				strippedVols[name] = true
				continue
			}
			kept = append(kept, item)
		}
		if len(kept) == 0 {
			delete(spec, "volumes")
		} else {
			spec["volumes"] = kept
		}
	}

	for _, cKey := range []string{"containers", "initContainers", "ephemeralContainers"} {
		if cs, ok := spec[cKey].([]any); ok {
			for _, item := range cs {
				if cm := asMap(item); cm != nil {
					cleanContainer(cm, strippedVols)
				}
			}
		}
	}
}

func cleanContainer(c map[string]any, strippedVols map[string]bool) {
	deleteIfEqual(c, "terminationMessagePath", "/dev/termination-log")
	deleteIfEqual(c, "terminationMessagePolicy", "File")

	image, _ := c["image"].(string)
	if isImplicitLatest(image) {
		deleteIfEqual(c, "imagePullPolicy", "Always")
	} else if image != "" {
		deleteIfEqual(c, "imagePullPolicy", "IfNotPresent")
	}

	if res := asMap(c["resources"]); res != nil && len(res) == 0 {
		delete(c, "resources")
	}
	if sc := asMap(c["securityContext"]); sc != nil && len(sc) == 0 {
		delete(c, "securityContext")
	}

	if vms, ok := c["volumeMounts"].([]any); ok {
		kept := make([]any, 0, len(vms))
		for _, item := range vms {
			vm := asMap(item)
			name, _ := vm["name"].(string)
			mountPath, _ := vm["mountPath"].(string)
			if strippedVols[name] || mountPath == "/var/run/secrets/kubernetes.io/serviceaccount" {
				continue
			}
			kept = append(kept, item)
		}
		if len(kept) == 0 {
			delete(c, "volumeMounts")
		} else {
			c["volumeMounts"] = kept
		}
	}
}

func isServiceAccountVolume(name string, v map[string]any) bool {
	if strings.HasPrefix(name, "kube-api-access-") || strings.HasPrefix(name, "default-token-") {
		return true
	}
	if proj := asMap(v["projected"]); proj != nil {
		if srcs, ok := proj["sources"].([]any); ok {
			for _, s := range srcs {
				if sm := asMap(s); sm != nil && sm["serviceAccountToken"] != nil {
					return strings.HasPrefix(name, "kube-api-access-")
				}
			}
		}
	}
	return false
}

func isDefaultNodeToleration(t map[string]any) bool {
	if t == nil {
		return false
	}
	key, _ := t["key"].(string)
	op, _ := t["operator"].(string)
	eff, _ := t["effect"].(string)
	return (key == "node.kubernetes.io/not-ready" || key == "node.kubernetes.io/unreachable") &&
		op == "Exists" && eff == "NoExecute"
}

func isImplicitLatest(image string) bool {
	if image == "" {
		return false
	}
	lastSlash := strings.LastIndex(image, "/")
	tail := image[lastSlash+1:]
	if strings.Contains(tail, "@") {
		return false
	}
	colon := strings.LastIndex(tail, ":")
	if colon < 0 {
		return true
	}
	return tail[colon+1:] == "latest"
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func deleteIfEqual(m map[string]any, key, want string) {
	if s, ok := m[key].(string); ok && s == want {
		delete(m, key)
	}
}

func deleteIfInt(m map[string]any, key string, want int64) {
	switch v := m[key].(type) {
	case int64:
		if v == want {
			delete(m, key)
		}
	case int:
		if int64(v) == want {
			delete(m, key)
		}
	case float64:
		if v == float64(want) {
			delete(m, key)
		}
	}
}

func pruneEmptyMaps(m map[string]any) {
	for k, v := range m {
		if sub := asMap(v); sub != nil {
			pruneEmptyMaps(sub)
			if len(sub) == 0 {
				delete(m, k)
			}
		}
	}
}
