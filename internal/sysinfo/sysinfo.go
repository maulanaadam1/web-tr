package sysinfo

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

var startTime = time.Now()

type SysStats struct {
	Uptime       string  `json:"uptime"`
	MemTotalMB   float64 `json:"memTotalMB"`
	MemUsedMB    float64 `json:"memUsedMB"`
	MemUsagePct  float64 `json:"memUsagePct"`
	CpuUsagePct  float64 `json:"cpuUsagePct"`
	AppUptime    string  `json:"appUptime"`
	StreamCount     int     `json:"streamCount"`
	ActiveStreams   int     `json:"activeStreams"`
	DisabledStreams int     `json:"disabledStreams"`
}

func GetStats() SysStats {
	stats := SysStats{
		AppUptime: time.Since(startTime).Round(time.Second).String(),
	}

	// 1. Host Uptime
	hInfo, err := host.Info()
	if err == nil {
		d := time.Duration(hInfo.Uptime) * time.Second
		days := int(d / (24 * time.Hour))
		d -= time.Duration(days) * 24 * time.Hour
		hours := int(d / time.Hour)
		d -= time.Duration(hours) * time.Hour
		mins := int(d / time.Minute)
		stats.Uptime = fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	} else {
		stats.Uptime = "Unavailable"
	}

	// 2. Memory Info
	vMem, err := mem.VirtualMemory()
	if err == nil {
		stats.MemTotalMB = float64(vMem.Total) / 1024 / 1024
		stats.MemUsedMB = float64(vMem.Used) / 1024 / 1024
		stats.MemUsagePct = vMem.UsedPercent
	}

	// 3. CPU Info (1-second average blocking call, but we can do a non-blocking sample using a quick interval)
	// Using 0 means it measures since last call, or total. We'll sample over 100ms so it doesn't hang the API too much.
	cpuPct, err := cpu.Percent(100*time.Millisecond, false)
	if err == nil && len(cpuPct) > 0 {
		stats.CpuUsagePct = cpuPct[0]
	}

	return stats
}
