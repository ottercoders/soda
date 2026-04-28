package tmux

import (
	"strings"
	"testing"
	"time"
)

func TestParse_Empty(t *testing.T) {
	got, err := Parse("")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil sessions; got %v", got)
	}
}

func TestParse_SingleSession(t *testing.T) {
	in := "main" + FieldSep + "1700000000" + FieldSep + "1" + FieldSep + "3"
	got, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d; want 1", len(got))
	}
	s := got[0]
	if s.Name != "main" || !s.Attached || s.Windows != 3 {
		t.Errorf("session = %+v", s)
	}
	if !s.Created.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("Created = %v", s.Created)
	}
}

func TestParse_Multiple(t *testing.T) {
	lines := []string{
		"main" + FieldSep + "1700000000" + FieldSep + "0" + FieldSep + "1",
		"debug" + FieldSep + "1700000500" + FieldSep + "1" + FieldSep + "5",
	}
	got, err := Parse(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2", len(got))
	}
	if got[0].Attached || !got[1].Attached {
		t.Errorf("attached flags wrong: %+v", got)
	}
}

func TestParse_Malformed_Skipped(t *testing.T) {
	in := "ok" + FieldSep + "1" + FieldSep + "0" + FieldSep + "1\n" +
		"bad-line-no-seps\n" +
		"two" + FieldSep + "2" + FieldSep + "0" + FieldSep + "1"
	got, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2 (got %+v)", len(got), got)
	}
	if got[0].Name != "ok" || got[1].Name != "two" {
		t.Errorf("wrong names: %+v", got)
	}
}
