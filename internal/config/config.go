package config

import (
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"web-tr/internal/models"

	"gopkg.in/yaml.v3"
)

type ConfigManager struct {
	FilePath string
	mu       sync.RWMutex
}

func NewConfigManager(filePath string) *ConfigManager {
	return &ConfigManager{
		FilePath: filePath,
	}
}

func (cm *ConfigManager) Load() (*models.Config, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	data, err := os.ReadFile(cm.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &models.Config{Streams: make(map[string]interface{})}, nil
		}
		return nil, err
	}

	var cfg models.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Streams == nil {
		cfg.Streams = make(map[string]interface{})
	}
	if cfg.Rest == nil {
		cfg.Rest = make(map[string]interface{})
	}
	return &cfg, nil
}

func (cm *ConfigManager) Save(cfg *models.Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(cm.FilePath, data, 0644)
}

func (cm *ConfigManager) saveStreamConfig(cfg *models.Config, name, url string) error {
	cfg.Streams[name] = url
	return cm.Save(cfg)
}

// Deprecated: But keeping signature for now as it matches new logic
func (cm *ConfigManager) AddStream(name, url string) error {
	cfg, err := cm.Load()
	if err != nil {
		return err
	}
	if _, exists := cfg.Streams[name]; exists {
		return fmt.Errorf("stream '%s' already exists", name)
	}
	return cm.saveStreamConfig(cfg, name, url)
}

func (cm *ConfigManager) SetStream(name, url string) error {
	cfg, err := cm.Load()
	if err != nil {
		return err
	}
	return cm.saveStreamConfig(cfg, name, url)
}

func (cm *ConfigManager) RemoveStream(name string) error {
	cfg, err := cm.Load()
	if err != nil {
		return err
	}

	delete(cfg.Streams, name)
	return cm.Save(cfg)
}

func (cm *ConfigManager) GetStreams() ([]models.Stream, error) {
	cfg, err := cm.Load()
	if err != nil {
		return nil, err
	}

	var streams []models.Stream
	for name, val := range cfg.Streams {
		var urlStr string
		switch v := val.(type) {
		case string:
			urlStr = v
		case []interface{}:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					urlStr = s
				}
			}
		case []string:
			if len(v) > 0 {
				urlStr = v[0]
			}
		default:
			log.Printf("Stream '%s' has unexpected type: %T value: %v", name, val, val)
			urlStr = "complex source"
		}

		streams = append(streams, models.Stream{
			Name: name,
			URL:  urlStr,
		})
	}

	// Sort by name
	sort.Slice(streams, func(i, j int) bool {
		return streams[i].Name < streams[j].Name
	})

	return streams, nil
}
