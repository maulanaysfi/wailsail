package kube

import (
	"context"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// helmSummary holds only the lightweight table row metadata for one release
// revision so full charts, templates, and manifests are never retained in RAM.
type helmSummary struct {
	version int
	row     Row
}

// helmTracker maintains lightweight revision summaries per secret and projects
// the latest revision per (namespace, releaseName) into table rows.
type helmTracker struct {
	byRelease       map[string]map[string]helmSummary // releaseKey -> secretKey -> summary
	secretToRelease map[string]string                 // secretKey -> releaseKey
}

func newHelmTracker() *helmTracker {
	return &helmTracker{
		byRelease:       map[string]map[string]helmSummary{},
		secretToRelease: map[string]string{},
	}
}

func (h *helmTracker) putSecret(sec *corev1.Secret) (releaseKey string, ok bool) {
	raw := sec.Data["release"]
	if len(raw) == 0 {
		return "", false
	}
	rel, err := decodeHelmReleaseData(raw)
	if err != nil || rel.Name == "" {
		return "", false
	}
	row := helmReleaseRow(rel, sec.Namespace)
	rKey := row.Key
	sKey := sec.Namespace + "/" + sec.Name
	if prevKey, existed := h.secretToRelease[sKey]; existed && prevKey != rKey {
		if revs := h.byRelease[prevKey]; revs != nil {
			delete(revs, sKey)
			if len(revs) == 0 {
				delete(h.byRelease, prevKey)
			}
		}
	}
	if h.byRelease[rKey] == nil {
		h.byRelease[rKey] = map[string]helmSummary{}
	}
	h.byRelease[rKey][sKey] = helmSummary{version: rel.Version, row: row}
	h.secretToRelease[sKey] = rKey
	return rKey, true
}

func (h *helmTracker) deleteSecret(sec *corev1.Secret) (releaseKey string, ok bool) {
	sKey := sec.Namespace + "/" + sec.Name
	rKey, exists := h.secretToRelease[sKey]
	if !exists {
		if name := sec.Labels["name"]; name != "" {
			return sec.Namespace + "/" + name, true
		}
		return "", false
	}
	delete(h.secretToRelease, sKey)
	if revs := h.byRelease[rKey]; revs != nil {
		delete(revs, sKey)
		if len(revs) == 0 {
			delete(h.byRelease, rKey)
		}
	}
	return rKey, true
}

func (h *helmTracker) latestRow(releaseKey string) (Row, bool) {
	revs := h.byRelease[releaseKey]
	if len(revs) == 0 {
		return Row{}, false
	}
	var best helmSummary
	first := true
	for _, s := range revs {
		if first || s.version > best.version {
			best = s
			first = false
		}
	}
	return best.row, true
}

func (h *helmTracker) allLatestRows() []Row {
	rows := make([]Row, 0, len(h.byRelease))
	for rKey := range h.byRelease {
		if row, ok := h.latestRow(rKey); ok {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

// runHelmWatch lists and watches Helm release Secrets, projecting the latest
// revision of each release into WatchEvents.
func (c *Clients) runHelmWatch(ctx context.Context, namespace string, opts watchOptions, emit func(WatchEvent)) {
	b := newBatcher()
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(flushInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				close(done)
				return
			case <-t.C:
				if ev, ok := b.take(); ok {
					emit(ev)
				}
			}
		}
	}()
	defer func() { <-done }()

	tracker := newHelmTracker()
	rv := ""
	backoff := time.Second
	sleep := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
		return true
	}

	labelSel := "owner=helm"
	if opts.labelSelector != "" {
		labelSel = labelSel + "," + opts.labelSelector
	}

	for ctx.Err() == nil {
		if rv == "" {
			list, err := c.Typed.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSel,
				FieldSelector: opts.fieldSelector,
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				b.fail(err.Error())
				if !sleep() {
					return
				}
				continue
			}
			tracker = newHelmTracker()
			for i := range list.Items {
				tracker.putSecret(&list.Items[i])
			}
			rv = list.ResourceVersion
			b.doReset(helmColumns, tracker.allLatestRows())
			backoff = time.Second
		}

		timeout := int64(300)
		w, err := c.Typed.CoreV1().Secrets(namespace).Watch(ctx, metav1.ListOptions{
			LabelSelector:       labelSel,
			FieldSelector:       opts.fieldSelector,
			ResourceVersion:     rv,
			AllowWatchBookmarks: true,
			TimeoutSeconds:      &timeout,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if apierrors.IsResourceExpired(err) || IsGone(err) {
				rv = ""
				continue
			}
			b.fail(err.Error())
			rv = ""
			if !sleep() {
				return
			}
			continue
		}

		expired := false
		for ev := range w.ResultChan() {
			if ctx.Err() != nil {
				w.Stop()
				return
			}
			if ev.Type == watch.Error {
				if st, ok := ev.Object.(*metav1.Status); ok {
					if st.Code == 410 || st.Reason == metav1.StatusReasonExpired {
						expired = true
					} else if st.Message != "" {
						b.fail(st.Message)
					}
				}
				break
			}
			sec, ok := ev.Object.(*corev1.Secret)
			if !ok {
				continue
			}
			if sec.ResourceVersion != "" {
				rv = sec.ResourceVersion
			}
			if ev.Type == watch.Bookmark {
				continue
			}
			switch ev.Type {
			case watch.Added, watch.Modified:
				if relKey, ok := tracker.putSecret(sec); ok {
					if row, exists := tracker.latestRow(relKey); exists {
						b.upsert(row)
					}
				}
			case watch.Deleted:
				if relKey, ok := tracker.deleteSecret(sec); ok {
					if row, exists := tracker.latestRow(relKey); exists {
						b.upsert(row)
					} else {
						b.remove(relKey)
					}
				}
			}
		}
		w.Stop()
		if expired {
			rv = ""
		}
	}
}
