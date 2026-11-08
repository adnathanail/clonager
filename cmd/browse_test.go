package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

// browseWith is a browse model for repos, each already inspected as given
// (nil for not cloned), offline, in a terminal width by height.
func browseWith(t *testing.T, width, height int, repos map[string]*repostatus.Status) (*browseModel, []config.Repo) {
	t.Helper()
	var rs []config.Repo
	for _, path := range []string{"/Users/me/Documents/vip-proj", "/Users/me/Documents/acmeltd-site", "/Users/me/Projects/notes"} {
		if _, ok := repos[path]; ok {
			rs = append(rs, config.Repo{Path: path, URL: "git@github.com:me/" + path[strings.LastIndex(path, "/")+1:] + ".git", Tags: []string{"work"}})
		}
	}
	m := newBrowseModel(rs, false)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	for i, r := range rs {
		s := repos[r.Path]
		if s == nil {
			s = &repostatus.Status{Missing: true}
		}
		s.Repo = r
		if !s.Missing && s.OriginURL == "" {
			s.OriginURL = r.URL
		}
		m.Update(inspectedMsg{i, s})
	}
	return m, rs
}

func key(text string) tea.KeyPressMsg {
	switch text {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	r := []rune(text)
	return tea.KeyPressMsg{Code: r[0], Text: text}
}

func TestBrowseCd(t *testing.T) {
	m, rs := browseWith(t, 100, 30, map[string]*repostatus.Status{
		"/Users/me/Documents/vip-proj":     {Head: "main"},
		"/Users/me/Documents/acmeltd-site": {Head: "main", Changed: 2},
		"/Users/me/Projects/notes":         nil,
	})
	m.Update(key("down"))
	if _, cmd := m.Update(key("enter")); cmd == nil || m.chosen != rs[1].Path {
		t.Fatalf("chosen %q, want %q and a quit", m.chosen, rs[1].Path)
	}
}

func TestBrowseNotCloned(t *testing.T) {
	m, _ := browseWith(t, 100, 30, map[string]*repostatus.Status{
		"/Users/me/Documents/vip-proj": {Head: "main"},
		"/Users/me/Projects/notes":     nil,
	})
	m.Update(key("j"))
	m.Update(key("enter"))
	if m.chosen != "" {
		t.Errorf("chose %q, a repo that isn't cloned", m.chosen)
	}
	if !strings.Contains(m.message, "isn't cloned") {
		t.Errorf("message %q, want it to say the repo isn't cloned", m.message)
	}
}

func TestBrowseFilter(t *testing.T) {
	m, rs := browseWith(t, 100, 30, map[string]*repostatus.Status{
		"/Users/me/Documents/vip-proj":     {Head: "main"},
		"/Users/me/Documents/acmeltd-site": {Head: "main"},
		"/Users/me/Projects/notes":         {Head: "main"},
	})
	for _, k := range []string{"/", "p", "r", "o", "j", "x", "backspace", "enter"} {
		m.Update(key(k))
	}
	if len(m.visible) != 2 { // vip-proj, and notes in ~/Projects
		t.Fatalf("filter %q shows %d repos, want 2", m.filter, len(m.visible))
	}
	m.Update(key("j"))
	m.Update(key("enter"))
	if m.chosen != rs[2].Path {
		t.Errorf("chosen %q, want %q", m.chosen, rs[2].Path)
	}
}

// TestBrowseViewStylesClosed checks no style is left on at the end of a
// line, where it would carry into the next row's list column: as faint did
// when the details pane wrapped a dim line mid-style.
func TestBrowseViewStylesClosed(t *testing.T) {
	long := &repostatus.Status{Head: "main", Branches: []repostatus.Branch{
		{Name: "a-branch-with-a-long-name", Remote: "origin/a-branch-with-a-long-name", Ahead: 35},
		{Name: "another-branch-with-a-long-name", Remote: "origin/another-branch-with-a-long-name", Ahead: 18},
	}}
	m, _ := browseWith(t, 90, 30, map[string]*repostatus.Status{
		"/Users/me/Documents/vip-proj":     long,
		"/Users/me/Documents/acmeltd-site": {Head: "main"},
	})
	for i, line := range strings.Split(m.View().Content, "\n") {
		if open := openStyle(line); open != "" {
			t.Errorf("line %d ends with %q still on: %q", i, open, line)
		}
	}
}

// openStyle is the SGR parameters still in effect at the end of line, ""
// if it ends reset.
func openStyle(line string) string {
	var open []string
	p := ansi.NewParser()
	state := byte(0)
	for len(line) > 0 {
		seq, _, n, newState := ansi.DecodeSequence(line, state, p)
		state = newState
		if ansi.HasCsiPrefix(seq) && strings.HasSuffix(seq, "m") {
			params := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(seq, "\x1b["), "\x9b"), "m")
			if params == "" || params == "0" {
				open = nil
			} else {
				open = append(open, params)
			}
		}
		line = line[n:]
	}
	return strings.Join(open, ";")
}

