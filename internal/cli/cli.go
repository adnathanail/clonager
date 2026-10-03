// Package cli is the only place clonager runs external programs.
//
// clonager only ever reads: it never changes a repo, a remote or a setting
// (prune prints commands for the user to run instead). To keep it that way,
// every call is checked against an allowlist of read-only subcommands below,
// and anything not on it is refused before it runs. Add to the list only
// subcommands, and flags, that can't modify anything. (ConfigHook, for the
// user's own commands that read and store clonager's config, is the one
// exception.)
package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// A check validates a subcommand's arguments (those after the subcommand).
// nil means any arguments are fine.
type check func(args []string) error

// allowed lists, per program, the subcommands clonager may run. gh's are two
// words where the first is a command group (e.g. "pr list").
var allowed = map[string]map[string]check{
	"git": {
		"diff":         gitNoOutputFile,
		"for-each-ref": nil,
		"log":          gitNoOutputFile,
		"ls-remote":    gitNoUploadPack, // only reads the remote's refs
		"merge-base":   nil,
		"patch-id":     nil,
		"rev-list":     nil,
		"rev-parse":    nil,
		"status":       nil, // run with GIT_OPTIONAL_LOCKS=0, so it doesn't refresh the index
		"config":       firstArgIn("--get", "--get-all", "--get-regexp"),
		"remote":       noArgs,          // bare `git remote` lists remotes
		"stash":        exactly("list"), // the rest of stash modifies
		"symbolic-ref": atMostPositional(1),
	},
	"gh": {
		"api":     ghGetOnly,
		"pr list": nil,
	},
	"but": {
		"status": nil,
	},
}

// Git runs git in dir and returns its stdout with trailing newlines trimmed.
func Git(dir string, args ...string) (string, error) {
	return GitStdin(dir, "", args...)
}

// GitStdin is Git with the given stdin.
func GitStdin(dir, stdin string, args ...string) (string, error) {
	out, err := run("git", dir, stdin, nil, args, "-C", dir)
	return strings.TrimRight(string(out), "\n"), err
}

// CanRead reports whether the repo at url exists and can be read, by listing
// its HEAD with git ls-remote: nil, or why not. It goes over the network.
//
// Unless the user has their own SSH command (GIT_SSH_COMMAND, GIT_SSH or
// core.sshCommand), ssh runs in batch mode, so an unknown host key or a key
// needing a passphrase fails rather than waiting at a prompt.
func CanRead(url string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var env []string
	if os.Getenv("GIT_SSH_COMMAND") == "" && os.Getenv("GIT_SSH") == "" {
		if own, _ := Git(home, "config", "--get", "core.sshCommand"); own == "" {
			env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10")
		}
	}
	_, err = run("git", home, "", env, []string{"ls-remote", url, "HEAD"}, "-C", home)
	return err
}

// GH runs the GitHub CLI and returns its stdout.
func GH(args ...string) ([]byte, error) {
	return run("gh", "", "", nil, args)
}

// But runs GitButler's CLI in dir and returns its stdout.
func But(dir string, args ...string) ([]byte, error) {
	return run("but", dir, "", nil, args)
}

// Installed reports whether program is on the PATH.
func Installed(program string) bool {
	_, err := exec.LookPath(program)
	return err == nil
}

// ConfigHook runs one of the user's own commands for reading or writing
// clonager's config (programs.clonager.configSource's decrypt and encrypt in
// the Home Manager module), with stdin as its input, and returns its stdout.
//
// This is the one deliberate exception to the allowlist: the command is the
// user's, from their own configuration, and it's only ever run to read or
// store clonager's config, which is the one file clonager may change. It
// mustn't prompt: stdin is the given input (or empty), not the terminal.
func ConfigHook(command string, stdin []byte) ([]byte, error) {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s: %w: %s", command, err, msg)
		}
		return nil, fmt.Errorf("%s: %w", command, err)
	}
	return stdout.Bytes(), nil
}

// ErrNotAllowed is returned for calls not on the allowlist.
var ErrNotAllowed = errors.New("not an allowed read-only command")

