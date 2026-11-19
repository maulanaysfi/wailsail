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
	if r.Len() != 1 {
		t.Fatalf("expected replacement stream to stay registered, len=%d", r.Len())
	}

	done2()
	if ctx2.Err() == nil {
		t.Fatal("done2() must cancel ctx2")
	}
	if r.Len() != 0 {
		t.Fatalf("expected empty registry, len=%d", r.Len())
	}
}
