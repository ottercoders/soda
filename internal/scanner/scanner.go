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

// Preview captures the visible content of the current pane in the named tmux
// session on the given host using `tmux capture-pane -p`. It uses the same
// ssh options as a scan (BatchMode + short ConnectTimeout) so a flaky host
// can't hang the TUI.
func Preview(ctx context.Context, alias, session string) (string, error) {
	if alias == "" || session == "" {
		return "", errors.New("preview: empty alias or session")
	}
	remote := "tmux capture-pane -p -J -t " + shellQuote(session)
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=3",
		"-o", "StrictHostKeyChecking=accept-new",
		alias,
		remote,
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("ssh exit %d: %s", ee.ExitCode(), strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}

func scanOne(ctx context.Context, h hosts.Host, runner Runner) Result {
	stdout, stderr, code, err := runner.Run(ctx, h.Alias)
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
