package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/discover"
)

// defaultDepth is how many folders deep discover looks by default, and how
// deep status looks for repos missing from the config.
const defaultDepth = 4

var discoverFlags struct {
	dryRun bool
	depth  int
}

var discoverCmd = &cobra.Command{
	Use:   "discover [<dir>...]",
	Short: "Find repos that aren't in the config and add them",
	Long: `Find git repos under each <dir> that aren't in the config, and add them.

Each repo is added with origin as its url, any other remotes, and
gitbutler: true if it's in a GitButler workspace. Repos without an origin are
skipped, as there'd be nothing to clone them from.

A repo goes under the most specific top-level key that contains it. If none
does, <dir> becomes a new top-level key. <dir> can also be a repo itself,
e.g. clonager discover ~/.config/nix-darwin.

With no <dir>, discover looks in the discoverPaths listed in
~/.config/clonager/settings.json (which the Home Manager module writes from
programs.clonager.discoverPaths), skipping any that don't exist.

If the config is installed by the Home Manager module with configSource set,
discover edits that source (e.g. a file in your nix-darwin repo, or one kept
encrypted there, through its decrypt and encrypt commands) instead of the
read-only installed copy. Review the diff there, then rebuild to apply it.`,
	RunE: runDiscover,
}

func init() {
	discoverCmd.Flags().BoolVarP(&discoverFlags.dryRun, "dry-run", "n", false, "show what would be added without changing the config")
	discoverCmd.Flags().IntVarP(&discoverFlags.depth, "depth", "d", defaultDepth, "how many folders deep to look below each <dir>")
	rootCmd.AddCommand(discoverCmd)
}

func runDiscover(cmd *cobra.Command, args []string) error {
	cfg, fromSource, err := editableConfig()
	if err != nil {
		return err
	}
	if err := cfg.Writable(); !discoverFlags.dryRun && errors.Is(err, config.ErrReadOnly) {
		return fmt.Errorf("%w. If Home Manager installs it, set programs.clonager.configSource "+
			"(to the file in your checkout, or commands to decrypt and encrypt it), "+
			"and discover will edit that instead", err)
	}

	dirs, err := discoverDirs(args)
	if err != nil {
		return err
	}

	type row struct{ path, detail, notes string }
	var added, skipped []row
	known := 0
	for _, dir := range dirs {
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
				skipped = append(skipped, row{path: path, detail: err.Error()})
				continue
			}
			var notes []string
			if repo.GitButler {
				notes = append(notes, styleGB.Render("gitbutler"))
			}
			for _, r := range repo.Remotes {
				notes = append(notes, styleBranch.Render("+"+r.Name))
			}
			added = append(added, row{path, repo.URL, strings.Join(notes, " ")})
		}
	}

	pathW := 0
	for _, r := range append(added, skipped...) {
		pathW = max(pathW, len(config.TildePath(r.path)))
	}
	printRows := func(heading, icon string, rows []row) {
		if len(rows) == 0 {
			return
		}
		lipgloss.Println(styleHeading.Render(heading))
		for _, r := range rows {
			path := fileLink(r.path, config.TildePath(r.path))
			line := fmt.Sprintf("  %s %s  %s  %s", icon, padRight(path, pathW), styleDim.Render(r.detail), r.notes)
			lipgloss.Println(strings.TrimRight(line, " "))
		}
		lipgloss.Println()
	}
	verb := "Added to "
	if discoverFlags.dryRun {
		verb = "Would add to "
	}
	printRows(verb+config.TildePath(cfg.Path), styleOK.Render("+"), added)
	printRows("Skipped", styleWarn.Render("!"), skipped)
	lipgloss.Println(styleDim.Render(fmt.Sprintf("%d new · %d already configured · %d skipped", len(added), known, len(skipped))))

	if discoverFlags.dryRun || len(added) == 0 {
		return nil
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if fromSource {
		lipgloss.Println(styleDim.Render("Rebuild (e.g. darwin-rebuild switch) to apply it."))
	}
	return nil
}

// discoverDirs returns the dirs to look in: those given, or else the
// discoverPaths in the settings that exist.
func discoverDirs(args []string) ([]string, error) {
	var dirs []string
	if len(args) > 0 {
		for _, arg := range args {
			dir, err := absDir(arg)
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, dir)
		}
		return dirs, nil
	}

	dirs, missing, err := settingsDirs()
	if err != nil {
		return nil, err
	}
	if len(dirs)+len(missing) == 0 {
		path, _ := config.SettingsPath()
		return nil, fmt.Errorf("give the dirs to look in, or list them as discoverPaths in %s "+
			"(programs.clonager.discoverPaths with the Home Manager module)", config.TildePath(path))
	}
	for _, p := range missing {
		lipgloss.Println(styleDim.Render("Skipping " + config.TildePath(p) + ", which doesn't exist"))
	}
	return dirs, nil
}

// settingsDirs returns the discoverPaths in the settings that exist, and
// separately those that don't.
func settingsDirs() (dirs, missing []string, err error) {
	settings, err := config.ReadSettings()
	if err != nil {
		return nil, nil, err
	}
	for _, p := range settings.DiscoverPaths {
		dir, err := absDir(p)
		if errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, p)
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		dirs = append(dirs, dir)
	}
	return dirs, missing, nil
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
