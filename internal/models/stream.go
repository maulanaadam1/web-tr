package models

type Stream struct {
	Name         string  `json:"name"`         // This will now be the UUID
	DisplayName  string  `json:"display_name"` // The human-readable visual name
	URL          string  `json:"url"`
	DisableAudio bool    `json:"disable_audio"`
	Backend      string  `json:"backend,omitempty"` // "go2rtc" or "mediamtx"
	Recording    bool    `json:"recording,omitempty"`
	Lat          float64 `json:"lat,omitempty"`
	Lng          float64 `json:"lng,omitempty"`
	Enabled      bool    `json:"enabled"`
	UserID       int     `json:"user_id"`
	IsPublic     bool    `json:"is_public"`
	Resolution   string  `json:"resolution,omitempty"`
}

type ApiConfig struct {
	Listen   string `yaml:"listen,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

type Config struct {
	Streams map[string]interface{} `yaml:"streams"`
	Api     ApiConfig              `yaml:"api,omitempty"`
	Rest    map[string]interface{} `yaml:",inline"`
}

type TestLog struct {
	ID        int    `json:"id"`
	URL       string `json:"url"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	CreatedAt string `json:"created_at"`
}
