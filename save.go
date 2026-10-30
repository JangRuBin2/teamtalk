package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	APIKey string `json:"api_key"`
}

func dataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".teamtalk")
}

func readJSON(name string, v any) error {
	b, err := os.ReadFile(filepath.Join(dataDir(), name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(name string, v any) error {
	if err := os.MkdirAll(dataDir(), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir(), name), b, 0o600)
}

func removeSave() { os.Remove(filepath.Join(dataDir(), "save.json")) }
