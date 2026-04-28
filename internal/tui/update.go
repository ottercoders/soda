package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ottercoders/soda/internal/scanner"
)

type previewMsg struct {
	host    string
	session string
	text    string
	err     error
}

// Update is the bubbletea reducer.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case scanResultMsg:
		r := scanner.Result(msg)
		m.results[r.Host.Alias] = r
		// Pull the next result.
		return m, m.nextScanCmd()

	case scanDoneMsg:
		m.scanCh = nil
		m.status = "scan complete"
		return m, nil

	case previewMsg:
		// Ignore stale results if the user navigated away.
		if m.mode != modePreview || msg.host != m.selected || msg.session != m.previewSession {
			return m, nil
		}
		m.previewLoading = false
		m.previewText = msg.text
		m.previewErr = msg.err
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeSessions:
			return m.updateSessions(msg)
		case modeNameNew:
			return m.updateNameNew(msg)
		case modePreview:
			return m.updatePreview(msg)
		}
	}
	return m, nil
}

func (m *Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		m.cancel()
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.all)-1 {
			m.cursor++
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.all) - 1
	case "r":
		m.status = "rescanning…"
		return m, m.startScan()
	case "n":
		return m.startNameNewForCursor()
	case "enter":
		return m.actOnSelectedHost()
	}
	return m, nil
}

// startNameNewForCursor opens the name-input prompt for the currently
// highlighted host, regardless of whether it already has sessions.
func (m *Model) startNameNewForCursor() (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.all) {
		return m, nil
	}
	m.selected = m.all[m.cursor].Alias
	m.nameInput = "main"
	m.mode = modeNameNew
	return m, nil
}

func (m *Model) updateSessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r, ok := m.results[m.selected]
	if !ok {
		m.mode = modeList
		return m, nil
	}
	switch msg.String() {
	case "q", "ctrl+c":
		m.cancel()
		return m, tea.Quit
	case "esc", "h", "left":
		m.mode = modeList
		m.sessionCur = 0
	case "up", "k":
		if m.sessionCur > 0 {
			m.sessionCur--
		}
	case "down", "j":
		if m.sessionCur < len(r.Sessions)-1 {
			m.sessionCur++
		}
	case "enter":
		s := r.Sessions[m.sessionCur]
		m.Outcome = Outcome{Action: ActionAttach, Host: m.selected, Session: s.Name}
		m.cancel()
		return m, tea.Quit
	case "n":
		// Create a brand-new session on this host without leaving the
		// session sub-list context.
		m.nameInput = "main"
		m.mode = modeNameNew
	case "p":
		s := r.Sessions[m.sessionCur]
		return m.startPreview(m.selected, s.Name)
	}
	return m, nil
}

// startPreview transitions to modePreview and kicks off a tmux capture-pane
// command in the background. The result arrives as a previewMsg.
func (m *Model) startPreview(host, session string) (tea.Model, tea.Cmd) {
	m.mode = modePreview
	m.previewSession = session
	m.previewText = ""
	m.previewErr = nil
	m.previewLoading = true
	return m, capturePaneCmd(host, session)
}

func capturePaneCmd(host, session string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		text, err := scanner.Preview(ctx, host, session)
		return previewMsg{host: host, session: session, text: text, err: err}
	}
}

func (m *Model) updatePreview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.cancel()
		return m, tea.Quit
	case "esc", "q", "p":
		m.mode = modeSessions
		m.previewText = ""
		m.previewErr = nil
		m.previewLoading = false
	case "enter":
		// Convenience: attach to the previewed session right from the modal.
		m.Outcome = Outcome{Action: ActionAttach, Host: m.selected, Session: m.previewSession}
		m.cancel()
		return m, tea.Quit
	case "r":
		// Re-fetch the preview.
		return m.startPreview(m.selected, m.previewSession)
	}
	return m, nil
}

func (m *Model) updateNameNew(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.cancel()
		return m, tea.Quit
	case "esc":
		m.mode = modeList
		m.nameInput = ""
	case "enter":
		name := strings.TrimSpace(m.nameInput)
		if name == "" {
			m.status = "session name cannot be empty"
			return m, nil
		}
		m.Outcome = Outcome{Action: ActionNew, Host: m.selected, Session: name}
		m.cancel()
		return m, tea.Quit
	case "backspace":
		if n := len(m.nameInput); n > 0 {
			m.nameInput = m.nameInput[:n-1]
		}
	case "ctrl+u":
		m.nameInput = ""
	default:
		// Accept printable single-rune key presses, excluding tmux-forbidden
		// characters (`:` and `.` are reserved for session targets).
		if len(msg.Runes) == 1 {
			r := msg.Runes[0]
			if r >= 0x20 && r != 0x7f && r != ':' && r != '.' {
				m.nameInput += string(r)
			}
		}
	}
	return m, nil
}

// actOnSelectedHost decides what to do when the user hits enter on a host row.
func (m *Model) actOnSelectedHost() (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.all) {
		return m, nil
	}
	h := m.all[m.cursor]
	r, ok := m.results[h.Alias]
	if !ok {
		return m, nil
	}
	switch r.State {
	case scanner.StateSessions:
		if len(r.Sessions) == 1 {
			m.Outcome = Outcome{Action: ActionAttach, Host: h.Alias, Session: r.Sessions[0].Name}
			m.cancel()
			return m, tea.Quit
		}
		m.selected = h.Alias
		m.sessionCur = 0
		m.mode = modeSessions
	case scanner.StateNoSessions:
		m.selected = h.Alias
		m.nameInput = "main"
		m.mode = modeNameNew
	case scanner.StateScanning:
		m.status = "still scanning " + h.Alias + "…"
	case scanner.StateUnreachable:
		if r.Err != nil {
			m.status = h.Alias + ": " + r.Err.Error()
		} else {
			m.status = h.Alias + ": unreachable"
		}
	}
	return m, nil
}
