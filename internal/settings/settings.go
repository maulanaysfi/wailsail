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
	Theme           string            `json:"theme"`           // "system", "light" or "dark"
	Scale           int               `json:"scale"`           // UI scale in percent: 50, 75, 100, 125 or 150
	KubeconfigPaths []string          `json:"kubeconfigPaths"` // kubeconfig files and/or directories to scan
	LastContext     string            `json:"lastContext"`
	LastNamespace   map[string]string `json:"lastNamespace"` // per context; "" = all namespaces
	LogTailLines    int64             `json:"logTailLines"`
	LogTimestamps   bool              `json:"logTimestamps"`
	ShowWide        bool                `json:"showWide"`
	Pinned          []string            `json:"pinned"`       // resource keys "group/resource"
	ColumnOrders    map[string][]string `json:"columnOrders"` // per resource key column order
	ColumnWidths    map[string]int      `json:"columnWidths"` // "<resourceKey>:<colLabel>" -> width in px
}

// DefaultScale is the UI scale used when none (or an invalid one) is stored.
const DefaultScale = 100

// ValidScales lists the selectable UI scales in percent.
var ValidScales = []int{50, 75, 100, 125, 150}

// DefaultKubeconfigPath returns the platform default kubeconfig location:
// $HOME/.kube/config on Linux/macOS, %USERPROFILE%\.kube\config on Windows.
func DefaultKubeconfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".kube", "config")
	}
	return filepath.Join(home, ".kube", "config")
}

// Defaults returns the initial settings.
func Defaults() Settings {
	return Settings{
		Theme:           "system",
		Scale:           DefaultScale,
		KubeconfigPaths: []string{DefaultKubeconfigPath()},
		LastNamespace:   map[string]string{},
		LogTailLines:    500,
		ShowWide:        true,
		Pinned:          []string{},
		ColumnOrders:    map[string][]string{},
		ColumnWidths:    map[string]int{},
	}
}

func validScale(v int) bool {
	for _, s := range ValidScales {
		if s == v {
			return true
		}
	}
	return false
}

// normalize fills missing or invalid fields with defaults.
func (st *Settings) normalize() {
	if !validScale(st.Scale) {
		st.Scale = DefaultScale
	}
	if st.KubeconfigPaths == nil {
		st.KubeconfigPaths = []string{DefaultKubeconfigPath()}
	}
	if st.LastNamespace == nil {
		st.LastNamespace = map[string]string{}
	}
	if st.Pinned == nil {
		st.Pinned = []string{}
	}
	if st.ColumnOrders == nil {
		st.ColumnOrders = map[string][]string{}
	}
	if st.ColumnWidths == nil {
		st.ColumnWidths = map[string]int{}
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
	st.normalize()
	return st, nil
}

// DefaultKubeconfig returns the platform default kubeconfig path.
func (s *Service) DefaultKubeconfig() string { return DefaultKubeconfigPath() }

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
