package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// liveTerminal reports whether f is a terminal that a spinner can be drawn
// in (and, for status, output redrawn).
func liveTerminal(f *os.File) bool {
	return term.IsTerminal(f.Fd()) && os.Getenv("TERM") != "dumb"
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spin draws a spinner and label() on the current line of w, a terminal
// width columns wide (0 if unknown), until finished is closed or Ctrl-C is
// pressed. It clears the line when it stops, leaving the cursor at its
// start, and reports whether it was interrupted, and whether the terminal
// was resized meanwhile.
func spin(w io.Writer, width int, label func() string, finished <-chan struct{}) (interrupted, resized bool) {
	resizes := make(chan os.Signal, 1)
	signal.Notify(resizes, syscall.SIGWINCH)
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(resizes)
	defer signal.Stop(interrupts)

	fmt.Fprint(w, ansi.HideCursor)
	defer fmt.Fprint(w, "\r"+ansi.EraseEntireLine+ansi.ShowCursor)
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	for frame := 0; ; frame++ {
		line := styleDim.Render(spinnerFrames[frame%len(spinnerFrames)] + " " + label())
		if width > 0 {
			line = ansi.Truncate(line, width-1, "")
		}
		fmt.Fprint(w, "\r"+ansi.EraseEntireLine+line)
		select {
		case <-tick.C:
		case <-resizes:
			resized = true
		case <-interrupts:
			return true, resized
		case <-finished:
			return false, resized
		}
	}
}
