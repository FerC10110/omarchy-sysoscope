package system

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/text"
)

// hyprMonitor es un monitor de j/monitors (Hyprland 0.56).
type hyprMonitor struct {
	Name        string   `json:"name"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	RefreshRate float64  `json:"refreshRate"`
	Scale       float64  `json:"scale"`
	VRR         bool     `json:"vrr"`
	Format      string   `json:"currentFormat"`
	Preset      string   `json:"colorManagementPreset"`
	Disabled    bool     `json:"disabled"`
	MirrorOf    string   `json:"mirrorOf"`
	Modes       []string `json:"availableModes"`
}

// Live completa el modo en uso de cada monitor conectado con j/monitors de
// Hyprland (por el socket, sin hyprctl), unido por conector. Sin Hyprland,
// sin ningún monitor conectado (M15: máquina headless, como el fixture
// "apps"), o con una respuesta que no se entiende, Live queda nil y la
// vista muestra el modo preferido del EDID.
func Live(env source.Env, mons []model.Monitor) {
	for i := range mons {
		mons[i].Live = nil
	}
	anyConnected := false
	for _, m := range mons {
		if m.Connected {
			anyConnected = true
			break
		}
	}
	if !anyConnected || env.Hypr == nil {
		return
	}
	b, err := env.Hypr.Request("j/monitors")
	if err != nil {
		return
	}
	var hs []hyprMonitor
	if json.Unmarshal(b, &hs) != nil {
		return
	}
	for _, h := range hs {
		if h.Name == "" {
			continue // no puede emparejar con un monitor sin Connector
		}
		for i := range mons {
			if mons[i].Connector != h.Name || !mons[i].Connected {
				continue
			}
			mirror := text.Clean(strings.TrimSpace(h.MirrorOf))
			if mirror == "none" {
				mirror = ""
			}
			mons[i].Live = &model.MonitorMode{Width: h.Width, Height: h.Height, RefreshHz: h.RefreshRate, Scale: h.Scale,
				VRR: h.VRR, Format: text.Clean(h.Format), Preset: text.Clean(h.Preset), Disabled: h.Disabled, MirrorOf: mirror,
				BestHz: bestHz(h.Modes, h.Width, h.Height)}
		}
	}
}

// bestHz es la mayor frecuencia de availableModes ("3440x1440@49.99Hz") a
// esa resolución; 0 si no ofrece ninguna. Lo que no tiene esa forma se
// ignora.
func bestHz(modes []string, w, h int) float64 {
	best := 0.0
	for _, m := range modes {
		res, hz, ok := strings.Cut(strings.TrimSuffix(strings.TrimSpace(m), "Hz"), "@")
		if !ok {
			continue
		}
		ws, hs, ok := strings.Cut(res, "x")
		if !ok {
			continue
		}
		mw, err1 := strconv.Atoi(ws)
		mh, err2 := strconv.Atoi(hs)
		f, err3 := strconv.ParseFloat(hz, 64)
		if err1 != nil || err2 != nil || err3 != nil || mw != w || mh != h || f <= 0 {
			continue
		}
		best = max(best, f)
	}
	return best
}
