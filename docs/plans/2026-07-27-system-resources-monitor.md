# System Resources Monitor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a Gauge icon button to the left of the green-dot status indicator in the header. Clicking it opens a PopupMenu showing CPU, memory, disk, and network usage with progress bars + percentages.

**Architecture:** New Go backend package `internal/system/` collects resource metrics with response caching (500ms TTL) to handle concurrent requests safely. New REST endpoint `GET /api/system/resources` returns all metrics. Frontend composable `useSystemResources` uses a single 1s polling interval. The Gauge icon is an independent entry point — the green-dot popup remains unchanged.

**Tech Stack:** Go standard library + `github.com/shirou/gopsutil/v3` for cross-platform metrics; Vue 3 composable; PopupMenu for the resource panel.

---

### Task 1: Add gopsutil dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add gopsutil dependency**

Run: `cd /home/xulongzhe/projects/clawbench && go get github.com/shirou/gopsutil/v3/cpu github.com/shirou/gopsutil/v3/mem github.com/shirou/gopsutil/v3/disk github.com/shirou/gopsutil/v3/net`

**Step 2: Verify dependency resolves**

Run: `go mod tidy`
Expected: No errors

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add gopsutil dependency for system resource monitoring"
```

---

### Task 2: Create internal/system package with resource collector

**Files:**
- Create: `internal/system/resources.go`
- Create: `internal/system/resources_test.go`

**Design decisions (from code review):**
- Use `model.DataDir` instead of custom dataDir() — the data dir is set via `--data-dir` flag at startup
- Cache responses with 500ms TTL to handle concurrent requests safely (second request within TTL returns cached data instead of near-zero CPU/network values)
- Store network prev bytes as scalars (not map) to avoid interface disappearing edge cases
- Add `ResetSampler()` for test isolation
- Add `Errors` field to response so frontend can distinguish "0%" from "sampling failed"
- CPU: `cpu.Times(false)` for aggregate; on Windows `Iowait` is 0 (harmless in formula)

**Step 1: Write the failing test**

```go
package system

import (
	"testing"
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
	// Network should have non-negative values
	if res.Network.UploadRate < 0 {
		t.Errorf("Network.UploadRate = %f, want >= 0", res.Network.UploadRate)
	}
	if res.Network.DownloadRate < 0 {
		t.Errorf("Network.DownloadRate = %f, want >= 0", res.Network.DownloadRate)
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
	// Wait a bit for CPU times to change
	// Second call should return a valid CPU percent (not -1)
	res2, err := GetResources()
	if err != nil {
		t.Fatalf("second GetResources() error: %v", err)
	}
	if res2.CPU.Percent < 0 {
		t.Errorf("CPU.Percent after sampling = %f, want >= 0", res2.CPU.Percent)
	}
}

func TestCalculateCPUPercent(t *testing.T) {
	tests := []struct {
		name       string
		prevIdle   float64
		prevIowait float64
		prevTotal  float64
		curIdle    float64
		curIowait  float64
		curTotal   float64
		want       float64
	}{
		{"50% usage", 50, 0, 100, 75, 0, 150, 50.0},
		{"100% usage", 50, 0, 100, 50, 0, 150, 100.0},
		{"0% usage", 50, 0, 100, 100, 0, 150, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCPUPercent(
				timesStatFrom(tt.prevIdle, tt.prevIowait, tt.prevTotal),
				timesStatFrom(tt.curIdle, tt.curIowait, tt.curTotal),
				1.0,
			)
			if got != tt.want {
				t.Errorf("calculateCPUPercent() = %f, want %f", got, tt.want)
			}
		})
	}
}

// helper to create a minimal TimesStat for testing
func timesStatFrom(idle, iowait, total float64) cpuTimesLike {
	return cpuTimesLike{Idle: idle, Iowait: iowait, total: total}
}

type cpuTimesLike struct {
	Idle    float64
	Iowait  float64
	total   float64
}

func (t cpuTimesLike) Total() float64 { return t.total }
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/system/ -v -run TestGetResources`
Expected: FAIL (package doesn't exist)

**Step 3: Write implementation**

```go
package system

