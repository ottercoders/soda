// Command soda is a tmux session reconnect tool. See README.md for usage.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ottercoders/soda/internal/attach"
	"github.com/ottercoders/soda/internal/hosts"
	"github.com/ottercoders/soda/internal/scanner"
	"github.com/ottercoders/soda/internal/tui"
)

var (
	flagConfigPath  string
	flagWorkers     int
	flagListJSON    bool
	flagNewName     string
	flagScanTimeout time.Duration
)

func main() {
	root := &cobra.Command{
		Use:   "soda",
		Short: "Scan SSH hosts for active tmux sessions and reconnect with one keystroke.",
		RunE:  runTUI,
	}
	root.PersistentFlags().StringVar(&flagConfigPath, "ssh-config", "", "path to ssh_config (default ~/.ssh/config)")
	root.PersistentFlags().IntVar(&flagWorkers, "workers", 16, "max concurrent ssh scans")
	root.PersistentFlags().DurationVar(&flagScanTimeout, "timeout", 30*time.Second, "max time to wait for all scans to finish")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Print host/session pairs (text or JSON).",
		RunE:  runList,
	}
	listCmd.Flags().BoolVar(&flagListJSON, "json", false, "emit JSON instead of text")

	attachCmd := &cobra.Command{
		Use:   "attach <host> [session]",
		Short: "Exec ssh -t <host> tmux attach -t <session>.",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runAttach,
	}

	newCmd := &cobra.Command{
		Use:   "new <host>",
		Short: "Exec ssh -t <host> tmux new -s <name>.",
		Args:  cobra.ExactArgs(1),
		RunE:  runNew,
	}
	newCmd.Flags().StringVar(&flagNewName, "name", "main", "new session name")

	hostsCmd := &cobra.Command{
		Use:   "hosts",
		Short: "Print scannable hosts from ssh_config and exit.",
		RunE:  runHosts,
	}

	root.AddCommand(listCmd, attachCmd, newCmd, hostsCmd)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func loadHosts() ([]hosts.Host, error) {
	hs, err := hosts.Load(flagConfigPath)
	if err != nil {
		return nil, err
	}
	if len(hs) == 0 {
		return nil, fmt.Errorf("no scannable hosts in ssh_config")
	}
	return hs, nil
}

func runTUI(_ *cobra.Command, _ []string) error {
	hs, err := loadHosts()
	if err != nil {
		return err
	}

	model := tui.New(hs, scanner.SSHRunner{}, flagWorkers)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return err
	}

	switch model.Outcome.Action {
	case tui.ActionAttach:
		return attach.Attach(model.Outcome.Host, model.Outcome.Session)
	case tui.ActionNew:
		name := model.Outcome.Session
		if name == "" {
			name = "main"
		}
		return attach.New(model.Outcome.Host, name)
	}
	return nil
}

func runList(_ *cobra.Command, _ []string) error {
	hs, err := loadHosts()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), flagScanTimeout)
	defer cancel()

	results := []scanner.Result{}
	for r := range scanner.Scan(ctx, hs, scanner.SSHRunner{}, flagWorkers) {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Host.Alias < results[j].Host.Alias })

	if flagListJSON {
		return emitJSON(results)
	}
	return emitText(results, os.Stdout)
}

type listJSONEntry struct {
	Host     string `json:"host"`
	State    string `json:"state"`
	Session  string `json:"session,omitempty"`
	Created  int64  `json:"created,omitempty"`
	Attached bool   `json:"attached,omitempty"`
	Windows  int    `json:"windows,omitempty"`
	Error    string `json:"error,omitempty"`
}

func emitJSON(results []scanner.Result) error {
	out := []listJSONEntry{}
	for _, r := range results {
		switch r.State {
		case scanner.StateSessions:
			for _, s := range r.Sessions {
				out = append(out, listJSONEntry{
					Host:     r.Host.Alias,
					State:    r.State.String(),
					Session:  s.Name,
					Created:  s.Created.Unix(),
					Attached: s.Attached,
					Windows:  s.Windows,
				})
			}
		case scanner.StateUnreachable:
			msg := ""
			if r.Err != nil {
				msg = r.Err.Error()
			}
			out = append(out, listJSONEntry{Host: r.Host.Alias, State: r.State.String(), Error: msg})
		default:
			out = append(out, listJSONEntry{Host: r.Host.Alias, State: r.State.String()})
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func emitText(results []scanner.Result, w *os.File) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintln(tw, "HOST\tSESSION\tWINDOWS\tCREATED\tATTACHED\tNOTES")
	for _, r := range results {
		switch r.State {
		case scanner.StateSessions:
			for _, s := range r.Sessions {
				attached := "-"
				if s.Attached {
					attached = "yes"
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t\n",
					r.Host.Alias, s.Name, s.Windows,
					s.Created.Format(time.RFC3339), attached)
			}
		case scanner.StateNoSessions:
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\tno sessions\n", r.Host.Alias)
		case scanner.StateUnreachable:
			note := "unreachable"
			if r.Err != nil {
				note = "unreachable: " + strings.SplitN(r.Err.Error(), "\n", 2)[0]
			}
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\t%s\n", r.Host.Alias, note)
		}
	}
	return nil
}

func runAttach(_ *cobra.Command, args []string) error {
	host := args[0]
	session := ""
	if len(args) == 2 {
		session = args[1]
	}
	return attach.Attach(host, session)
}

func runNew(_ *cobra.Command, args []string) error {
	return attach.New(args[0], flagNewName)
}

func runHosts(_ *cobra.Command, _ []string) error {
	hs, err := hosts.Load(flagConfigPath)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintln(tw, "ALIAS\tHOSTNAME\tUSER\tPORT")
	for _, h := range hs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", h.Alias, h.HostName, h.User, h.Port)
	}
	return nil
}
