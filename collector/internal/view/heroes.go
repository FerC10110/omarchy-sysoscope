package view

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// heroes arma la cabecera de cada sección (§3.2 de la spec estética). Una
// función devuelve nil cuando no hay con qué: la sección arranca con su
// contenido de siempre y su Unavailable explica el hueco.
var heroes = map[string]func(s *model.Snapshot, o Options) *Block{
	"cpu": cpuHero, "memory": memoryHero, "gpu": gpuHero, "storage": storageHero,
	"network": networkHero, "sensors": sensorsHero, "power": powerHero,
	"about": aboutHero, "maintenance": maintenanceHero, "devices": devicesHero, "security": securityHero,
}

func heroOf(stats []Block, lines []string, values []float64) *Block {
	h := Hero(stats, lines, values)
	return &h
}

func cpuHero(s *model.Snapshot, o Options) *Block {
	c, l := s.CPU, o.Lang
	if c == nil || c.UsagePct == nil {
		return nil
	}
	stats := []Block{statOf(i18n.T(l, "Usage"), Pct(*c.UsagePct), Level(*c.UsagePct, 70, 90))}
	if c.TempC != nil {
		var ls []string
		if p, ok := peak(o.recent("cpu.temp")); ok {
			ls = append(ls, i18n.F(l, "peak %s", Temp(p, o.Units)))
		}
		stats = append(stats, statOf(i18n.T(l, "Temperature"), Temp(*c.TempC, o.Units), Level(*c.TempC, 80, 90), ls...))
	}
	clock := mhz(c.AvgMHz)
	if m := mhz(c.MaxMHz); clock != "" && m != "" {
		clock += " / " + m
	}
	lines := []string{}
	if clock != "" {
		lines = append(lines, clock)
	}
	lines = append(lines, i18n.T(l, "Load")+" "+loadText(c.Load))
	if c.PSI != nil {
		lines = append(lines, i18n.F(l, "pressure %s", Pct(c.PSI.Some10)))
	}
	return heroOf(stats, lines, o.recent("cpu.usage"))
}

func memoryHero(s *model.Snapshot, o Options) *Block {
	m, l := s.Memory, o.Lang
	if m == nil {
		return nil
	}
	lvl := Level(m.UsedPct, 80, 90)
	stats := []Block{statOf(i18n.T(l, "Used"), Pct(m.UsedPct), lvl), statOf(i18n.T(l, "In use"), Bytes(m.UsedB), lvl)}
	lines := []string{i18n.F(l, "of %s", Bytes(m.TotalB))}
	if m.SwapTotalB > 0 {
		lines = append(lines, i18n.F(l, "swap %s of %s", Bytes(m.SwapUsedB), Bytes(m.SwapTotalB)))
	}
	for _, z := range m.Swaps {
		if z.Kind == "zram" {
			lines = append(lines, "zram "+i18n.F(l, "%s of %s", Bytes(z.UsedB), Bytes(z.SizeB)))
		}
	}
	return heroOf(stats, lines, o.recent("mem.used_pct"))
}

// gpuHero toma la primera GPU despierta, la misma que usan la barra y el
// historial gpu.*; las otras despiertas van en una línea cada una.
func gpuHero(s *model.Snapshot, o Options) *Block {
	l := o.Lang
	for i, g := range s.GPUs {
		if g.Suspended || g.UsagePct == nil {
			continue
		}
		stats := []Block{statOf(i18n.T(l, "Usage"), Pct(*g.UsagePct), Level(*g.UsagePct, 80, 95))}
		if g.TempC != nil {
			stats = append(stats, statOf(i18n.T(l, "Temperature"), Temp(*g.TempC, o.Units), Level(*g.TempC, 80, 90)))
		}
		var lines []string
		if g.PowerW != nil {
			lines = append(lines, Watts(*g.PowerW))
		}
		if g.VRAMUsedB != nil && g.VRAMTotalB != nil && *g.VRAMTotalB > 0 {
			lines = append(lines, "VRAM "+i18n.F(l, "%s of %s", Bytes(*g.VRAMUsedB), Bytes(*g.VRAMTotalB)))
		}
		if c := mhz(g.CoreMHz); c != "" {
			lines = append(lines, c)
		}
		for _, x := range s.GPUs[i+1:] {
			if !x.Suspended && x.UsagePct != nil {
				lines = append(lines, x.Name+" · "+Pct(*x.UsagePct))
			}
		}
		return heroOf(stats, lines, o.recent("gpu.usage"))
	}
	return nil
}