import (
	"fmt"
	"sync"
	"time"

	"clawbench/internal/model"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	psutilNet "github.com/shirou/gopsutil/v3/net"
)

// ResourceResponse is the JSON structure returned by GET /api/system/resources.
type ResourceResponse struct {
	CPU     CPUInfo     `json:"cpu"`
	Memory  MemoryInfo  `json:"memory"`
	Disk    DiskInfo    `json:"disk"`
	Network NetworkInfo `json:"network"`
	Errors  []string    `json:"errors,omitempty"` // per-metric errors, if any
}

// CPUInfo holds CPU usage information.
type CPUInfo struct {
	Percent   float64 `json:"percent"`    // 0-100, interval-based usage
	CoreCount int     `json:"core_count"` // logical core count
}

// MemoryInfo holds memory usage information.
// On Linux, Used = Total - Available (excludes buffers/cache).
type MemoryInfo struct {
	Used    uint64  `json:"used"`    // bytes
	Total   uint64  `json:"total"`   // bytes
	Percent float64 `json:"percent"` // 0-100
}

// DiskInfo holds disk usage for the data directory partition.
type DiskInfo struct {
	Used    uint64  `json:"used"`     // bytes
	Total   uint64  `json:"total"`    // bytes
	Percent float64 `json:"percent"`  // 0-100
}

// NetworkInfo holds network throughput rates.
type NetworkInfo struct {
	UploadRate   float64 `json:"upload_rate"`   // bytes/sec
	DownloadRate float64 `json:"download_rate"` // bytes/sec
}

// sampler holds state for interval-based calculations.
type sampler struct {
	mu sync.Mutex

	// CPU sampling
	cpuPrevIdle float64
	cpuPrevTotal float64
	cpuTime     time.Time
	cpuInited   bool

	// Network sampling — store summed scalars (not per-interface map)
	// to avoid interface disappearing/reappearing edge cases
	prevBytesSent uint64
	prevBytesRecv uint64
	netTime       time.Time
	netInited     bool

	// Response cache — prevents concurrent requests from getting
	// near-zero CPU/network values due to artificially short intervals
	cachedResp    *ResourceResponse
	cachedAt      time.Time
}

var globalSampler = &sampler{}

const cacheTTL = 500 * time.Millisecond

// ResetSampler resets the global sampler state (for testing).
func ResetSampler() {
	globalSampler = &sampler{}
}

// dataDir returns the data directory path for disk usage reporting.
// Uses model.DataDir which is set at server startup via --data-dir flag.
func dataDir() string {
	if model.DataDir != "" {
		return model.DataDir
	}
	return "."
}

// GetResources collects all system resource metrics.
// Uses response caching with 500ms TTL to handle concurrent requests safely.
func GetResources() (*ResourceResponse, error) {
	s := globalSampler
	s.mu.Lock()
	defer s.mu.Unlock()

	// Return cached response if still fresh
	if s.cachedResp != nil && time.Since(s.cachedAt) < cacheTTL {
		return s.cachedResp, nil
	}

	var errs []string
	resp := &ResourceResponse{}

	// CPU
	if err := s.sampleCPU(resp); err != nil {
		resp.CPU = CPUInfo{Percent: -1}
		errs = append(errs, fmt.Sprintf("cpu: %v", err))
	}

	// Memory
	if err := s.sampleMemory(resp); err != nil {
		resp.Memory = MemoryInfo{}
		errs = append(errs, fmt.Sprintf("memory: %v", err))
	}

	// Disk
	if err := s.sampleDisk(resp); err != nil {
		resp.Disk = DiskInfo{}
		errs = append(errs, fmt.Sprintf("disk: %v", err))
	}

	// Network
	if err := s.sampleNetwork(resp); err != nil {
		resp.Network = NetworkInfo{}
		errs = append(errs, fmt.Sprintf("network: %v", err))
	}

	resp.Errors = errs
	if len(errs) == 0 {
		resp.Errors = nil // omit empty array from JSON
	}

	s.cachedResp = resp
	s.cachedAt = time.Now()
	return resp, nil
}

