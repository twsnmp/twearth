package tensai

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	tensaimod "github.com/mattn/tensai"
	"github.com/mattn/tensai/gpu"
	"github.com/twsnmp/twearth/internal/platform"
	"golang.org/x/sys/cpu"
)

// AccelerationType represents the hardware backend in use.
type AccelerationType string

const (
	AccelGPU      AccelerationType = "GPU"
	AccelSIMD     AccelerationType = "SIMD"
	AccelSIMDAVX2 AccelerationType = "SIMD (AVX2)"
	AccelSIMDNEON AccelerationType = "SIMD (NEON)"
	AccelCPU      AccelerationType = "CPU (Portable)"
)

var (
	initLibOnce sync.Once
	gpuMu       sync.Mutex
	gpuDevice   *gpu.Device
	gpuInitErr  error
	gpuTried    bool
)

// ResetGPUInit clears cached GPU initialization failure so GetGPUDevice can retry
// after installing the wgpu-native library without restarting the app.
func ResetGPUInit() {
	gpuMu.Lock()
	defer gpuMu.Unlock()
	if gpuDevice == nil {
		gpuTried = false
		gpuInitErr = nil
	}
}

// InitWGPULibrary searches for the wgpu-native library in standard locations
// and sets TENSAI_WGPU_LIB if found.
func InitWGPULibrary() {
	initLibOnce.Do(func() {
		if os.Getenv("TENSAI_WGPU_LIB") != "" {
			return
		}

		libName := "libwgpu_native.so"
		switch runtime.GOOS {
		case "darwin":
			libName = "libwgpu_native.dylib"
		case "windows":
			libName = "wgpu_native.dll"
		}

		candidates := make([]string, 0, 10)

		// 1. Next to executable
		if exe, err := os.Executable(); err == nil {
			candidates = append(candidates, filepath.Join(filepath.Dir(exe), libName))
			if runtime.GOOS == "windows" {
				candidates = append(candidates, filepath.Join(filepath.Dir(exe), "libwgpu_native.dll"))
			}
		}

		// 2. Current working directory
		candidates = append(candidates, libName)
		if runtime.GOOS == "windows" {
			candidates = append(candidates, "libwgpu_native.dll")
		}

		// 3. ~/.twearth/lib/
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".twearth", "lib", libName))
			if runtime.GOOS == "windows" {
				candidates = append(candidates, filepath.Join(home, ".twearth", "lib", "libwgpu_native.dll"))
			}
		}

		// 4. Platform DataDir lib/
		if pInfo := platform.GetInfo(); pInfo.DataDir != "" {
			candidates = append(candidates, filepath.Join(pInfo.DataDir, "lib", libName))
		}

		// 5. macOS homebrew paths
		if runtime.GOOS == "darwin" {
			candidates = append(candidates, "/opt/homebrew/lib/"+libName, "/usr/local/lib/"+libName)
		}

		for _, path := range candidates {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				if abs, err := filepath.Abs(path); err == nil {
					_ = os.Setenv("TENSAI_WGPU_LIB", abs)
					break
				}
			}
		}
	})
}

// GetGPUDevice tries to open and initialize the WebGPU device (cached).
// It also registers the GPU device as a global tensai accelerator.
func GetGPUDevice() (*gpu.Device, error) {
	InitWGPULibrary()
	gpuMu.Lock()
	defer gpuMu.Unlock()
	if gpuTried {
		return gpuDevice, gpuInitErr
	}
	gpuTried = true
	dev, err := gpu.Open(gpu.HighPerformance)
	if err != nil {
		dev, err = gpu.Open(gpu.Default)
	}
	if err == nil && dev != nil {
		gpuDevice = dev
		gpuInitErr = nil
		tensaimod.UseAccelerator(dev)
	} else {
		gpuInitErr = err
		if err != nil {
			log.Printf("tensai: GetGPUDevice failed (TENSAI_WGPU_LIB=%q): %v", os.Getenv("TENSAI_WGPU_LIB"), err)
		}
	}
	return gpuDevice, gpuInitErr
}

// HasAVX2 reports whether the current CPU supports AVX2 instructions.
func HasAVX2() bool {
	return runtime.GOARCH == "amd64" && cpu.X86.HasAVX2
}

// HasNEON reports whether the current CPU supports NEON instructions (arm64).
func HasNEON() bool {
	return runtime.GOARCH == "arm64"
}

// DetectAcceleration returns the active acceleration type and details string.
func DetectAcceleration() (AccelerationType, string) {
	return DetectAccelerationWithOptions(false)
}

// DetectAccelerationWithOptions returns the active acceleration type with optional GPU bypass.
func DetectAccelerationWithOptions(noGPU bool) (AccelerationType, string) {
	if !noGPU {
		dev, err := GetGPUDevice()
		if err == nil && dev != nil {
			name := dev.Name()
			if name == "" {
				name = "WebGPU Adapter"
			}
			backend := gpu.Backend()
			if backend != "" {
				return AccelGPU, fmt.Sprintf("%s (%s)", name, backend)
			}
			return AccelGPU, name
		}
		if err != nil && strings.Contains(err.Error(), "built without wgpu support") {
			return AccelCPU, fmt.Sprintf("Pure Go (%s/%s) [binary built without -tags wgpu24]", runtime.GOOS, runtime.GOARCH)
		}
	}

	if HasAVX2() {
		return AccelSIMDAVX2, "AVX2 8-lane FMA Vectorized"
	}
	if HasNEON() {
		return AccelSIMDNEON, "NEON 4-lane Advanced SIMD Vectorized"
	}

	return AccelCPU, fmt.Sprintf("Pure Go (%s/%s)", runtime.GOOS, runtime.GOARCH)
}