// Check reports whether program with args is allowed to run.
func Check(program string, args []string) error {
	subs, ok := allowed[program]
	if !ok {
		return fmt.Errorf("%s: %w", program, ErrNotAllowed)
	}
	if len(args) == 0 {
		return fmt.Errorf("%s with no subcommand: %w", program, ErrNotAllowed)
	}
	name, rest := args[0], args[1:]
	if strings.HasPrefix(name, "-") {
		// Options before the subcommand (git -c, gh -R, ...) could change
		// what it does; the helpers add the ones clonager needs.
		return fmt.Errorf("%s %s: options must come after the subcommand: %w", program, name, ErrNotAllowed)
	}
	c, ok := subs[name]
	if !ok && len(rest) > 0 {
		name, rest = name+" "+rest[0], rest[1:]
		c, ok = subs[name]
	}
	if !ok {
		return fmt.Errorf("%s %s: %w", program, args[0], ErrNotAllowed)
	}
	if c != nil {
		if err := c(rest); err != nil {
			return fmt.Errorf("%s %s: %v: %w", program, name, err, ErrNotAllowed)
		}
	}
	return nil
}

// run checks and runs program. prefix is inserted before args (after the
// check), for options like git -C that the helpers supply, and env is added
// to the environment.
func run(program, dir, stdin string, env, args []string, prefix ...string) ([]byte, error) {
	if err := Check(program, args); err != nil {
		return nil, err
	}
	cmd := exec.Command(program, append(prefix, args...)...)
	if dir != "" && program != "git" { // git gets -C instead
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_OPTIONAL_LOCKS=0",  // don't take locks (or refresh the index) that could block the user's git
		"GIT_TERMINAL_PROMPT=0", // never prompt for credentials
		"GH_PROMPT_DISABLED=1",  // or anything else
		"GH_NO_UPDATE_NOTIFIER=1",
	)
	cmd.Env = append(cmd.Env, env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		name := program + " " + args[0]
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			line, _, _ := strings.Cut(msg, "\n")
			return stdout.Bytes(), fmt.Errorf("%s: %s", name, line)
		}
		return stdout.Bytes(), fmt.Errorf("%s: %w", name, err)
	}
	return stdout.Bytes(), nil
}

func noArgs(args []string) error {
	if len(args) > 0 {
		return errors.New("takes no arguments")
	}
	return nil
}

func exactly(want ...string) check {
	return func(args []string) error {
		if !slices.Equal(args, want) {
			return fmt.Errorf("only %q is allowed", strings.Join(want, " "))
		}
		return nil
	}
}

func firstArgIn(flags ...string) check {
	return func(args []string) error {
		if len(args) == 0 || !slices.Contains(flags, args[0]) {
			return fmt.Errorf("must start with one of %s", strings.Join(flags, ", "))
		}
		return nil
	}
}

func atMostPositional(n int) check {
	return func(args []string) error {
		count := 0
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				count++
			}
		}
		if count > n {
			return fmt.Errorf("at most %d positional argument(s)", n) // e.g. symbolic-ref NAME REF sets NAME
		}
		return nil
	}
}

// gitNoOutputFile refuses --output, with which diff and log write files.
func gitNoOutputFile(args []string) error {
	for _, a := range args {
		if a == "--output" || strings.HasPrefix(a, "--output=") {
			return errors.New("--output writes a file")
		}
	}
	return nil
}

// gitNoUploadPack refuses ls-remote's --upload-pack, which runs a command
// of its choosing in place of git-upload-pack.
func gitNoUploadPack(args []string) error {
	for _, a := range args {
		if a == "-u" || strings.HasPrefix(a, "--upload-pack") {
			return errors.New("--upload-pack runs a command")
		}
	}
	return nil
}

// ghGetOnly keeps gh api to GET requests: an explicit method, or any request
// body or field (which makes gh send a POST), is refused.
func ghGetOnly(args []string) error {
	for i, a := range args {
		switch {
		case a == "-X" || a == "--method":
			if i+1 >= len(args) || !strings.EqualFold(args[i+1], "GET") {
				return errors.New("only GET requests")
			}
		case strings.HasPrefix(a, "--method="):
			if !strings.EqualFold(strings.TrimPrefix(a, "--method="), "GET") {
				return errors.New("only GET requests")
			}
		case strings.HasPrefix(a, "-X") && len(a) > 2:
			if !strings.EqualFold(a[2:], "GET") {
				return errors.New("only GET requests")
			}
		case a == "-f" || a == "-F" || a == "--field" || a == "--raw-field" || a == "--input" ||
			strings.HasPrefix(a, "--field=") || strings.HasPrefix(a, "--raw-field=") || strings.HasPrefix(a, "--input=") ||
			(strings.HasPrefix(a, "-f") && len(a) > 2) || (strings.HasPrefix(a, "-F") && len(a) > 2):
			return errors.New("request fields and bodies make gh send a POST")
		}
	}
	return nil
}
