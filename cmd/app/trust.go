package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// Project trust gate (F26): project-local config (./SYSTEM.md,
// ./APPEND_SYSTEM.md, ./.tenzing/prompts/) is consumed only for directories
// the user has trusted. Decisions persist in <UserConfigDir>/tenzing/
// trust.json; absent a decision, TENZING_PROJECT_TRUST decides ("trust" or
// the default "skip" — safe for a headless server with no terminal to
// prompt). AGENTS.md context files are exempt: they inform the agent, they
// are not executed as instructions verbatim (matching Pi).

type trustEntry struct {
	Trusted bool   `json:"trusted"`
	Decided string `json:"decided"`
}

// trustFilePath returns <UserConfigDir>/tenzing/trust.json.
func trustFilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(base, "tenzing", "trust.json"), nil
}

// loadTrustFile reads the trust map; a missing file is an empty map.
func loadTrustFile(path string) (map[string]trustEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]trustEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read trust file: %w", err)
	}
	var m map[string]trustEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]trustEntry{}
	}
	return m, nil
}

// saveTrustFile writes the trust map atomically (temp file then rename).
func saveTrustFile(path string, m map[string]trustEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create trust dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trust file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".trust-*.json")
	if err != nil {
		return fmt.Errorf("create temp trust file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write trust file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close trust file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename trust file: %w", err)
	}
	return nil
}

// resolveProjectTrust reports whether project-local config in dir may be
// loaded, and where the answer came from ("persisted", "env", "default", or
// "error"). A persisted decision wins; otherwise envDefault "trust" grants,
// anything else skips.
func resolveProjectTrust(path, dir, envDefault string) (trusted bool, source string) {
	m, err := loadTrustFile(path)
	if err != nil {
		slog.Warn("trust file unreadable, treating project as untrusted", "path", path, "error", err)
		return false, "error"
	}
	if e, ok := m[filepath.Clean(dir)]; ok {
		return e.Trusted, "persisted"
	}
	if envDefault == "trust" {
		return true, "env"
	}
	return false, "default"
}

// setProjectTrust persists a trust decision for dir.
func setProjectTrust(path, dir string, trusted bool, now time.Time) error {
	m, err := loadTrustFile(path)
	if err != nil {
		return err
	}
	updated := make(map[string]trustEntry, len(m)+1)
	maps.Copy(updated, m)
	updated[filepath.Clean(dir)] = trustEntry{
		Trusted: trusted,
		Decided: now.UTC().Format(time.RFC3339),
	}
	return saveTrustFile(path, updated)
}
