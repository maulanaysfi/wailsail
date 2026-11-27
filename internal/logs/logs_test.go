package logs

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestPump(t *testing.T) {
	var got []string
	err := Pump(context.Background(), strings.NewReader("a\nb\nc"), 10*time.Millisecond,
		func(lines []string) { got = append(got, lines...) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("got %v", got)
	}
}

func TestPumpLongLines(t *testing.T) {
	exact := strings.Repeat("a", maxLine)
	over := strings.Repeat("b", maxLine+5)
	in := exact + "\n" + over + "\r\nnext\n"

	var got []string
	err := Pump(context.Background(), strings.NewReader(in), 10*time.Millisecond,
		func(lines []string) { got = append(got, lines...) })
	if err != nil {
		t.Fatalf("Pump returned error on >1 MiB line: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d lines, want 4", len(got))
	}
	if len(got[0]) != maxLine || got[0] != exact {
		t.Fatalf("line 0 len = %d, want %d", len(got[0]), maxLine)
	}
	if len(got[1]) != maxLine || got[2] != "bbbbb" || got[3] != "next" {
		t.Fatalf("unexpected split lines: len(1)=%d [2]=%q [3]=%q", len(got[1]), got[2], got[3])
	}
}

func TestSplitTailLinesAndSelectAggregatedPods(t *testing.T) {
	if got := splitTailLines(100, 3); got != 34 {
		t.Fatalf("splitTailLines(100, 3) = %d, want 34", got)
	}
	if got := splitTailLines(5, 10); got != 1 {
		t.Fatalf("splitTailLines(5, 10) = %d, want 1", got)
	}
	if got := splitTailLines(0, 3); got != 0 {
		t.Fatalf("splitTailLines(0, 3) = %d, want 0", got)
	}

	owners := map[types.UID]struct{}{"rs-1": {}, "rs-2": {}}
	makePod := func(name, ownerUID string, phase corev1.PodPhase) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				OwnerReferences: []metav1.OwnerReference{{UID: types.UID(ownerUID)}},
			},
			Status: corev1.PodStatus{Phase: phase},
		}
	}
	pods := []*corev1.Pod{
		makePod("old-done", "rs-1", corev1.PodSucceeded),
		makePod("new-b", "rs-2", corev1.PodRunning),
		makePod("other", "rs-other", corev1.PodRunning),
		makePod("new-a", "rs-1", corev1.PodPending),
	}

	selected, total := selectAggregatedPods(pods, owners, 2)
	if total != 3 || len(selected) != 2 {
		t.Fatalf("got selected=%d total=%d, want 2/3", len(selected), total)
	}
	if selected[0].Name != "new-a" || selected[1].Name != "new-b" {
		t.Fatalf("expected active pods prioritized by name, got %s, %s", selected[0].Name, selected[1].Name)
	}
}

func TestStartWorkloadFollowFalseTerminates(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "job-pod-1",
			Namespace:       "prod",
			UID:             "pod-1",
			OwnerReferences: []metav1.OwnerReference{{UID: "job-1", Kind: "Job", Name: "migrate"}},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
			},
		},
	}
	typed := k8sfake.NewSimpleClientset(pod)
	endedCh := make(chan struct{}, 1)
	var mu sync.Mutex
	var events []Event
	svc := NewService(nil, func(_ string, data any) {
		ev, ok := data.(Event)
		if !ok {
			return
		}
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		if ev.Ended {
			select {
			case endedCh <- struct{}{}:
			default:
			}
		}
	})

	id, err := svc.startWorkloadWithClient(typed, "prod", "Job", "job-1", Options{Follow: false}, "wl-test")
	if err != nil {
		t.Fatalf("startWorkloadWithClient failed: %v", err)
	}
	defer svc.Stop(id)

	select {
	case <-endedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("expected StartWorkload with Follow=false to emit Ended=true and terminate")
	}

	mu.Lock()
	defer mu.Unlock()
	sawConnected := false
	for _, ev := range events {
		if ev.Connected && ev.PodCount == 1 && ev.TotalPods == 1 {
			sawConnected = true
		}
	}
	if !sawConnected {
		t.Fatalf("expected Connected event with PodCount=1, got %+v", events)
	}
}
