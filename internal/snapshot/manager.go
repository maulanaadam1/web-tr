package snapshot

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// getEnvDuration reads an env var as minutes, falling back to defaultVal.
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	minutes, err := strconv.Atoi(v)
	if err != nil || minutes <= 0 {
		return defaultVal
	}
	return time.Duration(minutes) * time.Minute
}

// getEnvInt reads an env var as int, falling back to defaultVal.
func getEnvInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultVal
	}
	return n
}

const (
	go2rtcTimeout = 10 * time.Second // Timeout per camera for Go2RTC API
)

// StreamInfo holds name and URL for a stream
type StreamInfo struct {
	Name string
	URL  string
}

type Manager struct {
	Interval      time.Duration
	OutputDir     string
	go2rtcBase    string
	ffmpegPath    string
	batchSize     int
	batchDelay    time.Duration
	ffmpegTimeout time.Duration
	jpegQuality   int    // 1-31, lower = better quality
	resolution    string // e.g. "640x480" or "" for original
	stop          chan struct{}
	wg            sync.WaitGroup
	statusLock    sync.RWMutex
	online        map[string]bool
}

// sanitizeFilename replaces characters that are invalid in Windows filenames
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"(", "_",
		")", "_",
	)
	return replacer.Replace(name)
}

func NewManager(interval time.Duration, outputDir string, go2rtcBase string) *Manager {
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		absDir = outputDir
	}
	if err := os.MkdirAll(absDir, 0755); err != nil {
		log.Printf("[Snapshot] Failed to create output directory: %v", err)
	}

	// Read config from environment variables (configurable via EasyPanel)
	// SNAPSHOT_INTERVAL_MINUTES: How often to refresh snapshots (default: 15 minutes)
	// SNAPSHOT_BATCH_SIZE: How many cameras per batch (default: 5)
	// SNAPSHOT_BATCH_DELAY_SECONDS: Pause between batches in seconds (default: 3)
	// SNAPSHOT_FFMPEG_TIMEOUT_SECONDS: Max time per camera for ffmpeg (default: 60)
	// SNAPSHOT_JPEG_QUALITY: JPEG quality 1-31 lower=better (default: 2)
	// SNAPSHOT_RESOLUTION: Output resolution e.g. 640x480 (default: original)
	batchSz := getEnvInt("SNAPSHOT_BATCH_SIZE", 5)
	batchDly := time.Duration(getEnvInt("SNAPSHOT_BATCH_DELAY_SECONDS", 3)) * time.Second
	ffTimeout := time.Duration(getEnvInt("SNAPSHOT_FFMPEG_TIMEOUT_SECONDS", 60)) * time.Second
	jpegQuality := getEnvInt("SNAPSHOT_JPEG_QUALITY", 2)
	resolution := os.Getenv("SNAPSHOT_RESOLUTION") // e.g. "640x480"

	// If interval was passed as default, allow env override
	envInterval := getEnvDuration("SNAPSHOT_INTERVAL_MINUTES", 0)
	if envInterval > 0 {
		interval = envInterval
	}

	log.Printf("[Snapshot] Output directory: %s", absDir)
	log.Printf("[Snapshot] Interval: %v, Batch size: %d, Batch delay: %v, FFmpeg timeout: %v", interval, batchSz, batchDly, ffTimeout)
	if resolution != "" {
		log.Printf("[Snapshot] Resolution: %s, JPEG quality: %d", resolution, jpegQuality)
	} else {
		log.Printf("[Snapshot] Resolution: original, JPEG quality: %d", jpegQuality)
	}

	// Resolve ffmpeg path
	ffmpegBin := "ffmpeg"
	if runtime.GOOS == "windows" {
		ffmpegBin = "ffmpeg.exe"
	}
	ffmpegPath, err := exec.LookPath(ffmpegBin)
	if err != nil {
		localPath, _ := filepath.Abs(ffmpegBin)
		if _, statErr := os.Stat(localPath); statErr == nil {
			ffmpegPath = localPath
		}
	}
	if ffmpegPath != "" {
		log.Printf("[Snapshot] FFmpeg at: %s", ffmpegPath)
	} else {
		log.Printf("[Snapshot] FFmpeg not available")
	}

	return &Manager{
		Interval:      interval,
		OutputDir:     absDir,
		go2rtcBase:    go2rtcBase,
		ffmpegPath:    ffmpegPath,
		batchSize:     batchSz,
		batchDelay:    batchDly,
		ffmpegTimeout: ffTimeout,
		jpegQuality:   jpegQuality,
		resolution:    resolution,
		stop:          make(chan struct{}),
		online:        make(map[string]bool),
	}
}

