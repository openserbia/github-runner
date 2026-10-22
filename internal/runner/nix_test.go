// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT
package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFakeNix(t *testing.T, root string) {
	t.Helper()
	bin := filepath.Dir(filepath.Join(root, nixBinPath))
	if err := os.MkdirAll(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "nix"), nil, 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
}

func TestRestoreNixConfigFreshVolumeIsNoop(t *testing.T) {
	root := t.TempDir()
	shelter := filepath.Join(root, "homeless-shelter")
	if err := os.Mkdir(shelter, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := RestoreNixConfig(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, nixConfPath)); !os.IsNotExist(err) {
		t.Fatalf("nix.conf written without a Nix install (err=%v); the installer must own a fresh volume", err)
	}
	if _, err := os.Stat(shelter); !os.IsNotExist(err) {
		t.Fatalf("/homeless-shelter still present (err=%v)", err)
	}
}

func TestRestoreNixConfigAfterRecreate(t *testing.T) {
	root := t.TempDir()
	installFakeNix(t, root)
	if err := RestoreNixConfig(root); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, nixConfPath)) //nolint:gosec // t.TempDir() path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "build-users-group =\n") {
		t.Fatalf("nix.conf must clear build-users-group, got:\n%s", b)
	}
	if !strings.Contains(string(b), "sandbox = false\n") {
		t.Fatalf("nix.conf must disable the sandbox, got:\n%s", b)
	}
	if !strings.Contains(string(b), "max-jobs = 1\n") {
		t.Fatalf("nix.conf must serialize local builds, got:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(root, nixProfilePath)); err != nil {
		t.Fatalf("profile.d/nix.sh not restored: %v", err)
	}
}

func TestRestoreNixConfigKeepsInstallerConfig(t *testing.T) {
	root := t.TempDir()
	installFakeNix(t, root)
	conf := filepath.Join(root, nixConfPath)
	if err := os.MkdirAll(filepath.Dir(conf), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte("installer\n"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	shelter := filepath.Join(root, "homeless-shelter")
	if err := os.Mkdir(shelter, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := RestoreNixConfig(root); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(conf); string(b) != "installer\n" { //nolint:gosec // t.TempDir() path
		t.Fatalf("existing nix.conf overwritten: %q", b)
	}
	if _, err := os.Stat(shelter); !os.IsNotExist(err) {
		t.Fatalf("/homeless-shelter still present on restart (err=%v)", err)
	}
}
