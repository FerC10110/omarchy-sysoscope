package view

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func init() {
	builders["about"] = aboutView
	builders["devices"] = devicesView
	builders["security"] = securityView
	actions["about"] = func(_ *model.Snapshot, o Options) []Action {
		return []Action{{Key: "f", Label: i18n.T(o.Lang, "Check firmware updates"), ID: "firmware.check", Target: "section", Mode: "background"}}
	}
}

func join(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func secs(v float64) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f s", v)
}

func yesNo(v bool, l string) string {
	if v {
		return i18n.T(l, "Yes")
	}
	return i18n.T(l, "No")
}

func aboutView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	y := s.System
	if y == nil {
		return appendUnavailable([]Block{}, s, "system")
	}
	bios := join(" ", y.BIOS.Vendor, y.BIOS.Version)
	if y.BIOS.Date != "" {
		bios += " (" + y.BIOS.Date + ")"
	}
	bs := []Block{KV(i18n.T(l, "System"), rows(
		[2]string{"OS", y.OS},
		[2]string{"Omarchy", y.Omarchy},
		[2]string{"Kernel", y.Kernel},
		[2]string{i18n.T(l, "Hostname"), y.Hostname},
		[2]string{i18n.T(l, "Computer"), join(" ", y.Product.Vendor, y.Product.Name)},
		[2]string{i18n.T(l, "Board"), join(" ", y.Board.Vendor, y.Board.Name, y.Board.Version)},
		[2]string{"BIOS", bios},
		[2]string{i18n.T(l, "Chassis"), y.Chassis},
	)...)}
	if tm := y.Time; tm != nil {
		auto := i18n.T(l, "Not available")
		if tm.CanNTP {
			auto = yesNo(tm.NTP, l)
		}
		bs = append(bs, KV(i18n.T(l, "Date and time"), rows(
			[2]string{i18n.T(l, "Time zone"), tm.Zone},
			[2]string{i18n.T(l, "Automatic sync"), auto},
			[2]string{i18n.T(l, "Synchronized"), yesNo(tm.Synced, l)},
		)...))
	}
	foreign := ""
	if y.Packages.Foreign != nil {
		foreign = strconv.Itoa(*y.Packages.Foreign)
	}
	bs = append(bs, KV(i18n.T(l, "Packages"), rows(
		[2]string{"pacman", strconv.Itoa(y.Packages.Pacman)},
		[2]string{i18n.T(l, "AUR and local"), foreign},
		[2]string{"flatpak", strconv.Itoa(y.Packages.Flatpak)},
	)...))
	since := ""
	if !y.BootTime.IsZero() {
		since = y.BootTime.Local().Format("2006-01-02 15:04")
	}
	boot := [][2]string{}
	if b := y.Boot; b != nil {
		boot = [][2]string{
			{"Firmware", secs(b.FirmwareS)},
			{"Loader", secs(b.LoaderS)},
			{"Kernel", secs(b.KernelS)},
			{"Initrd", secs(b.InitrdS)},
			{"Userspace", secs(b.UserspaceS)},
			{"Total", secs(b.TotalS)},
		}
	}
	bs = append(bs, kvIf(i18n.T(l, "Boot"), append(boot, [2]string{i18n.T(l, "Up since"), since})...)...)
	if len(y.FailedUnits) > 0 {
		var ur []Row
		for _, u := range y.FailedUnits {
			ur = append(ur, Row{ID: u.Scope + ":" + u.Name, Cells: []string{u.Name, scopeName(u.Scope, l), dash(u.Description)}})
		}
		bs = append(bs, Table(i18n.T(l, "Failed services"), []string{i18n.T(l, "Service"), i18n.T(l, "Scope"), i18n.T(l, "Description")}, ur, true))
	} else if Unavailable(s, "system.failed_units") == nil {
		bs = append(bs, Text(i18n.T(l, "No failed services"), "ok"))
	}
	bs = appendUnavailable(bs, s, "system.failed_units")
	if f := s.Firmware; f != nil {
		var fr []Row
		for _, d := range f.Devices {
			fr = append(fr, Row{ID: d.Name, Cells: []string{d.Name, dash(d.Current), dash(d.Available)}})
		}
		bs = append(bs, Table(i18n.T(l, "Firmware"), []string{i18n.T(l, "Device"), i18n.T(l, "Current"), i18n.T(l, "Available")}, fr, false))
	} else {
		bs = append(bs, Text(i18n.T(l, "Firmware updates are checked on request (press f). This contacts the LVFS service."), ""))
	}
	return appendUnavailable(bs, s, "firmware")
}

