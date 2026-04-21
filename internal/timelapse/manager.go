package timelapse

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TimelapseSettings defines configuration for a single camera
type TimelapseSettings struct {
	Enabled  bool  `json:"enabled"`
	Interval int   `json:"interval"` // Seconds
	Width    int   `json:"width"`
	Height   int   `json:"height"`
	LastRun  int64 `json:"last_run"` // Unix timestamp
}

type Manager struct {
	ConfigPath string
	Settings   map[string]*TimelapseSettings
	mu         sync.Mutex
	stopChan   chan struct{}
	running    bool
}

func NewManager(configPath string) *Manager {
	return &Manager{
		ConfigPath: configPath,
		Settings:   make(map[string]*TimelapseSettings),
		stopChan:   make(chan struct{}),
	}
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.ConfigPath)
	if os.IsNotExist(err) {
		return nil // New file
	}
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &m.Settings)
}

func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.MarshalIndent(m.Settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.ConfigPath, data, 0644)
}

func (m *Manager) UpdateSettings(streamName string, settings TimelapseSettings) error {
	m.mu.Lock()
	// Ensure map exists (should already be there from NewManager/Load)
	if m.Settings == nil {
		m.Settings = make(map[string]*TimelapseSettings)
	}
	m.Settings[streamName] = &settings
	m.mu.Unlock()

	return m.Save()
}

func (m *Manager) GetSettings(streamName string) *TimelapseSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.Settings[streamName]; ok {
		// Return copy to avoid race
		val := *s
		return &val
	}
	return nil
}

func (m *Manager) Start(getStreamURL func(name string) string) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopChan = make(chan struct{})
	m.mu.Unlock()

	go m.loop(getStreamURL)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	close(m.stopChan)
	m.running = false
	m.mu.Unlock()
}

func (m *Manager) loop(getStreamURL func(name string) string) {
	ticker := time.NewTicker(10 * time.Second) // Check every 10s
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.processStreams(getStreamURL)
		}
	}
}

func (m *Manager) processStreams(getStreamURL func(name string) string) {
	m.mu.Lock()
	// Clone settings to avoid holding lock during capture
	streams := make(map[string]TimelapseSettings)
	for k, v := range m.Settings {
		if v.Enabled {
			streams[k] = *v
		}
	}
	m.mu.Unlock()

	for name, settings := range streams {
		now := time.Now().Unix()
		// Determine if it's time to capture
		if now-settings.LastRun >= int64(settings.Interval) {
			url := getStreamURL(name)
			if url != "" {
				go m.capture(name, url, settings)
			}
		}
	}
}

func (m *Manager) capture(name, url string, settings TimelapseSettings) {
	// 1. Create directory
	dir := filepath.Join("data", "timelapse", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Timelapse error creating dir for %s: %v", name, err)
		return
	}

	// 2. Generate filename (YYYY-MM-DD_HH-mm-ss.jpg)
	filename := time.Now().Format("2006-01-02_15-04-05") + ".jpg"
	path := filepath.Join(dir, filename)

	// 3. FFmpeg Capture
	// ffmpeg -y -rtsp_transport tcp -i <url> -vframes 1 -s ensure_size -f image2 <path>

	// Strip ffmpeg: prefix if present
	cleanUrl := url
	if len(cleanUrl) > 7 && cleanUrl[:7] == "ffmpeg:" {
		cleanUrl = cleanUrl[7:]
		// Simple strip. If complex args, might need more parsing, but usually direct RTSP/HTTP
	}

	args := []string{
		"-y",
		"-rtsp_transport", "tcp",
		"-i", cleanUrl,
		"-vframes", "1",
		"-f", "image2",
	}

	// Resolution override
	if settings.Width > 0 && settings.Height > 0 {
		args = append(args, "-s", fmt.Sprintf("%dx%d", settings.Width, settings.Height))
	}

	args = append(args, path)

	// Determine ffmpeg path
	ffmpegPath := "ffmpeg"
	if _, err := os.Stat("ffmpeg.exe"); err == nil {
		ffmpegPath = ".\\ffmpeg.exe"
	}

	cmd := exec.Command(ffmpegPath, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Timelapse capture failed for %s: %v. Output: %s", name, err, string(output))
		return
	}

	log.Printf("Timelapse captured for %s: %s", name, filename)

	// 4. Update LastRun
	m.mu.Lock()
	if s, ok := m.Settings[name]; ok {
		s.LastRun = time.Now().Unix()
	}
	m.mu.Unlock()

	// Save LastRun state occasionally?
	// For now, we save on update. If app restarts, it might recapture immediately, which is fine.
	// Saving on every capture might be too much I/O.
	// Make a separate save method or let it be transient until next config change?
	// Let's verify requirement. "Safe" approach: save periodically or on stop.
	// For robusteness, we will save state here to disk so restart remembers last run.
	m.Save()
}

