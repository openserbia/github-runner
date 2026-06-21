// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT

// Package config loads and validates runner-entrypoint configuration from the
// process environment (the myoung34/github-runner env contract).
package config

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// DefaultAPIBase is the public GitHub API; override with GITHUB_API_URL for GHES.
const DefaultAPIBase = "https://api.github.com"

const (
	defaultRunnerDir  = "/actions-runner"
	defaultWorkFolder = "_work"
	defaultGroupID    = 1 // the "Default" org runner group

	labelSelfHosted = "self-hosted" // GitHub's reserved self-hosted runner label
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

	// Custom labels (RUNNER_LABELS is our compose convention; LABELS is myoung34's
	// alias). We then PREPEND GitHub's default self-hosted/OS/arch labels —
	// generate-jitconfig, unlike config.sh, does NOT add them, so without this a
	// `runs-on: [self-hosted, ...]` job never matches the runner.
	raw := getenv("RUNNER_LABELS", os.Getenv("LABELS"))
	var custom []string
	for _, label := range strings.Split(raw, ",") {
		if label = strings.TrimSpace(label); label != "" {
			custom = append(custom, label)
		}
	}
	if len(custom) == 0 {
		return cfg, errors.New("RUNNER_LABELS (or LABELS) must list at least one custom label")
	}
	cfg.Labels = dedupeLabels(append(githubDefaultLabels(), custom...))

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

// githubDefaultLabels are the labels GitHub auto-adds for a config.sh runner but
// NOT for a JIT runner. The arch label follows GitHub's naming (X64/ARM64/ARM),
// keyed on the binary's build arch.
func githubDefaultLabels() []string {
	arch := "X64"
	switch runtime.GOARCH {
	case "arm64":
		arch = "ARM64"
	case "arm":
		arch = "ARM"
	}
	return []string{labelSelfHosted, "Linux", arch}
}

// dedupeLabels drops case-insensitive duplicates, preserving first-seen order —
// so an operator who already lists a default in RUNNER_LABELS doesn't get it twice.
func dedupeLabels(labels []string) []string {
	seen := make(map[string]bool, len(labels))
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		k := strings.ToLower(l)
		if !seen[k] {
			seen[k] = true
			out = append(out, l)
		}
	}
	return out
}
