package system

import (
	"fmt"
	"sync"
	"time"

	"clawbench/internal/model"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	psutilNet "github.com/shirou/gopsutil/v3/net"
)

// ResourceResponse is the JSON structure returned by GET /api/system/resources.
type ResourceResponse struct {
	CPU     CPUInfo     `json:"cpu"`
	Memory  MemoryInfo  `json:"memory"`
	Disk    DiskInfo    `json:"disk"`
	DiskIO  DiskIOInfo  `json:"disk_io"`
	Network NetworkInfo `json:"network"`
	Load    LoadInfo    `json:"load"`
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
	Used    uint64  `json:"used"`    // bytes
	Total   uint64  `json:"total"`   // bytes
	Percent float64 `json:"percent"` // 0-100
}

// NetworkInfo holds network throughput rates.
type NetworkInfo struct {
	UploadRate   float64 `json:"upload_rate"`   // bytes/sec
	DownloadRate float64 `json:"download_rate"` // bytes/sec
}

// DiskIOInfo holds disk I/O throughput rates.
type DiskIOInfo struct {
	ReadRate  float64 `json:"read_rate"`  // bytes/sec
	WriteRate float64 `json:"write_rate"` // bytes/sec
}

// LoadInfo holds system load averages.
type LoadInfo struct {
	Load1  float64 `json:"load1"`  // 1-minute load average
	Load5  float64 `json:"load5"`  // 5-minute load average
	Load15 float64 `json:"load15"` // 15-minute load average
}

// sampler holds state for interval-based calculations.
type sampler struct {
	mu sync.Mutex

	// CPU sampling — store scalar idle/total instead of full TimesStat
	cpuPrevIdle  float64
	cpuPrevTotal float64
	cpuTime      time.Time
	cpuInited    bool

	// Network sampling — store summed scalars (not per-interface map)
	// to avoid interface disappearing/reappearing edge cases
	prevBytesSent uint64
	prevBytesRecv uint64
	netTime       time.Time
	netInited     bool

	// Disk I/O sampling — same scalar prev pattern as network
	prevDiskReadBytes  uint64
	prevDiskWriteBytes uint64
	diskIOTime         time.Time
	diskIOInited       bool

	// Response cache — prevents concurrent requests from getting
	// near-zero CPU/network values due to artificially short intervals
	cachedResp *ResourceResponse
	cachedAt   time.Time

	// forceErr injects errors into all sample methods (for testing).
	// When non-empty, each sample method returns an error with this message.
	forceErr string
}

var globalSampler = &sampler{}

const cacheTTL = 500 * time.Millisecond

// ResetSampler resets the global sampler state (for testing).
func ResetSampler() {
	globalSampler = &sampler{}
}

