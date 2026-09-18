package utils

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	userConfigFileName      = "config.yml"
	UserConfigSchemaVersion = 1
)

type SuggestConfig struct {
	AutoFileDrafts bool `yaml:"autoFileDrafts"`
	MaxPerWeek     int  `yaml:"maxPerWeek"`
}

type UserConfig struct {
	Version       int           `yaml:"version"`
	Notifications bool          `yaml:"notifications"`
	Suggest       SuggestConfig `yaml:"suggest"`
}

func GetUserConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".corgi"), nil
}

func LoadUserConfig() (*UserConfig, error) {
	dir, err := GetUserConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, userConfigFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &UserConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read user config: %w", err)
	}

	var cfg UserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse user config: %w", err)
	}
	migrateUserConfig(&cfg)
	return &cfg, nil
}

func migrateUserConfig(cfg *UserConfig) {
	if cfg.Version == 0 {
		cfg.Version = UserConfigSchemaVersion
	}
}

func SaveUserConfig(cfg *UserConfig) error {
	dir, err := GetUserConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	cfg.Version = UserConfigSchemaVersion
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal user config: %w", err)
	}

	path := filepath.Join(dir, userConfigFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write user config: %w", err)
	}
	return nil
}
