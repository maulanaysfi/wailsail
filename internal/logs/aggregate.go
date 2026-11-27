package logs

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

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
	if uid == "" {
		return "", fmt.Errorf("workload UID is required")
	}
	ctx, id, _ := s.streams.Start(s.baseCtx, streamID)
	agg := &workloadAggregator{
		svc:       s,
		typed:     typed,
		ctx:       ctx,
		topic:     Topic(id),
		namespace: namespace,
		kind:      kind,
		uid:       types.UID(uid),
		parentSet: map[types.UID]struct{}{types.UID(uid): {}},
		opts:      opts,
		childUIDs: map[types.UID]struct{}{},
		podsByUID: map[types.UID]*corev1.Pod{},
		streams:   map[workloadStreamKey]*activeWorkloadStream{},
		childDone: kind != "Deployment" && kind != "CronJob",
		initial:   true,
		lastPods:  -1,
		lastTotal: -1,
	}
	go func() {
		defer s.streams.Stop(id)
		agg.run()
	}()
	return id, nil
}

type workloadStreamKey struct {
	pod       string
	container string
}

type activeWorkloadStream struct {
	cancel context.CancelFunc
	ended  bool
}

type workloadAggregator struct {
	svc       *Service
	typed     kubernetes.Interface
	ctx       context.Context
	topic     string
	namespace string
	kind      string
	uid       types.UID
	parentSet map[types.UID]struct{}
	opts      Options

	batchMu sync.Mutex
	batch   []LineEntry

	mu        sync.Mutex
	childUIDs map[types.UID]struct{}
	podsByUID map[types.UID]*corev1.Pod
	streams   map[workloadStreamKey]*activeWorkloadStream
	podsReady bool
	childDone bool
	initial   bool
	lastPods  int
	lastTotal int
	wg        sync.WaitGroup
}

func (a *workloadAggregator) flush() {
	a.batchMu.Lock()
	if len(a.batch) == 0 {
		a.batchMu.Unlock()
		return
	}
	out := a.batch
	a.batch = nil
	a.batchMu.Unlock()
	a.svc.emit(a.topic, Event{Entries: out})
}

func (a *workloadAggregator) addEntry(pod, container string, line []byte) {
	a.batchMu.Lock()
	a.batch = append(a.batch, LineEntry{Pod: pod, Container: container, Text: string(line)})
	full := len(a.batch) >= 256
	a.batchMu.Unlock()
	if full {
		a.flush()
	}
}

func (a *workloadAggregator) ownerSetLocked() map[types.UID]struct{} {
	if a.kind == "Deployment" || a.kind == "CronJob" {
		return a.childUIDs
	}
	return a.parentSet
}

func (a *workloadAggregator) reconcileLocked() {
	if !a.podsReady || !a.childDone || a.ctx.Err() != nil {
		return
	}
	allPods := make([]*corev1.Pod, 0, len(a.podsByUID))
	for _, p := range a.podsByUID {
		allPods = append(allPods, p)
	}
	selected, total := selectAggregatedPods(allPods, a.ownerSetLocked(), MaxAggregatedPods)
	if a.initial || len(selected) != a.lastPods || total != a.lastTotal {
		a.initial = false
		a.lastPods = len(selected)
		a.lastTotal = total
		a.svc.emit(a.topic, Event{Connected: true, PodCount: len(selected), TotalPods: total})
	}

	desired := map[workloadStreamKey]corev1.Pod{}
	for _, p := range selected {
		for _, ctr := range p.Spec.Containers {
			desired[workloadStreamKey{pod: p.Name, container: ctr.Name}] = *p
		}
	}

	for k, st := range a.streams {
		if _, keep := desired[k]; !keep {
			st.cancel()
			delete(a.streams, k)
		}
	}

	tailPerPod := int64(0)
	if a.opts.TailLines > 0 && len(selected) > 0 {
		tailPerPod = splitTailLines(a.opts.TailLines, len(selected))
	}

	for k, p := range desired {
		if cur, exists := a.streams[k]; exists {
			if !(a.opts.Follow && cur.ended && containerRunning(&p, k.container)) {
				continue
			}
			cur.cancel()
			delete(a.streams, k)
		}
		if !containerReadyForLogs(&p, k.container) {
			continue
		}
		sCtx, sCancel := context.WithCancel(a.ctx)
		st := &activeWorkloadStream{cancel: sCancel}
		a.streams[k] = st
		tail := tailPerPod
		if p.CreationTimestamp.Time.After(time.Now().Add(-5 * time.Second)) {
			tail = 0
		}
		a.wg.Add(1)
		go a.streamPod(k, p.UID, tail, st, sCtx)
	}
}

