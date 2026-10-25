package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/adnathanail/clonager/internal/repostatus"
)

func TestFetchAge(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	for _, tt := range []struct {
		ago   time.Duration
		want  string
		stale bool
	}{
		{-time.Minute, "0m", false}, // clock skew
		{30 * time.Second, "0m", false},
		{23 * time.Minute, "23m", false},
		{59 * time.Minute, "59m", false},
		{time.Hour, "1h", false},
		{7*time.Hour + 50*time.Minute, "7h", false},
		{day, "1d", false},
		{29 * day, "29d", false},
		{30 * day, "30d", true},
		{400 * day, "400d", true},
	} {
		got, stale := fetchAge(now.Add(-tt.ago), now)
		if got != tt.want || stale != tt.stale {
			t.Errorf("fetchAge(%v ago) = %q, %v; want %q, %v", tt.ago, got, stale, tt.want, tt.stale)
		}
	}
	if got, stale := fetchAge(time.Time{}, now); got != "never" || !stale {
		t.Errorf("fetchAge(zero) = %q, %v; want never, true", got, stale)
	}
}

func TestStatusCode(t *testing.T) {
	ok := report{issues: []issue{{sev: sevInfo, text: "1 branch merged"}}}
	warn := report{issues: []issue{{sev: sevWarn, text: "1 stash"}}}
	bad := report{issues: []issue{{sev: sevError, text: "not cloned"}}}
	for _, tt := range []struct {
		name    string
		reports []report
		want    int
	}{
		{"none", nil, exitOK},
		{"ok", []report{ok, {}}, exitOK},
		{"attention", []report{ok, warn}, exitAttention},
		{"errors", []report{warn, bad, ok}, exitErrors},
	} {
		if got := statusCode(tt.reports); got != tt.want {
			t.Errorf("%s: statusCode = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestOutputRows(t *testing.T) {
	for _, tt := range []struct {
		out   string
		width int
		want  int
	}{
		{"one\n", 10, 1},
		{"one\n\ntwo\n", 10, 3},
		{"0123456789\n", 10, 1}, // exactly the width doesn't wrap
		{"0123456789a\n", 10, 2},
		{styleWarn.Render("0123456789") + "\n", 10, 1}, // escape codes take no room
		{"✓ ● ✗ ⧓\n", 4, 2},
		{"no newline at the end", 100, 1},
	} {
		if got := outputRows(tt.out, tt.width); got != tt.want {
			t.Errorf("outputRows(%q, %d) = %d, want %d", tt.out, tt.width, got, tt.want)
		}
	}
}

func TestFoldForgeErrors(t *testing.T) {
	offline := errors.New("gh api: error connecting to api.github.com")
	missing := errors.New("gh api: HTTP 404: Not Found (https://api.github.com/repos/me/gone)")
	statuses := []*repostatus.Status{
		{ForgeErr: offline},
		{ForgeErr: missing},
		{},
		{ForgeErr: offline},
	}
	if got, want := foldForgeErrors(statuses), "Couldn't check GitHub for 2 repos: no connection."; got != want {
		t.Errorf("warning = %q, want %q", got, want)
	}
	if statuses[0].ForgeErr != nil || statuses[3].ForgeErr != nil {
		t.Error("connection errors were left on their repos")
	}
	if statuses[1].ForgeErr != missing {
		t.Error("an error about one repo was folded into the warning")
	}

	if got := foldForgeErrors([]*repostatus.Status{{ForgeErr: missing}}); got != "" {
		t.Errorf("warning = %q, want none", got)
	}
}

func TestForgeUnreachable(t *testing.T) {
	for msg, want := range map[string]string{
		"gh api: To get started with GitHub CLI, please run:  gh auth login": "gh isn't logged in (run gh auth login)",
		"gh api: HTTP 401: Bad credentials (https://api.github.com/graphql)": "gh isn't logged in (run gh auth login)",
		"gh api: error connecting to api.github.com":                         "no connection",
		"gh is not installed":             "gh isn't installed",
		"gh pr list: HTTP 404: Not Found": "",
	} {
		if got := forgeUnreachable(errors.New(msg)); got != want {
			t.Errorf("forgeUnreachable(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestUpdateSeparator(t *testing.T) {
	for width, want := range map[int]int{0: 79, 40: 39, 200: 79, 10: 27} {
		got := ansi.Strip(updateSeparator(width))
		if ansi.StringWidth(got) != want || !strings.Contains(got, " Updated from GitHub ") || !strings.HasPrefix(got, "===") {
			t.Errorf("updateSeparator(%d) = %q (%d wide), want %d wide", width, got, ansi.StringWidth(got), want)
		}
	}
}
