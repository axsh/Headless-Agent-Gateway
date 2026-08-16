package codingagent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// protectedSessionBasenames must never be removed or replaced by overlay,
// even if they incorrectly appear in an allowlist.
var protectedSessionBasenames = map[string]struct{}{
	"projects":      {},
	"sessions":      {},
	"statsig":       {},
	"debug":         {},
	"logs":          {},
	"tmp":           {},
	"cache":         {},
	"history":       {},
	"metadata.json": {},
	"context.json":  {},
	"record.json":   {},
	"native":        {},
}

// OverlayConfigDir copies or symlinks allowlisted names from configDir into
// sessionDir. Only entries that exist under configDir are applied. Existing
// session-only data outside the allowlist is never removed. Protected names
// under sessionDir are never deleted even if listed in allowlist by mistake.
func OverlayConfigDir(sessionDir, configDir string, allowlist []string) error {
	if sessionDir == "" {
		return fmt.Errorf("overlay config_dir: sessionDir is empty")
	}
	if configDir == "" {
		return fmt.Errorf("overlay config_dir: configDir is empty")
	}
	fi, err := os.Stat(configDir)
	if err != nil {
		return fmt.Errorf("overlay config_dir: stat configDir: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("overlay config_dir: configDir is not a directory: %s", configDir)
	}
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("overlay config_dir: mkdir sessionDir: %w", err)
	}

	for _, name := range allowlist {
		if name == "" || name == "." || name == ".." {
			continue
		}
		if _, protected := protectedSessionBasenames[name]; protected {
			continue
		}
		src := filepath.Join(configDir, name)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("overlay config_dir: stat %s: %w", src, err)
		}
		dst := filepath.Join(sessionDir, name)
		if err := linkOrCopy(src, dst); err != nil {
			return fmt.Errorf("overlay config_dir: apply %s: %w", name, err)
		}
	}
	return nil
}

// linkOrCopy creates dst as a symlink to src when possible; otherwise
// recursively copies src to dst. If dst exists, it is removed first.
func linkOrCopy(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("remove existing dst: %w", err)
	}
	if err := os.Symlink(src, dst); err == nil {
		return nil
	}
	return copyPath(src, dst)
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, info.Mode())
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if err := copyPath(s, d); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
