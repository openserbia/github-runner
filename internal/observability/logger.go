// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT

// Package observability configures the process logger.
//
// NOTE: the openserbia service baseline uses log/slog (+ tint); this CLI uses
// zerolog directly by request. Kept in its own package so it can later be
// swapped for the shared slog setup without touching call sites.
package observability

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Setup configures the global zerolog logger: human-readable console output to
// stderr (so it interleaves with the agent's plain-text logs in `docker logs`),
// level from LOG_LEVEL (default info).
func Setup() {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		if parsed, err := zerolog.ParseLevel(strings.ToLower(lvl)); err == nil {
			zerolog.SetGlobalLevel(parsed)
		}
	}
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Str("component", "runner-entrypoint").Logger()
}
