// Package exec provides interactive container shells.
package exec

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/wailsapp/wails/v3/pkg/application"

	"wailsail/internal/kube"
	"wailsail/internal/stream"
)

// DefaultShell prefers bash and falls back to sh.
var DefaultShell = []string{"/bin/sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"}

// Event is the payload pushed on Topic(id). Data is base64 encoded so that
// arbitrary byte sequences (split UTF-8, escape codes) survive JSON.
type Event struct {
	Connected bool   `json:"connected,omitempty"`
	Data      string `json:"data,omitempty"`
	Exited    bool   `json:"exited,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Topic is the event name for an exec session.
func Topic(id string) string { return "wailsail:exec:" + id }

type session struct {
	stdin  *io.PipeWriter
	sizes  chan remotecommand.TerminalSize
	cancel context.CancelFunc
	once   sync.Once
}

func (s *session) close() {
	s.once.Do(func() {
		s.cancel()
		_ = s.stdin.Close()
		close(s.sizes)
	})
}

// sizeQueue adapts a channel to remotecommand.TerminalSizeQueue and notifies
// once the remote stream begins pulling terminal sizes.
type sizeQueue struct {
	ch        chan remotecommand.TerminalSize
	onConnect func()
}

func (q sizeQueue) Next() *remotecommand.TerminalSize {
	s, ok := <-q.ch
	if !ok {
		return nil
	}
	if q.onConnect != nil {
		q.onConnect()
	}
	return &s
}

// emitWriter forwards output chunks as events.
type emitWriter struct {
	topic     string
	emit      stream.Emitter
	onConnect func()
}

func (w emitWriter) Write(p []byte) (int, error) {
	if w.onConnect != nil {
		w.onConnect()
	}
	w.emit(w.topic, Event{Data: base64.StdEncoding.EncodeToString(p)})
	return len(p), nil
}

// Service is the Wails-bound exec API.
type Service struct {
	mgr      *kube.Manager
	emit     stream.Emitter
	baseCtx  context.Context
	mu       sync.Mutex
	sessions map[string]*session
}

// NewService builds an ExecService.
func NewService(mgr *kube.Manager, emit stream.Emitter) *Service {
	return &Service{mgr: mgr, emit: emit, baseCtx: context.Background(), sessions: map[string]*session{}}
}

// ServiceStartup records the app context.
func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.baseCtx = ctx
	return nil
}

// ServiceShutdown closes all sessions.
func (s *Service) ServiceShutdown() error {
	s.mu.Lock()
	all := s.sessions
	s.sessions = map[string]*session{}
	s.mu.Unlock()
	for _, ss := range all {
		ss.close()
	}
	return nil
}

// Start opens an interactive TTY session in a container. command may be
// empty to use DefaultShell.
func (s *Service) Start(contextName, namespace, pod, container string, command []string, cols, rows uint16, sessionID string) (string, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return "", err
	}
	if len(command) == 0 {
		command = DefaultShell
	}
	if sessionID == "" {
		sessionID = stream.NewID()
	}
	req := c.Typed.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	spdyExec, err := remotecommand.NewSPDYExecutor(c.Config, "POST", req.URL())
	if err != nil {
		return "", err
	}
	executor := spdyExec
	if wsExec, werr := remotecommand.NewWebSocketExecutor(c.Config, "GET", req.URL().String()); werr == nil {
		if fb, ferr := remotecommand.NewFallbackExecutor(wsExec, spdyExec, func(err error) bool {
			return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
		}); ferr == nil {
			executor = fb
		}
	}

	ctx, cancel := context.WithCancel(s.baseCtx)
	pr, pw := io.Pipe()
	ss := &session{stdin: pw, sizes: make(chan remotecommand.TerminalSize, 4), cancel: cancel}
	if cols > 0 && rows > 0 {
		ss.sizes <- remotecommand.TerminalSize{Width: cols, Height: rows}
	}
	s.mu.Lock()
	if old, ok := s.sessions[sessionID]; ok {
		old.close()
	}
	s.sessions[sessionID] = ss
	s.mu.Unlock()

	topic := Topic(sessionID)
	var connOnce sync.Once
	markConnected := func() {
		connOnce.Do(func() { s.emit(topic, Event{Connected: true}) })
	}
	out := emitWriter{topic: topic, emit: s.emit, onConnect: markConnected}
	go func() {
		err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             pr,
			Stdout:            out,
			Tty:               true,
			TerminalSizeQueue: sizeQueue{ch: ss.sizes, onConnect: markConnected},
		})
		ev := Event{Exited: true}
		if err != nil && !errors.Is(err, context.Canceled) {
			ev.Error = err.Error()
		}
		s.emit(topic, ev)
		_ = pr.Close()
		s.mu.Lock()
		if s.sessions[sessionID] == ss {
			delete(s.sessions, sessionID)
		}
		s.mu.Unlock()
		ss.close()
	}()
	return sessionID, nil
}

func (s *Service) get(id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return ss, nil
}

// Write sends keyboard input to a session.
func (s *Service) Write(sessionID string, data string) error {
	ss, err := s.get(sessionID)
	if err != nil {
		return err
	}
	_, err = io.WriteString(ss.stdin, data)
	return err
}

// Resize updates the terminal size of a session.
func (s *Service) Resize(sessionID string, cols, rows uint16) (err error) {
	ss, err := s.get(sessionID)
	if err != nil {
		return err
	}
	defer func() {
		if recover() != nil { // channel closed concurrently
			err = fmt.Errorf("session %s closed", sessionID)
		}
	}()
	select {
	case ss.sizes <- remotecommand.TerminalSize{Width: cols, Height: rows}:
	default: // drop if the queue is full; a newer resize will follow
	}
	return nil
}

// Close terminates a session.
func (s *Service) Close(sessionID string) {
	s.mu.Lock()
	ss, ok := s.sessions[sessionID]
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	if ok {
		ss.close()
	}
}
