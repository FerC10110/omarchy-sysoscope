package view

import (
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func fakeStatfs(string) (uint64, uint64, uint64, error) { return 1000e9, 400e9, 300e9, nil }

// snap devuelve el segundo tick de un engine sobre el fixture (el primero no
// tiene porcentajes) y su engine, para Recent.
func snap(t *testing.T, fixture string) (*model.Snapshot, *engine.Engine) {
	t.Helper()
	env := source.Fixture(t, fixture)
	e := engine.New(env, engine.Options{Interval: 2 * time.Second, Statfs: fakeStatfs})
	e.Tick()
	s := e.Tick()
	// Los fixtures son estáticos: /proc/stat no cambia entre ticks y no hay
	// uso. Se inventa uno para que las vistas lo muestren.
	if s.CPU != nil && s.CPU.UsagePct == nil {
		u := 37.5
		s.CPU.UsagePct = &u
		s.CPU.PerCPUPct = make([]float64, s.CPU.Threads)
		for i := range s.CPU.PerCPUPct {
			s.CPU.PerCPUPct[i] = float64(i * 4)
		}
	}
	return s, e
}

// rows devuelve las filas de un bloque kv.
func kvRows(b Block) [][2]string {
	r, _ := b.Rows.([][2]string)
	return r
}

func hasRow(b Block, k, v string) bool {
	for _, r := range kvRows(b) {
		if r[0] == k && r[1] == v {
			return true
		}
	}
	return false
}

// appsSnap es un snapshot con aplicaciones inventadas (dos monitores).
func appsSnap() *model.Snapshot {
	gpu, rx, tx := 3.0, 1100.0, 20.0
	return &model.Snapshot{Lang: "en", Apps: &model.Apps{Monitors: []string{"DP-1", "HDMI-A-1"}, TotalCPUPct: 30, TotalMemB: 3 << 30,
		Frozen: []model.Frozen{{ID: "comm:make", Name: "make"}},
		List: []model.App{
			{ID: "app-org.chromium.Chromium-200.scope", Name: "Chromium", Procs: 2, CPUPct: 12, MemB: 1 << 30, GPUPct: &gpu, NetRxBps: &rx, NetTxBps: &tx,
				Windows: []model.Window{{Address: "0xa1", StableID: "20", Title: "GitHub", Workspace: "1", Monitor: "DP-1", Focused: true},
					{Address: "0xa2", Title: "YouTube", Workspace: "2", Monitor: "HDMI-A-1", Fullscreen: 2}}},
			{ID: "kitty-300-0.scope", Name: "kitty", Procs: 2, CPUPct: 1, MemB: 100 << 20, Protected: true,
				Windows: []model.Window{{Address: "0xb1", StableID: "30", Title: "~", Workspace: "special:scratch", Floating: true, Pinned: true}}},
			{ID: "comm:syncthing", Name: "syncthing", Procs: 1, CPUPct: 9, MemB: 200 << 20},
			{ID: "comm:Hyprland", Name: "Hyprland", Procs: 1, CPUPct: 5, MemB: 300 << 20, Protected: true},
			{ID: "comm:pipewire", Name: "pipewire", Procs: 1, CPUPct: 2, MemB: 10 << 20, Protected: true},
			{ID: "comm:make", Name: "make", Procs: 1, CPUPct: 1, MemB: 1 << 20, Frozen: true},
		}}}
}

func unavailable(what, msg string) model.Unavailable {
	return model.Unavailable{What: what, Reason: "not_present", Message: msg}
}

// findGroup devuelve el group con ese título, o uno vacío.
func findGroup(bs []Block, title string) Block {
	for _, b := range bs {
		if b.Type == "group" && b.Title == title {
			return b
		}
	}
	return Block{}
}

func tableRows(b Block) []Row { r, _ := b.Rows.([]Row); return r }
