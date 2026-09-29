package system

import (
	"path"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/ids"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/text"
)

// pciClass nombra la clase base PCI. Los textos son datos (el panel los traduce).
var pciClass = map[string]string{
	"01": "Storage", "02": "Network", "03": "Display", "04": "Multimedia", "05": "Memory",
	"06": "Bridge", "07": "Communication", "08": "System", "09": "Input", "0c": "Serial bus",
	"0d": "Wireless", "10": "Encryption", "11": "Signal processing", "12": "Accelerator",
}

func pci(env source.Env) []model.PCIDevice {
	devs, _ := env.FS.Glob("/sys/bus/pci/devices/*")
	var out []model.PCIDevice
	for _, d := range devs {
		cls, err := env.FS.Read(d + "/class")
		if err != nil {
			continue
		}
		cls = strings.TrimPrefix(strings.TrimSpace(cls), "0x")
		if len(cls) < 4 {
			continue
		}
		if sub := cls[:4]; sub >= "0600" && sub <= "0604" {
			continue // puentes del chipset: ruido
		}
		v, _ := env.FS.Read(d + "/vendor")
		dv, _ := env.FS.Read(d + "/device")
		p := model.PCIDevice{Slot: path.Base(d), VendorID: strings.TrimPrefix(v, "0x"), DeviceID: strings.TrimPrefix(dv, "0x")}
		p.Class = pciClass[cls[:2]]
		if p.Class == "" {
			p.Class = "Other"
		}
		p.Vendor, p.Device = ids.PCI(env, p.VendorID, p.DeviceID)
		if l, err := env.FS.Link(d + "/driver"); err == nil {
			p.Driver = path.Base(l)
		}
		out = append(out, p)
	}
	return out
}

func usb(env source.Env) []model.USBDevice {
	devs, _ := env.FS.Glob("/sys/bus/usb/devices/*")
	var out []model.USBDevice
	for _, d := range devs {
		name := path.Base(d)
		if strings.Contains(name, ":") {
			continue // interfaces
		}
		vid, err := env.FS.Read(d + "/idVendor")
		if err != nil || vid == "1d6b" {
			continue // hubs raíz del kernel
		}
		pid, _ := env.FS.Read(d + "/idProduct")
		u := model.USBDevice{Path: name, VendorID: vid, ProductID: pid}
		vn, pn := ids.USB(env, vid, pid)
		u.Vendor, _ = env.FS.Read(d + "/manufacturer")
		u.Product, _ = env.FS.Read(d + "/product")
		if u.Vendor = strings.TrimSpace(text.Clean(u.Vendor)); u.Vendor == "" {
			u.Vendor = vn
		}
		if u.Product = strings.TrimSpace(text.Clean(u.Product)); u.Product == "" {
			u.Product = pn
		}
		if s, err := env.FS.Read(d + "/speed"); err == nil {
			u.SpeedMbps, _ = strconv.ParseFloat(strings.TrimSpace(s), 64)
		}
		if l, err := env.FS.Link(d + "/" + name + ":1.0/driver"); err == nil {
			u.Driver = path.Base(l)
		}
		// bcdUSB (" 3.10"), la clase de la primera interfaz (08 almacenamiento,
		// 0e video, 09 hub) y si el puerto donde está enchufado tiene par
		// SuperSpeed: el puerto USB 2 físico tiene port/peer al USB 3.
		if v, err := env.FS.Read(d + "/version"); err == nil {
			u.Version = strings.TrimSpace(v)
		}
		if c, err := env.FS.Read(d + "/" + name + ":1.0/bInterfaceClass"); err == nil {
			u.Class = strings.TrimSpace(c)
		}
		if _, err := env.FS.Link(d + "/port/peer"); err == nil {
			u.SSPort = true
		}
		out = append(out, u)
	}
	return out
}

func monitors(env source.Env) []model.Monitor {
	conns, _ := env.FS.Glob("/sys/class/drm/card*-*")
	var out []model.Monitor
	for _, c := range conns {
		_, conn, _ := strings.Cut(path.Base(c), "-")
		st, err := env.FS.Read(c + "/status")
		if err != nil {
			continue
		}
		m := model.Monitor{Connector: conn, Connected: strings.TrimSpace(st) == "connected"}
		if m.Connected {
			if e, err := env.FS.Bytes(c + "/edid"); err == nil {
				parseEDID(e, &m)
			}
		}
		out = append(out, m)
	}
	return out
}

func audio(env source.Env) []string {
	lines, _ := env.FS.Lines("/proc/asound/cards")
	var out []string
	for _, l := range lines {
		// " 0 [Generic        ]: HDA-Intel - HD-Audio Generic"
		if i := strings.Index(l, "]:"); i >= 0 {
			if _, name, ok := strings.Cut(l[i+2:], " - "); ok {
				out = append(out, strings.TrimSpace(name))
			}
		}
	}
	return out
}
