package models

type Stream struct {
	Name      string  `json:"name"`
	URL       string  `json:"url"`
	Backend   string  `json:"backend,omitempty"` // "go2rtc" or "mediamtx"
	Recording bool    `json:"recording,omitempty"`
	Lat       float64 `json:"lat,omitempty"`
	Lng       float64 `json:"lng,omitempty"`
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
