package power

import (
	"path"
	"strings"

	hwids "github.com/FerC10110/omarchy-sysoscope/collector/internal/ids"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/text"
)

// Sleep lee, con lo lento, el modo de suspensión (/sys/power/mem_sleep) y
// qué puede despertar el equipo: dispositivos USB hoja y placas de red PCI
// con power/wakeup = enabled. Puentes PCI, controladoras y hubs USB no se
// listan: son entradas sin significado para el usuario (por lo mismo no se
// usa /proc/acpi/wakeup).
func Sleep(env source.Env, p *model.Power) {
	p.SleepMode, p.SleepModes = memSleep(env)
	p.Wakeup = wakeup(env)
}

// memSleep: "s2idle [deep]" → "deep" y [s2idle deep]. Sin el archivo, nada.
func memSleep(env source.Env) (string, []string) {
	s, err := env.FS.Read("/sys/power/mem_sleep")
	if err != nil {
		return "", nil
	}
	mode := ""
	var all []string
	for _, f := range strings.Fields(s) {
		if m, ok := strings.CutPrefix(f, "["); ok {
			f = strings.TrimSuffix(m, "]")
			mode = f
		}
		if f != "" {
			all = append(all, f)
		}
	}
	return mode, all
}

func wakeEnabled(env source.Env, dev string) bool {
	s, err := env.FS.Read(dev + "/power/wakeup")
	return err == nil && strings.TrimSpace(s) == "enabled"
}

func wakeup(env source.Env) []model.WakeDevice {
	var out []model.WakeDevice
	usbs, _ := env.FS.Glob("/sys/bus/usb/devices/*")
	for _, d := range usbs {
		name := path.Base(d)
		// interfaces (1-5:1.0) y hubs raíz (usb1) fuera
		if strings.Contains(name, ":") || strings.HasPrefix(name, "usb") || !wakeEnabled(env, d) {
			continue
		}
		if c, _ := env.FS.Read(d + "/bDeviceClass"); strings.TrimSpace(c) == "09" {
			continue // un hub
		}
		out = append(out, model.WakeDevice{Name: usbName(env, d, name), Kind: "usb", Path: name})
	}
	pcis, _ := env.FS.Glob("/sys/bus/pci/devices/*")
	for _, d := range pcis {
		cls, err := env.FS.Read(d + "/class")
		if err != nil || !strings.HasPrefix(strings.TrimSpace(cls), "0x02") || !wakeEnabled(env, d) {
			continue // solo placas de red (clase 0x02…)
		}
		out = append(out, model.WakeDevice{Name: pciName(env, d), Kind: "net", Path: path.Base(d)})
	}
	return out
}

// usbName: product (lo escribe el firmware: puede traer secuencias ANSI o
// caracteres de control/formato), si no el nombre de usb.ids, si no el
// puerto.
func usbName(env source.Env, d, name string) string {
	if p, err := env.FS.Read(d + "/product"); err == nil {
		if p = text.Clean(strings.TrimSpace(p)); p != "" {
			return p
		}
	}
	vid, _ := env.FS.Read(d + "/idVendor")
	pid, _ := env.FS.Read(d + "/idProduct")
	if _, pn := hwids.USB(env, vid, pid); pn != "" {
		return pn
	}
	return name
}

// pciName: fabricante y dispositivo de pci.ids, o el slot.
func pciName(env source.Env, d string) string {
	v, _ := env.FS.Read(d + "/vendor")
	dv, _ := env.FS.Read(d + "/device")
	if vn, dn := hwids.PCI(env, v, dv); dn != "" {
		return strings.TrimSpace(vn + " " + dn)
	}
	return path.Base(d)
}
