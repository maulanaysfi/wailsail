package exec

import (
	"context"
	"encoding/base64"
	"io"
	"sync"
	"testing"

	"k8s.io/client-go/tools/remotecommand"
)

func TestServiceWriteResizeCloseAndShutdown(t *testing.T) {
	svc := NewService(nil, func(string, any) {})

	if err := svc.Write("missing", "ls\n"); err == nil {
		t.Fatal("expected error writing to unknown session")
	}
	if err := svc.Resize("missing", 80, 24); err == nil {
		t.Fatal("expected error resizing unknown session")
	}

	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	ss := &session{stdin: pw, sizes: make(chan remotecommand.TerminalSize, 2), cancel: cancel}
	svc.sessions["s1"] = ss

	gotStdin := make(chan string, 1)
	go func() {
		buf := make([]byte, 32)
		n, _ := pr.Read(buf)
		gotStdin <- string(buf[:n])
	}()

	if err := svc.Write("s1", "pwd\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if s := <-gotStdin; s != "pwd\n" {
		t.Fatalf("stdin got %q, want %q", s, "pwd\n")
	}

	if err := svc.Resize("s1", 120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	sz := <-ss.sizes
	if sz.Width != 120 || sz.Height != 40 {
		t.Fatalf("got size %+v, want 120x40", sz)
	}

	// Closing removes session, cancels ctx, and makes double-close safe.
	svc.Close("s1")
	svc.Close("s1")
	if ctx.Err() == nil {
		t.Fatal("Close must cancel session context")
	}
	if err := svc.Resize("s1", 80, 24); err == nil {
		t.Fatal("expected error resizing closed session")
	}

	// Resize on a concurrently closed session recovers cleanly without panicking.
	pr2, pw2 := io.Pipe()
	defer pr2.Close()
	_, cancel2 := context.WithCancel(context.Background())
	ss2 := &session{stdin: pw2, sizes: make(chan remotecommand.TerminalSize), cancel: cancel2}
	svc.sessions["s2"] = ss2
	ss2.close()
	if err := svc.Resize("s2", 80, 24); err == nil {
		t.Fatal("expected error resizing closed channel")
	}

	// ServiceShutdown closes remaining sessions.
	pr3, pw3 := io.Pipe()
	defer pr3.Close()
	ctx3, cancel3 := context.WithCancel(context.Background())
	svc.sessions["s3"] = &session{stdin: pw3, sizes: make(chan remotecommand.TerminalSize, 1), cancel: cancel3}
	if err := svc.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}
	if ctx3.Err() == nil {
		t.Fatal("ServiceShutdown must cancel active sessions")
	}
	if len(svc.sessions) != 0 {
		t.Fatalf("expected empty sessions map after shutdown, got %d", len(svc.sessions))
	}
}

func TestConnectedEmittedOnceAcrossSizeQueueAndWriter(t *testing.T) {
	var events []Event
	emit := func(_ string, data any) {
		events = append(events, data.(Event))
	}
	var once sync.Once
	markConnected := func() {
		once.Do(func() { emit(Topic("s"), Event{Connected: true}) })
	}

	ch := make(chan remotecommand.TerminalSize, 2)
	ch <- remotecommand.TerminalSize{Width: 80, Height: 24}
	q := sizeQueue{ch: ch, onConnect: markConnected}
	if sz := q.Next(); sz == nil || sz.Width != 80 {
		t.Fatalf("unexpected size %+v", sz)
	}
	close(ch)
	if sz := q.Next(); sz != nil {
		t.Fatalf("expected nil after close, got %+v", sz)
	}

	w := emitWriter{topic: Topic("s"), emit: emit, onConnect: markConnected}
	if _, err := w.Write([]byte("hi")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events (connected + data), got %+v", events)
	}
	if !events[0].Connected {
		t.Fatalf("first event should be connected: %+v", events[0])
	}
	decoded, err := base64.StdEncoding.DecodeString(events[1].Data)
	if err != nil || string(decoded) != "hi" {
		t.Fatalf("unexpected data event: %+v (%v)", events[1], err)
	}
}
