// Package gpu enumera las placas de video (drm) y lee sus métricas: sysfs de
// amdgpu, hwmon e nvidia-smi para NVIDIA. Una GPU suspendida (runtime PM,
// laptops Optimus) no se toca: leerla la despertaría.
package gpu

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/ids"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var cardRe = regexp.MustCompile(`^card\d+$`)

func devDir(card string) string { return "/sys/class/drm/" + card + "/device" }

// Static lee lo que no cambia: identidad, driver, enlace máximo y VBIOS.
func Static(env source.Env, r *collect.Report) []model.GPU {
	cards, _ := env.FS.Glob("/sys/class/drm/card*")
	var out []model.GPU
	for _, c := range cards {
		name := path.Base(c)
		if !cardRe.MatchString(name) {
			continue
		}
		d := devDir(name)
		g := model.GPU{Card: name}
		ue := uevent(env, d)
		g.Driver, g.PCI = ue["DRIVER"], ue["PCI_SLOT_NAME"]
		if v, dv, ok := strings.Cut(ue["PCI_ID"], ":"); ok {
			g.VendorID, g.DeviceID = strings.ToLower(v), strings.ToLower(dv)
		}
		if g.VendorID == "" {
			v, _ := env.FS.Read(d + "/vendor")
			dv, _ := env.FS.Read(d + "/device")
			g.VendorID, g.DeviceID = strings.TrimPrefix(v, "0x"), strings.TrimPrefix(dv, "0x")
		}
		if g.VendorID == "" {
			continue // no es un dispositivo PCI (p. ej. un display virtual)
		}
		switch g.VendorID {
		case "10de":
			g.Vendor = "nvidia"
		case "1002":
			g.Vendor = "amd"
		case "8086":
			g.Vendor = "intel"
		default:
			g.Vendor = "other"
		}
		vn, dn := ids.PCI(env, g.VendorID, g.DeviceID)
		switch {
		case dn != "":
			g.Name = dn
		case vn != "":
			g.Name = vn + " " + g.DeviceID
		default:
			g.Name = g.VendorID + ":" + g.DeviceID
		}
		// En amdgpu, mem_busy_percent solo existe con VRAM dedicada: su
		// ausencia marca una APU (idea de Argus). Intel siempre es integrada.
		g.Integrated = g.Vendor == "intel" || (g.Vendor == "amd" && !env.FS.Exists(d+"/mem_busy_percent"))
		if v, err := env.FS.Read(d + "/vbios_version"); err == nil {
			g.VBIOS = strings.TrimSpace(v)
		}
		if mx := speed(env, d+"/max_link_speed"); mx > 0 {
			w, _ := env.FS.ReadInt(d + "/max_link_width")
			g.Link = &model.PCIeLink{MaxGTs: mx, MaxGen: genOf(mx), MaxWidth: int(w)}
		}
		out = append(out, g)
	}
	return out
}

func uevent(env source.Env, d string) map[string]string {
	m := map[string]string{}
	lines, _ := env.FS.Lines(d + "/uevent")
	for _, l := range lines {
		if k, v, ok := strings.Cut(l, "="); ok {
			m[k] = v
		}
	}
	return m
}

// speed lee "16.0 GT/s PCIe" → 16.
func speed(env source.Env, p string) float64 {
	s, err := env.FS.Read(p)
	if err != nil {
		return 0
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

// genOf traduce GT/s a la generación PCIe (0 si no corresponde a ninguna).
func genOf(gts float64) int {
	switch gts {
	case 2.5:
		return 1
	case 5:
		return 2
	case 8:
		return 3
	case 16:
		return 4
	case 32:
		return 5
	case 64:
		return 6
	}
	return 0
}
