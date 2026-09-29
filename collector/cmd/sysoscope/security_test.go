package main

import (
	"encoding/json"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestSnapshotSecuritySection(t *testing.T) {
	a, fake, _, _ := newApp(t, "")
	if code := a.run([]string{"snapshot", "--section", "security"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(fake.focus) != 1 || fake.focus[0] != "security" {
		t.Fatalf("focus = %q (the programs are read only with the focus)", fake.focus)
	}
	// Con el engine de verdad sobre threadripper: programas y firewall.
	a, _, out, _ := newApp(t, "")
	a.env = source.Fixture(t, "threadripper")
	a.newEngine = func(env source.Env, o engine.Options) sampler {
		o.Statfs = func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }
		return engine.New(env, o)
	}
	if code := a.run([]string{"snapshot", "--section", "security"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got["cpu"] != nil {
		t.Fatalf("snapshot = %s", out.String())
	}
	var sec model.Security
	if err := json.Unmarshal(got["security"], &sec); err != nil || len(sec.Listening) != 14 ||
		sec.Listening[0].Owners[0].Program != "prog2" || sec.Firewall == nil || sec.Firewall.State != "on" {
		t.Fatalf("security = %s", got["security"])
	}
}

func TestSectionOfSecurityUnavailable(t *testing.T) {
	s := &model.Snapshot{Schema: 1, Security: &model.Security{},
		Unavailable: []model.Unavailable{{What: "security.listening", Reason: "not_present", Message: "x"}, {What: "cpu.temp", Reason: "not_present", Message: "y"}}}
	out := sectionOf(s, "security")
	un, _ := out["unavailable"].([]model.Unavailable)
	if out["security"] == nil || len(un) != 1 || un[0].What != "security.listening" {
		t.Fatalf("section = %+v", out)
	}
}
