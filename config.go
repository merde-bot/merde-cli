// Copyright 2025 Bold Software, Inc. (https://merde.ai/)
// Released under the PolyForm Noncommercial License 1.0.0.
// Please see the README for details.

package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"merde.ai/git"
)

// TODO: maybe use more of the ff package to do this stuff?

const (
	tokenKey      = "token"
	serverRootKey = "server"
	gitExeKey     = "git"
)

var defaultValues = map[string]string{
	serverRootKey: "https://merde.ai",
}

type Config struct {
	// Stored values
	Values map[string]string `json:"values"`

	// Runtime-populated values
	Git        *git.Git `json:"-"`
	GitVersion string   `json:"-"`
	path       string
}

func LoadDefault(ctx context.Context) (*Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return Load(ctx, path)
}

func Load(ctx context.Context, path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{Values: make(map[string]string), path: path}, nil
	}
	if err != nil {
		return nil, err
	}
	var values map[string]string
	err = json.Unmarshal(data, &values)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Values: values,
		path:   path,
	}
	gg, err := git.NewGit(ctx, cfg.Get(gitExeKey))
	if err != nil {
		return nil, err
	}
	cfg.Git = gg
	cfg.GitVersion, _ = gg.Version(ctx) // best effort
	return cfg, nil
}

func DefaultPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	merdeName := "merde"
	// Keep dev configs separate from release configs.
	if version == "dev" {
		merdeName = "merde-dev"
	}
	return filepath.Join(configDir, merdeName, "config.json"), nil
}

func (c *Config) Update(pairs ...string) error {
	if len(pairs)%2 != 0 {
		return fmt.Errorf("Config.Update requires key-value pairs, got %d strings", len(pairs))
	}
	for i := 0; i < len(pairs); i += 2 {
		key, value := pairs[i], pairs[i+1]
		c.Values[key] = value
	}
	err := os.MkdirAll(filepath.Dir(c.path), 0o700)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.Values, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(c.path, data, 0o600)
	if err != nil {
		return err
	}
	return nil
}

// Get reads the value for key from c.Values, or from an environment variable override.
func (c *Config) Get(key string) string {
	return cmp.Or(os.Getenv("MERDE_"+strings.ToUpper(key)), c.Values[key], defaultValues[key])
}
