package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "settings.json")
	s := NewService(p)
	st, err := s.Get()
	if err != nil || st.LogTailLines != 500 {
		t.Fatalf("defaults: %+v %v", st, err)
	}
	st.LastContext = "kind"
	st.LastNamespace["kind"] = "default"
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get()
	if err != nil || got.LastContext != "kind" || got.LastNamespace["kind"] != "default" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := os.WriteFile(p, []byte("{garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(); err != nil || got.LastContext != "" {
		t.Fatalf("corrupt file should yield defaults: %+v %v", got, err)
	}
}

func TestScaleAndKubeconfigPaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	s := NewService(p)
	st, err := s.Get()
	if err != nil || st.Scale != 100 {
		t.Fatalf("default scale: %+v %v", st, err)
	}
	if len(st.KubeconfigPaths) != 1 || st.KubeconfigPaths[0] != DefaultKubeconfigPath() {
		t.Fatalf("default kubeconfig paths: %v", st.KubeconfigPaths)
	}

	st.Scale = 125
	st.KubeconfigPaths = []string{"/a/config", "/b/dir"}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get()
	if got.Scale != 125 || len(got.KubeconfigPaths) != 2 || got.KubeconfigPaths[1] != "/b/dir" {
		t.Fatalf("round trip: %+v", got)
	}

	// An explicitly emptied list stays empty; an invalid scale is reset.
	got.Scale = 110
	got.KubeconfigPaths = []string{}
	if err := s.Save(got); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get()
	if got.Scale != 100 || got.KubeconfigPaths == nil || len(got.KubeconfigPaths) != 0 {
		t.Fatalf("normalize: %+v", got)
	}

	// Older settings files without the new keys get defaults.
	if err := os.WriteFile(p, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get()
	if got.Theme != "dark" || got.Scale != 100 || len(got.KubeconfigPaths) != 1 {
		t.Fatalf("legacy file: %+v", got)
	}
}
