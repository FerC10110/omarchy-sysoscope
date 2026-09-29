// Package maint lee lo de la sección Mantenimiento: si hace falta
// reiniciar, la última actualización (pacman.log), las actualizaciones
// pendientes (checkupdates, a pedido), el tamaño de las cachés y los
// snapshots de snapper. Todo de solo lectura: nada de lo que está acá
// cambia el sistema.
package maint

import (
	"path"
	"regexp"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const (
	modulesGlob = "/usr/lib/modules/*/vmlinuz"
	pacmanLocal = "/var/lib/pacman/local"
	nvidiaMod   = "/sys/module/nvidia/version"
)

// nvidiaUtils es el directorio del paquete de espacio de usuario del driver
// (nvidia-utils o nvidia-<rama>-utils, nunca lib32-): nombre-[epoch:]versión-rel.
var nvidiaUtils = regexp.MustCompile(`^nvidia(?:-[0-9a-z]+)?-utils-(?:[0-9]+:)?([^-]+)-[^-]+$`)

// IsNVIDIAUtils dice si un directorio de /var/lib/pacman/local es el paquete
// de espacio de usuario del driver NVIDIA (la captura conserva su nombre).
func IsNVIDIAUtils(dir string) bool { return nvidiaUtils.MatchString(dir) }

// ReadReboot aplica las reglas de omarchy-update-restart: el kernel en uso
// sin su vmlinuz, la marca reboot-required y Hyprland reemplazado; suma los
// servicios que Omarchy reinicia solo y el desfase del driver NVIDIA.
// kernel es el release en uso; hyprLock, la ruta de hyprland.lock ("" = sin
// Hyprland). Cuesta unos stat, un Glob, un readlink y dos lecturas cortas.
func ReadReboot(env source.Env, kernel, hyprLock string) *model.Reboot {
	rb := &model.Reboot{Kernel: kernel, Installed: []string{}}
	vms, _ := env.FS.Glob(modulesGlob)
	for _, p := range vms {
		rb.Installed = append(rb.Installed, path.Base(path.Dir(p)))
	}
	if kernel != "" && !strings.Contains(kernel, "/") && len(vms) > 0 {
		rb.KernelGone = !env.FS.Exists("/usr/lib/modules/" + kernel + "/vmlinuz")
	}
	state := env.Home + "/.local/state/omarchy"
	rb.Marker = env.FS.Exists(state + "/reboot-required")
	marks, _ := env.FS.Glob(state + "/restart-*-required")
	for _, p := range marks {
		if s := strings.TrimSuffix(strings.TrimPrefix(path.Base(p), "restart-"), "-required"); s != "" {
			rb.Services = append(rb.Services, s)
		}
	}
	if pid := HyprPID(env, hyprLock); pid != "" {
		if l, err := env.FS.Link("/proc/" + pid + "/exe"); err == nil {
			replaced := strings.HasSuffix(l, " (deleted)")
			rb.Hyprland = &replaced
		}
	}
	if v, err := env.FS.Read(nvidiaMod); err == nil && strings.TrimSpace(v) != "" {
		rb.NVIDIALoaded = strings.TrimSpace(v)
		dirs, _ := env.FS.Dir(pacmanLocal)
		for _, d := range dirs {
			if m := nvidiaUtils.FindStringSubmatch(d); m != nil {
				rb.NVIDIAInstalled = m[1]
				break
			}
		}
	}
	return rb
}

// HyprPID es el pid de Hyprland: la primera línea de su hyprland.lock, si
// son solo dígitos. "" sin lock o con algo que no es un pid.
func HyprPID(env source.Env, lock string) string {
	if lock == "" {
		return ""
	}
	lines, err := env.FS.Lines(lock)
	if err != nil || len(lines) == 0 {
		return ""
	}
	pid := strings.TrimSpace(lines[0])
	if pid == "" || strings.Trim(pid, "0123456789") != "" {
		return ""
	}
	return pid
}
