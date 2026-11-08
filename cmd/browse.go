package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/cli"
	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var browseFlags struct {
	offline bool
	tags    []string
	sort    string
}

var browseCmd = &cobra.Command{
	Use:     "browse",
	Aliases: []string{"b"},
	Short:   "Browse the repos, to cd to one or open it in an app",
	Long:    helpText("browse"),
	Args:    cobra.NoArgs,
	RunE:    runBrowse,
}

func init() {
	browseCmd.Flags().BoolVarP(&browseFlags.offline, "offline", "o", false, "don't check GitHub (default from alwaysOffline in settings.json)")
	browseCmd.Flags().StringSliceVarP(&browseFlags.tags, "tag", "t", nil, "only repos with this tag (repeatable)")
	browseCmd.Flags().StringVarP(&browseFlags.sort, "sort", "s", "", "folder, or recent for the most recently changed first (default from browseSort in settings.json, else folder)")
	rootCmd.AddCommand(browseCmd)
}

// cdFileEnv names the file the shell integration (shell-init) has browse
// write the chosen folder to, for it to cd to once browse exits.
const cdFileEnv = "CLONAGER_CD_FILE"

func runBrowse(cmd *cobra.Command, args []string) error {
	settings, err := loadSettings()
	if err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	repos := filterByTags(cfg.Repos, browseFlags.tags)
	if len(repos) == 0 {
		if len(browseFlags.tags) > 0 {
			return errors.New("no repos tagged " + strings.Join(browseFlags.tags, " or "))
		}
		return errors.New("no repos configured in " + config.TildePath(cfg.Path))
	}
	sort := settings.BrowseSort
	if cmd.Flags().Changed("sort") {
		if !slices.Contains(config.BrowseSorts, browseFlags.sort) {
			return fmt.Errorf("--sort %q: must be one of %s", browseFlags.sort, strings.Join(config.BrowseSorts, ", "))
		}
		sort = browseFlags.sort
	}
	// It's drawn on stderr, so stdout only ever gets the chosen folder.
	if !liveTerminal(os.Stdin) || !liveTerminal(os.Stderr) {
		return errors.New("browse needs a terminal")
	}

	m := newBrowseModel(repos, !offline(cmd, browseFlags.offline, settings))
	m.recent = sort == "recent"
	m.applyFilter()
	if _, err := tea.NewProgram(m, tea.WithOutput(os.Stderr)).Run(); err != nil {
		return err
	}
	if m.chosen == "" {
		return nil
	}
	if file := os.Getenv(cdFileEnv); file != "" {
		return os.WriteFile(file, []byte(m.chosen), 0o600)
	}
	fmt.Println(m.chosen)
	if liveTerminal(os.Stdout) {
		fmt.Fprintln(cmd.ErrOrStderr(), styleDim.Render("To cd there instead, set up the shell integration: see "+progName()+" shell-init --help"))
	}
	return nil
}

// browseRow is one repo in browse's list. status is only read once it's
// been inspected, and isn't read at all while GitHub is being checked for it
// (in the background): rep is what's shown.
type browseRow struct {
	repo       config.Repo
	status     *repostatus.Status // nil until inspected
	rep        report
	checking   bool
	gitButler  bool      // in a GitButler workspace
	lastChange time.Time // Status.LastChange
	folder     string    // the end of its folder, e.g. "Projects/" (see folderLabels)
}

func (r browseRow) cloned() bool { return r.status != nil && !r.status.Missing }

type browseModel struct {
	rows  []browseRow
	forge bool
	sem   chan struct{} // limits how many repos are inspected or checked at once

	inspecting, checking int // repos still being inspected, and checked
	unreachable          unreachable

	filter    string
	filtering bool  // typing the filter
	recent    bool  // most recently changed first, rather than by folder
	visible   []int // the rows that match it, in the order shown
	cursor    int   // index into visible
	moved     bool  // whether the user has moved the cursor (or filtered)
	offset    int   // first list line shown

	width, height int
	frame         int    // spinner frame
	message       string // the result of the last action, shown in the footer
	lastClick     time.Time
	lastClickRow  int

	chosen string // the folder to cd to on exit
}

