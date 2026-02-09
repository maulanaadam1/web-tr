package models

type Stream struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Recording bool   `json:"recording,omitempty"`
}

type Config struct {
	Streams map[string]interface{} `yaml:"streams"`
	Rest    map[string]interface{} `yaml:",inline"`
}