// GetFiles returns list of captured files for frontend
func (m *Manager) GetFiles(name string) ([]string, error) {
	dir := filepath.Join("data", "timelapse", name)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}

	files := []string{}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".jpg" {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// DeleteFile removes a specific snapshot
func (m *Manager) DeleteFile(name string, filename string) error {
	// Sanitize filename to prevent directory traversal
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return fmt.Errorf("invalid filename")
	}
	path := filepath.Join("data", "timelapse", name, filename)
	return os.Remove(path)
}

// DeleteAll removes all snapshots for a stream
func (m *Manager) DeleteAll(name string) error {
	dir := filepath.Join("data", "timelapse", name)
	// Remove all .jpg files inside
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".jpg" {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

// ExportVideo generates MP4 from images within date range
func (m *Manager) ExportVideo(name string, startStr, endStr string) (string, error) {
	// Parse range (inclusive)
	// Input format: YYYY-MM-DDTHH:mm:ss
	// Files: YYYY-MM-DD_HH-mm-ss.jpg
	// We need to normalize formats for string comparison.

	// Convert input to file-like format: YYYY-MM-DD_HH-mm-ss
	startCmp := strings.ReplaceAll(startStr, "T", "_")
	startCmp = strings.ReplaceAll(startCmp, ":", "-")

	endCmp := strings.ReplaceAll(endStr, "T", "_")
	endCmp = strings.ReplaceAll(endCmp, ":", "-")

	files, err := m.GetFiles(name)
	if err != nil {
		return "", err
	}
	sort.Strings(files)

	// Filter
	var fileList []string
	for _, f := range files {
		// filename: YYYY-MM-DD_HH-mm-ss.jpg
		// Compare purely by string prefix (lexicographical works well for ISO-like dates)
		// file has .jpg, strip it
		namePart := strings.TrimSuffix(f, ".jpg")

		if namePart >= startCmp && namePart <= endCmp {
			fileList = append(fileList, f)
		}
	}

	if len(fileList) == 0 {
		return "", fmt.Errorf("no images found in range")
	}

	// Create list file for ffmpeg concat
	listFileName := filepath.Join("data", "timelapse", name, "ffmpeg_list.txt")
	f, err := os.Create(listFileName)
	if err != nil {
		return "", err
	}

	// Write relative paths
	for _, img := range fileList {
		// "file 'filename.jpg'"
		// duration 0.1 for 10fps
		f.WriteString(fmt.Sprintf("file '%s'\n", img))
		f.WriteString("duration 0.1\n")
	}
	// Repeat last frame to prevent cut off
	if len(fileList) > 0 {
		f.WriteString(fmt.Sprintf("file '%s'\n", fileList[len(fileList)-1]))
	}

	f.Close()
	// Cleanup list file later is handled by defer, but we need the path relative to cmd.Dir 
	// Or just use absolute path for listFileName
	absListPath, _ := filepath.Abs(listFileName)
	
	// Output file - Use sanitized time strings for filename (No colons for Windows)
	outName := fmt.Sprintf("%s_export_%s_%s.mp4", name, startCmp, endCmp)
	outDir := filepath.Join("data", "timelapse", name)
	outPath := filepath.Join(outDir, outName)
	absOutPath, _ := filepath.Abs(outPath)

	// FFmpeg command
	// ffmpeg -f concat -safe 0 -i list.txt -vsync vfr -pix_fmt yuv420p out.mp4

	// Sniff ffmpeg path
	ffmpegPath := "ffmpeg"
	if _, err := os.Stat("ffmpeg.exe"); err == nil {
		ffmpegPath, _ = filepath.Abs("ffmpeg.exe")
	}

	cmd := exec.Command(ffmpegPath,
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", absListPath,
		"-vsync", "vfr",
		"-pix_fmt", "yuv420p",
		absOutPath,
	)

	// Set working directory to where images are
	cmd.Dir = outDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Clean up the list file on error too
		os.Remove(listFileName)
		return "", fmt.Errorf("ffmpeg error: %v, output: %s", err, string(output))
	}

	// Remove list file on success
	os.Remove(listFileName)

	return outPath, nil
}
