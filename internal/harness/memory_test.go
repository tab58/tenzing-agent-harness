package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func redirectHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	return home
}

func fixedNow() time.Time { return time.Date(2026, 7, 11, 15, 36, 0, 0, time.UTC) }

func TestPersistRoutesMainToConfigAndChildToCache(t *testing.T) {
	redirectHome(t)
	ms := newMemoryStore("aabbccdd", fixedNow)

	ms.persist("aabbccdd", "main summary")
	ms.persist("aabbccdd_11223344", "child summary")

	configDir, cacheDir := memoryDirs()
	mainFiles, _ := filepath.Glob(filepath.Join(configDir, ".agent_memory-*-aabbccdd.md"))
	if len(mainFiles) != 1 {
		t.Fatalf("config dir main files = %v, want exactly 1", mainFiles)
	}
	if !strings.Contains(mainFiles[0], "20260711-1536") {
		t.Errorf("file name %q missing session stamp", mainFiles[0])
	}
	childFiles, _ := filepath.Glob(filepath.Join(cacheDir, ".agent_memory-*-aabbccdd_11223344.md"))
	if len(childFiles) != 1 {
		t.Fatalf("cache dir child files = %v, want exactly 1", childFiles)
	}
	data, _ := os.ReadFile(mainFiles[0])
	if !strings.Contains(string(data), "main summary") || !strings.Contains(string(data), "# Agent Memory") {
		t.Errorf("main file content wrong: %s", data)
	}
}

func TestPersistRewritesInPlaceWithinSession(t *testing.T) {
	redirectHome(t)
	ms := newMemoryStore("aabbccdd", fixedNow)
	ms.persist("aabbccdd", "first")
	ms.persist("aabbccdd", "second")

	configDir, _ := memoryDirs()
	files, _ := filepath.Glob(filepath.Join(configDir, ".agent_memory-*"))
	if len(files) != 1 {
		t.Fatalf("files = %v, want 1 (rewrite in place)", files)
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), "second") {
		t.Error("file not rewritten with latest summary")
	}
}

func TestLoadLatestPicksNewestForID(t *testing.T) {
	redirectHome(t)
	configDir, _ := memoryDirs()
	old := "# Agent Memory\n\nold state\n"
	newer := "# Agent Memory\n\nnew state\n"
	os.WriteFile(filepath.Join(configDir, ".agent_memory-20260701-0900-aabbccdd.md"), []byte(old), 0644)
	os.WriteFile(filepath.Join(configDir, ".agent_memory-20260710-0900-aabbccdd.md"), []byte(newer), 0644)
	os.WriteFile(filepath.Join(configDir, ".agent_memory-20260711-0900-ffffffff.md"), []byte("other convo"), 0644)

	ms := newMemoryStore("aabbccdd", fixedNow)
	got := ms.loadLatest("aabbccdd")
	if !strings.Contains(got, "new state") {
		t.Fatalf("loadLatest = %q, want newest file's content", got)
	}
	if ms.loadLatest("00000000") != "" {
		t.Error("unknown ID should load empty")
	}
}

func TestSweepReapsOldSparesResumeTarget(t *testing.T) {
	redirectHome(t)
	configDir, cacheDir := memoryDirs()
	oldTime := time.Now().Add(-8 * 24 * time.Hour)

	expired := filepath.Join(configDir, ".agent_memory-20260601-0900-11111111.md")
	spared := filepath.Join(configDir, ".agent_memory-20260601-0901-aabbccdd.md")
	fresh := filepath.Join(cacheDir, ".agent_memory-20260711-0900-22222222.md")
	expiredCache := filepath.Join(cacheDir, ".agent_memory-20260601-0900-33333333.md")
	for _, f := range []string{expired, spared, fresh, expiredCache} {
		if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{expired, spared, expiredCache} {
		if err := os.Chtimes(f, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}

	ms := newMemoryStore("aabbccdd", fixedNow)
	ms.sweep(7*24*time.Hour, "aabbccdd")

	for f, want := range map[string]bool{expired: false, spared: true, fresh: true, expiredCache: false} {
		_, err := os.Stat(f)
		exists := err == nil
		if exists != want {
			t.Errorf("%s exists=%v, want %v", filepath.Base(f), exists, want)
		}
	}
}
