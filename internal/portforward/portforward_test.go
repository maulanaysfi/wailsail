package portforward

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestTargetPort(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}},
	}}}}
	cases := []struct {
		sp   corev1.ServicePort
		want int32
	}{
		{corev1.ServicePort{Port: 80, TargetPort: intstr.FromInt32(9000)}, 9000},
		{corev1.ServicePort{Port: 80, TargetPort: intstr.FromString("http")}, 8080},
		{corev1.ServicePort{Port: 80}, 80},
	}
	for _, c := range cases {
		got, err := TargetPort(&c.sp, pod)
		if err != nil || got != c.want {
			t.Fatalf("got %d %v want %d", got, err, c.want)
		}
	}
	if _, err := TargetPort(&corev1.ServicePort{TargetPort: intstr.FromString("nope")}, pod); err == nil {
		t.Fatal("expected error for unknown named port")
	}
}

func TestPickPod(t *testing.T) {
	notReady := corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	notReady.Name = "a"
	ready := corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	ready.Name = "b"
	pending := corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}}
	if p := PickPod([]corev1.Pod{pending, notReady, ready}); p == nil || p.Name != "b" {
		t.Fatalf("got %v", p)
	}
	if p := PickPod([]corev1.Pod{pending, notReady}); p == nil || p.Name != "a" {
		t.Fatalf("fallback got %v", p)
	}
	if PickPod([]corev1.Pod{pending}) != nil {
		t.Fatal("pending pod must not be picked")
	}
}

func TestPortAvailable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer l.Close()
	port, _ := strconv.Atoi(l.Addr().(*net.TCPAddr).AddrPort().String()[len("127.0.0.1:"):])
	if PortAvailable(int32(port)) {
		t.Fatal("bound port reported available")
	}
}

func TestResolveTarget(t *testing.T) {
	ctx := context.Background()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "web"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromString("http-ctr")}},
		},
	}
	noSelSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "external", Namespace: "prod"},
	}
	readyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "prod", Labels: map[string]string{"app": "web"}},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Ports: []corev1.ContainerPort{{Name: "http-ctr", ContainerPort: 8080}}}},
		},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
	cs := k8sfake.NewSimpleClientset(svc, noSelSvc, readyPod)

	podName, podPort, err := resolveTarget(ctx, cs, Request{Kind: "Pod", Namespace: "prod", Name: "web-1", RemotePort: 8080})
	if err != nil || podName != "web-1" || podPort != 8080 {
		t.Fatalf("pod resolve got %q %d %v", podName, podPort, err)
	}

	podName, podPort, err = resolveTarget(ctx, cs, Request{Kind: "Service", Namespace: "prod", Name: "web", RemotePort: 80})
	if err != nil || podName != "web-1" || podPort != 8080 {
		t.Fatalf("service resolve got %q %d %v", podName, podPort, err)
	}

	if _, _, err := resolveTarget(ctx, cs, Request{Kind: "Service", Namespace: "prod", Name: "external", RemotePort: 80}); err == nil || !strings.Contains(err.Error(), "no selector") {
		t.Fatalf("expected no selector error, got %v", err)
	}

	if _, _, err := resolveTarget(ctx, cs, Request{Kind: "ConfigMap", Namespace: "prod", Name: "cm", RemotePort: 80}); err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}