type (
	inspectedMsg struct {
		i      int
		status *repostatus.Status
	}
	checkedMsg    struct{ i int }
	browseTick    struct{}
	browseOpenMsg struct{ message string }
)

func newBrowseModel(repos []config.Repo, forge bool) *browseModel {
	m := &browseModel{
		forge:      forge,
		sem:        make(chan struct{}, max(4, runtime.NumCPU())),
		inspecting: len(repos),
	}
	labels := folderLabels(repos)
	for _, r := range repos {
		m.rows = append(m.rows, browseRow{repo: r, folder: labels[filepath.Dir(r.Path)]})
	}
	m.applyFilter()
	return m
}

func (m *browseModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.tick()}
	for i, r := range m.rows {
		cmds = append(cmds, func() tea.Msg {
			m.sem <- struct{}{}
			defer func() { <-m.sem }()
			return inspectedMsg{i, repostatus.Inspect(r.repo, repostatus.Options{})}
		})
	}
	return tea.Batch(cmds...)
}

func (m *browseModel) tick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return browseTick{} })
}

func (m *browseModel) busy() bool { return m.inspecting+m.checking > 0 }

func (m *browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case browseTick:
		m.frame++
		if m.busy() {
			cmd = m.tick()
		}
	case inspectedMsg:
		m.inspecting--
		row := &m.rows[msg.i]
		row.status = msg.status
		row.rep = buildReport(msg.status)
		row.gitButler = msg.status.GitButler.Mode == repostatus.GitButlerActive
		row.lastChange = msg.status.LastChange
		if m.recent {
			m.applyFilter() // to sort it into place
			if !m.moved {
				m.cursor = 0 // stay on the most recent until the user moves
			}
		}
		if m.forge && row.cloned() && msg.status.Err == nil {
			row.checking = true
			m.checking++
			s := msg.status
			cmd = func() tea.Msg {
				m.sem <- struct{}{}
				defer func() { <-m.sem }()
				s.CheckForge()
				return checkedMsg{msg.i}
			}
		}
	case checkedMsg:
		m.checking--
		row := &m.rows[msg.i]
		row.checking = false
		if s := row.status; s.ForgeErr != nil && m.unreachable.add(s.ForgeErr) {
			s.ForgeErr = nil // one warning for all of them, in the footer
		}
		row.rep = buildReport(row.status)
	case browseOpenMsg:
		m.message = msg.message
	case tea.KeyPressMsg:
		m.message = ""
		if m.filtering {
			cmd = m.filterKey(msg)
		} else {
			cmd = m.key(msg)
		}
	case tea.MouseClickMsg:
		cmd = m.click(msg.Mouse())
	case tea.MouseWheelMsg:
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			m.move(-1)
		case tea.MouseWheelDown:
			m.move(1)
		}
	}
	m.scroll()
	return m, cmd
}

func (m *browseModel) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-max(1, m.listHeight()-1))
	case "pgdown":
		m.move(max(1, m.listHeight()-1))
	case "home":
		m.move(-len(m.rows))
	case "end":
		m.move(len(m.rows))
	case "/":
		m.filtering = true
	case "s":
		m.recent = !m.recent
		m.moved = true
		m.applyFilter()
	case "enter", "c":
		return m.cd()
	case "e":
		return m.open(false)
	case "g":
		return m.open(true)
	}
	return nil
}

// filterKey handles keys while the filter's being typed: typing narrows the
// list, enter keeps the filter, and esc clears it.
func (m *browseModel) filterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "enter":
		m.filtering = false
	case "esc":
		m.filtering = false
		m.filter = ""
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	case "backspace":
		if m.filter != "" {
			r := []rune(m.filter)
			m.filter = string(r[:len(r)-1])
		}
	default:
		if msg.Text == "" {
			return nil
		}
		m.filter += msg.Text
	}
	m.applyFilter()
	return nil
}