func (a *workloadAggregator) streamPod(key workloadStreamKey, podUID types.UID, tailLines int64, stRef *activeWorkloadStream, subCtx context.Context) {
	defer a.wg.Done()
	plo := &corev1.PodLogOptions{
		Container:  key.container,
		Follow:     a.opts.Follow,
		Timestamps: a.opts.Timestamps,
	}
	if tailLines > 0 {
		t := tailLines
		plo.TailLines = &t
	}
	rc, err := a.typed.CoreV1().Pods(a.namespace).GetLogs(key.pod, plo).Stream(subCtx)
	if err != nil {
		a.mu.Lock()
		if cur, ok := a.streams[key]; ok && cur == stRef && subCtx.Err() == nil {
			delete(a.streams, key)
		}
		a.mu.Unlock()
		return
	}
	defer rc.Close()
	_ = scanLines(rc, func(line []byte) {
		a.addEntry(key.pod, key.container, line)
	})
	a.mu.Lock()
	if cur, ok := a.streams[key]; ok && cur == stRef {
		cur.ended = true
	}
	curPod := a.podsByUID[podUID]
	shouldRetry := a.opts.Follow && subCtx.Err() == nil && curPod != nil && containerRunning(curPod, key.container)
	a.mu.Unlock()
	if shouldRetry {
		select {
		case <-subCtx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		a.mu.Lock()
		if cur, ok := a.streams[key]; ok && cur == stRef {
			delete(a.streams, key)
			a.reconcileLocked()
		}
		a.mu.Unlock()
	}
}

func (a *workloadAggregator) watchLoop(
	name string,
	listFn func(context.Context) (string, error),
	watchFn func(context.Context, string) (watch.Interface, error),
	onItem func(watch.EventType, any),
	onListDone func(),
) {
	rv := ""
	backoff := time.Second
	sleep := func() bool {
		select {
		case <-a.ctx.Done():
			return false
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
		return true
	}
	for a.ctx.Err() == nil {
		if rv == "" {
			newRV, err := listFn(a.ctx)
			if err != nil {
				if a.ctx.Err() != nil {
					return
				}
				a.svc.emit(a.topic, Event{Error: fmt.Sprintf("watch %s: %v", name, err)})
				if !a.opts.Follow || !sleep() {
					return
				}
				continue
			}
			rv = newRV
			backoff = time.Second
			onListDone()
			if !a.opts.Follow {
				return
			}
		}
		w, err := watchFn(a.ctx, rv)
		if err != nil {
			if a.ctx.Err() != nil {
				return
			}
			if apierrors.IsResourceExpired(err) {
				rv = ""
				continue
			}
			a.svc.emit(a.topic, Event{Error: fmt.Sprintf("watch %s: %v", name, err)})
			rv = ""
			if !sleep() {
				return
			}
			continue
		}
		expired := false
		for ev := range w.ResultChan() {
			if a.ctx.Err() != nil {
				w.Stop()
				return
			}
			if ev.Type == watch.Error {
				if st, ok := ev.Object.(*metav1.Status); ok && (st.Code == 410 || st.Reason == metav1.StatusReasonExpired) {
					expired = true
				}
				break
			}
			if metaObj, ok := ev.Object.(metav1.Object); ok && metaObj.GetResourceVersion() != "" {
				rv = metaObj.GetResourceVersion()
			}
			if ev.Type == watch.Bookmark {
				continue
			}
			onItem(ev.Type, ev.Object)
		}
		w.Stop()
		if expired {
			rv = ""
		}
	}
}

func (a *workloadAggregator) watchChildOwners(
	name string,
	listFn func(context.Context) (string, []metav1.Object, error),
	watchFn func(context.Context, string) (watch.Interface, error),
) {
	a.watchLoop(
		name,
		func(wctx context.Context) (string, error) {
			rv, items, err := listFn(wctx)
			if err != nil {
				return "", err
			}
			a.mu.Lock()
			a.childUIDs = map[types.UID]struct{}{}
			for _, obj := range items {
				if hasAnyOwnerUID(obj.GetOwnerReferences(), a.parentSet) {
					a.childUIDs[obj.GetUID()] = struct{}{}
				}
			}
			a.mu.Unlock()
			return rv, nil
		},
		watchFn,
		func(tp watch.EventType, obj any) {
			metaObj, ok := obj.(metav1.Object)
			if !ok {
				return
			}
			a.mu.Lock()
			if tp == watch.Deleted {
				delete(a.childUIDs, metaObj.GetUID())
			} else if hasAnyOwnerUID(metaObj.GetOwnerReferences(), a.parentSet) {
				a.childUIDs[metaObj.GetUID()] = struct{}{}
			}
			a.reconcileLocked()
			a.mu.Unlock()
		},
		func() {
			a.mu.Lock()
			a.childDone = true
			a.reconcileLocked()
			a.mu.Unlock()
		},
	)
}

func (a *workloadAggregator) watchPods() {
	a.watchLoop(
		"pods",
		func(wctx context.Context) (string, error) {
			list, err := a.typed.CoreV1().Pods(a.namespace).List(wctx, metav1.ListOptions{})
			if err != nil {
				return "", err
			}
			a.mu.Lock()
			a.podsByUID = map[types.UID]*corev1.Pod{}
			for i := range list.Items {
				p := list.Items[i].DeepCopy()
				a.podsByUID[p.UID] = p
			}
			a.mu.Unlock()
			return list.ResourceVersion, nil
		},
		func(wctx context.Context, rv string) (watch.Interface, error) {
			t := int64(300)
			return a.typed.CoreV1().Pods(a.namespace).Watch(wctx, metav1.ListOptions{
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
			a.mu.Lock()
			if tp == watch.Deleted {
				delete(a.podsByUID, p.UID)
			} else {
				a.podsByUID[p.UID] = p.DeepCopy()
			}
			a.reconcileLocked()
			a.mu.Unlock()
		},
		func() {
			a.mu.Lock()
			a.podsReady = true
			a.reconcileLocked()
			a.mu.Unlock()
		},
	)
}

func (a *workloadAggregator) run() {
	stopBatcher := make(chan struct{})
	batchDone := make(chan struct{})
	go func() {
		defer close(batchDone)
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stopBatcher:
				return
			case <-a.ctx.Done():
				return
			case <-t.C:
				a.flush()
			}
		}
	}()

	var watchWG sync.WaitGroup
	if a.kind == "Deployment" {
		watchWG.Add(1)
		go func() {
			defer watchWG.Done()
			a.watchChildOwners(
				"replicasets",
				func(wctx context.Context) (string, []metav1.Object, error) {
					list, err := a.typed.AppsV1().ReplicaSets(a.namespace).List(wctx, metav1.ListOptions{})
					if err != nil {
						return "", nil, err
					}
					items := make([]metav1.Object, len(list.Items))
					for i := range list.Items {
						items[i] = &list.Items[i]
					}
					return list.ResourceVersion, items, nil
				},
				func(wctx context.Context, rv string) (watch.Interface, error) {
					t := int64(300)
					return a.typed.AppsV1().ReplicaSets(a.namespace).Watch(wctx, metav1.ListOptions{
						ResourceVersion:     rv,
						AllowWatchBookmarks: true,
						TimeoutSeconds:      &t,
					})
				},
			)
		}()
	} else if a.kind == "CronJob" {
		watchWG.Add(1)
		go func() {
			defer watchWG.Done()
			a.watchChildOwners(
				"jobs",
				func(wctx context.Context) (string, []metav1.Object, error) {
					list, err := a.typed.BatchV1().Jobs(a.namespace).List(wctx, metav1.ListOptions{})
					if err != nil {
						return "", nil, err
					}
					items := make([]metav1.Object, len(list.Items))
					for i := range list.Items {
						items[i] = &list.Items[i]
					}
					return list.ResourceVersion, items, nil
				},
				func(wctx context.Context, rv string) (watch.Interface, error) {
					t := int64(300)
					return a.typed.BatchV1().Jobs(a.namespace).Watch(wctx, metav1.ListOptions{
						ResourceVersion:     rv,
						AllowWatchBookmarks: true,
						TimeoutSeconds:      &t,
					})
				},
			)
		}()
	}

	watchWG.Add(1)
	go func() {
		defer watchWG.Done()
		a.watchPods()
	}()

	watchWG.Wait()
	a.wg.Wait()
	close(stopBatcher)
	<-batchDone
	a.flush()
	a.svc.emit(a.topic, Event{Ended: true})
}
