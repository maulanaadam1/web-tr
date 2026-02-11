package stream

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"web-tr/internal/config"
	"web-tr/internal/db"
	"web-tr/internal/models"
)

type Manager struct {
	ConfigManager *config.ConfigManager
	Store         *db.Store
	cmd           *exec.Cmd
}

func NewManager(cfg *config.ConfigManager) *Manager {
	return &Manager{
		ConfigManager: cfg,
	}
}

// EnsureConfig checks if config file exists
func (m *Manager) EnsureConfig() error {
	// Default Go2RTC
	_, err := m.ConfigManager.Load()
	if err != nil {
		return m.ConfigManager.Save(&models.Config{Streams: map[string]interface{}{}})
	}
	return nil
}

func (m *Manager) Start() error {
	// Check binary
	binaryName := "go2rtc"
	configName := m.ConfigManager.FilePath

	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	// Check path
	path, err := exec.LookPath(binaryName)
	if err != nil {
		if _, err := os.Stat("./" + binaryName); err == nil {
			path = "./" + binaryName
		} else {
			return fmt.Errorf("%s binary not found in current directory or PATH", binaryName)
		}
	}

	cmd := exec.Command(path, "-c", configName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", binaryName, err)
	}

	m.cmd = cmd
	return nil
}

func (m *Manager) AddStream(name, url string) error {
	if m.Store != nil {
		// DB mode - simplified for now, assuming URL string
		if err := m.Store.UpdateStream(name, name, url); err != nil {
			return err
		}
		return m.SyncFromDB()
	}

	return m.ConfigManager.AddStream(name, url)
}

func (m *Manager) RemoveStream(name string) error {
	if m.Store != nil {
		if err := m.Store.RemoveStream(name); err != nil {
			return err
		}
		return m.SyncFromDB()
	}

	return m.ConfigManager.RemoveStream(name)
}

func (m *Manager) UpdateStream(oldName, name, url string) error {
	if m.Store != nil {
		if err := m.Store.UpdateStream(oldName, name, url); err != nil {
			return err
		}
		return m.SyncFromDB()
	}

	// Go2RTC File-based update logic
	if oldName != "" && oldName != name {
		if err := m.ConfigManager.RemoveStream(oldName); err != nil {
			return err
		}
		return m.ConfigManager.AddStream(name, url)
	}
	return m.ConfigManager.SetStream(name, url)
}

func (m *Manager) GetStreams() ([]models.Stream, error) {
	if m.Store != nil {
		return m.Store.GetStreams()
	}

	return m.ConfigManager.GetStreams()
}

func (m *Manager) Stop() error {
	if m.cmd != nil && m.cmd.Process != nil {
		return m.cmd.Process.Kill()
	}
	return nil
}

// SyncFromDB reads from DB and overrides the config file
func (m *Manager) SyncFromDB() error {
	streams, err := m.Store.GetStreams()
	if err != nil {
		return err
	}

	// Go2RTC Logic
	cfg, err := m.ConfigManager.Load()
	if err != nil {
		return err
	}

	// Reset streams map
	cfg.Streams = make(map[string]interface{})
	for _, s := range streams {
		cfg.Streams[s.Name] = s.URL
	}

	return m.ConfigManager.Save(cfg)
}

// ProbeStream runs ffprobe to check if the stream is reachable
func (m *Manager) ProbeStream(url string) error {
	// Increased timeout from 5s to 15s for slow/distant streams
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Adjust arguments. -rtsp_transport tcp is usually more reliable.
	args := []string{
		"-v", "error",
		"-show_entries", "stream=codec_type",
		"-rtsp_transport", "tcp",
		"-timeout", "10000000", // 10 second connection timeout (in microseconds)
		"-i", url,
	}

	// Resolve ffprobe path
	binaryName := "ffprobe"
	if runtime.GOOS == "windows" {
		binaryName = "ffprobe.exe"
	}

	path := binaryName
	// Check if it exists in current directory first
	if _, err := os.Stat(binaryName); err == nil {
		if abs, err := filepath.Abs(binaryName); err == nil {
			path = abs
		} else {
			path = "." + string(os.PathSeparator) + binaryName
		}
	} else {
		// If not in current dir, look in PATH
		if p, err := exec.LookPath(binaryName); err == nil {
			path = p
		}
	}

	cmd := exec.CommandContext(ctx, path, args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Provide more helpful error messages
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("connection timeout (15s) - stream might be too slow or unreachable")
		}

		outputStr := string(output)
		if outputStr != "" {
			return fmt.Errorf("stream validation failed: %s", outputStr)
		}

		return fmt.Errorf("cannot connect to stream - check URL, credentials, and network connectivity")
	}

	return nil
}

// DiscoveredStream holds info about a found camera
type DiscoveredStream struct {
	Address string `json:"address"`
	URL     string `json:"url"` // Suggested URL
}

// DiscoverStreams scans the local network for RTSP services (Port 554)
func (m *Manager) DiscoverStreams() ([]DiscoveredStream, error) {
	// 1. Get Local IP and Subnet
	ip, ipNet, err := getLocalIPAndNetwork()
	if err != nil {
		return nil, err
	}

	// 2. Scan Subnet (Simple /24 assumption for now for home networks)
	// We'll scan x.x.x.1 -> x.x.x.254
	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("only IPv4 supported for simple scan")
	}

	baseIP := ipv4.Mask(ipNet.Mask)

	discovered := []DiscoveredStream{} // Init as empty slice to return [] instead of null
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrency
	sem := make(chan struct{}, 50)

	for i := 1; i < 255; i++ {
		// Construct target IP
		// This is a naive way to iterate /24.
		// If mask is not /24, this might be slightly off but works for 99% of home cases.
		targetIP := net.IPv4(baseIP[0], baseIP[1], baseIP[2], byte(i))
		if targetIP.Equal(ip) {
			continue // skip self if needed, though self might run rtsp server
		}

		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			conn, err := net.DialTimeout("tcp", target+":554", 200*time.Millisecond)
			if err == nil {
				conn.Close()
				mu.Lock()
				discovered = append(discovered, DiscoveredStream{
					Address: target,
					URL:     fmt.Sprintf("rtsp://%s:554/stream", target), // Guess default
				})
				mu.Unlock()
			}
		}(targetIP.String())
	}

	wg.Wait()
	return discovered, nil
}

func getLocalIPAndNetwork() (net.IP, *net.IPNet, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, nil, err
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP, ipnet, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("no valid local IP found")
}
