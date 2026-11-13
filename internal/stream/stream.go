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

// Registry tracks cancellable streams by ID.
type Registry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{cancels: map[string]context.CancelFunc{}}
}

// NewID returns a random, URL-safe identifier.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Start creates a new cancellable context derived from parent, registers it
// under id (a random ID is generated when id is empty) and returns the
// context together with the stream ID. Letting callers choose the ID allows
// the frontend to subscribe to the stream's events before it starts, so no
// early event is lost. An existing stream with the same ID is cancelled.
func (r *Registry) Start(parent context.Context, id string) (context.Context, string) {
	ctx, cancel := context.WithCancel(parent)
	if id == "" {
		id = NewID()
	}
	r.mu.Lock()
	if old, ok := r.cancels[id]; ok {
		old()
	}
	r.cancels[id] = cancel
	r.mu.Unlock()
	return ctx, id
}

// Done removes the stream from the registry without cancelling it again.
// Streams should call this when they terminate on their own.
func (r *Registry) Done(id string) {
	r.mu.Lock()
	cancel, ok := r.cancels[id]
	delete(r.cancels, id)
	r.mu.Unlock()
	if ok {
		cancel()
	}
}

// Stop cancels the stream with the given ID. It reports whether it existed.
func (r *Registry) Stop(id string) bool {
	r.mu.Lock()
	cancel, ok := r.cancels[id]
	delete(r.cancels, id)
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// StopAll cancels every registered stream.
func (r *Registry) StopAll() {
	r.mu.Lock()
	cancels := r.cancels
	r.cancels = map[string]context.CancelFunc{}
	r.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// Len returns the number of active streams.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cancels)
}
