package config

import (
	"encoding/json"
	"os"
)

type AppSettings struct {
	StreamEngine string `json:"stream_engine"` // "go2rtc" or "mediamtx"
}

const SettingsFile = "app-settings.json"

func LoadAppSettings() (*AppSettings, error) {
	data, err := os.ReadFile(SettingsFile)
	if os.IsNotExist(err) {
		// Default
		return &AppSettings{StreamEngine: "go2rtc"}, nil
	}
	if err != nil {
		return nil, err
	}

	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	if settings.StreamEngine == "" {
		settings.StreamEngine = "go2rtc"
	}

	return &settings, nil
}

func SaveAppSettings(settings *AppSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(SettingsFile, data, 0644)
}
