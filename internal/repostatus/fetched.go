package repostatus

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// readLastFetch sets LastFetch to when the repo last fetched from any remote.
//
// Every fetch (and pull) rewrites FETCH_HEAD, even when nothing changed, and
// GitButler's background fetches do too, so its mtime is the best record.
// A clone doesn't write it, so a repo never fetched since cloning falls back
// to the clone entry in origin/HEAD's reflog. Remote-tracking reflogs in
// general won't do: they're only written when a ref moves, including by push.
func (s *Status) readLastFetch(g git, gitDir string) {
	if fi, err := os.Stat(filepath.Join(gitDir, "FETCH_HEAD")); err == nil {
		s.LastFetch = fi.ModTime()
		return
	}
	// %gd with --date=unix is e.g. refs/remotes/origin/HEAD@{1759140000}: the
	// entry's own time (%ct would be the commit's).
	out, err := g.run("log", "-g", "--date=unix", "--format=%gd%x00%gs", "refs/remotes/origin/HEAD")
	if err != nil {
		return // no origin/HEAD, or no reflog for it: never fetched
	}
	for _, line := range strings.Split(out, "\n") {
		selector, subject, _ := strings.Cut(line, "\x00")
		_, ts, _ := strings.Cut(selector, "@{")
		if strings.HasPrefix(subject, "clone:") {
			if sec, err := strconv.ParseInt(strings.TrimSuffix(ts, "}"), 10, 64); err == nil {
				s.LastFetch = time.Unix(sec, 0)
			}
		}
	}
}