func storageHero(s *model.Snapshot, o Options) *Block {
	st, l := s.Storage, o.Lang
	if st == nil {
		return nil
	}
	for _, f := range st.Filesystems {
		if !slices.Contains(f.Mounts, "/") {
			continue
		}
		free := statOf(i18n.T(l, "Free on /"), Bytes(f.FreeB), Level(f.UsedPct, 85, 95)).WithValue(f.UsedPct / 100)
		lines := []string{i18n.F(l, "of %s", Bytes(f.SizeB))}
		var r, w float64
		seen := false
		for _, d := range st.Disks {
			if d.ReadBps != nil {
				r += *d.ReadBps
				seen = true
			}
			if d.WriteBps != nil {
				w += *d.WriteBps
				seen = true
			}
		}
		if seen {
			lines = append(lines, i18n.F(l, "read %s · write %s", Rate(r), Rate(w)))
		}
		return heroOf([]Block{free}, lines, o.recent("disk.read"))
	}
	return nil
}

func networkHero(s *model.Snapshot, o Options) *Block {
	n, l := s.Network, o.Lang
	if n == nil {
		return nil
	}
	stats := []Block{statOf("↓ "+i18n.T(l, "Download"), Rate(n.RxBps), ""), statOf("↑ "+i18n.T(l, "Upload"), Rate(n.TxBps), "")}
	var lines []string
	for _, i := range n.Interfaces {
		if i.Name == n.GatewayIface {
			lines = append(lines, i.Name)
			if len(i.IPv4) > 0 {
				lines = append(lines, i.IPv4[0])
			}
		}
	}
	if n.Gateway != "" {
		lines = append(lines, i18n.F(l, "gateway %s", n.Gateway))
	}
	return heroOf(stats, lines, o.recent("net.rx"))
}

func sensorsHero(s *model.Snapshot, o Options) *Block {
	l := o.Lang
	if s.Sensors == nil {
		return nil
	}
	var hot, fan *model.Reading
	chip := ""
	for ci := range s.Sensors.Chips {
		c := &s.Sensors.Chips[ci]
		for ri := range c.Temps {
			if r := &c.Temps[ri]; hot == nil || r.Value > hot.Value {
				hot, chip = r, dash(c.Label)
			}
		}
		for ri := range c.Fans {
			if r := &c.Fans[ri]; fan == nil || r.Value > fan.Value {
				fan = r
			}
		}
	}
	if hot == nil {
		return nil
	}
	lvl := "ok"
	switch {
	case hot.Crit != nil && hot.Value >= *hot.Crit:
		lvl = "crit"
	case hot.High != nil && hot.Value >= *hot.High:
		lvl = "warn"
	}
	lines := []string{join(" · ", chip, hot.Label)}
	if fan != nil {
		lines = append(lines, i18n.F(l, "fastest fan %s", fmt.Sprintf("%.0f RPM", fan.Value)))
	}
	return heroOf([]Block{statOf(i18n.T(l, "Hottest"), Temp(hot.Value, o.Units), lvl)}, lines, nil)
}

func powerHero(s *model.Snapshot, o Options) *Block {
	p, l := s.Power, o.Lang
	if p == nil {
		return nil
	}
	var lines []string
	if p.Profile != "" {
		lines = append(lines, i18n.F(l, "profile %s", p.Profile))
	}
	for _, b := range p.Batteries {
		if b.Peripheral {
			continue
		}
		st := statOf(i18n.T(l, "Battery"), Pct(b.Pct), Level(100-b.Pct, 80, 90)).WithValue(b.Pct / 100)
		return heroOf([]Block{st}, append(lines, batteryStatus(b.Status, l)), o.recent("bat.pct"))
	}
	var w float64
	found := false
	for _, d := range p.RAPL {
		if strings.HasPrefix(d.Name, "package-") {
			w += d.Watts
			found = true
		}
	}
	if found {
		return heroOf([]Block{statOf(i18n.T(l, "CPU package"), Watts(w), "")}, lines, nil)
	}
	// Without a battery or RAPL, what is measured is the GPUs' draw.
	w, found = 0, false
	for _, g := range s.GPUs {
		if !g.Suspended && g.PowerW != nil {
			w += *g.PowerW
			found = true
		}
	}
	if found {
		return heroOf([]Block{statOf("GPU", Watts(w), "")}, lines, nil)
	}
	if len(lines) > 0 {
		return heroOf(nil, lines, nil)
	}
	return nil
}