func TestBrowseView(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {60, 20}, {20, 3}, {1, 1}} {
		m, _ := browseWith(t, size[0], size[1], map[string]*repostatus.Status{
			"/Users/me/Documents/vip-proj":     {Head: "main", Stashes: 1},
			"/Users/me/Documents/acmeltd-site": {Head: "feature/a-very-long-branch-name-indeed"},
			"/Users/me/Projects/notes":         nil,
		})
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: line %q is %d wide", size[0], size[1], ansi.Strip(l), w)
			}
		}
		if size[0] >= 60 && !strings.Contains(ansi.Strip(m.View().Content), "1 stash") {
			t.Errorf("%dx%d: no details for the selected repo:\n%s", size[0], size[1], ansi.Strip(m.View().Content))
		}
	}
}

func TestBrowseSortRecent(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2024, 1, d, 12, 0, 0, 0, time.UTC) }
	m, rs := browseWith(t, 100, 30, map[string]*repostatus.Status{
		"/Users/me/Documents/vip-proj":     {Head: "main", LastChange: day(2)},
		"/Users/me/Documents/acmeltd-site": {Head: "main"}, // no known change
		"/Users/me/Projects/notes":         {Head: "main", LastChange: day(5)},
	})
	m.Update(key("down")) // acmeltd-site
	m.Update(key("s"))
	order := func() []string {
		var names []string
		for _, i := range m.visible {
			names = append(names, m.rows[i].repo.Name())
		}
		return names
	}
	if got, want := order(), []string{"notes", "vip-proj", "acmeltd-site"}; !slices.Equal(got, want) {
		t.Errorf("recent first: got %v, want %v", got, want)
	}
	if r := m.selected(); r == nil || r.repo.Path != rs[1].Path {
		t.Errorf("selection didn't stay on acmeltd-site")
	}
	if strings.Contains(ansi.Strip(m.View().Content), "~/Documents\n") {
		t.Error("folder headings shown when sorted by recent change")
	}

	m.Update(key("s"))
	if got, want := order(), []string{"vip-proj", "acmeltd-site", "notes"}; !slices.Equal(got, want) {
		t.Errorf("by folder again: got %v, want %v", got, want)
	}
}

// TestBrowseSortRecentWhileInspecting checks repos move into place as
// they're inspected, keeping the selection on the same repo.
func TestBrowseSortRecentWhileInspecting(t *testing.T) {
	rs := []config.Repo{{Path: "/Users/me/a"}, {Path: "/Users/me/b"}}
	m := newBrowseModel(rs, false)
	m.recent = true
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(inspectedMsg{0, &repostatus.Status{Repo: rs[0], LastChange: time.Now().Add(-time.Hour)}})
	m.Update(key("down")) // b, not inspected yet
	m.Update(inspectedMsg{1, &repostatus.Status{Repo: rs[1], LastChange: time.Now()}})
	if m.visible[0] != 1 {
		t.Errorf("b changed most recently but isn't first: %v", m.visible)
	}
	if r := m.selected(); r == nil || r.repo.Path != rs[1].Path {
		t.Errorf("selection didn't follow b to the top")
	}
}

