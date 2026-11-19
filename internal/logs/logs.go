// Package logs streams container logs to the frontend.
package logs

import (
	"bufio"
	"context"
	"io"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/wailsapp/wails/v3/pkg/application"

	"wailsail/internal/kube"
	"wailsail/internal/stream"
)

// Options control a log follow request.
type Options struct {
	Container  string `json:"container"`
	TailLines  int64  `json:"tailLines"`
	Timestamps bool   `json:"timestamps"`
	Previous   bool   `json:"previous"`
	Follow     bool   `json:"follow"`
}

// Event is the payload pushed on Topic(id).
type Event struct {
	Connected bool     `json:"connected,omitempty"`
	Lines     []string `json:"lines,omitempty"`
	Ended     bool     `json:"ended,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// Topic is the event name for a log stream.
func Topic(id string) string { return "wailsail:logs:" + id }

// Service is the Wails-bound log API.
type Service struct {
	mgr     *kube.Manager
	streams *stream.Registry
	emit    stream.Emitter
	baseCtx context.Context
}

// NewService builds a LogService.
func NewService(mgr *kube.Manager, emit stream.Emitter) *Service {
	return &Service{mgr: mgr, streams: stream.NewRegistry(), emit: emit, baseCtx: context.Background()}
}

// ServiceStartup records the app context.
func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.baseCtx = ctx
	return nil
}

// ServiceShutdown stops every log stream.
func (s *Service) ServiceShutdown() error {
	s.streams.StopAll()
	return nil
}

// Stop cancels a log stream.
func (s *Service) Stop(streamID string) { s.streams.Stop(streamID) }

// Start streams logs of one container. Lines are batched and emitted on
// Topic(id); a final event has Ended set.
func (s *Service) Start(contextName, namespace, pod string, opts Options, streamID string) (string, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return "", err
	}
	plo := &corev1.PodLogOptions{
		Container:  opts.Container,
		Follow:     opts.Follow,
		Timestamps: opts.Timestamps,
		Previous:   opts.Previous,
	}
	if opts.TailLines > 0 {
		t := opts.TailLines
		plo.TailLines = &t
	}
	ctx, id := s.streams.Start(s.baseCtx, streamID)
	topic := Topic(id)
	go func() {
		defer s.streams.Done(id)
		rc, err := c.Typed.CoreV1().Pods(namespace).GetLogs(pod, plo).Stream(ctx)
		if err != nil {
			s.emit(topic, Event{Ended: true, Error: err.Error()})
			return
		}
		defer rc.Close()
		s.emit(topic, Event{Connected: true})
		err = Pump(ctx, rc, 100*time.Millisecond, func(lines []string) { s.emit(topic, Event{Lines: lines}) })
		ev := Event{Ended: true}
		if err != nil && ctx.Err() == nil {
			ev.Error = err.Error()
		}
		s.emit(topic, ev)
	}()
	return id, nil
}

// maxLine bounds a single log line (longer lines are split).
const maxLine = 1 << 20

// Pump reads lines from r and delivers them in batches at most every
// interval. It returns when r is exhausted or ctx is cancelled.
func Pump(ctx context.Context, r io.Reader, interval time.Duration, deliver func([]string)) error {
	var (
		mu      sync.Mutex
		pending []string
	)
	flush := func() {
		mu.Lock()
		batch := pending
		pending = nil
		mu.Unlock()
		if len(batch) > 0 {
			deliver(batch)
		}
	}
	stop := make(chan struct{})
	tickerDone := make(chan struct{})
	go func() {
		defer close(tickerDone)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				flush()
			}
		}
	}()

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		mu.Lock()
		pending = append(pending, sc.Text())
		mu.Unlock()
	}
	close(stop)
	<-tickerDone
	flush()
	return sc.Err()
}
