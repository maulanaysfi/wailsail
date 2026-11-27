package logs

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// MaxAggregatedPods caps concurrent Pod streams per aggregated workload log tab.
const MaxAggregatedPods = 15

func splitTailLines(totalTail int64, podCount int) int64 {
	if totalTail <= 0 || podCount <= 0 {
		return 0
	}
	n := (totalTail + int64(podCount) - 1) / int64(podCount)
	if n < 1 {
		return 1
	}
	return n
}

func hasAnyOwnerUID(refs []metav1.OwnerReference, uids map[types.UID]struct{}) bool {
	for _, r := range refs {
		if _, ok := uids[r.UID]; ok {
			return true
		}
	}
	return false
}

func selectAggregatedPods(pods []*corev1.Pod, ownerUIDs map[types.UID]struct{}, capPods int) (selected []*corev1.Pod, total int) {
	var owned []*corev1.Pod
	for _, p := range pods {
		if p != nil && hasAnyOwnerUID(p.OwnerReferences, ownerUIDs) {
			owned = append(owned, p)
		}
	}
	sort.Slice(owned, func(i, j int) bool {
		ti := owned[i].Status.Phase == corev1.PodSucceeded || owned[i].Status.Phase == corev1.PodFailed
		tj := owned[j].Status.Phase == corev1.PodSucceeded || owned[j].Status.Phase == corev1.PodFailed
		if ti != tj {
			return !ti
		}
		return owned[i].Name < owned[j].Name
	})
	total = len(owned)
	if capPods > 0 && len(owned) > capPods {
		owned = owned[:capPods]
	}
	return owned, total
}

func containerReadyForLogs(p *corev1.Pod, container string) bool {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Name == container {
			return cs.State.Running != nil || cs.State.Terminated != nil
		}
	}
	return p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed
}

func containerRunning(p *corev1.Pod, container string) bool {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Name == container {
			return cs.State.Running != nil
		}
	}
	return false
}

// StartWorkload aggregates logs across all main containers of Pods owned by a
// workload (Deployment, ReplicaSet, StatefulSet, DaemonSet, Job, CronJob),
// capping active Pod streams at MaxAggregatedPods and following new rollout Pods.
func (s *Service) StartWorkload(contextName, namespace, kind, uid, name string, opts Options, streamID string) (string, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return "", err
	}
	return s.startWorkloadWithClient(c.Typed, namespace, kind, uid, opts, streamID)
}

