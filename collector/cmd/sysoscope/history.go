package main

import (
	"flag"
	"path/filepath"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/history"
)

var spans = map[string]time.Duration{"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}

func (a *app) history(args []string) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	metric := fs.String("metric", "", "metric name")
	span := fs.String("span", "24h", "24h, 7d or 30d")
	format := fs.String("format", "csv", "csv or json")
	lang := fs.String("lang", "", "en or es")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a.setLang(*lang)
	d, ok := spans[*span]
	if !ok {
		return a.fail("unknown span %q (use 24h, 7d or 30d)", *span)
	}
	known := false
	for _, m := range history.Metrics {
		known = known || m == *metric
	}
	if !known {
		return a.fail("unknown metric %q; metrics: %s", *metric, strings.Join(history.Metrics, ", "))
	}
	store, err := history.Load(filepath.Join(a.env.State, "history.v1"))
	if err != nil {
		return a.fail("could not read the history: %s", err)
	}
	pts := store.Series(*metric, d, a.env.Now())
	switch *format {
	case "csv":
		err = history.WriteCSV(a.stdout, *metric, pts)
	case "json":
		err = history.WriteJSON(a.stdout, *metric, pts)
	default:
		return a.fail("unknown format %q (use csv or json)", *format)
	}
	if err != nil {
		return a.fail("could not write: %s", err)
	}
	return 0
}
