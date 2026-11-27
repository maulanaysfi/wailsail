package kube

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"text/template"

	sprig "github.com/Masterminds/sprig/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// coalesceValues deeply merges base chart defaults with user override values
// without mutating either input map.
func coalesceValues(base, override map[string]any) map[string]any {
	out := deepCopyMap(base)
	for k, v := range override {
		if v == nil {
			delete(out, k)
			continue
		}
		if vm, ok := toStringAnyMap(v); ok {
			if bm, ok := toStringAnyMap(out[k]); ok {
				out[k] = coalesceValues(bm, vm)
				continue
			}
		}
		out[k] = deepCopyValue(v)
	}
	return out
}

func toStringAnyMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	return nil, false
}

func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		cp := make([]any, len(val))
		for i := range val {
			cp[i] = deepCopyValue(val[i])
		}
		return cp
	default:
		return val
	}
}

// helmFiles implements Helm's .Files template object.
type helmFiles struct {
	byName map[string][]byte
}

func newHelmFiles(files []*helmFile) helmFiles {
	m := make(map[string][]byte, len(files))
	for _, f := range files {
		if f != nil {
			m[f.Name] = f.Data
		}
	}
	return helmFiles{byName: m}
}

func (f helmFiles) Get(name string) string {
	return string(f.byName[name])
}

func (f helmFiles) GetBytes(name string) []byte {
	return f.byName[name]
}

func (f helmFiles) Glob(pattern string) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range f.byName {
		if matched, _ := path.Match(pattern, k); matched {
			out[k] = v
		}
	}
	return out
}

