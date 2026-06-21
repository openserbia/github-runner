// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT
package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	base := map[string]string{
		"ORG_NAME":      "openserbia",
		"ACCESS_TOKEN":  "tok",
		"RUNNER_NAME":   "ax41-1",
		"RUNNER_LABELS": "ax41, docker",
	}

	tests := []struct {
		name     string
		env      map[string]string
		wantErr  string
		validate func(*testing.T, Config)
	}{
		{
			name: "happy path with defaults",
			validate: func(t *testing.T, c Config) {
				if c.GroupID != defaultGroupID {
					t.Errorf("GroupID = %d, want %d", c.GroupID, defaultGroupID)
				}
				if c.WorkFolder != defaultWorkFolder {
					t.Errorf("WorkFolder = %q, want %q", c.WorkFolder, defaultWorkFolder)
				}
				// generate-jitconfig needs the defaults prepended; the arch label
				// varies by build host (X64/ARM64), so assert structure not arch.
				if len(c.Labels) != 5 || c.Labels[0] != "self-hosted" || c.Labels[1] != "Linux" {
					t.Errorf("Labels = %v, want [self-hosted Linux <arch> ax41 docker]", c.Labels)
				}
				if c.Labels[3] != "ax41" || c.Labels[4] != "docker" {
					t.Errorf("custom labels = %v, want ax41,docker at the end", c.Labels)
				}
			},
		},
		{name: "missing org", env: map[string]string{"ORG_NAME": ""}, wantErr: "ORG_NAME is required"},
		{name: "missing token", env: map[string]string{"ACCESS_TOKEN": ""}, wantErr: "ACCESS_TOKEN is required"},
		{name: "missing name", env: map[string]string{"RUNNER_NAME": ""}, wantErr: "RUNNER_NAME is required"},
		{name: "no labels", env: map[string]string{"RUNNER_LABELS": "  , ,"}, wantErr: "at least one custom label"},
		{
			name: "dedupes a default already in RUNNER_LABELS",
			env:  map[string]string{"RUNNER_LABELS": "self-hosted, ax41, docker"},
			validate: func(t *testing.T, c Config) {
				n := 0
				for _, l := range c.Labels {
					if strings.EqualFold(l, "self-hosted") {
						n++
					}
				}
				if n != 1 {
					t.Errorf("self-hosted appears %d times, want 1 (deduped): %v", n, c.Labels)
				}
			},
		},
		{name: "repo scope rejected", env: map[string]string{"RUNNER_SCOPE": "repo"}, wantErr: "unsupported"},
		{name: "bad group id", env: map[string]string{"RUNNER_GROUP_ID": "notanint"}, wantErr: "not an integer"},
		{
			name: "LABELS alias and custom group",
			env:  map[string]string{"RUNNER_LABELS": "", "LABELS": "rpi,docker", "RUNNER_GROUP_ID": "7"},
			validate: func(t *testing.T, c Config) {
				if c.Labels[0] != "self-hosted" {
					t.Errorf("Labels[0] = %q, want self-hosted prepended", c.Labels[0])
				}
				if c.Labels[len(c.Labels)-2] != "rpi" || c.Labels[len(c.Labels)-1] != "docker" {
					t.Errorf("Labels = %v, want rpi,docker (via LABELS alias) at the end", c.Labels)
				}
				if c.GroupID != 7 {
					t.Errorf("GroupID = %d, want 7", c.GroupID)
				}
			},
		},
	}

	contractKeys := []string{
		"ORG_NAME", "ACCESS_TOKEN", "RUNNER_NAME", "RUNNER_LABELS", "LABELS",
		"RUNNER_SCOPE", "RUNNER_GROUP_ID", "RUNNER_WORKDIR", "RUNNER_DIR", "GITHUB_API_URL",
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range contractKeys {
				t.Setenv(k, "")
			}
			for k, v := range base {
				t.Setenv(k, v)
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}