// SetForceError injects errors into all sample methods (for testing).
func SetForceError(err string) {
	globalSampler.forceErr = err
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
// Sampling is done outside the lock to avoid blocking concurrent readers.
func GetResources() (*ResourceResponse, error) {
	s := globalSampler

	// Check cache under lock
	s.mu.Lock()
	if s.cachedResp != nil && time.Since(s.cachedAt) < cacheTTL {
		cached := s.cachedResp
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()

	// Sample outside the lock — these read /proc and may take ~100ms
	var errs []string
	resp := &ResourceResponse{}

	if err := s.sampleCPU(resp); err != nil {
		resp.CPU = CPUInfo{Percent: -1}
		errs = append(errs, fmt.Sprintf("cpu: %v", err))
	}

	if err := s.sampleMemory(resp); err != nil {
		resp.Memory = MemoryInfo{}
		errs = append(errs, fmt.Sprintf("memory: %v", err))
	}

	if err := s.sampleDisk(resp); err != nil {
		resp.Disk = DiskInfo{}
		errs = append(errs, fmt.Sprintf("disk: %v", err))
	}

	if err := s.sampleNetwork(resp); err != nil {
		resp.Network = NetworkInfo{}
		errs = append(errs, fmt.Sprintf("network: %v", err))
	}

	if err := s.sampleDiskIO(resp); err != nil {
		resp.DiskIO = DiskIOInfo{}
		errs = append(errs, fmt.Sprintf("disk_io: %v", err))
	}

	if err := s.sampleLoad(resp); err != nil {
		resp.Load = LoadInfo{}
		errs = append(errs, fmt.Sprintf("load: %v", err))
	}

	resp.Errors = errs
	if len(errs) == 0 {
		resp.Errors = nil // omit empty array from JSON
	}

	// Update cache under lock (may race with another goroutine that also
	// sampled — last writer wins, which is fine for metrics data)
	s.mu.Lock()
	s.cachedResp = resp
	s.cachedAt = time.Now()
	s.mu.Unlock()

	return resp, nil
}

func (s *sampler) sampleCPU(resp *ResourceResponse) error {
	if s.forceErr != "" {
		return fmt.Errorf("%s", s.forceErr)
	}
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
	curTotal := cpuTimesTotal(cur)

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

// cpuTimesTotal returns the total CPU time (sum of all fields).
// Replaces the deprecated cpu.TimesStat.Total() method.
func cpuTimesTotal(t cpu.TimesStat) float64 {
	return t.User + t.System + t.Idle + t.Nice + t.Iowait +
		t.Irq + t.Softirq + t.Steal + t.Guest + t.GuestNice
}

func (s *sampler) sampleMemory(resp *ResourceResponse) error {
	if s.forceErr != "" {
		return fmt.Errorf("%s", s.forceErr)
	}
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
	if s.forceErr != "" {
		return fmt.Errorf("%s", s.forceErr)
	}
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
	if s.forceErr != "" {
		return fmt.Errorf("%s", s.forceErr)
	}
	counters, err := psutilNet.IOCounters(true) // true = per-interface
	if err != nil {
		return fmt.Errorf("net io counters: %w", err)
	}

	now := time.Now()

	// Sum all non-loopback interfaces (lo on Linux, lo0 on macOS)
	var totalBytesSent, totalBytesRecv uint64
	for _, c := range counters {
		if c.Name == "lo" || c.Name == "lo0" {
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

func (s *sampler) sampleDiskIO(resp *ResourceResponse) error {
	if s.forceErr != "" {
		return fmt.Errorf("%s", s.forceErr)
	}
	counters, err := disk.IOCounters() // all devices
	if err != nil {
		return fmt.Errorf("disk io counters: %w", err)
	}

	now := time.Now()

	var totalReadBytes, totalWriteBytes uint64
	for _, c := range counters {
		totalReadBytes += c.ReadBytes
		totalWriteBytes += c.WriteBytes
	}

	if !s.diskIOInited {
		s.prevDiskReadBytes = totalReadBytes
		s.prevDiskWriteBytes = totalWriteBytes
		s.diskIOTime = now
		s.diskIOInited = true
		resp.DiskIO = DiskIOInfo{}
		return nil
	}

	elapsed := now.Sub(s.diskIOTime).Seconds()
	if elapsed > 0 {
		readDiff := int64(totalReadBytes - s.prevDiskReadBytes)
		writeDiff := int64(totalWriteBytes - s.prevDiskWriteBytes)
		if readDiff < 0 {
			readDiff = 0
		}
		if writeDiff < 0 {
			writeDiff = 0
		}
		resp.DiskIO.ReadRate = float64(readDiff) / elapsed
		resp.DiskIO.WriteRate = float64(writeDiff) / elapsed
	}

	s.prevDiskReadBytes = totalReadBytes
	s.prevDiskWriteBytes = totalWriteBytes
	s.diskIOTime = now
	return nil
}

func (s *sampler) sampleLoad(resp *ResourceResponse) error {
	if s.forceErr != "" {
		return fmt.Errorf("%s", s.forceErr)
	}
	avg, err := load.Avg()
	if err != nil {
		return fmt.Errorf("load avg: %w", err)
	}
	resp.Load.Load1 = avg.Load1
	resp.Load.Load5 = avg.Load5
	resp.Load.Load15 = avg.Load15
	return nil
}
