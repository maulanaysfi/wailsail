package stream

import (
	"context"
	"testing"
)

func TestRegistryReusedIDDoneDoesNotCancelReplacement(t *testing.T) {
	r := NewRegistry()
	ctx1, id1, done1 := r.Start(context.Background(), "same")
	ctx2, id2, done2 := r.Start(context.Background(), "same")
	if id1 != "same" || id2 != "same" {
		t.Fatalf("unexpected ids %q %q", id1, id2)
	}
	if ctx1.Err() == nil {
		t.Fatal("replacing a stream must cancel the previous context")
	}

	// Old goroutine exiting after replacement must not remove or cancel ctx2.
	done1()
	if ctx2.Err() != nil {
		t.Fatalf("old done() cancelled replacement stream: %v", ctx2.Err())
	}
	if len(r.entries) != 1 {
		t.Fatalf("expected replacement stream to stay registered, len=%d", len(r.entries))
	}

	done2()
	if ctx2.Err() == nil {
		t.Fatal("done2() must cancel ctx2")
	}
	if len(r.entries) != 0 {
		t.Fatalf("expected empty registry, len=%d", len(r.entries))
	}
}

func TestRegistryStopAndStopAll(t *testing.T) {
	r := NewRegistry()
	ctxA, idA, _ := r.Start(context.Background(), "")
	ctxB, _, _ := r.Start(context.Background(), "b")

	if !r.Stop(idA) {
		t.Fatal("expected Stop(idA) to report true")
	}
	if r.Stop(idA) {
		t.Fatal("expected second Stop(idA) to report false")
	}
	if ctxA.Err() == nil {
		t.Fatal("Stop must cancel ctxA")
	}
	if ctxB.Err() != nil {
		t.Fatal("Stop(idA) must not cancel ctxB")
	}

	r.StopAll()
	if ctxB.Err() == nil {
		t.Fatal("StopAll must cancel ctxB")
	}
	if len(r.entries) != 0 {
		t.Fatalf("expected empty registry after StopAll, len=%d", len(r.entries))
	}
}
