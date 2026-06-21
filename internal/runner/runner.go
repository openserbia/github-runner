// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT

// Package runner sets up workflow auth and launches the runner agent.
package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/openserbia/github-runner/internal/config"
)

const (
	patFile     = "/tmp/.github-pat" //nolint:gosec // path, not a credential
	patFileMode = 0o600
	dirMode     = 0o755
	curlrcMode  = 0o644
)

// curlrc gives every `curl` in a job a stable, identifiable UA. The runner pool
// shares NAT with the operator's home line, so a bare `curl/X` UA on our own
// sites trips scraper-lab's tool-ua-blatant rule and self-bans the IP. A
// dedicated runner UA (distinct from the operator's home-lab-ops) keeps the pool
// identifiable in logs while staying clear of the `^curl/` ban. The old runner
// image baked this into its Dockerfile; the Wolfi image doesn't, so the
// entrypoint sets it.
const curlrc = `user-agent = "open-serbia-gh-runner/1.0 (+https://www.izjava.rs; ops@izjava.rs)"
connect-timeout = 10
show-error
remote-time
proto-default = https
`

// SetupWorkflowAuth ports the old entrypoint-wrapper.sh: registry auth, git URL
// rewrites for private repos, and Go private-module settings. Behaviour-identical
// to the bash version so the migration is a no-op for running workflows.
func SetupWorkflowAuth(ctx context.Context, cfg config.Config) error {
	home := os.Getenv("HOME")

	// Copy host registry auth into a writable location (host mount is read-only).
	if src := "/docker-config/config.json"; fileExists(src) {
		dst := filepath.Join(home, ".docker", "config.json")
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy docker config: %w", err)
		}
	}

	// Operator curl UA (see curlrc above) — avoids self-banning the home IP.
	if home != "" {
		//nolint:gosec // non-secret config; 0644 matches modules/shell/curl.nix
		if err := os.WriteFile(filepath.Join(home, ".curlrc"), []byte(curlrc), curlrcMode); err != nil {
			return fmt.Errorf("write curlrc: %w", err)
		}
	}

	if cfg.GitPAT != "" {
		// git URL rewrites (identical to the previous wrapper: three insteadOf
		// rules so https/ssh/scp remotes all resolve through the authed https URL).
		authURL := "https://oauth2:" + cfg.GitPAT + "@github.com/"
		for _, from := range []string{"https://github.com/", "git@github.com:", "ssh://git@github.com/"} {
			if err := gitConfigGlobal(ctx, "url."+authURL+".insteadOf", from); err != nil {
				return fmt.Errorf("git config insteadOf %q: %w", from, err)
			}
		}
		// PAT as a file for Docker build secrets (--secret id=...,src=$GITHUB_TOKEN_FILE).
		if err := os.WriteFile(patFile, []byte(cfg.GitPAT), patFileMode); err != nil {
			return fmt.Errorf("write pat file: %w", err)
		}
		setenv("GITHUB_TOKEN_FILE", patFile)
	}

	setenvDefault("GOPRIVATE", "github.com/openserbia/*")
	setenvDefault("GONOSUMCHECK", "github.com/openserbia/*")
	return nil
}

// Exec replaces this process with the runner agent so SIGTERM/SIGINT reach it
// directly (clean job completion + ephemeral deregistration). We exec
// bin/Runner.Listener DIRECTLY — not run.sh — matching how the myoung34 base runs
// it (`dumb-init … ./bin/Runner.Listener run`): run.sh adds an interactive-sudo /
// root guard and an auto-update restart loop we don't want (updates are disabled;
// the ephemeral re-run is the container restart). `run --jitconfig` is the
// documented just-in-time launch.
func Exec(cfg config.Config, encodedJIT string) error {
	listener := filepath.Join(cfg.RunnerDir, "bin", "Runner.Listener")
	if err := os.Chdir(cfg.RunnerDir); err != nil {
		return fmt.Errorf("chdir %s: %w", cfg.RunnerDir, err)
	}
	argv := []string{listener, "run", "--jitconfig", encodedJIT}
	log.Info().Str("listener", listener).Msg("exec runner agent (jitconfig redacted)")
	// syscall.Exec only returns on failure.
	return syscall.Exec(listener, argv, os.Environ()) //nolint:gosec // listener path is from trusted config
}

func gitConfigGlobal(ctx context.Context, key, value string) error {
	cmd := exec.CommandContext(ctx, "git", "config", "--global", key, value) //nolint:gosec // args are internally constructed, not external input
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func setenv(key, value string) {
	if err := os.Setenv(key, value); err != nil {
		log.Warn().Err(err).Str("key", key).Msg("failed to set env var")
	}
}

func setenvDefault(key, def string) {
	if os.Getenv(key) == "" {
		setenv(key, def)
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil { //nolint:gosec // dst is $HOME + fixed path segments, not external input
		return err
	}
	in, err := os.ReadFile(src) //nolint:gosec // src is a fixed, trusted path
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, patFileMode) //nolint:gosec // dst is $HOME + fixed path segments, not external input
}
