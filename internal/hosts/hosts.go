// Package hosts loads SSH host aliases from ~/.ssh/config and filters out
// wildcard / template entries that aren't useful targets for tmux scanning.
package hosts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	sshconfig "github.com/kevinburke/ssh_config"
)

// Host is a scannable target — either an SSH alias resolved from ssh_config
// or the synthetic local machine (see Localhost).
type Host struct {
	Alias    string // the name passed to `ssh <alias>` (or the literal "localhost")
	HostName string // resolved Hostname (informational)
	User     string // resolved User    (informational)
	Port     string // resolved Port    (informational, default "22")
}

// Localhost returns the synthetic Host that represents the local machine.
// Scanning / attaching to it bypasses ssh and runs tmux directly.
func Localhost() Host {
	return Host{Alias: "localhost", HostName: "localhost", Port: "-"}
}

// IsLocalhost reports whether an alias names the local machine.
func IsLocalhost(alias string) bool {
	switch alias {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// Load reads ~/.ssh/config (and any Include files the library resolves) and
// returns the filtered list of scannable hosts. The configPath argument is
// optional; if empty, ~/.ssh/config is used.
func Load(configPath string) ([]Host, error) {
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		configPath = filepath.Join(home, ".ssh", "config")
	}

	f, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", configPath, err)
	}
	defer f.Close()

	cfg, err := sshconfig.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}

	seen := map[string]struct{}{}
	var hosts []Host
	for _, h := range cfg.Hosts {
		for _, p := range h.Patterns {
			alias := p.String()
			if !scannable(alias) {
				continue
			}
			if _, dup := seen[alias]; dup {
				continue
			}
			seen[alias] = struct{}{}

			hostname, _ := cfg.Get(alias, "HostName")
			user, _ := cfg.Get(alias, "User")
			port, _ := cfg.Get(alias, "Port")
			if hostname == "" {
				// No resolvable hostname → ssh would just try the alias as-is.
				// Keep it: many users rely on /etc/hosts or DNS aliases.
				hostname = alias
			}
			if port == "" {
				port = "22"
			}
			hosts = append(hosts, Host{
				Alias:    alias,
				HostName: hostname,
				User:     user,
				Port:     port,
			})
		}
	}

	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Alias < hosts[j].Alias })
	return hosts, nil
}

// scannable reports whether an ssh_config Host pattern is a real target alias
// (not a wildcard / negation template).
func scannable(alias string) bool {
	if alias == "" {
		return false
	}
	if strings.ContainsAny(alias, "*?!") {
		return false
	}
	return true
}