func (m *Manager) Start(getStreams func() []StreamInfo) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		// Wait for Go2RTC to initialize
		log.Println("[Snapshot] Waiting 15s for Go2RTC to initialize...")
		time.Sleep(15 * time.Second)

		streams := getStreams()
		failed := m.captureAllBatched(streams)

		// Retry failed ones after a pause
		if len(failed) > 0 {
			log.Printf("[Snapshot] Retrying %d failed streams in 10s...", len(failed))
			time.Sleep(10 * time.Second)
			stillFailed := m.captureAllBatched(failed)
			if len(stillFailed) > 0 {
				log.Printf("[Snapshot] %d streams still unreachable after retry.", len(stillFailed))
			}
		}

		ticker := time.NewTicker(m.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				streams = getStreams()
				m.captureAllBatched(streams)
			case <-m.stop:
				return
			}
		}
	}()
}

func (m *Manager) Stop() {
	close(m.stop)
	m.wg.Wait()
}

// captureAllBatched processes cameras in small batches to avoid overwhelming the NVR
func (m *Manager) captureAllBatched(streams []StreamInfo) []StreamInfo {
	total := len(streams)
	log.Printf("[Snapshot] Starting batched capture for %d streams (batch size: %d)...", total, m.batchSize)

	var allFailed []StreamInfo
	success := 0

	for i := 0; i < total; i += m.batchSize {
		end := i + m.batchSize
		if end > total {
			end = total
		}
		batch := streams[i:end]
		batchNum := (i / m.batchSize) + 1
		totalBatches := (total + m.batchSize - 1) / m.batchSize

		log.Printf("[Snapshot] Batch %d/%d: %d cameras", batchNum, totalBatches, len(batch))

		// Process this batch sequentially (one at a time within the batch)
		for _, s := range batch {
			if m.capture(s) {
				success++
			} else {
				allFailed = append(allFailed, s)
			}
		}

		// Pause between batches to let NVR recover
		if end < total {
			time.Sleep(m.batchDelay)
		}
	}

	log.Printf("[Snapshot] Capture complete: %d/%d succeeded, %d failed.", success, total, len(allFailed))
	return allFailed
}

// GetOnlineCount returns the number of streams that successfully captured a snapshot in the last run
func (m *Manager) GetOnlineCount() int {
	m.statusLock.RLock()
	defer m.statusLock.RUnlock()
	count := 0
	for _, isOnline := range m.online {
		if isOnline {
			count++
		}
	}
	return count
}

// GetStreamStatus returns true if the stream successfully captured a snapshot recently
func (m *Manager) GetStreamStatus(name string) bool {
	m.statusLock.RLock()
	defer m.statusLock.RUnlock()
	return m.online[name]
}

