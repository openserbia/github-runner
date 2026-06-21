// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT

// Package githubapi is a tiny client for the GitHub Actions self-hosted runner
// JIT-config registration endpoints — just enough to register and replace an
// ephemeral org runner, with no third-party SDK.
package githubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	apiVersion    = "2022-11-28"
	maxAttempts   = 4
	backoffStep   = 2 * time.Second
	maxBodyBytes  = 1 << 20 // 1 MiB cap on API response bodies
	snippetMaxLen = 300
	listPerPage   = 100
)

// ErrNameConflict reports that a runner with the requested name already exists.
var ErrNameConflict = errors.New("runner name already registered")

// Client talks to the org runner API at a given API base.
type Client struct {
	apiBase string
	org     string
	token   string
	http    *http.Client
}

// New builds a Client for an org, authenticated by a runner-admin token.
func New(apiBase, org, token string) *Client {
	return &Client{apiBase: apiBase, org: org, token: token, http: http.DefaultClient}
}

// JITParams describes the ephemeral runner to register.
type JITParams struct {
	Name       string
	GroupID    int
	Labels     []string
	WorkFolder string
}

// JITConfig is a successful registration: the encoded config the agent consumes,
// plus the created runner's identity.
type JITConfig struct {
	Encoded    string
	RunnerID   int64
	RunnerName string
}

type jitRequestBody struct {
	Name          string   `json:"name"`
	RunnerGroupID int      `json:"runner_group_id"`
	Labels        []string `json:"labels"`
	WorkFolder    string   `json:"work_folder"`
}

type jitResponseBody struct {
	EncodedJITConfig string `json:"encoded_jit_config"`
	Runner           struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runner"`
}

// GenerateJIT registers the runner, transparently replacing a stale same-named
// registration (left over from an unclean restart — the JIT API has no replace
// flag, so we delete-then-retry on a name conflict).
func (c *Client) GenerateJIT(ctx context.Context, p JITParams) (JITConfig, error) {
	jit, err := c.requestJIT(ctx, p)
	if errors.Is(err, ErrNameConflict) {
		log.Warn().Str("runner", p.Name).Msg("name already registered (stale); removing it and retrying")
		if rmErr := c.removeRunnerByName(ctx, p.Name); rmErr != nil {
			return JITConfig{}, fmt.Errorf("removing stale runner %q: %w", p.Name, rmErr)
		}
		jit, err = c.requestJIT(ctx, p)
	}
	return jit, err
}

func (c *Client) requestJIT(ctx context.Context, p JITParams) (JITConfig, error) {
	endpoint := c.apiBase + "/orgs/" + c.org + "/actions/runners/generate-jitconfig"
	payload, err := json.Marshal(jitRequestBody{
		Name:          p.Name,
		RunnerGroupID: p.GroupID,
		Labels:        p.Labels,
		WorkFolder:    p.WorkFolder,
	})
	if err != nil {
		return JITConfig{}, err
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			wait := time.Duration(attempt-1) * backoffStep
			log.Warn().Err(lastErr).Dur("backoff", wait).Int("attempt", attempt).Msg("retrying JIT registration")
			if err := sleepCtx(ctx, wait); err != nil {
				return JITConfig{}, err
			}
		}

		status, body, reqErr := c.do(ctx, http.MethodPost, endpoint, payload)
		if reqErr != nil {
			lastErr = reqErr
			continue
		}
		jit, retry, err := parseJITResponse(status, body)
		if retry {
			lastErr = err
			continue
		}
		return jit, err
	}
	return JITConfig{}, fmt.Errorf("exhausted retries: %w", lastErr)
}

// parseJITResponse classifies a generate-jitconfig response. retry is true for
// transient failures the caller should back off and retry.
func parseJITResponse(status int, body []byte) (jit JITConfig, retry bool, err error) {
	switch {
	case status == http.StatusCreated:
		var jr jitResponseBody
		if uErr := json.Unmarshal(body, &jr); uErr != nil {
			return JITConfig{}, false, fmt.Errorf("decoding JIT response: %w", uErr)
		}
		if jr.EncodedJITConfig == "" {
			return JITConfig{}, false, errors.New("JIT response missing encoded_jit_config")
		}
		return JITConfig{Encoded: jr.EncodedJITConfig, RunnerID: jr.Runner.ID, RunnerName: jr.Runner.Name}, false, nil
	case status == http.StatusConflict:
		return JITConfig{}, false, fmt.Errorf("%w: %s", ErrNameConflict, snippet(body))
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return JITConfig{}, false, fmt.Errorf("auth error (HTTP %d): %s", status, snippet(body))
	case status >= http.StatusInternalServerError, status == http.StatusTooManyRequests:
		return JITConfig{}, true, fmt.Errorf("HTTP %d: %s", status, snippet(body))
	default:
		return JITConfig{}, false, fmt.Errorf("HTTP %d: %s", status, snippet(body))
	}
}

// removeRunnerByName finds the org runner(s) named name and deletes them,
// freeing the name for a fresh JIT registration.
func (c *Client) removeRunnerByName(ctx context.Context, name string) error {
	listURL := c.apiBase + "/orgs/" + c.org + "/actions/runners?per_page=" +
		strconv.Itoa(listPerPage) + "&name=" + url.QueryEscape(name)
	status, body, err := c.do(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("list runners HTTP %d: %s", status, snippet(body))
	}
	var list struct {
		Runners []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"runners"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return fmt.Errorf("decoding runner list: %w", err)
	}

	removed := 0
	for _, r := range list.Runners {
		if r.Name != name {
			continue
		}
		delURL := c.apiBase + "/orgs/" + c.org + "/actions/runners/" + strconv.FormatInt(r.ID, 10)
		st, b, delErr := c.do(ctx, http.MethodDelete, delURL, nil)
		if delErr != nil {
			return delErr
		}
		if st != http.StatusNoContent {
			return fmt.Errorf("delete runner %d HTTP %d: %s", r.ID, st, snippet(b))
		}
		log.Warn().Str("runner", r.Name).Int64("id", r.ID).Msg("removed stale runner registration")
		removed++
	}
	if removed == 0 {
		return fmt.Errorf("no runner named %q found to remove", name)
	}
	return nil
}

// do performs one authenticated GitHub API request and returns status + body.
func (c *Client) do(ctx context.Context, method, endpoint string, body []byte) (status int, respBody []byte, err error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rdr) //nolint:gosec // endpoint is built from trusted config (apiBase+org), not external input
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	return resp.StatusCode, respBody, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// snippet trims an API error body to a single short line for logging.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > snippetMaxLen {
		s = s[:snippetMaxLen] + "…"
	}
	return s
}