// click selects the repo clicked, and cds to it if it was already selected
// and clicked again straight away (a double click).
func (m *browseModel) click(mouse tea.Mouse) tea.Cmd {
	if mouse.Button != tea.MouseLeft {
		return nil
	}
	_, lineRows := m.listLines()
	line := m.offset + mouse.Y - browseHeaderLines
	if mouse.Y < browseHeaderLines || line >= len(lineRows) || mouse.X >= m.listWidth() || lineRows[line] < 0 {
		return nil
	}
	v := lineRows[line]
	double := v == m.cursor && v == m.lastClickRow && time.Since(m.lastClick) < 400*time.Millisecond
	m.cursor, m.lastClickRow, m.lastClick = v, v, time.Now()
	m.moved = true
	if double {
		return m.cd()
	}
	return nil
}

func (m *browseModel) move(by int) {
	m.moved = true
	m.cursor = max(0, min(len(m.visible)-1, m.cursor+by))
}

// applyFilter shows the rows whose name, folder or tags contain the filter,
// ignoring case, in config order (which groups them by folder) or most
// recently changed first, keeping the selected repo selected if it still
// matches.
func (m *browseModel) applyFilter() {
	var selected = -1
	if m.cursor < len(m.visible) {
		selected = m.visible[m.cursor]
	}
	f := strings.ToLower(m.filter)
	m.visible = m.visible[:0]
	for i, r := range m.rows {
		text := strings.ToLower(config.TildePath(r.repo.Path) + " " + strings.Join(r.repo.Tags, " "))
		if strings.Contains(text, f) {
			m.visible = append(m.visible, i)
		}
	}
	if m.recent {
		// Repos not inspected yet, or with no known change, go last.
		slices.SortStableFunc(m.visible, func(a, b int) int {
			return m.rows[b].lastChange.Compare(m.rows[a].lastChange)
		})
	}
	m.cursor = max(0, slices.Index(m.visible, selected))
}

// selected is the selected row, or nil if the filter matches nothing.
func (m *browseModel) selected() *browseRow {
	if m.cursor >= len(m.visible) {
		return nil
	}
	return &m.rows[m.visible[m.cursor]]
}

// cd picks the selected repo's folder and exits, for runBrowse to hand to
// the shell.
func (m *browseModel) cd() tea.Cmd {
	r := m.selected()
	switch {
	case r == nil:
		return nil
	case r.status == nil:
		m.message = "Still inspecting " + r.repo.Name() + "."
		return nil
	case !r.cloned():
		m.message = r.repo.Name() + " isn't cloned (see " + progName() + " clone)."
		return nil
	}
	m.chosen = r.repo.Path
	return tea.Quit
}

// open opens the selected repo in GitButler, or the openIn app.
func (m *browseModel) open(gitButler bool) tea.Cmd {
	r := m.selected()
	if r == nil || !r.cloned() {
		return nil
	}
	name := r.repo.Name()
	var u, app string
	switch {
	case gitButler && !r.gitButler:
		m.message = name + " isn't in a GitButler workspace."
		return nil
	case gitButler:
		u, app = gitButlerURL(r.repo.Path), "GitButler"
	case openIn == "none":
		m.message = "openIn is none in settings.json, so there's nothing to open " + name + " in."
		return nil
	default:
		u, app = folderURL(r.repo.Path), openInName()
	}
	return func() tea.Msg {
		if err := cli.Open(u); err != nil {
			return browseOpenMsg{styleError.Render(err.Error())}
		}
		return browseOpenMsg{"Opened " + name + " in " + app + "."}
	}
}

// The screen is a header line, the list (with the selected repo's details
// beside it, or below it in a narrow terminal), and two footer lines: what
// happened, and the keys.
const (
	browseHeaderLines = 1
	browseFooterLines = 2
	browseWide        = 80 // columns from which the details go beside the list
)

