// Package portforward forwards local TCP ports to pods.
package portforward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/wailsapp/wails/v3/pkg/application"

	"wailsail/internal/kube"
	"wailsail/internal/stream"
)

// Topic is emitted with the full forward list whenever it changes.
const Topic = "wailsail:portforwards"

// Request describes a forward to create.
type Request struct {
	Context    string `json:"context"`
	Namespace  string `json:"namespace"`
	Kind       string `json:"kind"` // "Pod" or "Service"
	Name       string `json:"name"`
	RemotePort int32  `json:"remotePort"`
	LocalPort  int32  `json:"localPort"` // 0 picks a free port
}

// Forward is an active (or failed) port-forward.
type Forward struct {
	ID         string `json:"id"`
	Context    string `json:"context"`
	Namespace  string `json:"namespace"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Pod        string `json:"pod"`
	RemotePort int32  `json:"remotePort"`
	PodPort    int32  `json:"podPort"`
	LocalPort  int32  `json:"localPort"`
	Status     string `json:"status"` // "active" or "failed"
	Error      string `json:"error,omitempty"`
	Started    string `json:"started"`
}

type entry struct {
	Forward
	stop chan struct{}
	once sync.Once
}

func (e *entry) halt() { e.once.Do(func() { close(e.stop) }) }

// Service is the Wails-bound port-forward API.
type Service struct {
	mgr     *kube.Manager
	emit    stream.Emitter
	baseCtx context.Context
	mu      sync.Mutex
	entries map[string]*entry
}

// NewService builds a PortForwardService.
func NewService(mgr *kube.Manager, emit stream.Emitter) *Service {
	return &Service{mgr: mgr, emit: emit, baseCtx: context.Background(), entries: map[string]*entry{}}
}

// ServiceStartup records the app context.
func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.baseCtx = ctx
	return nil
}

// ServiceShutdown stops all forwards.
func (s *Service) ServiceShutdown() error {
	s.mu.Lock()
	all := s.entries
	s.entries = map[string]*entry{}
	s.mu.Unlock()
	for _, e := range all {
		e.halt()
	}
	return nil
}

// List returns all forwards sorted by start time.
func (s *Service) List() []Forward {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Forward, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e.Forward)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started < out[j].Started })
	return out
}

func (s *Service) changed() { s.emit(Topic, s.List()) }

// Stop stops and removes a forward.
func (s *Service) Stop(id string) {
	s.mu.Lock()
	e, ok := s.entries[id]
	delete(s.entries, id)
	s.mu.Unlock()
	if ok {
		e.halt()
		s.changed()
	}
}

// ServicePorts lists the ports of a Service.
func (s *Service) ServicePorts(contextName, namespace, name string) ([]kube.ContainerPort, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return nil, err
	}
	svc, err := c.Typed.CoreV1().Services(namespace).Get(s.baseCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var out []kube.ContainerPort
	for _, p := range svc.Spec.Ports {
		out = append(out, kube.ContainerPort{Name: p.Name, Port: p.Port, Protocol: string(p.Protocol)})
	}
	return out, nil
}

// Start creates a new forward and waits until it is listening.
func (s *Service) Start(req Request) (Forward, error) {
	c, err := s.mgr.For(req.Context)
	if err != nil {
		return Forward{}, err
	}
	if req.RemotePort <= 0 {
		return Forward{}, errors.New("remote port is required")
	}
	ctx, cancel := context.WithTimeout(s.baseCtx, 20*time.Second)
	defer cancel()
	pod, podPort, err := resolveTarget(ctx, c.Typed, req)
	if err != nil {
		return Forward{}, err
	}
	if req.LocalPort != 0 && !PortAvailable(req.LocalPort) {
		return Forward{}, fmt.Errorf("local port %d is already in use", req.LocalPort)
	}

	url := c.Typed.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(req.Namespace).Name(pod).SubResource("portforward").URL()
	transport, upgrader, err := spdy.RoundTripperFor(c.Config)
	if err != nil {
		return Forward{}, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, url)

	e := &entry{stop: make(chan struct{})}
	ready := make(chan struct{})
	fw, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"},
		[]string{fmt.Sprintf("%d:%d", req.LocalPort, podPort)}, e.stop, ready, io.Discard, io.Discard)
	if err != nil {
		return Forward{}, err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- fw.ForwardPorts() }()

	select {
	case <-ready:
	case err := <-errCh:
		if err == nil {
			err = errors.New("port-forward ended before becoming ready")
		}
		return Forward{}, err
	case <-ctx.Done():
		e.halt()
		return Forward{}, errors.New("timed out establishing port-forward")
	}
	local := req.LocalPort
	if ports, perr := fw.GetPorts(); perr == nil && len(ports) > 0 {
		local = int32(ports[0].Local)
	}
	e.Forward = Forward{
		ID: stream.NewID(), Context: req.Context, Namespace: req.Namespace, Kind: req.Kind,
		Name: req.Name, Pod: pod, RemotePort: req.RemotePort, PodPort: podPort, LocalPort: local,
		Status: "active", Started: time.Now().UTC().Format(time.RFC3339Nano),
	}
	s.mu.Lock()
	s.entries[e.ID] = e
	s.mu.Unlock()
	s.changed()

	go func() {
		err := <-errCh
		s.mu.Lock()
		cur, ok := s.entries[e.ID]
		if ok && cur == e {
			cur.Status = "failed"
			if err != nil {
				cur.Error = err.Error()
			} else {
				cur.Error = "connection to pod closed"
			}
		}
		s.mu.Unlock()
		if ok {
			s.changed()
		}
	}()
	return e.Forward, nil
}

// PortAvailable reports whether 127.0.0.1:port can be bound.
func PortAvailable(port int32) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// resolveTarget maps a request to a concrete pod name and container port.
func resolveTarget(ctx context.Context, cs kubernetes.Interface, req Request) (string, int32, error) {
	switch req.Kind {
	case "Pod", "pod", "pods":
		return req.Name, req.RemotePort, nil
	case "Service", "service", "services":
		svc, err := cs.CoreV1().Services(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
		if err != nil {
			return "", 0, err
		}
		if len(svc.Spec.Selector) == 0 {
			return "", 0, fmt.Errorf("service %s has no selector", req.Name)
		}
		var sp *corev1.ServicePort
		for i := range svc.Spec.Ports {
			if svc.Spec.Ports[i].Port == req.RemotePort {
				sp = &svc.Spec.Ports[i]
				break
			}
		}
		if sp == nil {
			return "", 0, fmt.Errorf("service %s has no port %d", req.Name, req.RemotePort)
		}
		pods, err := cs.CoreV1().Pods(req.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String(),
		})
		if err != nil {
			return "", 0, err
		}
		pod := PickPod(pods.Items)
		if pod == nil {
			return "", 0, fmt.Errorf("no running pod backs service %s", req.Name)
		}
		port, err := TargetPort(sp, pod)
		if err != nil {
			return "", 0, err
		}
		return pod.Name, port, nil
	default:
		return "", 0, fmt.Errorf("port-forward to %s is not supported", req.Kind)
	}
}

// PickPod prefers ready, running, non-terminating pods.
func PickPod(pods []corev1.Pod) *corev1.Pod {
	var fallback *corev1.Pod
	for i := range pods {
		p := &pods[i]
		if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return p
			}
		}
		if fallback == nil {
			fallback = p
		}
	}
	return fallback
}

// TargetPort resolves a service port to a container port on pod.
func TargetPort(sp *corev1.ServicePort, pod *corev1.Pod) (int32, error) {
	tp := sp.TargetPort
	switch {
	case tp.Type == intstr.Int && tp.IntVal != 0:
		return tp.IntVal, nil
	case tp.Type == intstr.String && tp.StrVal != "":
		for _, c := range pod.Spec.Containers {
			for _, p := range c.Ports {
				if p.Name == tp.StrVal {
					return p.ContainerPort, nil
				}
			}
		}
		return 0, fmt.Errorf("pod %s has no port named %q", pod.Name, tp.StrVal)
	default:
		return sp.Port, nil
	}
}
