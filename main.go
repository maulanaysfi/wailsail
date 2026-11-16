package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"wailsail/internal/exec"
	"wailsail/internal/kube"
	"wailsail/internal/logs"
	"wailsail/internal/portforward"
	"wailsail/internal/settings"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed icon.png
var appIcon []byte

func main() {
	// Services are constructed before the app exists, so the emitter looks
	// the running app up lazily.
	emit := func(name string, data any) {
		if app := application.Get(); app != nil {
			app.Event.Emit(name, data)
		}
	}

	settingsPath, err := settings.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}

	settingsSvc := settings.NewService(settingsPath)
	mgr := kube.NewManager()
	if st, err := settingsSvc.Get(); err != nil {
		log.Printf("settings: %v", err)
	} else {
		mgr.SetKubeconfigPaths(st.KubeconfigPaths)
	}

	app := application.New(application.Options{
		Name:        "wailsail",
		Description: "An original Kubernetes desktop client written in Go with Wails v3",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(kube.NewService(mgr, emit)),
			application.NewService(logs.NewService(mgr, emit)),
			application.NewService(exec.NewService(mgr, emit)),
			application.NewService(portforward.NewService(mgr, emit)),
			application.NewService(settingsSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "wailsail",
		Width:            1360,
		Height:           860,
		MinWidth:         900,
		MinHeight:        560,
		BackgroundColour: application.NewRGB(20, 22, 28),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