// capture tries Go2RTC API with warmup first, then FFmpeg as fallback
func (m *Manager) capture(stream StreamInfo) bool {
	safeName := sanitizeFilename(stream.Name)
	outPath := filepath.Join(m.OutputDir, fmt.Sprintf("%s.jpg", safeName))
	tmpPath := outPath + ".tmp"

	// Method 1: Go2RTC API with warmup (shares connection with web players)
	m.warmupGo2RTC(stream.Name)
	if m.captureViaGo2RTC(stream.Name, tmpPath) {
		os.Rename(tmpPath, outPath)
		log.Printf("[Snapshot]   ✓ %s (Go2RTC)", stream.Name)

		m.statusLock.Lock()
		m.online[stream.Name] = true
		m.statusLock.Unlock()

		return true
	}

	// Method 2: FFmpeg directly to RTSP as fallback
	if m.ffmpegPath != "" && stream.URL != "" {
		if m.captureViaFFmpeg(stream.Name, stream.URL, tmpPath) {
			os.Rename(tmpPath, outPath)
			log.Printf("[Snapshot]   ✓ %s (FFmpeg)", stream.Name)

			m.statusLock.Lock()
			m.online[stream.Name] = true
			m.statusLock.Unlock()

			return true
		}
	}

	log.Printf("[Snapshot]   ✗ %s (failed)", stream.Name)
	os.Remove(tmpPath)

	m.statusLock.Lock()
	m.online[stream.Name] = false
	m.statusLock.Unlock()

	return false
}

// warmupGo2RTC sends a dummy request to force Go2RTC to establish the RTSP connection
func (m *Manager) warmupGo2RTC(name string) {
	warmupURL := fmt.Sprintf("%s/stream.html?src=%s", m.go2rtcBase, url.QueryEscape(name))
	client := &http.Client{Timeout: 5 * time.Second}
	// We just need to trigger the connection, we don't care about the response body
	resp, err := client.Get(warmupURL)
	if err == nil {
		resp.Body.Close()
	}
	// Give Go2RTC 3 seconds to establish the RTSP connection before we ask for a frame
	time.Sleep(3 * time.Second)
}

func (m *Manager) captureViaGo2RTC(name, tmpPath string) bool {
	snapshotURL := fmt.Sprintf("%s/api/frame.jpeg?src=%s", m.go2rtcBase, url.QueryEscape(name))

	client := &http.Client{Timeout: go2rtcTimeout}
	resp, err := client.Get(snapshotURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	f, err := os.Create(tmpPath)
	if err != nil {
		return false
	}

	written, err := io.Copy(f, resp.Body)
	f.Close()

	if err != nil || written < 100 {
		os.Remove(tmpPath)
		return false
	}

	return true
}

func (m *Manager) captureViaFFmpeg(name, rawURL, tmpPath string) bool {
	cleanUrl := rawURL
	if strings.HasPrefix(cleanUrl, "ffmpeg:") {
		cleanUrl = strings.TrimPrefix(cleanUrl, "ffmpeg:")
		if idx := strings.Index(cleanUrl, "#"); idx != -1 {
			cleanUrl = cleanUrl[:idx]
		}
	}

	args := []string{
		"-y",
		"-rtsp_transport", "tcp",
		"-i", cleanUrl,
		"-vframes", "1",
		"-q:v", strconv.Itoa(m.jpegQuality),
	}

	// Add resolution scaling if configured
	if m.resolution != "" {
		parts := strings.SplitN(m.resolution, "x", 2)
		if len(parts) == 2 {
			args = append(args, "-vf", fmt.Sprintf("scale=%s:%s", parts[0], parts[1]))
		}
	}

	args = append(args, tmpPath)

	cmd := exec.Command(m.ffmpegPath, args...)

	if err := cmd.Start(); err != nil {
		return false
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(m.ffmpegTimeout):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return false
	case err := <-done:
		if err != nil {
			os.Remove(tmpPath)
			return false
		}
		info, statErr := os.Stat(tmpPath)
		if statErr != nil || info.Size() < 100 {
			os.Remove(tmpPath)
			return false
		}
		return true
	}
}

// GetSnapshotPath returns the path to the latest snapshot
func (m *Manager) GetSnapshotPath(name string) string {
	safeName := sanitizeFilename(name)
	path := filepath.Join(m.OutputDir, fmt.Sprintf("%s.jpg", safeName))
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}
