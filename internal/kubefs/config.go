package kubefs

import (
	"bytes"
	"os"

	"sigs.k8s.io/yaml"
)

type Config struct {
	ShowManagedFields bool `yaml:"showManagedFields"`
}

func DefaultConfig() Config {
	return Config{
		ShowManagedFields: false,
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}