func (m *browseModel) wide() bool { return m.width >= browseWide }

func (m *browseModel) listHeight() int {
	h := max(1, m.height-browseHeaderLines-browseFooterLines)
	if !m.wide() {
		h = max(1, h/2)
	}
	return h
}

func (m *browseModel) listWidth() int {
	if !m.wide() {
		return m.width
	}
	// As listLines lays it out: marker, age when sorted by it, icon, the
	// name padded to the longest, and the branch column.
	nameW, headW := 0, 0
	for _, r := range m.rows {
		nameW = max(nameW, lipgloss.Width(m.rowName(r)))
		headW = max(headW, lipgloss.Width(r.rep.head))
	}
	w := 4 + nameW
	if m.recent {
		w += m.ageWidth() + 1
	}
	if headW > 0 {
		w += 2 + headW
	}
	return min(w, m.width*45/100)
}

// listLines renders the list: a heading for each folder, then its repos,
// or when sorted by most recent change, just the repos, each with how long
// ago that was. lineRows gives, for each line, its index in visible (-1 for
// headings).
func (m *browseModel) listLines() (lines []string, lineRows []int) {
	nameW, ageW := 0, m.ageWidth()
	for _, i := range m.visible {
		nameW = max(nameW, lipgloss.Width(m.rowName(m.rows[i])))
	}
	// Before the name: the marker, the age when sorted by it, and the icon.
	// The names are padded to line up, but no further than the list is wide,
	// and the branch column comes after them only if there's enough room
	// left to show some of it (it's in the details too).
	prefixW := 2 + 2
	if m.recent {
		prefixW += ageW + 1
	}
	nameW = max(1, min(nameW, m.listWidth()-prefixW))
	showHead := m.listWidth()-prefixW-nameW-2 >= minHeadWidth
	fit := func(s string) string { return padRight(ansi.Truncate(s, nameW, "…"), nameW) }
	group := ""
	for v, i := range m.visible {
		r := m.rows[i]
		if dir := config.TildePath(filepath.Dir(r.repo.Path)); dir != group && !m.recent {
			if group != "" {
				lines, lineRows = append(lines, ""), append(lineRows, -1)
			}
			lines, lineRows = append(lines, styleHeading.Render(dir)), append(lineRows, -1)
			group = dir
		}
		// Without folder headings, the folder goes before the name, so
		// repos with the same name can be told apart.
		var name string
		switch {
		case v == m.cursor:
			name = lipgloss.NewStyle().Reverse(true).Render(fit(m.rowName(r)))
		case m.recent:
			name = fit(styleDim.Render(r.folder) + r.repo.Name())
		default:
			name = fit(r.repo.Name())
		}
		marker := "  "
		if v == m.cursor {
			marker = "▸ "
		}
		line := marker
		if m.recent {
			line += padLeft(styleDim.Render(changeAge(r.lastChange)), ageW) + " "
		}
		line += m.icon(r) + " " + name
		if r.rep.head != "" && showHead {
			line += "  " + r.rep.head
		}
		lines, lineRows = append(lines, line), append(lineRows, v)
	}
	return lines, lineRows
}

// minHeadWidth is the fewest columns the list shows a branch in, truncated.
const minHeadWidth = 4

// rowName is the repo's name as the list shows it: after its folder when
// sorted by most recent change, as there are no folder headings then.
func (m *browseModel) rowName(r browseRow) string {
	if m.recent {
		return r.folder + r.repo.Name()
	}
	return r.repo.Name()
}