func devicesView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	y := s.System
	if y == nil {
		return appendUnavailable([]Block{}, s, "system")
	}
	var bs []Block
	var pr []Row
	for _, d := range y.PCI {
		pr = append(pr, Row{ID: d.Slot, Cells: []string{d.Slot, dash(d.Class), dash(join(" ", d.Vendor, d.Device)), dash(d.Driver)}})
	}
	bs = append(bs, Table("PCI", []string{i18n.T(l, "Slot"), i18n.T(l, "Class"), i18n.T(l, "Device"), i18n.T(l, "Driver")}, pr, false))
	bs = append(bs, usbBlocks(y, l)...)
	bs = append(bs, btBlocks(s, l)...)
	bs = append(bs, monitorBlocks(y, l)...)
	bs = append(bs, soundBlocks(y.Sound, l)...)
	if len(y.Audio) > 0 {
		var ar [][2]string
		for i, a := range y.Audio {
			ar = append(ar, [2]string{strconv.Itoa(i), a})
		}
		bs = append(bs, KV("Audio", ar...))
	}
	return bs
}

func securityView(s *model.Snapshot, o Options) []Block {
	l := o.Lang
	var bs []Block
	if p := s.Privacy; p != nil {
		if len(p.Camera) > 0 {
			bs = append(bs, Text(i18n.F(l, "Camera in use by: %s", strings.Join(p.Camera, ", ")), "warn"))
		} else {
			bs = append(bs, Text(i18n.T(l, "Camera not in use"), "ok"))
		}
		if len(p.Mic) > 0 {
			bs = append(bs, Text(i18n.F(l, "Microphone in use by: %s", strings.Join(p.Mic, ", ")), "warn"))
		} else if p.MicActive {
			bs = append(bs, Text(i18n.T(l, "Microphone in use"), "warn"))
		} else {
			bs = append(bs, Text(i18n.T(l, "Microphone not in use"), "ok"))
		}
	} else {
		bs = append(bs, Text(i18n.T(l, "Checking camera and microphone…"), ""))
	}
	bs = append(bs, exposureBlocks(s, l)...)
	bs = append(bs, encryptionBlocks(s, l)...)
	if y := s.System; y != nil {
		sb := "—"
		if y.SecureBoot != nil {
			sb = i18n.T(l, "Off")
			if *y.SecureBoot {
				sb = i18n.T(l, "On")
			}
		}
		tpm := y.TPM
		if tpm == "" {
			tpm = i18n.T(l, "none")
		}
		bs = append(bs, KV(i18n.T(l, "Platform"), [2]string{"Secure Boot", sb}, [2]string{"TPM", tpm}))
	}
	c := s.CPU
	if c == nil {
		return appendUnavailable(bs, s, "cpu")
	}
	for _, g := range []struct{ id, title string }{
		{"simd", "SIMD"},
		{"crypto", i18n.T(l, "Cryptography")},
		{"virtualization", i18n.T(l, "Virtualization")},
		{"security", i18n.T(l, "Security features")},
	} {
		var r [][2]string
		for _, k := range c.Capabilities {
			if k.Group == g.id {
				yes := i18n.T(l, "no")
				if k.Supported {
					yes = i18n.T(l, "yes")
				}
				r = append(r, [2]string{k.Name, yes})
			}
		}
		if len(r) > 0 {
			bs = append(bs, KV(g.title, r...))
		}
	}
	n := 0
	var vr []Row
	for _, v := range c.Vulnerabilities {
		if v.State == "vulnerable" {
			n++
		}
		vr = append(vr, Row{ID: v.Name, Cells: []string{v.Name, vulnState(v.State, l), dash(v.Detail)}})
	}
	if n > 0 {
		bs = append(bs, Text(i18n.F(l, "%d vulnerable", n), "crit"))
	}
	if len(vr) > 0 {
		bs = append(bs, Table(i18n.T(l, "Vulnerabilities"), []string{i18n.T(l, "Name"), i18n.T(l, "State"), i18n.T(l, "Detail")}, vr, false))
	}
	return bs
}

func vulnState(st, l string) string {
	switch st {
	case "not_affected":
		return i18n.T(l, "Not affected")
	case "mitigated":
		return i18n.T(l, "Mitigated")
	case "vulnerable":
		return i18n.T(l, "Vulnerable")
	}
	return i18n.T(l, "Unknown")
}
