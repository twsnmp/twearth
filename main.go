package main

import (
	"context"
	"embed"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twsnmp/twearth/internal/analysis"
	"github.com/twsnmp/twearth/internal/app"
	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/mcp"
	"github.com/twsnmp/twearth/internal/platform"
	"github.com/twsnmp/twearth/internal/store"
	"github.com/twsnmp/twearth/internal/updater"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Version is overridden at build time via -ldflags "-X main.Version=...".
var Version = "0.1.0"

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[store.Config]("twearth:config-updated")
	application.RegisterEvent[string]("twearth:foreground-resumed")
	application.RegisterEvent[app.DownloadProgressEvent]("twearth:model-download-progress")
	application.RegisterEvent[app.DownloadProgressEvent]("twearth:gpu-download-progress")
	application.RegisterEvent[analysis.DiscoveryProgress]("twearth:correlation-discovery-progress")
	application.RegisterEvent[analysis.DiscoveryResultItem]("twearth:correlation-discovery-result")
	application.RegisterEvent[analysis.CorrelationRequest]("twearth:correlation-inspect")
	application.RegisterEvent[analysis.RankingProgress]("twearth:ranking-progress")
	application.RegisterEvent[[]analysis.RankingResultItem]("twearth:ranking-results-batch")
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

	mcpSvc := mcp.NewService(mcp.Dependencies{
		Store:     st,
		Collector: col,
		Version:   Version,
		OnConfigUpdated: func(updated store.Config) {
			wailsApp.Event.Emit("twearth:config-updated", updated)
		},
	})
	svc.SetMCPService(mcpSvc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	col.Start(ctx)

	if cfg.MCPEnabled {
		if err := mcpSvc.Configure(ctx, true, cfg.MCPPort); err != nil {
			log.Printf("mcp server start warning: %v", err)
		}
	}

	if pInfo.CanSelfUpdate {
		if err := upd.Init(wailsApp, Version, cfg.AutoCheckUpdate); err != nil {
			log.Printf("updater init warning: %v", err)
		}
	}

	var shuttingDown atomic.Bool
	var quitOnce sync.Once
	wailsApp.OnShutdown(func() {
		shuttingDown.Store(true)
		svc.ShutdownCorrelationDiscovery()
		svc.ShutdownMunicipalityRanking()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		_ = mcpSvc.Stop(stopCtx)
	})

	mainWin := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
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

	mainWin.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if shuttingDown.Load() {
			return
		}
		svc.CloseAllAnalysisWindows()
		quitOnce.Do(func() {
			wailsApp.Quit()
		})
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
