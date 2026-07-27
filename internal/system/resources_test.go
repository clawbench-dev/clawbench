package system

import (
	"testing"
	"time"
)

func TestGetResources(t *testing.T) {
	ResetSampler()
	res, err := GetResources()
	if err != nil {
		t.Fatalf("GetResources() error: %v", err)
	}
	// CPU percent should be 0-100 (first call returns 0 since we just reset)
	if res.CPU.Percent < 0 || res.CPU.Percent > 100 {
		t.Errorf("CPU.Percent = %f, want [0, 100]", res.CPU.Percent)
	}
	if res.CPU.CoreCount <= 0 {
		t.Errorf("CPU.CoreCount = %d, want > 0", res.CPU.CoreCount)
	}
	// Memory should have positive values
	if res.Memory.Total == 0 {
		t.Error("Memory.Total = 0, want > 0")
	}
	if res.Memory.Used > res.Memory.Total {
		t.Errorf("Memory.Used = %d > Memory.Total = %d", res.Memory.Used, res.Memory.Total)
	}
	// Disk should have positive values for data dir partition
	if res.Disk.Total == 0 {
		t.Error("Disk.Total = 0, want > 0")
	}
	if res.Disk.Used > res.Disk.Total {
		t.Errorf("Disk.Used = %d > Disk.Total = %d", res.Disk.Used, res.Disk.Total)
	}
	// Network should have non-negative values (first call returns 0)
	if res.Network.UploadRate < 0 {
		t.Errorf("Network.UploadRate = %f, want >= 0", res.Network.UploadRate)
	}
	if res.Network.DownloadRate < 0 {
		t.Errorf("Network.DownloadRate = %f, want >= 0", res.Network.DownloadRate)
	}
	// Disk I/O should have non-negative values (first call returns 0)
	if res.DiskIO.ReadRate < 0 {
		t.Errorf("DiskIO.ReadRate = %f, want >= 0", res.DiskIO.ReadRate)
	}
	if res.DiskIO.WriteRate < 0 {
		t.Errorf("DiskIO.WriteRate = %f, want >= 0", res.DiskIO.WriteRate)
	}
	// Load should have non-negative values
	if res.Load.Load1 < 0 {
		t.Errorf("Load.Load1 = %f, want >= 0", res.Load.Load1)
	}
}

func TestGetResourcesCPUSampling(t *testing.T) {
	ResetSampler()
	// First call initializes the CPU sampler, returns 0%
	res1, err := GetResources()
	if err != nil {
		t.Fatalf("first GetResources() error: %v", err)
	}
	if res1.CPU.Percent != 0 {
		t.Errorf("first call CPU.Percent = %f, want 0 (initializing)", res1.CPU.Percent)
	}
	// Second call should return a valid CPU percent (not -1)
	res2, err := GetResources()
	if err != nil {
		t.Fatalf("second GetResources() error: %v", err)
	}
	if res2.CPU.Percent < 0 {
		t.Errorf("CPU.Percent after sampling = %f, want >= 0", res2.CPU.Percent)
	}
}

func TestCalculateCPUPercentRaw(t *testing.T) {
	tests := []struct {
		name      string
		prevIdle  float64
		prevTotal float64
		curIdle   float64
		curTotal  float64
		elapsed   time.Duration
		want      float64
	}{
		{"50% usage", 50, 100, 75, 150, time.Second, 50.0},
		{"100% usage", 50, 100, 50, 150, time.Second, 100.0},
		{"0% usage", 50, 100, 100, 150, time.Second, 0.0},
		{"zero elapsed", 50, 100, 75, 150, 0, 0.0},
		{"over 100% clamped to 100", 100, 100, 50, 150, time.Second, 100.0},
		{"total diff <= 0", 50, 150, 50, 100, time.Second, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCPUPercentRaw(tt.prevIdle, tt.prevTotal, tt.curIdle, tt.curTotal, tt.elapsed)
			if got != tt.want {
				t.Errorf("calculateCPUPercentRaw() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestGetResourcesCaching(t *testing.T) {
	ResetSampler()
	res1, err := GetResources()
	if err != nil {
		t.Fatalf("first GetResources() error: %v", err)
	}
	// Immediate second call should return cached response (same pointer)
	res2, err := GetResources()
	if err != nil {
		t.Fatalf("second GetResources() error: %v", err)
	}
	if res1 != res2 {
		t.Error("cached response should be the same object within TTL")
	}
}

func TestResetSampler(t *testing.T) {
	ResetSampler()
	_, _ = GetResources() // initialize
	s := globalSampler
	if !s.cpuInited {
		t.Error("expected cpuInited after GetResources")
	}
	ResetSampler()
	s = globalSampler
	if s.cpuInited {
		t.Error("expected cpuInited=false after ResetSampler")
	}
	if s.cachedResp != nil {
		t.Error("expected cachedResp=nil after ResetSampler")
	}
}
