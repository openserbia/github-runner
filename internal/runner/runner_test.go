// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT
package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openserbia/github-runner/internal/config"
)

// With no GITHUB_PAT, SetupWorkflowAuth skips the git-config shell-out, so this
// exercises the curlrc write in isolation.
func TestSetupWorkflowAuthWritesCurlrc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOPRIVATE", "x")    // pre-set so setenvDefault is a no-op
	t.Setenv("GONOSUMCHECK", "x") // (avoids mutating the real default)

	if err := SetupWorkflowAuth(context.Background(), config.Config{}); err != nil {
		t.Fatalf("SetupWorkflowAuth: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(home, ".curlrc")) //nolint:gosec // path is t.TempDir(), trusted
	if err != nil {
		t.Fatalf("read curlrc: %v", err)
	}
	if !strings.Contains(string(b), "open-serbia-gh-runner/1.0") {
		t.Errorf("curlrc missing runner UA:\n%s", b)
	}
}
