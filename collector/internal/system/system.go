// Package system lee el inventario del equipo: DMI, sistema operativo,
// Omarchy, paquetes, tiempos de arranque, unidades caídas, dispositivos PCI
// y USB, monitores, audio, Secure Boot, TPM y firmware (Bluetooth: paquete bluetooth).
package system

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const pacmanLocal = "/var/lib/pacman/local"

// Static lee lo que no cambia durante el arranque (va en caché).
func Static(env source.Env, r *collect.Report) *model.System {
	s := &model.System{FailedUnits: []model.Unit{}}
	readDMI(env, s)
	s.Hostname = Hostname(env)
	s.OS = osName(env)
	s.Kernel, _ = env.FS.Read("/proc/sys/kernel/osrelease")
	s.Omarchy = omarchyVersion(env)
	packages(env, s, r)
	boot(env, s, r)
	s.PCI, s.USB, s.Monitors, s.Audio = pci(env), usb(env), monitors(env), audio(env)
	if b, err := env.FS.Read("/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"); err == nil && len(b) >= 5 {
		on := b[4] == 1
		s.SecureBoot = &on
	}
	if v, err := env.FS.Read("/sys/class/tpm/tpm0/tpm_version_major"); err == nil {
		if strings.TrimSpace(v) == "2" {
			s.TPM = "2.0"
		} else {
			s.TPM = "1.2"
		}
	}
	return s
}

// Hostname lee el nombre del equipo (/etc/hostname o, si no hay, el del
// kernel).
func Hostname(env source.Env) string {
	h, _ := env.FS.Read("/etc/hostname")
	if h == "" {
		h, _ = env.FS.Read("/proc/sys/kernel/hostname")
	}
	return strings.TrimSpace(h)
}

// Identity completa lo que el caché del inventario no guarda porque
// identifica al equipo: el hostname y la serie de cada monitor (se relee
// su EDID, unos pocos archivos de /sys).
func Identity(env source.Env, s *model.System) {
	if s == nil {
		return
	}
	s.Hostname = Hostname(env)
	serial := map[string]string{}
	for _, m := range monitors(env) {
		serial[m.Connector] = m.Serial
	}
	for i := range s.Monitors {
		s.Monitors[i].Serial = serial[s.Monitors[i].Connector]
	}
}

func osName(env source.Env) string {
	lines, _ := env.FS.Lines("/etc/os-release")
	name := ""
	for _, l := range lines {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		switch k {
		case "PRETTY_NAME":
			return v
		case "NAME":
			name = v
		}
	}
	return name
}

// omarchyVersion busca el paquete omarchy (u omarchy-dev) en la base local de
// pacman, como omarchy-version, sin crear un proceso.
func omarchyVersion(env source.Env) string {
	descs, _ := env.FS.Glob(pacmanLocal + "/omarchy*/desc")
	for _, d := range descs {
		name, version := pkgDesc(env, d)
		if name == "omarchy" || name == "omarchy-dev" {
			return version
		}
	}
	return ""
}

func pkgDesc(env source.Env, p string) (name, version string) {
	lines, _ := env.FS.Lines(p)
	for i := 0; i+1 < len(lines); i++ {
		switch lines[i] {
		case "%NAME%":
			name = lines[i+1]
		case "%VERSION%":
			version = lines[i+1]
		}
	}
	return name, version
}

func packages(env source.Env, s *model.System, r *collect.Report) {
	entries, err := os.ReadDir(env.FS.Real(pacmanLocal))
	if err != nil {
		r.FromErr("system.packages", "pacman", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			s.Packages.Pacman++
		}
	}
	s.Packages.Flatpak = 0
	for _, d := range []string{"/var/lib/flatpak/app", env.Home + "/.local/share/flatpak/app"} {
		if names, err := env.FS.Dir(d); err == nil {
			s.Packages.Flatpak += len(names)
		}
	}
}

var bootRe = regexp.MustCompile(`((?:\d+min )?[\d.]+m?s) \((firmware|loader|kernel|initrd|userspace)\)`)
var totalRe = regexp.MustCompile(`= ((?:\d+min )?[\d.]+m?s)`)