// folderLabels gives each of the repos' folders a short label for the list
// when it has no folder headings: as few of the folder's last parts as tell
// it apart from the others, with a trailing slash ("Projects/", or
// "Work/Projects/" and "~/Projects/" where both are there).
func folderLabels(repos []config.Repo) map[string]string {
	var dirs []string
	for _, r := range repos {
		if dir := filepath.Dir(r.Path); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	parts := func(dir string) []string {
		return strings.Split(config.TildePath(dir), string(filepath.Separator))
	}
	suffix := func(dir string, n int) string {
		p := parts(dir)
		return strings.Join(p[max(0, len(p)-n):], string(filepath.Separator)) + string(filepath.Separator)
	}
	labels := map[string]string{}
	for _, dir := range dirs {
		for n := 1; ; n++ {
			label := suffix(dir, n)
			clash := false
			for _, other := range dirs {
				if other != dir && suffix(other, n) == label {
					clash = true
					break
				}
			}
			if !clash || n >= len(parts(dir)) {
				labels[dir] = label
				break
			}
		}
	}
	return labels
}

// changeAge is how long ago a repo last changed, in short form, or "" if
// that isn't known.
func changeAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return shortAge(max(0, time.Since(t)))
}

// ageWidth is the width of the age column when sorted by most recent change.
func (m *browseModel) ageWidth() int {
	w := 0
	for _, r := range m.rows {
		w = max(w, len(changeAge(r.lastChange)))
	}
	return w
}

// icon is a repo's status: a spinner while it's being inspected or checked.
func (m *browseModel) icon(r browseRow) string {
	if r.status == nil || r.checking {
		frame := styleDim.Render(spinnerFrames[m.frame%len(spinnerFrames)])
		if r.status == nil {
			return frame
		}
		return severityStyle(r.rep.severity()).Render(spinnerFrames[m.frame%len(spinnerFrames)])
	}
	switch r.rep.severity() {
	case sevError:
		return styleError.Render("✗")
	case sevWarn:
		return styleWarn.Render("●")
	}
	return styleOK.Render("✓")
}

// scroll keeps the selected repo's line on screen, with its folder's heading
// too where it fits.
func (m *browseModel) scroll() {
	_, lineRows := m.listLines()
	line := slices.Index(lineRows, m.cursor)
	if line < 0 {
		m.offset = 0
		return
	}
	h := m.listHeight()
	top := line
	for top > 0 && lineRows[top-1] < 0 && line-top < 2 {
		top--
	}
	if top < m.offset {
		m.offset = top
	}
	if line >= m.offset+h {
		m.offset = line - h + 1
	}
	m.offset = max(0, min(m.offset, len(lineRows)-h))
}

// details describes the selected repo, in lines at most width wide.
func (m *browseModel) details(width int) []string {
	r := m.selected()
	if r == nil {
		return []string{styleDim.Render("No repos match the filter.")}
	}
	var w strings.Builder
	fmt.Fprintln(&w, styleHeading.Render(r.repo.Name()))
	fmt.Fprintln(&w, styleDim.Render(config.TildePath(r.repo.Path)))
	fmt.Fprintln(&w, styleDim.Render(r.repo.URL))
	if len(r.repo.Tags) > 0 {
		fmt.Fprintln(&w, styleDim.Render("tags: "+strings.Join(r.repo.Tags, ", ")))
	}
	fmt.Fprintln(&w)
	if r.status == nil {
		fmt.Fprintln(&w, styleDim.Render("Inspecting…"))
	} else {
		if r.rep.head != "" || r.rep.fetch != "" {
			line := r.rep.head
			if r.rep.fetch != "" {
				line += styleDim.Render("  fetched ") + r.rep.fetch + styleDim.Render(" ago")
			}
			if age := changeAge(r.lastChange); age != "" {
				line += styleDim.Render("  changed " + age + " ago")
			}
			fmt.Fprintln(&w, strings.TrimLeft(line, " "))
		}
		for _, i := range r.rep.issues {
			fmt.Fprintln(&w, severityStyle(i.sev).Render("• "+i.text))
			for _, d := range i.details {
				fmt.Fprintln(&w, "    "+d)
			}
		}
		if len(r.rep.issues) == 0 {
			fmt.Fprintln(&w, styleOK.Render("Nothing to do"))
		}
		if r.checking {
			fmt.Fprintln(&w, styleDim.Render("Checking GitHub…"))
		}
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(w.String(), "\n"), "\n") {
		// lipgloss.Wrap closes styles at each break and reopens them on the
		// next line (ansi.Wrap doesn't), so none leaks into the list beside it.
		lines = append(lines, strings.Split(lipgloss.Wrap(l, width, ""), "\n")...)
	}
	return lines
}