// TestBrowseSortRecentStartsAtTop checks that, until the user moves, the
// selection stays on the most recently changed repo as repos sort into
// place, rather than on the first in the config.
func TestBrowseSortRecentStartsAtTop(t *testing.T) {
	rs := []config.Repo{{Path: "/Users/me/a"}, {Path: "/Users/me/b"}}
	m := newBrowseModel(rs, false)
	m.recent = true
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(inspectedMsg{0, &repostatus.Status{Repo: rs[0], LastChange: time.Now().Add(-time.Hour)}})
	m.Update(inspectedMsg{1, &repostatus.Status{Repo: rs[1], LastChange: time.Now()}})
	if r := m.selected(); r == nil || r.repo.Path != rs[1].Path {
		t.Errorf("selection isn't on b, the most recently changed")
	}
}

func TestFolderLabels(t *testing.T) {
	home, _ := os.UserHomeDir()
	repos := func(paths ...string) []config.Repo {
		var rs []config.Repo
		for _, p := range paths {
			rs = append(rs, config.Repo{Path: filepath.Join(home, p)})
		}
		return rs
	}
	cases := []struct {
		repos []config.Repo
		want  map[string]string // folder (under home) to label
	}{
		{repos("Documents/ACME/vip-proj", "Documents/ACME/site", "Projects/notes"),
			map[string]string{"Documents/ACME": "ACME/", "Projects": "Projects/"}},
		{repos("Work/Projects/vip-proj", "Projects/vip-proj"),
			map[string]string{"Work/Projects": "Work/Projects/", "Projects": "~/Projects/"}},
		{repos("notes"), map[string]string{"": "~/"}},
	}
	for _, c := range cases {
		got := folderLabels(c.repos)
		for dir, want := range c.want {
			if g := got[filepath.Join(home, dir)]; g != want {
				t.Errorf("%s: got %q, want %q", dir, g, want)
			}
		}
	}
}

func TestBrowseRecentShowsFolders(t *testing.T) {
	m, _ := browseWith(t, 120, 30, map[string]*repostatus.Status{
		"/Users/me/Documents/vip-proj": {Head: "main", LastChange: time.Now()},
		"/Users/me/Projects/notes":     {Head: "main", LastChange: time.Now().Add(-time.Hour)},
	})
	m.Update(key("s"))
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Documents/vip-proj", "Projects/notes"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q in:\n%s", want, view)
		}
	}
	m.Update(key("s"))
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "Projects/notes") {
		t.Errorf("folder before the name when grouped by folder:\n%s", view)
	}
}

// TestBrowseHideFolders checks f hides and shows the folders before names
// when sorted by recent change, and leaves the folder headings alone.
func TestBrowseHideFolders(t *testing.T) {
	m, _ := browseWith(t, 120, 30, map[string]*repostatus.Status{
		"/Users/me/Documents/vip-proj": {Head: "main", LastChange: time.Now()},
		"/Users/me/Projects/notes":     {Head: "main", LastChange: time.Now().Add(-time.Hour)},
	})
	// The list alone: the details pane has the selected repo's full path.
	list := func() string {
		lines, _ := m.listLines()
		return ansi.Strip(strings.Join(lines, "\n"))
	}
	m.Update(key("f"))
	if l := list(); !strings.Contains(l, "/Users/me/Projects") {
		t.Errorf("f hid the folder headings:\n%s", l)
	}
	m.Update(key("s"))
	if l := list(); !strings.Contains(l, "Projects/notes") {
		t.Fatalf("no folder before the name to start with:\n%s", l)
	}
	m.Update(key("f"))
	if l := list(); strings.Contains(l, "Projects/notes") || !strings.Contains(l, "notes") {
		t.Errorf("folder before the name once hidden:\n%s", l)
	}
	m.Update(key("f"))
	if l := list(); !strings.Contains(l, "Projects/notes") {
		t.Errorf("no folder before the name once shown again:\n%s", l)
	}
}

func TestBrowseGitButlerKey(t *testing.T) {
	m, _ := browseWith(t, 100, 30, map[string]*repostatus.Status{
		"/Users/me/Documents/vip-proj": {Head: "main"}, // not in a GitButler workspace
	})
	if _, cmd := m.Update(key("g")); cmd != nil || m.message != "" {
		t.Errorf("g did something: %q", m.message)
	}
	m.Update(key("b"))
	if !strings.Contains(m.message, "isn't in a GitButler workspace") {
		t.Errorf("b: got message %q, want it to say the repo isn't in GitButler", m.message)
	}
}