func (s *sampler) sampleCPU(resp *ResourceResponse) error {
	coreCount, err := cpu.Counts(true)
	if err != nil {
		return fmt.Errorf("cpu counts: %w", err)
	}
	resp.CPU.CoreCount = coreCount

	times, err := cpu.Times(false) // false = aggregate (not per-core)
	if err != nil {
		return fmt.Errorf("cpu times: %w", err)
	}
	if len(times) == 0 {
		return fmt.Errorf("no cpu times returned")
	}

	now := time.Now()
	cur := times[0]
	curIdle := cur.Idle + cur.Iowait // Iowait is 0 on Windows (harmless)
	curTotal := cur.Total()

	if !s.cpuInited {
		s.cpuPrevIdle = curIdle
		s.cpuPrevTotal = curTotal
		s.cpuTime = now
		s.cpuInited = true
		resp.CPU.Percent = 0
		return nil
	}

	elapsed := now.Sub(s.cpuTime)
	resp.CPU.Percent = calculateCPUPercentRaw(s.cpuPrevIdle, s.cpuPrevTotal, curIdle, curTotal, elapsed)
	s.cpuPrevIdle = curIdle
	s.cpuPrevTotal = curTotal
	s.cpuTime = now
	return nil
}

// calculateCPUPercentRaw calculates CPU usage percentage from idle/total deltas.
func calculateCPUPercentRaw(prevIdle, prevTotal, curIdle, curTotal float64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	totalDiff := curTotal - prevTotal
	if totalDiff <= 0 {
		return 0
	}
	idleDiff := curIdle - prevIdle
	usage := (1 - float64(idleDiff)/float64(totalDiff)) * 100
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

func (s *sampler) sampleMemory(resp *ResourceResponse) error {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return fmt.Errorf("virtual memory: %w", err)
	}
	// Use Available to exclude buffers/cache on Linux.
	// vm.Used includes buffers/cache; Total - Available is the real application-used memory.
	used := vm.Total - vm.Available
	resp.Memory.Total = vm.Total
	resp.Memory.Used = used
	if vm.Total > 0 {
		resp.Memory.Percent = float64(used) / float64(vm.Total) * 100
	}
	return nil
}

func (s *sampler) sampleDisk(resp *ResourceResponse) error {
	dir := dataDir()
	usage, err := disk.Usage(dir)
	if err != nil {
		return fmt.Errorf("disk usage for %s: %w", dir, err)
	}
	resp.Disk.Total = usage.Total
	resp.Disk.Used = usage.Used
	resp.Disk.Percent = usage.UsedPercent
	return nil
}