func boot(env source.Env, s *model.System, r *collect.Report) {
	out, err := env.Run.Run(context.Background(), "systemd-analyze", "time")
	if err != nil {
		r.FromErr("system.boot", "systemd-analyze", err)
		return
	}
	text := string(out)
	b := &model.BootTiming{}
	for _, m := range bootRe.FindAllStringSubmatch(text, -1) {
		v := duration(m[1])
		switch m[2] {
		case "firmware":
			b.FirmwareS = v
		case "loader":
			b.LoaderS = v
		case "kernel":
			b.KernelS = v
		case "initrd":
			b.InitrdS = v
		case "userspace":
			b.UserspaceS = v
		}
	}
	if m := totalRe.FindStringSubmatch(text); m != nil {
		b.TotalS = duration(m[1])
	}
	if b.TotalS == 0 {
		r.Absent("system.boot")
		return
	}
	s.Boot = b
}

// duration interpreta "1min 20.260s", "4.541s" o "850ms" en segundos, con
// milisegundos exactos.
func duration(s string) float64 {
	ms := int64(0)
	for _, part := range strings.Fields(s) {
		switch {
		case strings.HasSuffix(part, "min"):
			n, _ := strconv.ParseInt(strings.TrimSuffix(part, "min"), 10, 64)
			ms += n * 60000
		case strings.HasSuffix(part, "ms"):
			f, _ := strconv.ParseFloat(strings.TrimSuffix(part, "ms"), 64)
			ms += int64(f + 0.5)
		case strings.HasSuffix(part, "s"):
			f, _ := strconv.ParseFloat(strings.TrimSuffix(part, "s"), 64)
			ms += int64(f*1000 + 0.5)
		}
	}
	return float64(ms) / 1000
}

// Slow actualiza cada 60 s: uptime, zona horaria y NTP, paquetes, versión de Omarchy, USB,
// monitores y unidades caídas (Bluetooth lo lee el engine con bluetooth.Sample).
// Devuelve el estado de las unidades de WatchedUnits: una sola llamada a
// systemd1 que comparten el reloj y el firewall de Seguridad.
func Slow(env source.Env, s *model.System, r *collect.Report) Units {
	s.Packages.Pacman = 0
	packages(env, s, r)
	s.Omarchy = omarchyVersion(env)             // cambia con omarchy-update
	s.USB, s.Monitors = usb(env), monitors(env) // se enchufan y desenchufan
	if up, err := env.FS.Read("/proc/uptime"); err == nil {
		if f := strings.Fields(up); len(f) > 0 {
			s.UptimeS, _ = strconv.ParseFloat(f[0], 64)
			s.BootTime = env.Now().Add(-time.Duration(s.UptimeS * float64(time.Second))).UTC()
		}
	}
	units := ListUnits(env)
	s.Time = clock(env, units) // zona horaria y NTP
	s.FailedUnits = []model.Unit{}
	sysUnits, err := failedUnits(env, true)
	if err != nil {
		r.Missing("system.failed_units", "systemd")
	}
	userUnits, _ := failedUnits(env, false)
	s.FailedUnits = append(append(s.FailedUnits, sysUnits...), userUnits...)
	return units
}

func failedUnits(env source.Env, system bool) ([]model.Unit, error) {
	body, err := env.Bus.Call(system, "org.freedesktop.systemd1", "/org/freedesktop/systemd1",
		"org.freedesktop.systemd1.Manager.ListUnitsFiltered", []string{"failed"})
	if err != nil {
		return nil, err
	}
	scope := "user"
	if system {
		scope = "system"
	}
	var out []model.Unit
	if len(body) == 0 {
		return out, nil
	}
	list, _ := body[0].([]any)
	for _, it := range list {
		f, ok := it.([]any)
		if !ok || len(f) < 2 {
			continue
		}
		name, _ := f[0].(string)
		desc, _ := f[1].(string)
		out = append(out, model.Unit{Name: name, Scope: scope, Description: desc})
	}
	return out, nil
}

// Foreign cuenta los paquetes que no vienen de los repositorios (AUR y
// locales) con `pacman -Qqm`.
func Foreign(env source.Env, s *model.System, r *collect.Report) {
	out, err := env.Run.Run(context.Background(), "pacman", "-Qqm")
	if err != nil && source.ExitStatus(err) != 1 {
		r.FromErr("system.packages.foreign", "pacman", err)
		s.Packages.Foreign = nil
		return
	}
	n := 0
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	s.Packages.Foreign = &n
}