func (f helmFiles) Lines(name string) []string {
	s := string(f.byName[name])
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (f helmFiles) AsConfig() string {
	m := map[string]string{}
	for k, v := range f.byName {
		m[path.Base(k)] = string(v)
	}
	b, _ := yaml.Marshal(m)
	return strings.TrimSpace(string(b))
}

func (f helmFiles) AsSecrets() string {
	m := map[string]string{}
	for k, v := range f.byName {
		m[path.Base(k)] = base64.StdEncoding.EncodeToString(v)
	}
	b, _ := yaml.Marshal(m)
	return strings.TrimSpace(string(b))
}

// helmAPIVersions backs Helm's .Capabilities.APIVersions.Has helper.
type helmAPIVersions map[string]struct{}

func (a helmAPIVersions) Has(apiVersion string) bool {
	_, ok := a[apiVersion]
	return ok
}

func discoverHelmAPIVersions(c *Clients) helmAPIVersions {
	out := helmAPIVersions{"v1": {}}
	if c == nil || c.Discovery == nil {
		return out
	}
	groups, lists, _ := c.Discovery.ServerGroupsAndResources()
	for _, g := range groups {
		for _, v := range g.Versions {
			if v.GroupVersion != "" {
				out[v.GroupVersion] = struct{}{}
			}
		}
	}
	for _, l := range lists {
		if l.GroupVersion == "" {
			continue
		}
		out[l.GroupVersion] = struct{}{}
		for _, r := range l.APIResources {
			if r.Kind != "" {
				out[l.GroupVersion+"/"+r.Kind] = struct{}{}
			}
		}
	}
	return out
}

// renderHelmChart executes the chart's embedded Go templates against merged values.
func renderHelmChart(ch *helmChart, releaseName, namespace string, revision int, userConfig map[string]any, kubeVer string, c *Clients) (manifest string, notes string, err error) {
	if ch == nil {
		return "", "", errors.New("nil chart")
	}

	t := template.New("gotpl")
	includedNames := map[string]int{}

	var funcMap template.FuncMap
	funcMap = helmTemplateFuncs(t, includedNames, &funcMap, c)
	t.Funcs(funcMap)

	apiVersions := discoverHelmAPIVersions(c)

	type chartRenderJob struct {
		chartPath string
		chart     *helmChart
		values    map[string]any
		file      *helmFile
	}

	mergedRoot := coalesceValues(ch.Values, userConfig)
	var jobs []chartRenderJob

	var collect func(prefix string, cur *helmChart, vals map[string]any) error
	collect = func(prefix string, cur *helmChart, vals map[string]any) error {
		if cur == nil {
			return nil
		}
		cName := prefix
		if cName == "" && cur.Metadata != nil {
			cName = cur.Metadata.Name
		}
		if cName == "" {
			cName = "chart"
		}
		for _, f := range cur.Templates {
			if f == nil {
				continue
			}
			fullName := path.Join(cName, f.Name)
			if _, perr := t.New(fullName).Parse(string(f.Data)); perr != nil {
				return fmt.Errorf("parse %s: %w", fullName, perr)
			}
			base := path.Base(f.Name)
			if strings.HasPrefix(base, "_") {
				continue
			}
			jobs = append(jobs, chartRenderJob{
				chartPath: cName,
				chart:     cur,
				values:    vals,
				file:      f,
			})
		}
		globalVals, _ := toStringAnyMap(vals["global"])
		for _, dep := range cur.Dependencies {
			if dep == nil || dep.Metadata == nil {
				continue
			}
			depName := dep.Metadata.Name
			subOverride, _ := toStringAnyMap(vals[depName])
			depBase := coalesceValues(dep.Values, subOverride)
			if len(globalVals) > 0 {
				existingGlobal, _ := toStringAnyMap(depBase["global"])
				depBase["global"] = coalesceValues(existingGlobal, globalVals)
			}
			if err := collect(path.Join(cName, "charts", depName), dep, depBase); err != nil {
				return err
			}
		}
		return nil
	}

	if err := collect("", ch, mergedRoot); err != nil {
		return "", "", err
	}

	sort.SliceStable(jobs, func(i, j int) bool {
		return path.Join(jobs[i].chartPath, jobs[i].file.Name) < path.Join(jobs[j].chartPath, jobs[j].file.Name)
	})

	major, minor := "1", "30"
	cleanVer := strings.TrimPrefix(kubeVer, "v")
	if parts := strings.Split(cleanVer, "."); len(parts) >= 2 {
		major = parts[0]
		minor = parts[1]
	}

	var manifestBuf strings.Builder
	var notesBuf strings.Builder

	for _, job := range jobs {
		fullName := path.Join(job.chartPath, job.file.Name)
		meta := job.chart.Metadata
		if meta == nil {
			meta = &helmChartMeta{Name: job.chartPath}
		}
		data := map[string]any{
			"Values": job.values,
			"Release": map[string]any{
				"Name":      releaseName,
				"Namespace": namespace,
				"Revision":  revision,
				"IsUpgrade": true,
				"IsInstall": false,
				"Service":   "Helm",
			},
			"Chart": struct {
				*helmChartMeta
				IsRoot bool
			}{
				helmChartMeta: meta,
				IsRoot:        job.chart == ch,
			},
			"Files": newHelmFiles(job.chart.Files),
			"Capabilities": map[string]any{
				"KubeVersion": map[string]any{
					"Version":    kubeVer,
					"GitVersion": kubeVer,
					"Major":      major,
					"Minor":      minor,
				},
				"HelmVersion": map[string]any{
					"Version": "v3.17.0",
				},
				"APIVersions": apiVersions,
			},
			"Template": map[string]any{
				"Name":     fullName,
				"BasePath": path.Join(job.chartPath, "templates"),
			},
		}

		var out bytes.Buffer
		if err := t.ExecuteTemplate(&out, fullName, data); err != nil {
			return "", "", fmt.Errorf("render %s: %w", fullName, err)
		}
		rendered := strings.TrimSpace(out.String())
		if rendered == "" {
			continue
		}
		base := path.Base(job.file.Name)
		if strings.EqualFold(base, "NOTES.txt") {
			if job.chart == ch {
				notesBuf.WriteString(rendered)
			}
			continue
		}
		if !strings.HasSuffix(base, ".yaml") && !strings.HasSuffix(base, ".yml") {
			continue
		}
		fmt.Fprintf(&manifestBuf, "---\n# Source: %s\n%s\n", fullName, rendered)
	}

	return strings.TrimSpace(manifestBuf.String()), strings.TrimSpace(notesBuf.String()), nil
}

func helmTemplateFuncs(t *template.Template, includedNames map[string]int, funcMapPtr *template.FuncMap, c *Clients) template.FuncMap {
	f := sprig.TxtFuncMap()
	// Match upstream Helm sandbox restrictions.
	delete(f, "env")
	delete(f, "expandenv")

	toYAML := func(v any) (string, error) {
		b, err := yaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(b), "\n"), nil
	}
	toJSON := func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}

	helmExtras := template.FuncMap{
		"include": func(name string, data any) (string, error) {
			if includedNames[name] > 1000 {
				return "", fmt.Errorf("rendering template has a recursive reference name: %s", name)
			}
			includedNames[name]++
			defer func() { includedNames[name]-- }()
			var buf bytes.Buffer
			err := t.ExecuteTemplate(&buf, name, data)
			return buf.String(), err
		},
		"tpl": func(tplStr string, data any) (string, error) {
			if strings.TrimSpace(tplStr) == "" {
				return "", nil
			}
			sub, err := t.Clone()
			if err != nil {
				sub = template.New("gotpl").Funcs(*funcMapPtr)
			}
			sub, err = sub.New("inline-tpl").Parse(tplStr)
			if err != nil {
				return "", err
			}
			var buf bytes.Buffer
			if err := sub.ExecuteTemplate(&buf, "inline-tpl", data); err != nil {
				return "", err
			}
			return buf.String(), nil
		},
		"required": func(warn string, val any) (any, error) {
			if val == nil {
				return nil, errors.New(warn)
			}
			if s, ok := val.(string); ok && s == "" {
				return nil, errors.New(warn)
			}
			return val, nil
		},
		"toYaml": func(v any) string {
			s, _ := toYAML(v)
			return s
		},
		"mustToYaml": toYAML,
		"fromYaml": func(str string) map[string]any {
			m := map[string]any{}
			if err := yaml.Unmarshal([]byte(str), &m); err != nil {
				m["Error"] = err.Error()
			}
			return m
		},
		"fromYamlArray": func(str string) []any {
			var a []any
			if err := yaml.Unmarshal([]byte(str), &a); err != nil {
				return []any{err.Error()}
			}
			return a
		},
		"toJson": func(v any) string {
			s, _ := toJSON(v)
			return s
		},
		"mustToJson": toJSON,
		"fromJson": func(str string) map[string]any {
			m := map[string]any{}
			if err := json.Unmarshal([]byte(str), &m); err != nil {
				m["Error"] = err.Error()
			}
			return m
		},
		"fromJsonArray": func(str string) []any {
			var a []any
			if err := json.Unmarshal([]byte(str), &a); err != nil {
				return []any{err.Error()}
			}
			return a
		},
		"lookup": func(apiVersion, kind, namespace, name string) (map[string]any, error) {
			if c == nil || c.Mapper == nil || c.Dynamic == nil || kind == "" {
				return map[string]any{}, nil
			}
			gv, err := schema.ParseGroupVersion(apiVersion)
			if err != nil {
				return map[string]any{}, nil
			}
			mapping, err := c.Mapper.RESTMapping(schema.GroupKind{Group: gv.Group, Kind: kind}, gv.Version)
			if err != nil {
				return map[string]any{}, nil
			}
			var ri dynamic.ResourceInterface = c.Dynamic.Resource(mapping.Resource)
			if mapping.Scope.Name() == "namespace" && namespace != "" {
				ri = c.Dynamic.Resource(mapping.Resource).Namespace(namespace)
			}
			if name == "" {
				list, err := ri.List(context.Background(), metav1.ListOptions{})
				if err != nil {
					return map[string]any{}, nil
				}
				return list.UnstructuredContent(), nil
			}
			obj, err := ri.Get(context.Background(), name, metav1.GetOptions{})
			if err != nil {
				return map[string]any{}, nil
			}
			return obj.UnstructuredContent(), nil
		},
	}
	for k, v := range helmExtras {
		f[k] = v
	}
	return f
}
