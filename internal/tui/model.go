// Package tui implements the bubbletea-based interactive picker.
//
// The TUI itself never execs ssh; instead it sets an Outcome on the model and
// quits. The caller (cmd/soda) then performs the syscall.Exec into ssh+tmux,
// handing the terminal off cleanly.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ottercoders/soda/internal/hosts"
	"github.com/ottercoders/soda/internal/scanner"
)

type mode int

const (
	modeList mode = iota
	modeSessions
	modeNameNew // prompting for the name of a new tmux session
	modePreview // showing `tmux capture-pane` output for a session
)

// Action is what the user picked when the TUI exits.
type Action int

const (
	ActionNone Action = iota
	ActionAttach
	ActionNew
)

// Outcome is the result of running the TUI. The caller inspects this after
// tea.Program.Run() returns and execs ssh accordingly.
type Outcome struct {
	Action  Action
	Host    string
	Session string // empty when ActionNew with default name
}

// Model is the bubbletea Model for the picker.
type Model struct {
	ctx     context.Context
	cancel  context.CancelFunc
	runner  scanner.Runner
	workers int

	all     []hosts.Host
	results map[string]scanner.Result // keyed by alias

	scanCh <-chan scanner.Result // current scan channel; nil when idle

	mode       mode
	cursor     int
	sessionCur int
	selected   string // alias being inspected in modeSessions / modeNameNew / modePreview
	nameInput  string // edit buffer for modeNameNew

	previewSession string // session name being previewed
	previewText    string // captured stdout (cleared while loading)
	previewErr     error  // last preview error, if any
	previewLoading bool

	width, height int
	status        string

	Outcome Outcome
}

// New returns a Model configured with the given hosts and runner.
// The model starts a scan immediately on Init().
func New(hs []hosts.Host, runner scanner.Runner, workers int) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	results := make(map[string]scanner.Result, len(hs))
	for _, h := range hs {
		results[h.Alias] = scanner.Result{Host: h, State: scanner.StateScanning}
	}
	return &Model{
		ctx:     ctx,
		cancel:  cancel,
		runner:  runner,
		workers: workers,
		all:     hs,
		results: results,
	}
}

// Init kicks off the first scan.
func (m *Model) Init() tea.Cmd {
	return m.startScan()
}

type scanResultMsg scanner.Result
type scanDoneMsg struct{}

// startScan resets all hosts to scanning and launches a fresh worker pool.
// It returns a Cmd that reads the first result; subsequent results are pulled
// by re-arming the same Cmd from Update.
func (m *Model) startScan() tea.Cmd {
	for _, h := range m.all {
		m.results[h.Alias] = scanner.Result{Host: h, State: scanner.StateScanning}
	}
	m.scanCh = scanner.Scan(m.ctx, m.all, m.runner, m.workers)
	return m.nextScanCmd()
}

// nextScanCmd reads one result from the current scan channel.
func (m *Model) nextScanCmd() tea.Cmd {
	ch := m.scanCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return scanDoneMsg{}
		}
		return scanResultMsg(r)
	}
}
