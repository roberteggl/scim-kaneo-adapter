// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/roberteggl/scim-kaneo-adapter/internal/assignments"
)

// Config holds process configuration.
type Config struct {
	ListenAddr string

	SCIMToken string

	KaneoURL    string
	KaneoAPIKey string

	AssignmentsFile string
	Assignments     *assignments.Config

	// StoreFile, if set, persists SCIM Users/Groups JSON across restarts.
	StoreFile string
}

// FromEnv builds Config from environment variables.
func FromEnv() (*Config, error) {
	c := &Config{
		ListenAddr:      envOr("LISTEN_ADDR", ":8080"),
		SCIMToken:       os.Getenv("SCIM_TOKEN"),
		KaneoURL:        strings.TrimRight(os.Getenv("KANEO_URL"), "/"),
		KaneoAPIKey:     os.Getenv("KANEO_API_KEY"),
		AssignmentsFile: os.Getenv("ASSIGNMENTS_FILE"),
		StoreFile:       os.Getenv("STORE_FILE"),
	}
	if c.SCIMToken == "" {
		return nil, fmt.Errorf("SCIM_TOKEN is required")
	}
	if c.KaneoURL == "" {
		return nil, fmt.Errorf("KANEO_URL is required")
	}
	if c.KaneoAPIKey == "" {
		return nil, fmt.Errorf("KANEO_API_KEY is required")
	}
	if c.AssignmentsFile == "" {
		return nil, fmt.Errorf("ASSIGNMENTS_FILE is required")
	}
	asg, err := assignments.Load(c.AssignmentsFile)
	if err != nil {
		return nil, err
	}
	c.Assignments = asg
	return c, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
