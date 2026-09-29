package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/discover"
)

var discoverFlags struct {
	dryRun bool
	depth  int
}

var discoverCmd = &cobra.Command{
	Use:   "discover <dir>...",
	Short: "Find repos that aren't in the config and add them",
	Long: `Find git repos under each <dir> that aren't in the config, and add them.

Each repo is added with origin as its url, any other remotes, and
gitbutler: true if it's in a GitButler workspace. Repos without an origin are
skipped, as there'd be nothing to clone them from.

A repo goes under the most specific top-level key that contains it. If none
does, <dir> becomes a new top-level key. <dir> can also be a repo itself,
e.g. clonager discover ~/.config/nix-darwin.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runDiscover,
}

func init() {
	discoverCmd.Flags().BoolVarP(&discoverFlags.dryRun, "dry-run", "n", false, "show what would be added without changing the config")
	discoverCmd.Flags().IntVarP(&discoverFlags.depth, "depth", "d", 4, "how many folders deep to look below each <dir>")
	rootCmd.AddCommand(discoverCmd)
}

func runDiscover(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if errors.Is(err, fs.ErrNotExist) {
		cfg = config.New(configFile())
		err = nil
	}
	if err != nil {
		return err
	}

	type row struct{ path, detail, notes string }
	var added, skipped []row
	known := 0
	for _, arg := range args {
		dir, err := absDir(arg)
		if err != nil {
			return err
		}
		paths, err := discover.Find(dir, discoverFlags.depth)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if cfg.Contains(path) {
				known++
				continue
			}
			repo, err := discover.Describe(path)
			if err == nil {
				err = cfg.Add(repo, dir)
			}
			if err != nil {
				skipped = append(skipped, row{path: config.TildePath(path), detail: err.Error()})
				continue
			}
			var notes []string
			if repo.GitButler {
				notes = append(notes, styleGB.Render("gitbutler"))
			}
			for _, r := range repo.Remotes {
				notes = append(notes, styleBranch.Render("+"+r.Name))
			}
			added = append(added, row{config.TildePath(path), repo.URL, strings.Join(notes, " ")})
		}
	}

	pathW := 0
	for _, r := range append(added, skipped...) {
		pathW = max(pathW, len(r.path))
	}
	printRows := func(heading, icon string, rows []row) {
		if len(rows) == 0 {
			return
		}
		fmt.Println(styleHeading.Render(heading))
		for _, r := range rows {
			line := fmt.Sprintf("  %s %-*s  %s  %s", icon, pathW, r.path, styleDim.Render(r.detail), r.notes)
			fmt.Println(strings.TrimRight(line, " "))
		}
		fmt.Println()
	}
	verb := "Added to "
	if discoverFlags.dryRun {
		verb = "Would add to "
	}
	printRows(verb+config.TildePath(cfg.Path), styleOK.Render("+"), added)
	printRows("Skipped", styleWarn.Render("!"), skipped)
	fmt.Println(styleDim.Render(fmt.Sprintf("%d new · %d already configured · %d skipped", len(added), known, len(skipped))))

	if discoverFlags.dryRun || len(added) == 0 {
		return nil
	}
	return cfg.Save()
}

func absDir(arg string) (string, error) {
	p, err := config.ExpandHome(arg)
	if err != nil {
		return "", err
	}
	p, err = filepath.Abs(p)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", arg)
	}
	return p, nil
}
