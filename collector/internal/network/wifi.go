package network

import (
	"context"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// wifi completa señal (de /proc/net/wireless) y SSID y tasa (de `iw dev X
// link`) para las interfaces Wi-Fi activas.
func wifi(env source.Env, n *model.Network, r *collect.Report) {
	levels := map[string]int{}
	lines, _ := env.FS.Lines("/proc/net/wireless")
	for _, l := range lines {
		name, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 3 {
			continue
		}
		if v, err := strconv.ParseFloat(strings.TrimSuffix(f[2], "."), 64); err == nil {
			levels[strings.TrimSpace(name)] = int(v)
		}
	}
	for i := range n.Interfaces {
		it := &n.Interfaces[i]
		it.Wifi = nil
		if it.Kind != "wifi" || !it.Up {
			continue
		}
		w := &model.Wifi{}
		if lv, ok := levels[it.Name]; ok {
			w.SignalDBm = &lv
		}
		out, err := env.Run.Run(context.Background(), "iw", "dev", it.Name, "link")
		if err != nil {
			r.FromErr("network.wifi.ssid", "iw", err)
		} else {
			parseIw(string(out), w)
		}
		it.Wifi = w
	}
}

func parseIw(text string, w *model.Wifi) {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "SSID:"):
			w.SSID = strings.TrimSpace(strings.TrimPrefix(l, "SSID:"))
		case strings.HasPrefix(l, "signal:"):
			if f := strings.Fields(l); len(f) >= 2 {
				if v, err := strconv.Atoi(f[1]); err == nil {
					w.SignalDBm = &v
				}
			}
		case strings.HasPrefix(l, "tx bitrate:"):
			if f := strings.Fields(l); len(f) >= 3 {
				if v, err := strconv.ParseFloat(f[2], 64); err == nil {
					w.BitrateMbps = &v
				}
			}
		}
	}
}
