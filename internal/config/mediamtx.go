package config

import (
	"fmt"
	"os"
	"web-tr/internal/models"

	"gopkg.in/yaml.v3"
)

// MediaMTXConfig represents the structure of mediamtx.yml
type MediaMTXConfig struct {
	LogLevel           string                  `yaml:"logLevel"`
	LogDestinations    []string                `yaml:"logDestinations"`
	API                bool                    `yaml:"api"`
	APIAddress         string                  `yaml:"apiAddress"`
	HLS                bool                    `yaml:"hls"`
	HLSAddress         string                  `yaml:"hlsAddress"`
	HLSVariant         string                  `yaml:"hlsVariant"`
	HLSSegmentCount    int                     `yaml:"hlsSegmentCount"`
	HLSSegmentDuration string                  `yaml:"hlsSegmentDuration"`
	HLSPartDuration    string                  `yaml:"hlsPartDuration"`
	HLSSegmentMaxSize  string                  `yaml:"hlsSegmentMaxSize"`
	WebRTC             bool                    `yaml:"webrtc"`
	WebRTCAddress      string                  `yaml:"webrtcAddress"`
	WebRTCICEServers   []map[string]string     `yaml:"webrtcICEServers"`
	RTSP               bool                    `yaml:"rtsp"`
	RTSPAddress        string                  `yaml:"rtspAddress"`
	Protocols          []string                `yaml:"protocols"`
	Paths              map[string]MediaMTXPath `yaml:"paths"`
}

type MediaMTXPath struct {
	Source string `yaml:"source,omitempty"`
}

// GenerateMediaMTXConfig creates or updates mediamtx.yml from stream list
func GenerateMediaMTXConfig(streams []models.Stream, filepath string) error {
	// Filter only MediaMTX streams
	mtxStreams := make(map[string]MediaMTXPath)
	for _, s := range streams {
		if s.Backend == "mediamtx" {
			mtxStreams[s.Name] = MediaMTXPath{
				Source: s.URL,
			}
		}
	}

	// Add default "all" path if no streams
	if len(mtxStreams) == 0 {
		mtxStreams["all"] = MediaMTXPath{}
	}

	config := MediaMTXConfig{
		LogLevel:           "info",
		LogDestinations:    []string{"stdout"},
		API:                true,
		APIAddress:         ":9997",
		HLS:                true,
		HLSAddress:         ":8888",
		HLSVariant:         "mpegts",
		HLSSegmentCount:    3,
		HLSSegmentDuration: "1s",
		HLSPartDuration:    "200ms",
		HLSSegmentMaxSize:  "50M",
		WebRTC:             true,
		WebRTCAddress:      ":8889",
		WebRTCICEServers: []map[string]string{
			{"url": "stun:stun.l.google.com:19302"},
		},
		RTSP:        true,
		RTSPAddress: ":8555",
		Protocols:   []string{"tcp"},
		Paths:       mtxStreams,
	}

	data, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal mediamtx config: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write mediamtx.yml: %w", err)
	}

	return nil
}
