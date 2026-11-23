package logs

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
