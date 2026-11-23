// release-notes generates the shared Markdown used by GitHub releases and
// their OTA version.json metadata. For dev builds, run before moving the
// rolling dev tag to the new commit.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const maxCommits = 30

var (
	conventionalCommit = regexp.MustCompile(`^([a-z]+)(\([^)]+\))?(!)?:\s*(.+)$`)
	markdownSpecial    = regexp.MustCompile("[\\\\`*_{}\\[\\]()<>#+.!|~-]")
	validRepo          = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	return strings.TrimSpace(string(output)), err
}

func escape(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	return markdownSpecial.ReplaceAllStringFunc(text, func(s string) string { return "\\" + s })
}

func generate(dir, repo, channel, version, commit, base, buildTime string) (string, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel == "" {
		channel = "dev"
	}
	if channel != "dev" && channel != "stable" {
		return "", fmt.Errorf("unsupported channel %q (expected dev or stable)", channel)
	}

	head, err := git(dir, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve build commit: %w", err)
	}

	baseRef := strings.TrimSpace(base)
	baseLabel := ""
	if baseRef == "" {
		if channel == "dev" {
			baseRef = "refs/tags/dev"
		} else if prevTag, err := git(dir, "describe", "--tags", "--abbrev=0", "--match", "v*", head+"^"); err == nil && prevTag != "" {
			baseRef = "refs/tags/" + prevTag
			baseLabel = prevTag
		}
	}

	var previous string
	if baseRef != "" {
		previous, _ = git(dir, "rev-parse", "--verify", baseRef+"^{commit}")
	}

	revision, limit := head, maxCommits+1
	heading := "## Development build"
	intro := "Recent commits included in this build."
	if channel == "stable" {
		heading = "## Release"
		intro = "Recent commits included in this release."
	}

	baseURL := "https://github.com/" + repo
	changesURL := baseURL + "/commits/" + head
	if previous == head {
		limit = 1
		if channel == "stable" {
			intro = "Rebuild of the current release revision."
		} else {
			intro = "Rebuild of the current dev revision."
		}
	} else if previous != "" {
		if _, err := git(dir, "merge-base", "--is-ancestor", previous, head); err == nil {
			revision = previous + ".." + head
			if channel == "stable" {
				if baseLabel != "" {
					intro = fmt.Sprintf("Changes since `%s`.", baseLabel)
				} else {
					intro = "Changes since the previous release."
				}
			} else {
				intro = "Changes since the previous dev build."
			}
			changesURL = baseURL + "/compare/" + previous + "..." + head
		}
	}

	logOutput, err := git(dir, "log", "--format=%H%x00%s", fmt.Sprintf("-n%d", limit), revision, "--")
	if err != nil {
		return "", fmt.Errorf("read commit history: %w", err)
	}
	var lines []string
	if logOutput != "" {
		lines = strings.Split(logOutput, "\n")
	}
	truncated := len(lines) > maxCommits
	if truncated {
		lines = lines[:maxCommits]
	}

	groups := make([][]string, 4)
	for _, line := range lines {
		sha, subject, ok := strings.Cut(line, "\x00")
		if !ok || len(sha) < 7 {
			continue
		}
		// Keep even unusually long subjects within the OTA manifest's 64 KiB limit.
		if runes := []rune(subject); len(runes) > 160 {
			subject = string(runes[:160]) + "…"
		}
		group, label := 3, ""
		if match := conventionalCommit.FindStringSubmatch(subject); match != nil {
			switch match[1] {
			case "feat":
				group = 0
			case "fix", "revert":
				group = 1
			case "perf", "refactor", "style":
				group = 2
			}
			if match[2] != "" {
				label = "**" + escape(strings.Trim(match[2], "()")) + ":** "
			}
			if match[3] != "" {
				label = "**Breaking change ·** " + label
			}
			subject = match[4]
		}
		entry := fmt.Sprintf("- %s%s · [`%s`](%s/commit/%s)", label, escape(subject), sha[:7], baseURL, sha)
		groups[group] = append(groups[group], entry)
	}

	var notes strings.Builder
	fmt.Fprintf(&notes, "%s\n\n`%s`\n\n%s\n\n", heading, version, intro)
	titles := []string{"✨ Features", "🐛 Fixes", "⚡ Improvements", "🔧 Maintenance & other changes"}
	for i, entries := range groups {
		if len(entries) != 0 {
			fmt.Fprintf(&notes, "### %s\n\n%s\n\n", titles[i], strings.Join(entries, "\n"))
		}
	}
	if truncated {
		fmt.Fprintf(&notes, "Showing the latest %d commits. See the full changelog for all changes.\n\n", maxCommits)
	}
	fmt.Fprintf(&notes, "[Full changelog](%s) · [Build commit `%s`](%s/commit/%s)\n", changesURL, head[:7], baseURL, head)
	if stamp, err := time.Parse(time.RFC3339, buildTime); err == nil {
		fmt.Fprintf(&notes, "\nBuilt %s\n", stamp.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return notes.String(), nil
}

func main() {
	repo := flag.String("repo", "", "GitHub owner/repository")
	channel := flag.String("channel", "dev", "release channel (dev or stable)")
	version := flag.String("version", "dev", "build version")
	commit := flag.String("commit", "HEAD", "build commit")
	base := flag.String("base", "", "previous revision (defaults to refs/tags/dev for dev, previous v* tag for stable)")
	buildTime := flag.String("build-time", "", "RFC3339 build time")
	flag.Parse()
	if !validRepo.MatchString(*repo) {
		fmt.Fprintln(os.Stderr, "-repo must be a GitHub owner/repository")
		os.Exit(1)
	}
	notes, err := generate(".", *repo, *channel, *version, *commit, *base, *buildTime)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(notes)
}
