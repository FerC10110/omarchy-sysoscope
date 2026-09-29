package main

import (
	"flag"
	"fmt"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/fixture"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
)

func (a *app) fixture(args []string) int {
	if len(args) == 0 || args[0] != "capture" {
		return a.fail("usage: sysoscope fixture capture --out DIR [--anonymize]")
	}
	fs := flag.NewFlagSet("fixture capture", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	out := fs.String("out", "", "output directory")
	anon := fs.Bool("anonymize", false, "replace serials, MACs, IPs, UUIDs, SSIDs and the hostname")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *out == "" {
		return a.fail("usage: sysoscope fixture capture --out DIR [--anonymize]")
	}
	if err := fixture.Capture(a.env, *out, *anon); err != nil {
		return a.fail("capture failed: %s", err)
	}
	fmt.Fprintln(a.stdout, i18n.F(a.env.Lang, "fixture saved to %s", *out))
	return 0
}
