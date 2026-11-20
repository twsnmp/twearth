package main

import (
	"context"
	"embed"
	"log"

	"github.com/twsnmp/twearth/internal/app"
	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/platform"
	"github.com/twsnmp/twearth/internal/store"
	"github.com/twsnmp/twearth/internal/updater"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Version is overridden at build time via -ldflags "-X main.Version=...".
var Version = "0.1.0"

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[store.Config]("twearth:config-updated")
	application.RegisterEvent[string]("twearth:foreground-resumed")
}

func main() {
	pInfo := platform.GetInfo()
	st, err := store.Open(pInfo.DataDir)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	cfg, err := st.GetConfig()
	if err != nil {
		log.Printf("failed to load config, using defaults: %v", err)
		cfg = store.DefaultConfig()
	}

	upd := updater.NewService()
	col := collector.NewManager(st)
	svc := app.NewAppService(st, upd, col)

	wailsApp := application.New(application.Options{
		Name:        "twearth",
		Description: "twearth - Standalone Natural Environment & Weather Monitor",
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	svc.SetApp(wailsApp)
	col.SetApp(wailsApp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	col.Start(ctx)

	if pInfo.CanSelfUpdate {
		if err := upd.Init(wailsApp, Version, cfg.AutoCheckUpdate); err != nil {
			log.Printf("updater init warning: %v", err)
		}
	}

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "twearth",
		Width:  1280,
		Height: 800,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
