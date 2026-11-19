package kube

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func kubeconfigYAML(name string) string {
	return `apiVersion: v1
kind: Config
clusters:
- name: ` + name + `
  cluster: {server: https://` + name + `.example:6443}
users:
- name: ` + name + `
  user: {token: t}
contexts:
- name: ` + name + `
  context: {cluster: ` + name + `, user: ` + name + `}
current-context: ` + name + `
`
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveKubeconfigPaths(t *testing.T) {
	root := t.TempDir()
	single := filepath.Join(root, "single.yaml")
	writeFile(t, single, kubeconfigYAML("single"))

	dir := filepath.Join(root, "clusters")
	writeFile(t, filepath.Join(dir, "b.yaml"), kubeconfigYAML("b"))
	writeFile(t, filepath.Join(dir, "a"), kubeconfigYAML("a"))
	writeFile(t, filepath.Join(dir, "notes.txt"), "just some notes\n")
	writeFile(t, filepath.Join(dir, ".hidden"), kubeconfigYAML("hidden"))
	writeFile(t, filepath.Join(dir, "a~"), kubeconfigYAML("backup"))
	writeFile(t, filepath.Join(dir, "sub", "c.yaml"), kubeconfigYAML("c"))

	got := ResolveKubeconfigPaths([]string{single, dir, single, filepath.Join(root, "missing")})
	want := []string{single, filepath.Join(dir, "a"), filepath.Join(dir, "b.yaml")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestExpandPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WAILSAIL_TEST_DIR", "kube")

	cases := map[string]string{
		"~/.kube/config":             filepath.Join(home, ".kube", "config"),
		"$HOME/.kube/config":         filepath.Join(home, ".kube", "config"),
		"${WAILSAIL_TEST_DIR}/x":     filepath.Join("kube", "x"),
		"%WAILSAIL_TEST_DIR%/x":      filepath.Join("kube", "x"),
		"  ":                         "",
		"%WAILSAIL_UNSET_VAR_XYZ%/x": filepath.Clean("%WAILSAIL_UNSET_VAR_XYZ%/x"),
	}
	for in, want := range cases {
		if got := ExpandPath(in); got != want {
			t.Errorf("ExpandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestManagerSetKubeconfigPaths(t *testing.T) {
	t.Setenv("KUBECONFIG", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "one.yaml"), kubeconfigYAML("one"))
	writeFile(t, filepath.Join(root, "more", "two.yaml"), kubeconfigYAML("two"))
	writeFile(t, filepath.Join(root, "more", "three.yaml"), kubeconfigYAML("three"))

	m := NewManager()
	m.SetKubeconfigPaths([]string{filepath.Join(root, "one.yaml"), filepath.Join(root, "more")})
	ctxs, err := m.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range ctxs {
		names = append(names, c.Name)
	}
	if !reflect.DeepEqual(names, []string{"one", "three", "two"}) {
		t.Fatalf("contexts %v", names)
	}

	// New files in a configured directory show up after Reset (Reload).
	writeFile(t, filepath.Join(root, "more", "four.yaml"), kubeconfigYAML("four"))
	m.Reset()
	if ctxs, _ = m.Contexts(); len(ctxs) != 4 {
		t.Fatalf("after reset: %v", ctxs)
	}
	if files := m.KubeconfigFiles(); len(files) != 4 {
		t.Fatalf("files %v", files)
	}

	// Reset and SetKubeconfigPaths advance generation so in-flight builds
	// cannot populate the cache with clients from older rules.
	g0 := m.gen
	m.Reset()
	if m.gen <= g0 {
		t.Fatalf("Reset did not increment gen: %d -> %d", g0, m.gen)
	}
	g1 := m.gen
	m.SetKubeconfigPaths(nil)
	if m.gen <= g1 {
		t.Fatalf("SetKubeconfigPaths did not increment gen: %d -> %d", g1, m.gen)
	}
}
