// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT

// Command runner-entrypoint is a small replacement for the myoung34/github-runner
// bash registration entrypoint. It ports the git/registry/Go-module auth setup,
// registers an ephemeral runner via the GitHub JIT-config API (replacing a stale
// same-named registration on conflict), then replaces its process image with
// bin/Runner.Listener so the agent receives signals directly.
//
// The ephemeral "loop" is the container restart policy: the agent runs one job,
// exits, the container restarts, and this binary registers afresh.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/openserbia/github-runner/internal/config"
	"github.com/openserbia/github-runner/internal/githubapi"
	"github.com/openserbia/github-runner/internal/observability"
	"github.com/openserbia/github-runner/internal/runner"
)

const registrationTimeout = 60 * time.Second

func main() {
	observability.Setup()
	if err := run(); err != nil {
		log.Fatal().Err(err).Msg("runner-entrypoint failed")
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), registrationTimeout)
	defer cancel()

	if err := runner.SetupWorkflowAuth(ctx, cfg); err != nil {
		return fmt.Errorf("workflow auth setup: %w", err)
	}

	client := githubapi.New(cfg.APIBase, cfg.Org, cfg.Token)
	jit, err := client.GenerateJIT(ctx, githubapi.JITParams{
		Name:       cfg.Name,
		GroupID:    cfg.GroupID,
		Labels:     cfg.Labels,
		WorkFolder: cfg.WorkFolder,
	})
	if err != nil {
		return fmt.Errorf("JIT registration: %w", err)
	}
	// NB: jit.Encoded embeds the runner's credentials — never log it.
	log.Info().
		Str("runner", jit.RunnerName).
		Int64("id", jit.RunnerID).
		Str("labels", strings.Join(cfg.Labels, ",")).
		Int("group", cfg.GroupID).
		Msg("registered ephemeral runner")

	// Exec replaces this process on success; it only returns on failure.
	return runner.Exec(cfg, jit.Encoded)
}
