// Package settings persists wailsail user preferences.
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Settings are the persisted user preferences.
type Settings struct {
	Theme         string            `json:"theme"` // "system", "light" or "dark"
	LastContext   string            `json:"lastContext"`
	LastNamespace map[string]string `json:"lastNamespace"` // per context; "" = all namespaces
	LogTailLines  int64             `json:"logTailLines"`
	LogTimestamps bool              `json:"logTimestamps"`
	ShowWide      bool              `json:"showWide"`
	Pinned        []string          `json:"pinned"` // resource keys "group/resource"
}

// Defaults returns the initial settings.
func Defaults() Settings {
	return Settings{
		Theme:         "system",
		LastNamespace: map[string]string{},
		LogTailLines:  500,
		Pinned:        []string{},
	}
}

// Service is the Wails-bound settings API.
type Service struct {
	mu   sync.Mutex
	path string
}

// DefaultPath returns <UserConfigDir>/wailsail/settings.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wailsail", "settings.json"), nil
}

// NewService builds a SettingsService storing data at path.
func NewService(path string) *Service { return &Service{path: path} }

// Get loads settings, returning defaults when the file is missing.
func (s *Service) Get() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Defaults()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return Defaults(), nil // corrupt file: start fresh rather than fail
	}
	if st.LastNamespace == nil {
		st.LastNamespace = map[string]string{}
	}
	if st.Pinned == nil {
		st.Pinned = []string{}
	}
	return st, nil
}

// Save writes settings atomically.
func (s *Service) Save(st Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
