package view

import (
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func clockSnap(tm *model.TimeSync, uptime float64) *model.Snapshot {
	return &model.Snapshot{Lang: "en", System: &model.System{Hostname: "h", UptimeS: uptime, FailedUnits: []model.Unit{}, Time: tm}}
}

func TestAboutClockRows(t *testing.T) {
	v := build(t, "about", "threadripper", Options{})
	if kvValue(v.Blocks, "Date and time", "Time zone") != "America/Argentina/Buenos_Aires" ||
		kvValue(v.Blocks, "Date and time", "Automatic sync") != "Yes" || kvValue(v.Blocks, "Date and time", "Synchronized") != "Yes" {
		t.Fatalf("blocks = %+v", v.Blocks)
	}
	noEmptyValues(t, v)
	es, err := Build("about", clockSnap(&model.TimeSync{Zone: "UTC"}, 3600), Options{Lang: "es"})
	if err != nil {
		t.Fatal(err)
	}
	if kvValue(es.Blocks, "Fecha y hora", "Sincronización automática") != "No disponible" ||
		kvValue(es.Blocks, "Fecha y hora", "Sincronizado") != "No" || kvValue(es.Blocks, "Fecha y hora", "Zona horaria") != "UTC" {
		t.Fatalf("es blocks = %+v", es.Blocks)
	}
	off, _ := Build("about", clockSnap(&model.TimeSync{CanNTP: true}, 3600), Options{Lang: "en"})
	if kvValue(off.Blocks, "Date and time", "Automatic sync") != "No" {
		t.Fatalf("sync off = %+v", off.Blocks)
	}
	none, _ := Build("about", clockSnap(nil, 3600), Options{Lang: "en"})
	for _, b := range none.Blocks {
		if b.Title == "Date and time" {
			t.Fatal("no clock data: no Date and time rows")
		}
	}
}

func TestClockWarnings(t *testing.T) {
	for _, c := range []struct {
		name string
		tm   *model.TimeSync
		up   float64
		want string
	}{
		{"sync off", &model.TimeSync{CanNTP: true}, 3600, "The time is not synchronized automatically"},
		{"not synced", &model.TimeSync{CanNTP: true, NTP: true}, 3600, "The clock is not synchronized"},
		{"just booted", &model.TimeSync{CanNTP: true, NTP: true}, 300, ""},
		{"exactly 10 min", &model.TimeSync{CanNTP: true, NTP: true}, 600, ""},
		{"synced", &model.TimeSync{CanNTP: true, NTP: true, Synced: true}, 3600, ""},
		{"no NTP service", &model.TimeSync{}, 3600, ""},
		{"no clock data", nil, 3600, ""},
	} {
		s := clockSnap(c.tm, c.up)
		w := Warnings(s, Options{Lang: "en"})
		got := ""
		switch {
		case len(w) == 1:
			got = w[0].Text
			if w[0].Link != "about" || w[0].Level != "warn" {
				t.Errorf("%s: warning = %+v", c.name, w[0])
			}
		case len(w) > 1:
			t.Fatalf("%s: warnings = %+v", c.name, w)
		}
		if got != c.want {
			t.Errorf("%s: warning %q, want %q", c.name, got, c.want)
		}
		badge := 0
		if c.want != "" {
			badge = 1
		}
		if b := Sections(s, Options{Lang: "en"})[8]; b.ID != "about" || b.Badge != badge {
			t.Errorf("%s: about badge = %+v", c.name, b)
		}
	}
	es := Warnings(clockSnap(&model.TimeSync{CanNTP: true}, 3600), Options{Lang: "es"})
	if len(es) != 1 || es[0].Text != "La hora no se sincroniza sola" {
		t.Fatalf("es = %+v", es)
	}
	es = Warnings(clockSnap(&model.TimeSync{CanNTP: true, NTP: true}, 3600), Options{Lang: "es"})
	if len(es) != 1 || es[0].Text != "El reloj no está sincronizado" {
		t.Fatalf("es = %+v", es)
	}
}