func (m *browseModel) header() string {
	parts := []string{styleHeading.Render(progName()), plural(len(m.rows), "repo", "repos")}
	if m.filter != "" || m.filtering {
		f := "/" + m.filter
		if m.filtering {
			f += "█"
		}
		parts = append(parts, styleBranch.Render(f)+styleDim.Render(fmt.Sprintf(" (%d)", len(m.visible))))
	}
	if m.recent {
		parts = append(parts, "recently changed first")
	}
	spinner := styleDim.Render(spinnerFrames[m.frame%len(spinnerFrames)] + " ")
	switch {
	case m.inspecting > 0:
		parts = append(parts, spinner+styleDim.Render(fmt.Sprintf("Inspecting… %d/%d", len(m.rows)-m.inspecting, len(m.rows))))
	case m.checking > 0:
		parts = append(parts, spinner+styleDim.Render(fmt.Sprintf("Checking GitHub… %d left", m.checking)))
	}
	return strings.Join(parts, styleDim.Render(" · "))
}

func (m *browseModel) footer() (message, keys string) {
	message = m.message
	if message == "" && len(m.unreachable) > 0 {
		message = forgeWarningLine(m.unreachable.String())
	}
	if m.filtering {
		return message, styleDim.Render("type to filter · enter keep · esc clear")
	}
	var hints []string
	if r := m.selected(); r != nil && r.cloned() {
		hints = append(hints, "enter cd")
		if openIn != "none" {
			hints = append(hints, "e "+openInName())
		}
		if r.gitButler {
			hints = append(hints, "g GitButler")
		}
	}
	sort := "s recent first"
	if m.recent {
		sort = "s by folder"
	}
	hints = append(hints, "/ filter", sort, "q quit")
	return message, styleDim.Render(strings.Join(hints, " · "))
}

func (m *browseModel) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = progName() + " browse"
	if m.width == 0 {
		return v
	}

	lines, _ := m.listLines()
	h := m.listHeight()
	lines = lines[min(m.offset, len(lines)):]
	lines = lines[:min(h, len(lines))]
	listW := m.listWidth()
	for i, l := range lines {
		lines[i] = padRight(ansi.Truncate(l, listW, "…"), listW)
	}

	var body []string
	if m.wide() {
		const gap = " │ "
		detailW := max(1, m.width-listW-lipgloss.Width(gap))
		details := m.details(detailW)
		total := m.height - browseHeaderLines - browseFooterLines
		for i := range total {
			left := strings.Repeat(" ", listW)
			if i < len(lines) {
				left = lines[i]
			}
			right := ""
			if i < len(details) {
				right = details[i]
			}
			body = append(body, left+styleDim.Render(gap)+right)
		}
	} else {
		body = append(body, lines...)
		for len(body) < h {
			body = append(body, "")
		}
		body = append(body, styleDim.Render(strings.Repeat("─", m.width)))
		details := m.details(m.width)
		rest := max(0, m.height-browseHeaderLines-browseFooterLines-len(body))
		body = append(body, details[:min(rest, len(details))]...)
	}

	message, keys := m.footer()
	out := []string{ansi.Truncate(m.header(), m.width, "…")}
	out = append(out, body...)
	out = append(out, ansi.Truncate(message, m.width, "…"), ansi.Truncate(keys, m.width, "…"))
	v.SetContent(strings.Join(out[:min(len(out), m.height)], "\n"))
	return v
}
