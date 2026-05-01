// Package scanner runs `ssh <host> tmux list-sessions` against many hosts in
// parallel and classifies each result as Sessions / NoSessions / Unreachable.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/ottercoders/soda/internal/hosts"
	"github.com/ottercoders/soda/internal/tmux"
)

// State enumerates the three terminal scan outcomes per host.
type State int

const (
	StateScanning State = iota
	StateSessions
	StateNoSessions
	StateUnreachable
)

func (s State) String() string {
	switch s {
	case StateScanning:
		return "scanning"
	case StateSessions:
		return "sessions"
	case StateNoSessions:
		return "no sessions"
	case StateUnreachable:
		return "unreachable"
	}
	return "unknown"
}

// Result is what we emit per host once its scan finishes (or fails).
type Result struct {
	Host     hosts.Host
	State    State
	Sessions []tmux.Session
	Err      error // populated for StateUnreachable; informational only
}

// Runner is the seam tests use to stub out exec.
type Runner interface {
	Run(ctx context.Context, alias string) (stdout string, stderr string, exitCode int, err error)
}

// SSHRunner shells out to the system `ssh` binary.
type SSHRunner struct {
	ConnectTimeoutSec int // default 3
	ServerAliveSec    int // default 2
}

func (r SSHRunner) Run(ctx context.Context, alias string) (string, string, int, error) {
	if r.ConnectTimeoutSec == 0 {
		r.ConnectTimeoutSec = 3
	}
	if r.ServerAliveSec == 0 {
		r.ServerAliveSec = 2
	}
	// ssh joins trailing args with spaces and runs the result via the remote
	// shell, so we must pre-quote the tmux format string. Without quoting, the
	// remote shell treats the leading `#` in `#{session_name}` as a comment
	// and tmux receives `-F` with no argument.
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=" + strconv.Itoa(r.ConnectTimeoutSec),
		"-o", "ServerAliveInterval=" + strconv.Itoa(r.ServerAliveSec),
		"-o", "StrictHostKeyChecking=accept-new",
		alias,
		"tmux list-sessions -F " + shellQuote(tmux.Format),
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
			err = nil // exit codes aren't transport errors
		}
	}
	return stdout.String(), stderr.String(), exitCode, err
}

// shellQuote wraps s in POSIX single quotes, escaping any embedded single
// quotes. The remote command is reassembled and re-parsed by the user's
// shell on the server, so anything containing shell metacharacters
// (including `#`, which starts a comment in non-interactive bash) needs
// to be quoted before it goes over the wire.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	out := make([]byte, 0, len(s)+2)
	out = append(out, '\'')
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\\', '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	out = append(out, '\'')
	return string(out)
}

// Scan fans out scans across hosts and returns a channel that emits one
// Result per host in completion order. The channel is closed when done.
// concurrency <= 0 defaults to min(16, len(hs)).
func Scan(ctx context.Context, hs []hosts.Host, runner Runner, concurrency int) <-chan Result {
	out := make(chan Result, len(hs))
	if len(hs) == 0 {
		close(out)
		return out
	}
	if concurrency <= 0 {
		concurrency = 16
	}
	if concurrency > len(hs) {
		concurrency = len(hs)
	}

	jobs := make(chan hosts.Host)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for h := range jobs {
				out <- scanOne(ctx, h, runner)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, h := range hs {
			select {
			case <-ctx.Done():
				return
			case jobs <- h:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// runLocalScan executes `tmux list-sessions -F <Format>` directly on the local
// machine. Mirrors the contract of Runner.Run (stdout, stderr, exitCode, err)
// so scanOne can branch transparently between local and remote.
func runLocalScan(ctx context.Context) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", tmux.Format)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
			runErr = nil
		}
	}
	return stdout.String(), stderr.String(), exitCode, runErr
}

// runRemoteOneShot runs a single remote command via ssh on the named host
// using the same BatchMode + short-timeout flags the scanner uses.
func runRemoteOneShot(ctx context.Context, alias, remoteCmd string) (stdout string, err error) {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=3",
		"-o", "StrictHostKeyChecking=accept-new",
		alias,
		remoteCmd,
	}
	return runAndCapture(exec.CommandContext(ctx, "ssh", args...), "ssh")
}

// runLocalOneShot runs a local command directly — used for the synthetic
// localhost host so we don't loop ssh back through itself.
func runLocalOneShot(ctx context.Context, name string, args ...string) (stdout string, err error) {
	return runAndCapture(exec.CommandContext(ctx, name, args...), name)
}

func runAndCapture(cmd *exec.Cmd, label string) (string, error) {
	var so, se strings.Builder
	cmd.Stdout = &so
	cmd.Stderr = &se
	if runErr := cmd.Run(); runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			return "", fmt.Errorf("%s exit %d: %s", label, ee.ExitCode(), strings.TrimSpace(se.String()))
		}
		return "", runErr
	}
	return so.String(), nil
}

// Preview captures the visible content of the current pane in the named tmux
// session on the given host using `tmux capture-pane -p`.
func Preview(ctx context.Context, alias, session string) (string, error) {
	if alias == "" || session == "" {
		return "", errors.New("preview: empty alias or session")
	}
	if hosts.IsLocalhost(alias) {
		return runLocalOneShot(ctx, "tmux", "capture-pane", "-p", "-J", "-t", session)
	}
	return runRemoteOneShot(ctx, alias, "tmux capture-pane -p -J -t "+shellQuote(session))
}

// RenameSession renames a tmux session on the remote host (or locally when
// alias is the synthetic localhost).
func RenameSession(ctx context.Context, alias, oldName, newName string) error {
	if alias == "" || oldName == "" || newName == "" {
		return errors.New("rename: empty alias/old/new")
	}
	if oldName == newName {
		return nil
	}
	if hosts.IsLocalhost(alias) {
		_, err := runLocalOneShot(ctx, "tmux", "rename-session", "-t", oldName, newName)
		return err
	}
	_, err := runRemoteOneShot(ctx, alias,
		"tmux rename-session -t "+shellQuote(oldName)+" "+shellQuote(newName))
	return err
}

func scanOne(ctx context.Context, h hosts.Host, runner Runner) Result {
	var stdout, stderr string
	var code int
	var err error
	if hosts.IsLocalhost(h.Alias) {
		stdout, stderr, code, err = runLocalScan(ctx)
	} else {
		stdout, stderr, code, err = runner.Run(ctx, h.Alias)
	}
	if err != nil {
		return Result{Host: h, State: StateUnreachable, Err: err}
	}
	stderrLow := strings.ToLower(stderr)
	switch code {
	case 0:
		sessions, perr := tmux.Parse(stdout)
		if perr != nil {
			return Result{Host: h, State: StateUnreachable, Err: perr}
		}
		if len(sessions) == 0 {
			return Result{Host: h, State: StateNoSessions}
		}
		return Result{Host: h, State: StateSessions, Sessions: sessions}
	case 1:
		// tmux exits 1 with "no server running on /tmp/tmux-...":
		// that's a reachable host with no tmux sessions, not unreachable.
		if strings.Contains(stderrLow, "no server running") ||
			strings.Contains(stderrLow, "error connecting to") {
			return Result{Host: h, State: StateNoSessions}
		}
		// Could also be tmux not installed; still informational.
		return Result{Host: h, State: StateUnreachable, Err: errors.New(strings.TrimSpace(stderr))}
	default:
		return Result{Host: h, State: StateUnreachable, Err: errors.New(strings.TrimSpace(stderr))}
	}
}
