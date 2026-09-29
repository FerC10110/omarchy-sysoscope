package collect

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"testing"
)

func TestFromErr(t *testing.T) {
	cases := []struct {
		err    error
		reason string
	}{
		{&exec.Error{Name: "inxi", Err: exec.ErrNotFound}, "missing_tool"},
		{fmt.Errorf("open: %w", fs.ErrPermission), "permission"},
		{context.DeadlineExceeded, "timeout"},
		{fmt.Errorf("open: %w", fs.ErrNotExist), "not_present"},
		{errors.New("boom"), "error"},
	}
	for _, c := range cases {
		r := &Report{Lang: "en"}
		r.FromErr("memory.modules", "inxi", c.err)
		if len(r.List) != 1 || r.List[0].Reason != c.reason || r.List[0].What != "memory.modules" || r.List[0].Message == "" {
			t.Errorf("FromErr(%v) = %+v, want reason %s", c.err, r.List, c.reason)
		}
	}
}

func TestMissingMessage(t *testing.T) {
	r := &Report{Lang: "en"}
	r.Missing("memory.modules", "inxi")
	if r.List[0].Message != "requires inxi" {
		t.Fatalf("message = %q", r.List[0].Message)
	}
	r = &Report{Lang: "es"}
	r.Missing("memory.modules", "inxi")
	if r.List[0].Message != "requiere inxi" {
		t.Fatalf("mensaje = %q", r.List[0].Message)
	}
}

func TestReportDedup(t *testing.T) {
	r := &Report{Lang: "en"}
	r.Absent("power.battery")
	r.Absent("power.battery")
	if len(r.List) != 1 {
		t.Fatalf("duplicates kept: %+v", r.List)
	}
}

func TestParsePSI(t *testing.T) {
	p := ParsePSI("some avg10=0.00 avg60=0.01 avg300=0.00 total=189129998\nfull avg10=1.50 avg60=0.25 avg300=0.00 total=12")
	if p == nil || p.Some10 != 0 || p.Some60 != 0.01 || p.Full10 == nil || *p.Full10 != 1.5 || *p.Full60 != 0.25 {
		t.Fatalf("PSI = %+v", p)
	}
	p = ParsePSI("some avg10=2.00 avg60=0.01 avg300=0.00 total=189129998")
	if p == nil || p.Some10 != 2 || p.Full10 != nil {
		t.Fatalf("PSI sin full = %+v", p)
	}
	if ParsePSI("") != nil || ParsePSI("garbage") != nil {
		t.Fatal("bad PSI must be nil")
	}
}

func TestClean(t *testing.T) {
	for _, s := range []string{"To be filled by O.E.M.", "Default string", "System Product Name", "System Version",
		"System manufacturer", "SKU", "Not Specified", "None", "0123456789", "", "  ", "default STRING"} {
		if got := Clean(s); got != "" {
			t.Errorf("Clean(%q) = %q", s, got)
		}
	}
	if got := Clean(" ROG ZENITH EXTREME "); got != "ROG ZENITH EXTREME" {
		t.Fatalf("Clean = %q", got)
	}
}
