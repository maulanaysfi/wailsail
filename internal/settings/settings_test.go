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