func (s *sampler) sampleNetwork(resp *ResourceResponse) error {
	counters, err := psutilNet.IOCounters(true) // true = per-interface
	if err != nil {
		return fmt.Errorf("net io counters: %w", err)
	}

	now := time.Now()

	// Sum all non-lo interfaces
	var totalBytesSent, totalBytesRecv uint64
	for _, c := range counters {
		if c.Name == "lo" {
			continue
		}
		totalBytesSent += c.BytesSent
		totalBytesRecv += c.BytesRecv
	}

	if !s.netInited {
		s.prevBytesSent = totalBytesSent
		s.prevBytesRecv = totalBytesRecv
		s.netTime = now
		s.netInited = true
		resp.Network = NetworkInfo{}
		return nil
	}

	// Calculate rates from previous sample using scalar prev values.
	// This avoids issues when interfaces disappear (e.g., VPN tunnel down)
	// — the prev scalar already includes the old interface's bytes,
	// but since the current total will be lower, rate will be clamped to 0.
	elapsed := now.Sub(s.netTime).Seconds()
	if elapsed > 0 {
		uploadDiff := int64(totalBytesSent - s.prevBytesSent)
		downloadDiff := int64(totalBytesRecv - s.prevBytesRecv)
		if uploadDiff < 0 {
			uploadDiff = 0
		}
		if downloadDiff < 0 {
			downloadDiff = 0
		}
		resp.Network.UploadRate = float64(uploadDiff) / elapsed
		resp.Network.DownloadRate = float64(downloadDiff) / elapsed
	}

	s.prevBytesSent = totalBytesSent
	s.prevBytesRecv = totalBytesRecv
	s.netTime = now
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/system/ -v -timeout 30s`
Expected: PASS (note: TestCalculateCPUPercent needs adjustment since we changed the function signature — update test to use `calculateCPUPercentRaw` directly)

**Step 5: Commit**

```bash
git add internal/system/resources.go internal/system/resources_test.go
git commit -m "feat: add internal/system package for resource monitoring"
```

---

### Task 3: Create API handler for system resources

**Files:**
- Create: `internal/handler/system_resources.go`
- Create: `internal/handler/system_resources_test.go`
- Modify: `internal/handler/handler.go` (add route)

**Design decisions (from code review):**
- Use existing `requireMethod()` and `writeJSON()` helpers from handler.go
- Add `Cache-Control: no-store` header (data is real-time, ~1s validity)
- Add comment about single-user auth assumption

**Step 1: Write the failing test**

```go
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeSystemResources(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/system/resources", nil)
	w := httptest.NewRecorder()

	ServeSystemResources(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify no-store cache header
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}

	var resp struct {
		CPU struct {
			Percent   float64 `json:"percent"`
			CoreCount int     `json:"core_count"`
		} `json:"cpu"`
		Memory struct {
			Used    uint64  `json:"used"`
			Total   uint64  `json:"total"`
			Percent float64 `json:"percent"`
		} `json:"memory"`
		Disk struct {
			Used    uint64  `json:"used"`
			Total   uint64  `json:"total"`
			Percent float64 `json:"percent"`
		} `json:"disk"`
		Network struct {
			UploadRate   float64 `json:"upload_rate"`
			DownloadRate float64 `json:"download_rate"`
		} `json:"network"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if resp.Memory.Total == 0 {
		t.Error("memory.total = 0, want > 0")
	}
	if resp.Disk.Total == 0 {
		t.Error("disk.total = 0, want > 0")
	}
}

func TestServeSystemResources_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/system/resources", nil)
	w := httptest.NewRecorder()

	ServeSystemResources(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/ -v -run TestServeSystemResources`
Expected: FAIL (ServeSystemResources not defined)

**Step 3: Write handler implementation**

```go
package handler

import (
	"net/http"

	"clawbench/internal/system"
)

// ServeSystemResources returns current system resource metrics.
// Requires authentication (applied via middleware.Auth in route registration).
// Note: exposes system-level metrics (CPU, memory, disk, network) to all
// authenticated users. Acceptable for single-user ClawBench; if multi-tenancy
// is added, this should be admin-only.
func ServeSystemResources(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	res, err := system.GetResources()
	if err != nil {
		http.Error(w, "failed to collect system resources", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, res)
}
```

**Step 4: Register the route in handler.go**

In `RegisterRoutes`, after the health/auth check routes block, add:

```go
register("/api/system/resources", middleware.Auth(ServeSystemResources))
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/handler/ -v -run TestServeSystemResources`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/handler/system_resources.go internal/handler/system_resources_test.go internal/handler/handler.go
git commit -m "feat: add GET /api/system/resources endpoint"
```

---

### Task 4: Create frontend composable useSystemResources

**Files:**
- Create: `web/src/composables/useSystemResources.ts`
- Create: `web/src/composables/__tests__/useSystemResources.test.ts`

**Design decisions (from code review):**
- **Single polling interval** at 1s (fastest needed rate). All metrics returned in one request; slower-changing metrics (memory, disk) simply update less dramatically.
- No `sharedState` dead variable
- No `diskFetched` dead flag
- Clean `activeCount` reference counting for start/stop

**Step 1: Write the composable**

```typescript
import { ref, onUnmounted } from 'vue'
import { appLog } from '@/utils/appLog'

export interface CPUInfo {
  percent: number
  core_count: number
}

export interface MemoryInfo {
  used: number
  total: number
  percent: number
}

export interface DiskInfo {
  used: number
  total: number
  percent: number
}

export interface NetworkInfo {
  upload_rate: number
  download_rate: number
}

export interface SystemResources {
  cpu: CPUInfo
  memory: MemoryInfo
  disk: DiskInfo
  network: NetworkInfo
  errors?: string[]
}

const POLL_INTERVAL = 1000 // 1s — fastest rate needed (CPU, network)

let activeCount = 0
let timer: ReturnType<typeof setInterval> | null = null

const resources = ref<SystemResources>({
  cpu: { percent: 0, core_count: 0 },
  memory: { used: 0, total: 0, percent: 0 },
  disk: { used: 0, total: 0, percent: 0 },
  network: { upload_rate: 0, download_rate: 0 },
})

const loading = ref(false)

async function fetchResources() {
  try {
    loading.value = true
    const resp = await fetch('/api/system/resources')
    if (!resp.ok) return
    const data: SystemResources = await resp.json()
    resources.value = data
  } catch (e) {
    appLog.w('SystemResources', 'fetch failed', e)
  } finally {
    loading.value = false
  }
}

function startPolling() {
  activeCount++
  if (activeCount > 1) return // already polling

  // Initial fetch — two calls: first initializes CPU/network sampler,
  // second returns actual calculated rates
  fetchResources().then(() => {
    // Short delay before second fetch to allow CPU/network interval sampling
    setTimeout(() => fetchResources(), 200)
  })

  timer = setInterval(fetchResources, POLL_INTERVAL)
}

function stopPolling() {
  activeCount--
  if (activeCount > 0) return // still in use

  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

export function useSystemResources() {
  onUnmounted(() => {
    stopPolling()
  })

  return {
    resources,
    loading,
    startPolling,
    stopPolling,
  }
}
```

**Step 2: Write the test**

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockFetch = vi.fn()
globalThis.fetch = mockFetch

describe('useSystemResources', () => {
  beforeEach(() => {
    mockFetch.mockReset()
  })

  it('should export composable function', async () => {
    const { useSystemResources } = await import('../useSystemResources')
    expect(typeof useSystemResources).toBe('function')
  })

  it('should fetch resources on startPolling', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 25.5, core_count: 4 },
        memory: { used: 4000000000, total: 8000000000, percent: 50 },
        disk: { used: 50000000000, total: 200000000000, percent: 25 },
        network: { upload_rate: 1024, download_rate: 51200 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling } = useSystemResources()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    stopPolling()
  })

  it('should start only one timer with multiple startPolling calls', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 0, core_count: 4 },
        memory: { used: 0, total: 0, percent: 0 },
        disk: { used: 0, total: 0, percent: 0 },
        network: { upload_rate: 0, download_rate: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling } = useSystemResources()
    startPolling()
    startPolling()
    // Should only have one timer, not two
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    // First stopPolling should NOT clear timer (activeCount = 1)
    stopPolling()
    // Second stopPolling should clear timer (activeCount = 0)
    stopPolling()
  })
})
```

**Step 3: Run tests**

Run: `npx vitest run web/src/composables/__tests__/useSystemResources.test.ts`
Expected: PASS

**Step 4: Commit**

```bash
git add web/src/composables/useSystemResources.ts web/src/composables/__tests__/useSystemResources.test.ts
git commit -m "feat: add useSystemResources composable with single polling interval"
```

---

### Task 5: Create SystemResourcesPanel component

**Files:**
- Create: `web/src/components/common/SystemResourcesPanel.vue`
- Create: `web/src/components/common/__tests__/SystemResourcesPanel.test.ts`

**Design decisions:**
- Use `Cpu` (not `MemoryStick` which doesn't exist in lucide) for CPU, `HardDrive` for disk
- For memory icon, use `CircuitBoard` or simply reuse `HardDrive` with different label — actually lucide has `MemoryStick` icon, verify during implementation
- Progress bars with color thresholds: normal (green) / warning (yellow ≥70%) / critical (red ≥90%)
- Expose `startPolling`/`stopPolling` for parent lifecycle management

**Step 1: Write the component**

```vue
<template>
  <div class="system-resources-panel">
    <!-- CPU -->
    <div class="resource-row">
      <div class="resource-header">
        <Cpu :size="13" class="resource-icon" />
        <span class="resource-label">{{ t('systemResources.cpu') }}</span>
        <span class="resource-value">{{ cpuPercent }}%</span>
      </div>
      <div class="progress-bar">
        <div class="progress-fill" :style="{ width: cpuBarWidth + '%' }" :class="getBarClass(resources.cpu.percent)"></div>
      </div>
    </div>
    <!-- Memory -->
    <div class="resource-row">
      <div class="resource-header">
        <HardDrive :size="13" class="resource-icon" />
        <span class="resource-label">{{ t('systemResources.memory') }}</span>
        <span class="resource-value">{{ formatBytes(resources.memory.used) }} / {{ formatBytes(resources.memory.total) }}</span>
      </div>
      <div class="progress-bar">
        <div class="progress-fill" :style="{ width: resources.memory.percent.toFixed(1) + '%' }" :class="getBarClass(resources.memory.percent)"></div>
      </div>
    </div>
    <!-- Disk -->
    <div class="resource-row">
      <div class="resource-header">
        <Database :size="13" class="resource-icon" />
        <span class="resource-label">{{ t('systemResources.disk') }}</span>
        <span class="resource-value">{{ formatBytes(resources.disk.used) }} / {{ formatBytes(resources.disk.total) }}</span>
      </div>
      <div class="progress-bar">
        <div class="progress-fill" :style="{ width: resources.disk.percent.toFixed(1) + '%' }" :class="getBarClass(resources.disk.percent)"></div>
      </div>
    </div>
    <!-- Network Up -->
    <div class="resource-row">
      <div class="resource-header">
        <ArrowUp :size="13" class="resource-icon net-up" />
        <span class="resource-label">{{ t('systemResources.upload') }}</span>
        <span class="resource-value">{{ formatRate(resources.network.upload_rate) }}</span>
      </div>
    </div>
    <!-- Network Down -->
    <div class="resource-row">
      <div class="resource-header">
        <ArrowDown :size="13" class="resource-icon net-down" />
        <span class="resource-label">{{ t('systemResources.download') }}</span>
        <span class="resource-value">{{ formatRate(resources.network.download_rate) }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { Cpu, HardDrive, Database, ArrowUp, ArrowDown } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSystemResources } from '@/composables/useSystemResources'

const { t } = useI18n()
const { resources, startPolling, stopPolling } = useSystemResources()

const cpuPercent = computed(() => {
  const p = resources.value.cpu.percent
  return p < 0 ? '0.0' : p.toFixed(1)
})

const cpuBarWidth = computed(() => {
  const p = resources.value.cpu.percent
  return p < 0 ? 0 : p
})

function getBarClass(percent) {
  if (percent >= 90) return 'bar-critical'
  if (percent >= 70) return 'bar-warning'
  return 'bar-normal'
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return (bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
}

function formatRate(bytesPerSec) {
  if (bytesPerSec <= 0) return '0 B/s'
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  const i = Math.min(Math.floor(Math.log(bytesPerSec) / Math.log(1024)), units.length - 1)
  return (bytesPerSec / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
}

defineExpose({ startPolling, stopPolling })
</script>

<style scoped>
.system-resources-panel {
  padding: 8px 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.resource-row {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.resource-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  line-height: 1.2;
}

.resource-icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.resource-icon.net-up {
  color: var(--color-green, #22c55e);
}

.resource-icon.net-down {
  color: var(--accent-color, #3b82f6);
}

.resource-label {
  color: var(--text-secondary);
  flex-shrink: 0;
  min-width: 32px;
}

.resource-value {
  margin-left: auto;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.progress-bar {
  height: 4px;
  background: var(--bg-tertiary, #e5e7eb);
  border-radius: 2px;
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.3s ease;
}

.bar-normal {
  background: var(--color-green, #22c55e);
}

.bar-warning {
  background: var(--color-yellow, #eab308);
}

.bar-critical {
  background: var(--color-red, #ef4444);
}
</style>
```

**Step 2: Write the test**

```typescript
import { describe, it, expect } from 'vitest'

describe('SystemResourcesPanel', () => {
  it('should be importable', async () => {
    const mod = await import('../../common/SystemResourcesPanel.vue')
    expect(mod.default).toBeDefined()
  })
})
```

**Step 3: Run tests**

Run: `npx vitest run web/src/components/common/__tests__/SystemResourcesPanel.test.ts`
Expected: PASS

**Step 4: Commit**

```bash
git add web/src/components/common/SystemResourcesPanel.vue web/src/components/common/__tests__/SystemResourcesPanel.test.ts
git commit -m "feat: add SystemResourcesPanel component with progress bars"
```

---

### Task 6: Add i18n strings

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

**Step 1: Add systemResources section to en.ts**

After the `appHeader` section, add:

```typescript
systemResources: {
  cpu: 'CPU',
  memory: 'Memory',
  disk: 'Disk',
  upload: 'Upload',
  download: 'Download',
},
```

**Step 2: Add systemResources section to zh.ts**

After the `appHeader` section, add:

```typescript
systemResources: {
  cpu: 'CPU',
  memory: '内存',
  disk: '磁盘',
  upload: '上传',
  download: '下载',
},
```

**Step 3: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat: add i18n strings for system resources"
```

---

### Task 7: Add Gauge icon button and PopupMenu to AppHeader

**Files:**
- Modify: `web/src/components/common/AppHeader.vue`
- Modify: `web/src/components/common/__tests__/AppHeader.test.ts`

This is the main integration task. The Gauge icon is an **independent entry** to the left of the status dot. It has its own PopupMenu containing SystemResourcesPanel. The green-dot popup remains unchanged.

**Step 1: Add imports**

In the `<script setup>` section, add:

```javascript
import { Gauge } from 'lucide-vue-next'
import SystemResourcesPanel from '@/components/common/SystemResourcesPanel.vue'
```

**Step 2: Add template elements**

In the header, between the badge-capsule and the status-dot button, add the Gauge icon button and its PopupMenu:

```html
<!-- System resources monitor -->
<button ref="gaugeBtnRef" class="gauge-toggle" @click="toggleResourcesMenu" :title="t('systemResources.title')">
  <Gauge :size="15" />
</button>

<!-- System resources popup (both Web and APP mode) -->
<PopupMenu v-model:show="resourcesMenuOpen" :target-element="gaugeBtnRef" :max-width="320" :max-height="400" :menu-items-count="5" anchor="right">
  <SystemResourcesPanel ref="resourcesPanelRef" />
</PopupMenu>
```

**Step 3: Add script logic**

```javascript
// System resources menu
const gaugeBtnRef = ref(null)
const resourcesMenuOpen = ref(false)
const resourcesPanelRef = ref(null)

function toggleResourcesMenu() {
  resourcesMenuOpen.value = !resourcesMenuOpen.value
}

watch(resourcesMenuOpen, (open) => {
  if (open) {
    resourcesPanelRef.value?.startPolling?.()
  } else {
    resourcesPanelRef.value?.stopPolling?.()
  }
})
```

**Step 4: Add CSS styles (scoped)**

```css
/* Gauge icon button */
.gauge-toggle {
  padding: 6px;
  border: none;
  background: transparent;
  cursor: pointer;
  border-radius: var(--radius-sm);
  transition: background 0.15s;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-secondary);
}

@media (hover: hover) {
  .gauge-toggle:hover {
    background: var(--bg-tertiary);
  }
}
```

**Step 5: Update existing tests**

Update the AppHeader test to account for the new Gauge button. The existing green-dot tests should remain unchanged.

**Step 6: Run tests**

Run: `npx vitest run web/src/components/common/__tests__/AppHeader.test.ts`
Expected: PASS

**Step 7: Commit**

```bash
git add web/src/components/common/AppHeader.vue web/src/components/common/__tests__/AppHeader.test.ts
git commit -m "feat: add Gauge icon with SystemResourcesPanel popup to header"
```

---

### Task 8: Run full pre-push checks and fix any issues

**Files:**
- Any files that need fixes

**Step 1: Run pre-push checks**

Run: `./scripts/pre-push-checks.sh`

**Step 2: Fix any lint, test, or build issues**

**Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: resolve pre-push check issues"
```
