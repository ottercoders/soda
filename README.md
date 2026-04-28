# soda

Scan your SSH hosts for active tmux sessions and reconnect with one keystroke.

## Build

```
sudo dnf install golang   # or: brew install go / apt install golang-go
make tidy                 # download deps
make                      # produces ./soda
```

## Usage

```
soda                          # interactive TUI: scan all ssh_config hosts
soda list                     # plain list of host/session pairs
soda list --json              # JSON for piping
soda attach <host> [<name>]   # exec ssh -t <host> tmux a -t <name>
soda new    <host> [--name n] # exec ssh -t <host> tmux new -s n
soda hosts                    # print scannable hosts from ssh_config
```

Hosts come from `~/.ssh/config`. Wildcard `Host` entries (`*`, `?`) are skipped.
Optional excludes: `~/.config/soda/config.toml` with `excluded_hosts = ["..."]`
(not implemented in v1 — open an issue if needed).

## Releases

Releases are automated:

- **Conventional commits** drive versioning. Use `feat:` for minor bumps,
  `fix:` for patch, `feat!:` / `BREAKING CHANGE:` for major. Other types
  (`chore:`, `docs:`, `ci:`, `refactor:`, `test:`) land without a release.
- **release-please** opens a PR on `main` that maintains `CHANGELOG.md`
  and `.release-please-manifest.json`. Merging that PR creates a `vX.Y.Z`
  tag and GitHub Release.
- **goreleaser** builds tarballs (`linux/amd64`, `linux/arm64`,
  `darwin/amd64`, `darwin/arm64`) plus a `checksums.txt` and uploads
  them to the Release.

Run `soda --version` to see the embedded version, commit, and build date.

## Keys (TUI)

| Key       | Action                                    |
|-----------|-------------------------------------------|
| ↑ / ↓     | Move selection                            |
| enter     | Attach (or expand sessions / start new)   |
| r         | Rescan                                    |
| /         | Filter                                    |
| q / esc   | Quit / back                               |
