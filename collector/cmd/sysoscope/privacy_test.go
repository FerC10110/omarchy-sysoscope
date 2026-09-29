package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/ondisk"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

// latest.json no tiene lo que identifica: solo sirve a las vistas que no lo
// muestran (Resumen, CPU y Memoria, estas dos con las apps) y a la lista de
// secciones; Almacenamiento, Acerca de, Red y la búsqueda toman una muestra.
func TestLatestServesOnlyViewsWithoutPrivateData(t *testing.T) {
	s := &model.Snapshot{Apps: &model.Apps{List: []model.App{{ID: "x"}}},
		Network: &model.Network{Interfaces: []model.Iface{{Name: "enp5s0", Kind: "ethernet"}}}}
	for _, id := range view.IDs {
		want := id == "summary" || id == "cpu" || id == "memory"
		if got := latestServes(s, id); got != want {
			t.Errorf("latestServes(%q) = %v, want %v", id, got, want)
		}
	}
	if !latestServes(s, "") || latestServes(s, "search") {
		t.Fatal("sections yes, search no")
	}
}

// Las vistas que sí se sirven de latest.json se ven igual con la muestra
// en vivo y con la de disco (con una IPv4 privada: una pública no se guarda
// y el Resumen de latest.json sale sin ella).
func TestLatestViewsMatchLive(t *testing.T) {
	for _, fx := range []string{"threadripper", "apps", "laptop", "wifi"} {
		env := source.Fixture(t, fx)
		env.Cache, env.State = t.TempDir(), t.TempDir()
		e := engine.New(env, engine.Options{Interval: 2 * time.Second, Focus: "apps"})
		e.Tick()
		live := e.Tick()
		e.Close()
		if live.Network != nil {
			for i := range live.Network.Interfaces {
				if len(live.Network.Interfaces[i].IPv4) > 0 {
					live.Network.Interfaces[i].IPv4 = []string{"192.168.1.10/24"}
				}
			}
			live.Network.Gateway = "192.168.1.1"
		}
		// Como la lee el CLI: escrita por el stream y cargada por latest().
		data, _ := json.Marshal(ondisk.Snapshot(live, env.Home, env.User.Name))
		a := &app{env: env}
		p := filepath.Join(env.State, "latest.json")
		os.WriteFile(p, data, 0o644)
		now := time.Now()
		os.Chtimes(p, now, now)
		a.env.Lang, a.env.Now = live.Lang, func() time.Time { return now }
		disk := a.latest()
		if disk == nil {
			t.Fatalf("%s: latest.json not read", fx)
		}
		o := view.Options{Lang: "en", Units: "c"}
		for _, id := range []string{"summary", "cpu", "memory"} {
			a, err1 := view.Build(id, live, o)
			b, err2 := view.Build(id, disk, o)
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			if err1 != nil || err2 != nil || string(ja) != string(jb) {
				t.Errorf("%s/%s differs:\nlive %s\ndisk %s", fx, id, ja, jb)
			}
		}
		ja, _ := json.Marshal(view.Sections(live, o))
		jb, _ := json.Marshal(view.Sections(disk, o))
		if string(ja) != string(jb) {
			t.Errorf("%s: sections differ:\nlive %s\ndisk %s", fx, ja, jb)
		}
	}
}

func freshLatest(t *testing.T, a *app) {
	t.Helper()
	data, _ := json.Marshal(model.Snapshot{Schema: 1, Lang: "en", Time: source.FixtureNow, CPU: &model.CPU{Model: "FromLatest"},
		System: &model.System{}, Storage: &model.Storage{}, Network: &model.Network{}})
	p := filepath.Join(a.env.State, "latest.json")
	os.WriteFile(p, data, 0o644)
	now := time.Now()
	os.Chtimes(p, now, now)
	a.env.Now = func() time.Time { return now.Add(time.Second) }
}

// snapshot solo reusa latest.json para las secciones que no pierden nada
// al guardarse; el snapshot entero, System, Storage y Network toman una
// muestra (latest.json no tiene hostname, series ni MACs).
func TestSnapshotLatestOnlyForUntouchedSections(t *testing.T) {
	for _, sec := range []string{"cpu", "memory", "gpu", "sensors", "self"} {
		a, fake, out, _ := newApp(t, "")
		freshLatest(t, a)
		if a.run([]string{"snapshot", "--section", sec}) != 0 || fake.ticks != 0 {
			t.Errorf("--section %s: ticks %d, out %s", sec, fake.ticks, out)
		}
	}
	for _, args := range [][]string{{"snapshot"}, {"snapshot", "--section", "system"}, {"snapshot", "--section", "storage"}, {"snapshot", "--section", "network"}} {
		a, fake, out, _ := newApp(t, "")
		freshLatest(t, a)
		if a.run(args) != 0 || fake.ticks != 2 || strings.Contains(out.String(), "FromLatest") {
			t.Errorf("%q must not come from latest.json: ticks %d", args, fake.ticks)
		}
	}
}

// search busca también en lo que latest.json no guarda: toma una muestra.
func TestSearchNotFromLatest(t *testing.T) {
	a, fake, _, errb := newApp(t, "")
	freshLatest(t, a)
	if code := a.run([]string{"search", "Fake"}); code != 0 || fake.ticks != 2 {
		t.Fatalf("exit %d, ticks %d: %s", code, fake.ticks, errb.String())
	}
}