func aboutHero(s *model.Snapshot, o Options) *Block {
	y, l := s.System, o.Lang
	if y == nil {
		return nil
	}
	stats := []Block{
		Stat(i18n.T(l, "Uptime"), Duration(y.UptimeS, l), "", ""),
		Stat(i18n.T(l, "Packages"), strconv.Itoa(y.Packages.Pacman), "", ""),
	}
	lines := []string{y.Kernel}
	if y.Omarchy != "" {
		lines = append(lines, "Omarchy "+y.Omarchy)
	}
	switch n := len(y.FailedUnits); {
	case n == 1:
		lines = append(lines, i18n.T(l, "1 failed service"))
	case n > 1:
		lines = append(lines, i18n.F(l, "%d failed services", n))
	}
	return heroOf(stats, lines, nil)
}

func devicesHero(s *model.Snapshot, o Options) *Block {
	y := s.System
	if y == nil {
		return nil
	}
	// A source that could not be read is "—": the reason is further down.
	count := func(label, what string, n int) Block {
		if n == 0 && Unavailable(s, "system."+what) != nil {
			return Stat(label, "—", "", "")
		}
		return Stat(label, strconv.Itoa(n), "", "")
	}
	return heroOf([]Block{
		count("PCI", "pci", len(y.PCI)),
		count("USB", "usb", len(y.USB)),
		count("Bluetooth", "bluetooth", len(y.Bluetooth)),
	}, nil, nil)
}

func securityHero(s *model.Snapshot, o Options) *Block {
	y, sec, l := s.System, s.Security, o.Lang
	if y == nil && sec == nil {
		return nil
	}
	var stats []Block
	if y != nil {
		if y.SecureBoot != nil {
			v, lvl := i18n.T(l, "off"), "warn"
			if *y.SecureBoot {
				v, lvl = i18n.T(l, "on"), "ok"
			}
			stats = append(stats, Stat("Secure Boot", v, "", lvl))
		}
		tpm, lvl := y.TPM, "ok"
		if tpm == "" {
			tpm, lvl = i18n.T(l, "none"), "warn"
		}
		stats = append(stats, Stat("TPM", tpm, "", lvl))
	}
	if sec != nil && sec.Firewall != nil {
		v, lvl := i18n.T(l, "Off"), ""
		if sec.Firewall.Active {
			v, lvl = i18n.T(l, "On"), "ok"
			// ufw activado pero sin poder leer sus reglas: no se sabe qué
			// deja pasar, así que no es un "On" tranquilizador (verde).
			if sec.Firewall.RulesUnknown {
				lvl = ""
			}
		}
		stats = append(stats, Stat(i18n.T(l, "Firewall"), v, "", lvl))
	}
	if sec != nil && sec.Listening != nil {
		text, lvl := strconv.Itoa(openToAll(sec)), ""
		switch {
		case anyUnknown(sec):
			// Con algún veredicto sin leer (ufw con reglas ilegibles,
			// firewalld o nftables) no se sabe cuántos puertos quedan
			// realmente abiertos: mostrar un número sería tranquilizador
			// de más. Nivel neutro: no saber no es un hallazgo.
			text, lvl = "?", ""
		case len(exposureWarnings(s, l)) > 0:
			lvl = "warn"
		}
		stats = append(stats, Stat(i18n.T(l, "Open to every network"), text, "", lvl))
	}
	var lines []string
	if p := s.Privacy; p != nil {
		if len(p.Camera) > 0 {
			lines = append(lines, i18n.T(l, "camera in use"))
		}
		if len(p.Mic) > 0 || p.MicActive {
			lines = append(lines, i18n.T(l, "microphone in use"))
		}
	}
	return heroOf(stats, lines, nil)
}
