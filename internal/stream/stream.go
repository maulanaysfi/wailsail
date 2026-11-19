// Package stream provides a registry for long-lived, cancellable backend
// streams (watches, log follows, exec sessions) and an event-emitter
// abstraction so services can be tested without a running Wails app.
package stream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// Emitter publishes an event with a payload to the frontend.
type Emitter func(name string, data any)

type entry struct {
	cancel context.CancelFunc
}

// Registry tracks cancellable streams by ID.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: map[string]*entry{}}
}

// NewID returns a random, URL-safe identifier.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Start creates a new cancellable context derived from parent, registers it
// under id (a random ID is generated when id is empty) and returns the
// context, the stream ID, and a done callback. Calling done cancels this
// stream's context and removes it only if it has not been replaced by a newer
// stream with the same ID. An existing stream with the same ID is cancelled.
func (r *Registry) Start(parent context.Context, id string) (context.Context, string, func()) {
	ctx, cancel := context.WithCancel(parent)
	if id == "" {
		id = NewID()
	}
	e := &entry{cancel: cancel}
	r.mu.Lock()
	old := r.entries[id]
	r.entries[id] = e
	r.mu.Unlock()
	if old != nil {
		old.cancel()
	}
	done := func() {
		r.mu.Lock()
		if r.entries[id] == e {
			delete(r.entries, id)
		}
		r.mu.Unlock()
		cancel()
	}
	return ctx, id, done
}

// Stop cancels the stream with the given ID. It reports whether it existed.
func (r *Registry) Stop(id string) bool {
	r.mu.Lock()
	e, ok := r.entries[id]
	delete(r.entries, id)
	r.mu.Unlock()
	if ok {
		e.cancel()
	}
	return ok
}

// StopAll cancels every registered stream.
func (r *Registry) StopAll() {
	r.mu.Lock()
	entries := r.entries
	r.entries = map[string]*entry{}
	r.mu.Unlock()
	for _, e := range entries {
		e.cancel()
	}
}

// Len returns the number of active streams.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
