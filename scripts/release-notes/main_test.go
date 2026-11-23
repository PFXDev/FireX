package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func initTestRepo(t *testing.T) (string, func(...string) string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		output, err := git(dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return output
	}
	run("init")
	run("config", "user.name", "Release Test")
	run("config", "user.email", "test@example.com")
	run("config", "commit.gpgsign", "false")
	run("config", "tag.gpgsign", "false")
	return dir, run
}

func TestGenerateDevHistory(t *testing.T) {
	dir, run := initTestRepo(t)
	run("commit", "--allow-empty", "-m", "chore: previous release")
	previous := run("rev-parse", "HEAD")
	run("tag", "dev")
	run("commit", "--allow-empty", "-m", "feat(ota): show [notes] <script> &copy;")
	run("commit", "--allow-empty", "-m", "fix: preserve `commit` links")
	run("commit", "--allow-empty", "-m", "refactor(ui)!: reorganize system card")
	head := run("rev-parse", "HEAD")

	generateNotes := func(base string) string {
		t.Helper()
		notes, err := generate(dir, "owner/repo", "dev", "dev-test", "HEAD", base, "2026-11-15T01:02:03Z")
		if err != nil {
			t.Fatal(err)
		}
		return notes
	}

	notes := generateNotes("")
	for _, want := range []string{
		"## Development build",
		"Changes since the previous dev build.",
		"### ✨ Features",
		"### 🐛 Fixes",
		"### ⚡ Improvements",
		"**ota:** show \\[notes\\] \\<script\\> &amp;copy;",
		"preserve \\`commit\\` links",
		"**Breaking change ·** **ui:** reorganize system card",
		"/commit/" + head,
		"/compare/" + previous + "..." + head,
		"Built 2026-11-15 01:02 UTC",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes missing %q:\n%s", want, notes)
		}
	}
	if strings.Contains(notes, "previous release") {
		t.Fatal("included a commit from the previous release")
	}

	if first := generateNotes("refs/tags/missing"); !strings.Contains(first, "Recent commits") || !strings.Contains(first, "previous release") {
		t.Fatalf("first release must fall back to recent history: %s", first)
	}
	if rebuild := generateNotes("HEAD"); !strings.Contains(rebuild, "Rebuild") || strings.Contains(rebuild, "### ✨ Features") {
		t.Fatalf("a rebuild must show only its current commit: %s", rebuild)
	}

	// Rewritten dev history must not produce an invalid comparison range.
	run("checkout", "--orphan", "rewritten")
	run("commit", "--allow-empty", "-m", "feat!: new history")
	if rewritten := generateNotes("refs/tags/dev"); !strings.Contains(rewritten, "Recent commits") || strings.Contains(rewritten, "/compare/") {
		t.Fatalf("non-ancestor baseline must fall back to history: %s", rewritten)
	}

	for range maxCommits {
		run("commit", "--allow-empty", "-m", "fix: "+strings.Repeat("&", 300))
	}
	bounded := generateNotes("refs/tags/dev")
	if !strings.Contains(bounded, "Showing the latest 30 commits") || strings.Contains(bounded, "new history") {
		t.Fatal("long history was not capped")
	}
	payload, err := json.Marshal(map[string]string{"release_notes": bounded})
	if err != nil || len(payload) > 60<<10 {
		t.Fatalf("notes leave insufficient room for metadata in the 64 KiB manifest: %d bytes, %v", len(payload), err)
	}
}

func TestGenerateStableHistory(t *testing.T) {
	dir, run := initTestRepo(t)
	run("commit", "--allow-empty", "-m", "feat: initial release")
	firstSHA := run("rev-parse", "HEAD")
	run("tag", "v1.0.0")

	firstNotes, err := generate(dir, "owner/repo", "stable", "v1.0.0", "HEAD", "", "2026-11-15T01:02:03Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Release",
		"`v1.0.0`",
		"Recent commits included in this release.",
		"initial release",
		"/commits/" + firstSHA,
	} {
		if !strings.Contains(firstNotes, want) {
			t.Errorf("first stable release notes missing %q:\n%s", want, firstNotes)
		}
	}

	run("commit", "--allow-empty", "-m", "fix(updater): read manifest notes")
	run("tag", "v1.1.0")
	headSHA := run("rev-parse", "HEAD")

	nextNotes, err := generate(dir, "owner/repo", "stable", "v1.1.0", "HEAD", "", "2026-11-16T03:04:05Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Release",
		"`v1.1.0`",
		"Changes since `v1.0.0`.",
		"### 🐛 Fixes",
		"**updater:** read manifest notes",
		"/compare/" + firstSHA + "..." + headSHA,
		"Built 2026-11-16 03:04 UTC",
	} {
		if !strings.Contains(nextNotes, want) {
			t.Errorf("subsequent stable release notes missing %q:\n%s", want, nextNotes)
		}
	}
	if strings.Contains(nextNotes, "initial release") {
		t.Fatalf("stable release included commit from previous tag:\n%s", nextNotes)
	}
}
