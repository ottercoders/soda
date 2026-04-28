package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ottercoders/soda/internal/scanner"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	selectedRow   = lipgloss.NewStyle().Bold(true)
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).MarginTop(1)
	statusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	sessionHeader = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
)

// View renders the current Model.
func (m *Model) View() string {
	switch m.mode {
	case modeSessions:
		return m.viewSessions()
	case modeNameNew:
		return m.viewNameNew()
	case modePreview:
		return m.viewPreview()
	default:
		return m.viewList()
	}
}

func (m *Model) viewList() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("soda — tmux session reconnect"))
	b.WriteString("\n\n")

	if len(m.all) == 0 {
		b.WriteString(mutedStyle.Render("No hosts found in ~/.ssh/config."))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("q quit"))
		return b.String()
	}

	colWidth := 0
	for _, h := range m.all {
		if l := len(h.Alias); l > colWidth {
			colWidth = l
		}
	}
	if colWidth > 30 {
		colWidth = 30
	}

	for i, h := range m.all {
		r := m.results[h.Alias]
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("▶ ")
		}
		alias := padRight(h.Alias, colWidth)
		row := fmt.Sprintf("%s%s  %s", cursor, alias, renderState(r))
		if i == m.cursor {
			row = selectedRow.Render(row)
		}
		b.WriteString(row)
		b.WriteString("\n")
	}

	if m.status != "" {
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(m.status))
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("↑/↓ select   ⏎ attach   n new   r rescan   q quit"))
	return b.String()
}

func (m *Model) viewSessions() string {
	var b strings.Builder
	r, ok := m.results[m.selected]
	if !ok {
		return ""
	}
	b.WriteString(sessionHeader.Render(fmt.Sprintf("soda — %s", m.selected)))
	b.WriteString("\n\n")

	colName := 0
	for _, s := range r.Sessions {
		if l := len(s.Name); l > colName {
			colName = l
		}
	}

	for i, s := range r.Sessions {
		cursor := "  "
		if i == m.sessionCur {
			cursor = cursorStyle.Render("▶ ")
		}
		attached := ""
		if s.Attached {
			attached = warnStyle.Render(" (attached)")
		}
		windows := mutedStyle.Render(fmt.Sprintf("%d window%s", s.Windows, plural(s.Windows)))
		row := fmt.Sprintf("%s%s  %s%s", cursor, padRight(s.Name, colName), windows, attached)
		if i == m.sessionCur {
			row = selectedRow.Render(row)
		}
		b.WriteString(row)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ select   ⏎ attach   p preview   n new   esc back   q quit"))
	return b.String()
}

func (m *Model) viewPreview() string {
	var b strings.Builder
	b.WriteString(sessionHeader.Render(fmt.Sprintf("soda — %s : %s (preview)", m.selected, m.previewSession)))
	b.WriteString("\n\n")

	switch {
	case m.previewLoading:
		b.WriteString(mutedStyle.Render("loading preview…"))
	case m.previewErr != nil:
		b.WriteString(errStyle.Render("preview failed: " + m.previewErr.Error()))
	case strings.TrimSpace(m.previewText) == "":
		b.WriteString(mutedStyle.Render("(pane is empty)"))
	default:
		// Clip to roughly the available terminal height so the help line
		// remains visible. Keep the most recent lines (bottom of the pane).
		max := m.height - 6
		if max <= 0 {
			max = 20
		}
		lines := strings.Split(strings.TrimRight(m.previewText, "\n"), "\n")
		if len(lines) > max {
			lines = lines[len(lines)-max:]
		}
		b.WriteString(strings.Join(lines, "\n"))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("⏎ attach   r refresh   esc/p back   q quit"))
	return b.String()
}

func (m *Model) viewNameNew() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("soda"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("New tmux session on %s\n\n", selectedRow.Render(m.selected)))
	b.WriteString("name: ")
	b.WriteString(selectedRow.Render(m.nameInput))
	b.WriteString(cursorStyle.Render("▏"))
	if m.status != "" {
		b.WriteString("\n\n")
		b.WriteString(statusStyle.Render(m.status))
	}
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("⏎ create   esc cancel   ctrl+u clear"))
	return b.String()
}

func renderState(r scanner.Result) string {
	switch r.State {
	case scanner.StateScanning:
		return mutedStyle.Render("scanning…")
	case scanner.StateSessions:
		names := make([]string, 0, len(r.Sessions))
		for _, s := range r.Sessions {
			n := s.Name
			if s.Attached {
				n += "*"
			}
			names = append(names, n)
		}
		count := okStyle.Render(fmt.Sprintf("%d session%s", len(r.Sessions), plural(len(r.Sessions))))
		return fmt.Sprintf("%s  %s", count, mutedStyle.Render(strings.Join(names, ", ")))
	case scanner.StateNoSessions:
		return mutedStyle.Render("no sessions")
	case scanner.StateUnreachable:
		msg := "unreachable"
		if r.Err != nil {
			s := r.Err.Error()
			if len(s) > 60 {
				s = s[:60] + "…"
			}
			msg = "unreachable: " + s
		}
		return errStyle.Render(msg)
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
