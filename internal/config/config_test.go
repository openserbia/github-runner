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
				if got := strings.Join(c.Labels, ","); got != "ax41,docker" {
					t.Errorf("Labels = %q, want trimmed ax41,docker", got)
				}
			},
		},
		{name: "missing org", env: map[string]string{"ORG_NAME": ""}, wantErr: "ORG_NAME is required"},
		{name: "missing token", env: map[string]string{"ACCESS_TOKEN": ""}, wantErr: "ACCESS_TOKEN is required"},
		{name: "missing name", env: map[string]string{"RUNNER_NAME": ""}, wantErr: "RUNNER_NAME is required"},
		{name: "no labels", env: map[string]string{"RUNNER_LABELS": "  , ,"}, wantErr: "at least one label"},
		{name: "repo scope rejected", env: map[string]string{"RUNNER_SCOPE": "repo"}, wantErr: "unsupported"},
		{name: "bad group id", env: map[string]string{"RUNNER_GROUP_ID": "notanint"}, wantErr: "not an integer"},
		{
			name: "LABELS alias and custom group",
			env:  map[string]string{"RUNNER_LABELS": "", "LABELS": "rpi,docker", "RUNNER_GROUP_ID": "7"},
			validate: func(t *testing.T, c Config) {
				if strings.Join(c.Labels, ",") != "rpi,docker" {
					t.Errorf("Labels = %v, want rpi,docker via LABELS alias", c.Labels)
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
