// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT

// Package config loads and validates runner-entrypoint configuration from the
// process environment (the myoung34/github-runner env contract).
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DefaultAPIBase is the public GitHub API; override with GITHUB_API_URL for GHES.
const DefaultAPIBase = "https://api.github.com"

const (
	defaultRunnerDir  = "/actions-runner"
	defaultWorkFolder = "_work"
	defaultGroupID    = 1 // the "Default" org runner group
)

// Config is the resolved runner-entrypoint configuration.
type Config struct {
	APIBase    string
	Org        string
	Token      string // ACCESS_TOKEN — registers the runner (needs org runner-admin)
	GitPAT     string // GITHUB_PAT — git/private-module auth for workflow steps
	Name       string
	Labels     []string
	GroupID    int
	WorkFolder string
	RunnerDir  string
}

// Load reads and validates configuration from environment variables.
func Load() (Config, error) {
	cfg := Config{
		APIBase:    getenv("GITHUB_API_URL", DefaultAPIBase),
		Org:        os.Getenv("ORG_NAME"),
		Token:      os.Getenv("ACCESS_TOKEN"),
		GitPAT:     os.Getenv("GITHUB_PAT"),
		Name:       os.Getenv("RUNNER_NAME"),
		WorkFolder: getenv("RUNNER_WORKDIR", defaultWorkFolder),
		RunnerDir:  getenv("RUNNER_DIR", defaultRunnerDir),
		GroupID:    defaultGroupID,
	}

	if scope := os.Getenv("RUNNER_SCOPE"); scope != "" && scope != "org" {
		return cfg, fmt.Errorf("RUNNER_SCOPE=%q unsupported (org-scoped only)", scope)
	}
	switch {
	case cfg.Org == "":
		return cfg, errors.New("ORG_NAME is required")
	case cfg.Token == "":
		return cfg, errors.New("ACCESS_TOKEN is required")
	case cfg.Name == "":
		return cfg, errors.New("RUNNER_NAME is required")
	}

	// Custom labels; GitHub auto-adds the default self-hosted/OS/arch labels.
	// RUNNER_LABELS is our compose convention; LABELS is myoung34's alias.
	raw := getenv("RUNNER_LABELS", os.Getenv("LABELS"))
	for _, label := range strings.Split(raw, ",") {
		if label = strings.TrimSpace(label); label != "" {
			cfg.Labels = append(cfg.Labels, label)
		}
	}
	if len(cfg.Labels) == 0 {
		return cfg, errors.New("RUNNER_LABELS (or LABELS) must list at least one label")
	}

	if g := os.Getenv("RUNNER_GROUP_ID"); g != "" {
		id, err := strconv.Atoi(g)
		if err != nil {
			return cfg, fmt.Errorf("RUNNER_GROUP_ID=%q is not an integer: %w", g, err)
		}
		cfg.GroupID = id
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
