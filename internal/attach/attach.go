// Package attach hands the controlling terminal to ssh+tmux by replacing
// the soda process via execve. After tmux detaches, the user lands back in
// their original shell instead of the soda TUI.
package attach

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"

	"github.com/ottercoders/soda/internal/hosts"
)

// Attach replaces the current process with `ssh -t <host> tmux attach -t <name>`,
// or — when host is the local machine — directly with `tmux attach -t <name>`.
// Returns only on error; on success this function does not return.
func Attach(host, session string) error {
	if host == "" {
		return fmt.Errorf("attach: empty host")
	}
	if hosts.IsLocalhost(host) {
		args := []string{"attach"}
		if session != "" {
			args = []string{"attach", "-t", session}
		}
		return execLocal("tmux", args...)
	}
	tmuxCmd := "tmux attach"
	if session != "" {
		tmuxCmd = fmt.Sprintf("tmux attach -t %s", shellQuote(session))
	}
	return execSSH(host, tmuxCmd)
}

// New replaces the current process with `ssh -t <host> tmux new -A -s <name>`,
// or — when host is the local machine — directly with `tmux new -A -s <name>`.
// If name is empty, defaults to "main".
func New(host, name string) error {
	if host == "" {
		return fmt.Errorf("new: empty host")
	}
	if name == "" {
		name = "main"
	}
	if hosts.IsLocalhost(host) {
		// `new -A` attaches if the session already exists; harmless and friendlier.
		return execLocal("tmux", "new", "-A", "-s", name)
	}
	return execSSH(host, fmt.Sprintf("tmux new -A -s %s", shellQuote(name)))
}

func execSSH(host, remoteCmd string) error {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("locate ssh: %w", err)
	}
	argv := []string{sshPath, "-t", host, remoteCmd}
	// unix.Exec replaces this process; on success it does not return.
	if err := unix.Exec(sshPath, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec ssh: %w", err)
	}
	return nil
}

// execLocal replaces the current process with the named program, looked up on
// PATH. Used for direct local tmux attach/new without going through ssh.
func execLocal(name string, args ...string) error {
	bin, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("locate %s: %w", name, err)
	}
	argv := append([]string{bin}, args...)
	if err := unix.Exec(bin, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", name, err)
	}
	return nil
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
// The remote command is interpreted by the user's login shell on the server,
// so session names containing spaces or shell metacharacters need quoting.
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
