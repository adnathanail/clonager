package cmd

import (
	"strings"
	"testing"

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
