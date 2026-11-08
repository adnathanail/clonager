package repostatus

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// noteChange moves LastChange up to t, if t is later.
//
// LastChange comes from what Inspect reads anyway, so it costs no more git
// calls: each local branch's tip commit (by committer date, so a rebase or
// amend counts), from readBranches, and the files with uncommitted changes
// (see noteEdits). GitButler's own gitbutler/* branches don't count, as
// GitButler rewrites them itself.
func (s *Status) noteChange(t time.Time) {
	if t.After(s.LastChange) {
		s.LastChange = t
	}
}

// noteEdits notes when each file `git status --porcelain=v2` lists as
// changed was last modified. Deleted files can't be, and an untracked
// folder is listed as just the folder, whose time only changes when files
// in it are added or removed.
func (s *Status) noteEdits(porcelain string) {
	for _, line := range strings.Split(porcelain, "\n") {
		path := changedPath(line)
		if path == "" {
			continue
		}
		if fi, err := os.Lstat(filepath.Join(s.Repo.Path, filepath.FromSlash(path))); err == nil {
			s.noteChange(fi.ModTime())
		}
	}
}

// changedPath is the path in a line of `git status --porcelain=v2` output
// (relative to the top of the repo), or "" for a header line or one it
// can't read.
func changedPath(line string) string {
	// Fields before the path: 8 for an ordinary change, 9 for a rename or
	// copy (followed by a tab and the original path), 10 for a conflict,
	// and 1 for untracked or ignored files.
	var fields int
	switch {
	case strings.HasPrefix(line, "1 "):
		fields = 8
	case strings.HasPrefix(line, "2 "):
		fields = 9
	case strings.HasPrefix(line, "u "):
		fields = 10
	case strings.HasPrefix(line, "? "), strings.HasPrefix(line, "! "):
		fields = 1
	default:
		return ""
	}
	f := strings.SplitN(line, " ", fields+1)
	if len(f) != fields+1 {
		return ""
	}
	path := f[fields]
	if fields == 9 {
		path, _, _ = strings.Cut(path, "\t")
	}
	// Unusual characters get C-style quoting, which Go's is close enough to.
	if strings.HasPrefix(path, `"`) {
		unquoted, err := strconv.Unquote(path)
		if err != nil {
			return ""
		}
		path = unquoted
	}
	return path
}
