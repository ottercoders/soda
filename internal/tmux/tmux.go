// Package tmux parses the output of `tmux list-sessions` so the scanner can
// turn ssh stdout into structured Session records.
package tmux

import (
	"bufio"
	"strconv"
	"strings"
	"time"
)

// FieldSep is the byte used between fields in our custom
// `tmux list-sessions -F` format. tmux disallows `:` in session names
// (per tmux(1)), and the other fields we ask for are integers, so `:`
// is a guaranteed-safe separator that survives the ssh→remote-shell pipe
// without needing exotic quoting.
const FieldSep = ":"

// Format is the -F template the scanner sends to tmux. Keep in sync with Parse.
const Format = "#{session_name}" + FieldSep +
	"#{session_created}" + FieldSep +
	"#{session_attached}" + FieldSep +
	"#{session_windows}"

// Session is a single tmux session on a host.
type Session struct {
	Name     string
	Created  time.Time
	Attached bool
	Windows  int
}

// Parse converts the raw output of `tmux ls -F <Format>` into Session records.
// Blank input returns (nil, nil) — that's a reachable host with no sessions.
func Parse(output string) ([]Session, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var sessions []Session
	sc := bufio.NewScanner(strings.NewReader(output))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		parts := strings.Split(line, FieldSep)
		if len(parts) < 4 {
			continue // malformed; skip rather than fail the whole scan
		}
		s := Session{Name: parts[0]}
		if ts, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			s.Created = time.Unix(ts, 0)
		}
		// session_attached is a count (0 = none); treat anything > 0 as attached.
		if n, err := strconv.Atoi(parts[2]); err == nil {
			s.Attached = n > 0
		}
		if w, err := strconv.Atoi(parts[3]); err == nil {
			s.Windows = w
		}
		sessions = append(sessions, s)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}
