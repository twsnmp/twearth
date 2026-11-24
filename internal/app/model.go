package app

import (
	"context"
	"log"
	"os"
	"sync"

	"github.com/dustin/go-humanize"
	"github.com/twsnmp/twearth/internal/ai/model"
	"github.com/twsnmp/twearth/internal/ai/tensai"
)

// AIHardwareStatus contains hardware acceleration details and local model/lib paths.
type AIHardwareStatus struct {
	Acceleration string `json:"acceleration"`
	Detail       string `json:"detail"`
	ModelDir     string `json:"modelDir"`
	LibDir       string `json:"libDir"`
	WGPULibPath  string `json:"wgpuLibPath"`
	HasGPULib    bool   `json:"hasGpuLib"`
}

// DownloadProgressEvent is emitted to the frontend during model/GPU downloads.
type DownloadProgressEvent struct {
	Downloaded      int64  `json:"downloaded"`
	Total           int64  `json:"total"`
	Percent         int    `json:"percent"`
	DownloadedHuman string `json:"downloadedHuman"`
	TotalHuman      string `json:"totalHuman"`
}

var (
	modelCancelMu  sync.Mutex
	modelCancelCtx context.CancelFunc

	gpuCancelMu  sync.Mutex
	gpuCancelCtx context.CancelFunc
)

// GetAIHardwareStatus returns hardware acceleration info and library paths.
func (s *AppService) GetAIHardwareStatus() AIHardwareStatus {
	tensai.InitWGPULibrary()
	accType, accDetail := tensai.DetectAcceleration()

	wgpuLib := os.Getenv("TENSAI_WGPU_LIB")
	hasLib := wgpuLib != ""

	return AIHardwareStatus{
		Acceleration: string(accType),
		Detail:       accDetail,
		ModelDir:     model.DefaultModelDir(),
		LibDir:       model.DefaultLibDir(),
		WGPULibPath:  wgpuLib,
		HasGPULib:    hasLib,
	}
}

// GetLocalModels returns a list of available local GGUF models.
func (s *AppService) GetLocalModels() []model.ModelInfo {
	models, err := model.ListModels("")
	if err != nil {
		log.Printf("GetLocalModels err=%v", err)
		return []model.ModelInfo{}
	}
	return models
}

// GetModelPresets returns the list of recommended preset models.
func (s *AppService) GetModelPresets() []model.PresetModelInfo {
	return model.PresetModelMetadata
}

// DownloadModel downloads a model by preset name, HuggingFace repo, or URL.
func (s *AppService) DownloadModel(target string) string {
	modelCancelMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	modelCancelCtx = cancel
	modelCancelMu.Unlock()

	defer func() {
		modelCancelMu.Lock()
		modelCancelCtx = nil
		modelCancelMu.Unlock()
	}()

	progress := func(downloaded, total int64) {
		pct := 0
		if total > 0 {
			pct = int(float64(downloaded) / float64(total) * 100)
		}
		if s.app != nil {
			s.app.Event.Emit("twearth:model-download-progress", DownloadProgressEvent{
				Downloaded:      downloaded,
				Total:           total,
				Percent:         pct,
				DownloadedHuman: humanize.Bytes(uint64(downloaded)),
				TotalHuman:      humanize.Bytes(uint64(total)),
			})
		}
	}

	path, err := model.DownloadModel(ctx, "", target, progress)
	if err != nil {
		log.Printf("DownloadModel err=%v", err)
		return err.Error()
	}
	log.Printf("DownloadModel success path=%s", path)
	return ""
}

// CancelModelDownload cancels the ongoing model download.
func (s *AppService) CancelModelDownload() string {
	modelCancelMu.Lock()
	defer modelCancelMu.Unlock()
	if modelCancelCtx != nil {
		modelCancelCtx()
		modelCancelCtx = nil
		return ""
	}
	return "no download in progress"
}

// DeleteLocalModel deletes a local model.
func (s *AppService) DeleteLocalModel(name string) string {
	if err := model.RemoveModel("", name); err != nil {
		log.Printf("DeleteLocalModel err=%v", err)
		return err.Error()
	}
	log.Printf("DeleteLocalModel success name=%s", name)
	return ""
}

// DownloadGPULibrary downloads and installs the wgpu-native library.
func (s *AppService) DownloadGPULibrary() string {
	gpuCancelMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	gpuCancelCtx = cancel
	gpuCancelMu.Unlock()

	defer func() {
		gpuCancelMu.Lock()
		gpuCancelCtx = nil
		gpuCancelMu.Unlock()
	}()

	progress := func(downloaded, total int64) {
		pct := 0
		if total > 0 {
			pct = int(float64(downloaded) / float64(total) * 100)
		}
		if s.app != nil {
			s.app.Event.Emit("twearth:gpu-download-progress", DownloadProgressEvent{
				Downloaded:      downloaded,
				Total:           total,
				Percent:         pct,
				DownloadedHuman: humanize.Bytes(uint64(downloaded)),
				TotalHuman:      humanize.Bytes(uint64(total)),
			})
		}
	}

	path, err := model.DownloadWGPULibrary(ctx, "", progress)
	if err != nil {
		log.Printf("DownloadGPULibrary err=%v", err)
		return err.Error()
	}

	_ = os.Setenv("TENSAI_WGPU_LIB", path)
	tensai.ResetGPUInit()
	log.Printf("DownloadGPULibrary installed to %s", path)
	return ""
}

// CancelGPUDownload cancels the ongoing GPU library download.
func (s *AppService) CancelGPUDownload() string {
	gpuCancelMu.Lock()
	defer gpuCancelMu.Unlock()
	if gpuCancelCtx != nil {
		gpuCancelCtx()
		gpuCancelCtx = nil
		return ""
	}
	return "no download in progress"
}
