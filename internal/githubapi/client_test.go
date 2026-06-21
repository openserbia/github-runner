// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT
package githubapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testParams() JITParams {
	return JITParams{Name: "ax41-1", GroupID: 1, Labels: []string{"ax41", "docker"}, WorkFolder: "_work"}
}

func TestGenerateJIT_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/generate-jitconfig") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header = %q", got)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"encoded_jit_config":"ENC","runner":{"id":42,"name":"ax41-1"}}`))
	}))
	defer srv.Close()

	jit, err := New(srv.URL, "openserbia", "tok").GenerateJIT(context.Background(), testParams())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if jit.Encoded != "ENC" || jit.RunnerID != 42 {
		t.Errorf("got %+v", jit)
	}
}

func TestGenerateJIT_ReplacesStaleOnConflict(t *testing.T) {
	var posts, deletes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			posts++
			if posts == 1 {
				// First attempt: name already taken by a stale runner.
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"Already exists"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"encoded_jit_config":"ENC2","runner":{"id":99,"name":"ax41-1"}}`))
		case http.MethodGet:
			if got := r.URL.Query().Get("name"); got != "ax41-1" {
				t.Errorf("list name filter = %q", got)
			}
			_, _ = w.Write([]byte(`{"runners":[{"id":7,"name":"ax41-1"},{"id":8,"name":"other"}]}`))
		case http.MethodDelete:
			deletes++
			if !strings.HasSuffix(r.URL.Path, "/7") {
				t.Errorf("deleted wrong runner: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	jit, err := New(srv.URL, "openserbia", "tok").GenerateJIT(context.Background(), testParams())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if jit.Encoded != "ENC2" || jit.RunnerID != 99 {
		t.Errorf("got %+v, want the post-replace config", jit)
	}
	if posts != 2 || deletes != 1 {
		t.Errorf("posts=%d deletes=%d, want 2 and 1 (one delete of the stale id=7 only)", posts, deletes)
	}
}

func TestGenerateJIT_AuthErrorNoRetry(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "openserbia", "tok").GenerateJIT(context.Background(), testParams())
	if err == nil || !strings.Contains(err.Error(), "auth error") {
		t.Fatalf("err = %v, want auth error", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (auth errors must not retry)", calls)
	}
}
