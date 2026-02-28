package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Settings struct {
	SpinnerTexts []string `json:"spinner_texts"`
}

var defaultSettings = Settings{
	SpinnerTexts: []string{
		"Thinking deeply...",
		"Exploring possibilities...",
		"Connecting the dots...",
		"Almost there...",
		"Analyzing patterns...",
		"Gathering insights...",
		"Processing information...",
		"Synthesizing results...",
		"Working on it...",
		"Crunching data...",
	},
}

func LoadSettings() *Settings {
	s := defaultSettings
	dir := ShannonDir()
	if dir == "" {
		return &s
	}
	path := filepath.Join(dir, "settings.json")

	data, err := os.ReadFile(path)
	if err != nil {
		return &s
	}

	// Unmarshal on top of defaults — only overrides fields present in JSON
	if err := json.Unmarshal(data, &s); err != nil {
		return &defaultSettings
	}

	// Ensure non-empty spinner texts
	if len(s.SpinnerTexts) == 0 {
		s.SpinnerTexts = defaultSettings.SpinnerTexts
	}
	return &s
}

func SaveSettings(s *Settings) error {
	dir := ShannonDir()
	if dir == "" {
		return fmt.Errorf("failed to resolve home directory for settings")
	}
	path := filepath.Join(dir, "settings.json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// InitSettingsFile creates settings.json with defaults if it doesn't exist.
func InitSettingsFile(dir string) error {
	if dir == "" {
		return fmt.Errorf("failed to resolve home directory for settings")
	}
	path := filepath.Join(dir, "settings.json")
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	return SaveSettings(&defaultSettings)
}
