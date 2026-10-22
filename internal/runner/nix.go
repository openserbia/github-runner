// SPDX-FileCopyrightText: 2026 OpenSerbia
// SPDX-License-Identifier: MIT

package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

// nixConf replaces the installer's /etc/nix/nix.conf after a recreate. The
// installer's own settings, plus:
//   - an empty build-users-group: the nixbld group and its 32 users lived in
//     the old container's /etc/group and /etc/passwd. Jobs run as root, so
//     builds need no build users. Without this, Nix defaults to group nixbld
//     and every devbox job fails with "the group 'nixbld' specified in
//     'build-users-group' does not exist".
//   - sandbox = false and max-jobs = 1: this container sets
//     no-new-privileges and has no user namespaces, so the sandbox cannot
//     start. Nix then falls back to unsandboxed builds, which all use
//     /homeless-shelter as HOME and abort when that directory already exists.
//     One local build at a time avoids the race. Substitutions stay parallel.
const nixConf = `# Restored by runner-entrypoint: /nix is a persistent volume but /etc is not,
# so a recreated container keeps the Nix store and loses this file. See
# internal/runner/nix.go.
build-users-group =
sandbox = false
max-jobs = 1
extra-experimental-features = nix-command
auto-optimise-store = true
always-allow-substitutes = true
`

// nixProfile puts nix on PATH for login shells (`bash -el`), as the installer's
// /etc/profile.d/nix.sh did.
const nixProfile = `# Restored by runner-entrypoint (see internal/runner/nix.go).
if [ -e '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh' ]; then
  . '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh'
fi
`

const (
	nixDirMode  = 0o755
	nixFileMode = 0o644
)

// Paths relative to the filesystem root passed to RestoreNixConfig.
var (
	nixBinPath     = filepath.Join("nix", "var", "nix", "profiles", "default", "bin", "nix")
	nixConfPath    = filepath.Join("etc", "nix", "nix.conf")
	nixProfilePath = filepath.Join("etc", "profile.d", "nix.sh")
)

// RestoreNixConfig rewrites the /etc side of a Nix install whose store survived
// on the persistent /nix volume while the container's /etc was recreated. It is
// a no-op on a fresh volume (devbox's installer then runs as usual) and when
// the config is already present (a plain container restart keeps /etc).
// It always removes /homeless-shelter: a container restart keeps the writable
// layer, and a build killed mid-way leaves that directory behind, which makes
// the next unsandboxed build fail. root is "/" in production; tests pass a temp dir.
func RestoreNixConfig(root string) error {
	// Nix refuses an unsandboxed build when this path exists. RemoveAll on a
	// missing path is a no-op; on a symlink it removes the link, not the target.
	if err := os.RemoveAll(filepath.Join(root, "homeless-shelter")); err != nil {
		return fmt.Errorf("remove /homeless-shelter: %w", err)
	}

	nixBin := filepath.Join(root, nixBinPath)
	if _, err := os.Stat(nixBin); errors.Is(err, fs.ErrNotExist) {
		return nil // no Nix on the volume yet
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", nixBin, err)
	}

	conf := filepath.Join(root, nixConfPath)
	if _, err := os.Stat(conf); err == nil {
		return nil // installer's config still here
	}

	files := []struct{ path, body string }{
		{conf, nixConf},
		{filepath.Join(root, nixProfilePath), nixProfile},
	}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.path), nixDirMode); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(f.path), err)
		}
		//nolint:gosec // world-readable config, as the Nix installer writes it
		if err := os.WriteFile(f.path, []byte(f.body), nixFileMode); err != nil {
			return fmt.Errorf("write %s: %w", f.path, err)
		}
	}
	log.Info().Msg("restored /etc/nix config for the persistent /nix store")
	return nil
}