func (s *Service) startWorkloadWithClient(typed kubernetes.Interface, namespace, kind, uid string, opts Options, streamID string) (string, error) {
	switch kind {
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job", "CronJob":
	default:
		return "", fmt.Errorf("aggregated logs are not supported for kind %q", kind)
	}

	ctx, id, done := s.streams.Start(s.baseCtx, streamID)
	topic := Topic(id)

	go func() {
		defer done()

		var (
			bMu     sync.Mutex
			pending []LineEntry
		)
		flush := func() {
			bMu.Lock()
			batch := pending
			pending = nil
			bMu.Unlock()
			if len(batch) > 0 {
				s.emit(topic, Event{Entries: batch})
			}
		}
		addEntry := func(podName, containerName string, line []byte) {
			bMu.Lock()
			pending = append(pending, LineEntry{Pod: podName, Container: containerName, Text: string(line)})
			bMu.Unlock()
		}

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		stopBatcher := make(chan struct{})
		batchDone := make(chan struct{})
		go func() {
			defer close(batchDone)
			for {
				select {
				case <-stopBatcher:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					flush()
				}
			}
		}()

		type streamKey struct {
			pod       string
			container string
		}
		type activeStream struct {
			cancel context.CancelFunc
			ended  bool
		}

		var (
			mu        sync.Mutex
			childUIDs = map[types.UID]struct{}{}
			podsByUID = map[types.UID]*corev1.Pod{}
			streams   = map[streamKey]*activeStream{}
			podsReady bool
			childDone = kind != "Deployment" && kind != "CronJob"
			initial   = true
			lastPods  = -1
			lastTotal = -1
			wg        sync.WaitGroup
		)

		ownerSetLocked := func() map[types.UID]struct{} {
			if kind == "Deployment" || kind == "CronJob" {
				return childUIDs
			}
			return map[types.UID]struct{}{types.UID(uid): {}}
		}

		var reconcileLocked func()
		reconcileLocked = func() {
			if !podsReady || !childDone || ctx.Err() != nil {
				return
			}
			allPods := make([]*corev1.Pod, 0, len(podsByUID))
			for _, p := range podsByUID {
				allPods = append(allPods, p)
			}
			selected, total := selectAggregatedPods(allPods, ownerSetLocked(), MaxAggregatedPods)
			if initial || len(selected) != lastPods || total != lastTotal {
				initial = false
				lastPods = len(selected)
				lastTotal = total
				s.emit(topic, Event{Connected: true, PodCount: len(selected), TotalPods: total})
			}

			desired := map[streamKey]corev1.Pod{}
			for _, p := range selected {
				for _, ctr := range p.Spec.Containers {
					desired[streamKey{pod: p.Name, container: ctr.Name}] = *p
				}
			}

			for k, st := range streams {
				if _, keep := desired[k]; !keep {
					st.cancel()
					delete(streams, k)
				}
			}

			tailPerPod := int64(0)
			// Apply user's TailLines across the current batch when streams are not yet open.
			if opts.TailLines > 0 && len(selected) > 0 {
				tailPerPod = splitTailLines(opts.TailLines, len(selected))
			}

			for k, p := range desired {
				if cur, exists := streams[k]; exists {
					// Reconnect an ended stream if the container is running again while following.
					if !(opts.Follow && cur.ended && containerRunning(&p, k.container)) {
						continue
					}
					cur.cancel()
					delete(streams, k)
				}
				if !containerReadyForLogs(&p, k.container) {
					continue
				}
				sCtx, sCancel := context.WithCancel(ctx)
				st := &activeStream{cancel: sCancel}
				streams[k] = st
				tail := tailPerPod
				// Brand-new pods starting after initial connect stream from line 0.
				if p.CreationTimestamp.Time.After(time.Now().Add(-5 * time.Second)) {
					tail = 0
				}

				wg.Add(1)
				go func(key streamKey, podUID types.UID, tailLines int64, stRef *activeStream, subCtx context.Context) {
					defer wg.Done()
					plo := &corev1.PodLogOptions{
						Container:  key.container,
						Follow:     opts.Follow,
						Timestamps: opts.Timestamps,
					}
					if tailLines > 0 {
						t := tailLines
						plo.TailLines = &t
					}
					rc, err := typed.CoreV1().Pods(namespace).GetLogs(key.pod, plo).Stream(subCtx)
					if err != nil {
						mu.Lock()
						if cur, ok := streams[key]; ok && cur == stRef && subCtx.Err() == nil {
							delete(streams, key)
						}
						mu.Unlock()
						return
					}
					defer rc.Close()
					_ = scanLines(rc, func(line []byte) {
						addEntry(key.pod, key.container, line)
					})
					mu.Lock()
					if cur, ok := streams[key]; ok && cur == stRef {
						cur.ended = true
					}
					curPod := podsByUID[podUID]
					shouldRetry := opts.Follow && subCtx.Err() == nil && curPod != nil && containerRunning(curPod, key.container)
					mu.Unlock()
					if shouldRetry {
						select {
						case <-subCtx.Done():
							return
						case <-time.After(2 * time.Second):
						}
						mu.Lock()
						if cur, ok := streams[key]; ok && cur == stRef {
							delete(streams, key)
							reconcileLocked()
						}
						mu.Unlock()
					}
				}(k, p.UID, tail, st, sCtx)
			}
		}

		watchLoop := func(name string, listFn func(context.Context) (string, error), watchFn func(context.Context, string) (watch.Interface, error), onItem func(watch.EventType, any), onListDone func()) {
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
			for ctx.Err() == nil {
				if rv == "" {
					newRV, err := listFn(ctx)
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						s.emit(topic, Event{Error: fmt.Sprintf("watch %s: %v", name, err)})
						if !opts.Follow {
							return
						}
						if !sleep() {
							return
						}
						continue
					}
					rv = newRV
					backoff = time.Second
					onListDone()
					if !opts.Follow {
						return
					}
				}
				w, err := watchFn(ctx, rv)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					if apierrors.IsResourceExpired(err) {
						rv = ""
						continue
					}
					s.emit(topic, Event{Error: fmt.Sprintf("watch %s: %v", name, err)})
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
						if st, ok := ev.Object.(*metav1.Status); ok && (st.Code == 410 || st.Reason == metav1.StatusReasonExpired) {
							expired = true
						}
						break
					}
					if ev.Type == watch.Bookmark {
						if metaObj, ok := ev.Object.(metav1.Object); ok && metaObj.GetResourceVersion() != "" {
							rv = metaObj.GetResourceVersion()
						}
						continue
					}
					if metaObj, ok := ev.Object.(metav1.Object); ok && metaObj.GetResourceVersion() != "" {
						rv = metaObj.GetResourceVersion()
					}
					onItem(ev.Type, ev.Object)
				}
				w.Stop()
				if expired {
					rv = ""
				}
			}
		}

		parentSet := map[types.UID]struct{}{types.UID(uid): {}}
		var watchWG sync.WaitGroup

		if kind == "Deployment" {
			watchWG.Add(1)
			go func() {
				defer watchWG.Done()
				watchLoop(
					"replicasets",
					func(wctx context.Context) (string, error) {
						list, err := typed.AppsV1().ReplicaSets(namespace).List(wctx, metav1.ListOptions{})
						if err != nil {
							return "", err
						}
						mu.Lock()
						childUIDs = map[types.UID]struct{}{}
						for i := range list.Items {
							rs := &list.Items[i]
							if hasAnyOwnerUID(rs.OwnerReferences, parentSet) {
								childUIDs[rs.UID] = struct{}{}
							}
						}
						mu.Unlock()
						return list.ResourceVersion, nil
					},
					func(wctx context.Context, rv string) (watch.Interface, error) {
						t := int64(300)
						return typed.AppsV1().ReplicaSets(namespace).Watch(wctx, metav1.ListOptions{
							ResourceVersion:     rv,
							AllowWatchBookmarks: true,
							TimeoutSeconds:      &t,
						})
					},
					func(tp watch.EventType, obj any) {
						rs, ok := obj.(*appsv1.ReplicaSet)
						if !ok {
							return
						}
						mu.Lock()
						if tp == watch.Deleted {
							delete(childUIDs, rs.UID)
						} else if hasAnyOwnerUID(rs.OwnerReferences, parentSet) {
							childUIDs[rs.UID] = struct{}{}
						}
						reconcileLocked()
						mu.Unlock()
					},
					func() {
						mu.Lock()
						childDone = true
						reconcileLocked()
						mu.Unlock()
					},
				)
			}()
		} else if kind == "CronJob" {
			watchWG.Add(1)
			go func() {
				defer watchWG.Done()
				watchLoop(
					"jobs",
					func(wctx context.Context) (string, error) {
						list, err := typed.BatchV1().Jobs(namespace).List(wctx, metav1.ListOptions{})
						if err != nil {
							return "", err
						}
						mu.Lock()
						childUIDs = map[types.UID]struct{}{}
						for i := range list.Items {
							jb := &list.Items[i]
							if hasAnyOwnerUID(jb.OwnerReferences, parentSet) {
								childUIDs[jb.UID] = struct{}{}
							}
						}
						mu.Unlock()
						return list.ResourceVersion, nil
					},
					func(wctx context.Context, rv string) (watch.Interface, error) {
						t := int64(300)
						return typed.BatchV1().Jobs(namespace).Watch(wctx, metav1.ListOptions{
							ResourceVersion:     rv,
							AllowWatchBookmarks: true,
							TimeoutSeconds:      &t,
						})
					},
					func(tp watch.EventType, obj any) {
						jb, ok := obj.(*batchv1.Job)
						if !ok {
							return
						}
						mu.Lock()
						if tp == watch.Deleted {
							delete(childUIDs, jb.UID)
						} else if hasAnyOwnerUID(jb.OwnerReferences, parentSet) {
							childUIDs[jb.UID] = struct{}{}
						}
						reconcileLocked()
						mu.Unlock()
					},
					func() {
						mu.Lock()
						childDone = true
						reconcileLocked()
						mu.Unlock()
					},
				)
			}()
		}

		watchWG.Add(1)
		go func() {
			defer watchWG.Done()
			watchLoop(
				"pods",
				func(wctx context.Context) (string, error) {
					list, err := typed.CoreV1().Pods(namespace).List(wctx, metav1.ListOptions{})
					if err != nil {
						return "", err
					}
					mu.Lock()
					podsByUID = map[types.UID]*corev1.Pod{}
					for i := range list.Items {
						p := list.Items[i].DeepCopy()
						podsByUID[p.UID] = p
					}
					mu.Unlock()
					return list.ResourceVersion, nil
				},
				func(wctx context.Context, rv string) (watch.Interface, error) {
					t := int64(300)
					return typed.CoreV1().Pods(namespace).Watch(wctx, metav1.ListOptions{
						ResourceVersion:     rv,
						AllowWatchBookmarks: true,
						TimeoutSeconds:      &t,
					})
				},
				func(tp watch.EventType, obj any) {
					p, ok := obj.(*corev1.Pod)
					if !ok {
						return
					}
					mu.Lock()
					if tp == watch.Deleted {
						delete(podsByUID, p.UID)
					} else {
						podsByUID[p.UID] = p.DeepCopy()
					}
					reconcileLocked()
					mu.Unlock()
				},
				func() {
					mu.Lock()
					podsReady = true
					reconcileLocked()
					mu.Unlock()
				},
			)
		}()

		watchWG.Wait()
		wg.Wait()
		close(stopBatcher)
		<-batchDone
		flush()
		s.emit(topic, Event{Ended: true})
	}()
	return id, nil
}
