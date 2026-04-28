package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/ottercoders/soda/internal/hosts"
	"github.com/ottercoders/soda/internal/tmux"
)

type stubRunner struct {
	stdout map[string]string
	stderr map[string]string
	code   map[string]int
}

func (s stubRunner) Run(_ context.Context, alias string) (string, string, int, error) {
	return s.stdout[alias], s.stderr[alias], s.code[alias], nil
}

func TestScan_Classification(t *testing.T) {
	sessLine := "main" + tmux.FieldSep + "1700000000" + tmux.FieldSep + "0" + tmux.FieldSep + "1"

	stub := stubRunner{
		stdout: map[string]string{
			"with-sessions": sessLine,
			"empty-tmux":    "",
			"no-server":     "",
			"broken":        "",
		},
		stderr: map[string]string{
			"no-server": "no server running on /tmp/tmux-1000/default",
			"broken":    "ssh: connect to host broken port 22: Connection refused",
		},
		code: map[string]int{
			"with-sessions": 0,
			"empty-tmux":    0,
			"no-server":     1,
			"broken":        255,
		},
	}

	hs := []hosts.Host{
		{Alias: "with-sessions"},
		{Alias: "empty-tmux"},
		{Alias: "no-server"},
		{Alias: "broken"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got := map[string]State{}
	for r := range Scan(ctx, hs, stub, 4) {
		got[r.Host.Alias] = r.State
	}

	want := map[string]State{
		"with-sessions": StateSessions,
		"empty-tmux":    StateNoSessions,
		"no-server":     StateNoSessions,
		"broken":        StateUnreachable,
	}
	for alias, w := range want {
		if got[alias] != w {
			t.Errorf("%s: state = %v; want %v", alias, got[alias], w)
		}
	}
}

func TestScan_EmptyHostList(t *testing.T) {
	ch := Scan(context.Background(), nil, stubRunner{}, 4)
	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("got %d results; want 0", count)
	}
}

