package hosts

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `
Host bastion
    HostName bastion.example.com
    User ops
    Port 2222

Host *.internal
    User svc

Host web db
    User deploy

Host
    HostName ignored
`

func TestLoad_FiltersWildcardsAndPreservesOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(sample), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantAliases := []string{"bastion", "db", "web"}
	if len(got) != len(wantAliases) {
		t.Fatalf("len = %d; want %d (got %+v)", len(got), len(wantAliases), got)
	}
	for i, w := range wantAliases {
		if got[i].Alias != w {
			t.Errorf("hosts[%d].Alias = %q; want %q", i, got[i].Alias, w)
		}
	}

	bastion := got[0]
	if bastion.HostName != "bastion.example.com" || bastion.User != "ops" || bastion.Port != "2222" {
		t.Errorf("bastion resolved fields wrong: %+v", bastion)
	}
}

func TestLoad_MissingFileNoError(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestScannable(t *testing.T) {
	cases := map[string]bool{
		"":          false,
		"web":       true,
		"web-01":    true,
		"*.dev":     false,
		"prod-?":    false,
		"!skip":     false,
		"a.b.c":     true,
		"192.0.2.1": true,
	}
	for in, want := range cases {
		if got := scannable(in); got != want {
			t.Errorf("scannable(%q) = %v; want %v", in, got, want)
		}
	}
}
