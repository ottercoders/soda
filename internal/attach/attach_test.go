package attach

import "testing"

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"":                "''",
		"main":            "'main'",
		"with space":      "'with space'",
		"weird'name":      "'weird'\\''name'",
		"$(rm -rf /)":     "'$(rm -rf /)'",
		"semis;and|pipes": "'semis;and|pipes'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q; want %q", in, got, want)
		}
	}
}
