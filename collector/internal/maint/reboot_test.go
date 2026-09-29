package maint

import (
	"reflect"
	"testing"
)

func TestRebootKernel(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, "root/usr/lib/modules/7.2.3-arch1-3/vmlinuz", "")
	put(t, dir, "root/usr/lib/modules/7.2.5-3-omarchy/vmlinuz", "")
	// Un kernel quitado: dkms dejó su directorio, sin vmlinuz.
	put(t, dir, "root/usr/lib/modules/7.2.1-1/updates/dkms/nvidia.ko", "")
	rb := ReadReboot(env, "7.2.5-3-omarchy", "")
	if rb.Kernel != "7.2.5-3-omarchy" || rb.KernelGone || rb.Marker || rb.Hyprland != nil || rb.NVIDIALoaded != "" ||
		!reflect.DeepEqual(rb.Installed, []string{"7.2.3-arch1-3", "7.2.5-3-omarchy"}) {
		t.Fatalf("reboot = %+v", rb)
	}
	if rb := ReadReboot(env, "7.2.1-1", ""); !rb.KernelGone {
		t.Errorf("a kernel with only its dkms directory left must be gone: %+v", rb)
	}
	// Sin ningún vmlinuz (un contenedor, otra distribución): sin aviso.
	bare, _ := tempEnv(t)
	if rb := ReadReboot(bare, "7.2.5-3-omarchy", ""); rb.KernelGone || rb.Installed == nil || len(rb.Installed) != 0 {
		t.Errorf("no vmlinuz at all = %+v", rb)
	}
	// Un release con barra no se usa como ruta.
	if rb := ReadReboot(env, "../../etc", ""); rb.KernelGone {
		t.Errorf("a kernel release with a slash = %+v", rb)
	}
}

func TestRebootOmarchyMarkers(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, "root/home/user/.local/state/omarchy/reboot-required", "")
	put(t, dir, "root/home/user/.local/state/omarchy/restart-walker-required", "")
	put(t, dir, "root/home/user/.local/state/omarchy/restart-pipewire-required", "")
	rb := ReadReboot(env, "x", "")
	if !rb.Marker || !reflect.DeepEqual(rb.Services, []string{"pipewire", "walker"}) {
		t.Fatalf("reboot = %+v", rb)
	}
}

func TestRebootHyprland(t *testing.T) {
	env, dir := tempEnv(t)
	const lock = "/run/user/1000/hypr/sig/hyprland.lock"
	put(t, dir, "root"+lock, "613755\nwayland-1\n")
	put(t, dir, "root/proc/613755/exe.link", "/usr/bin/Hyprland (deleted)\n")
	if rb := ReadReboot(env, "x", lock); rb.Hyprland == nil || !*rb.Hyprland {
		t.Fatalf("replaced Hyprland = %+v", rb.Hyprland)
	}
	put(t, dir, "root/proc/613755/exe.link", "/usr/bin/Hyprland\n")
	if rb := ReadReboot(env, "x", lock); rb.Hyprland == nil || *rb.Hyprland {
		t.Fatalf("running Hyprland = %+v", rb.Hyprland)
	}
	put(t, dir, "root"+lock, "not-a-pid\n")
	if rb := ReadReboot(env, "x", lock); rb.Hyprland != nil {
		t.Errorf("garbage lock = %v", *rb.Hyprland)
	}
	if rb := ReadReboot(env, "x", "/run/user/1000/hypr/other/hyprland.lock"); rb.Hyprland != nil {
		t.Error("missing lock must mean no Hyprland")
	}
	if rb := ReadReboot(env, "x", ""); rb.Hyprland != nil {
		t.Error("no lock path must mean no Hyprland")
	}
	if HyprPID(env, lock) != "" {
		t.Error("HyprPID of a garbage lock")
	}
}

func TestRebootNVIDIA(t *testing.T) {
	cases := []struct {
		name      string
		module    bool
		pkgs      []string
		installed string
	}{
		{"same", true, []string{"nvidia-utils-610.57.04-1", "opencl-nvidia-610.57.04-1", "nvidia-open-dkms-610.57.04-1"}, "610.57.04"},
		{"legacy branch", true, []string{"nvidia-470xx-utils-470.256.02-5"}, "470.256.02"},
		{"epoch", true, []string{"nvidia-utils-1:610.57.04-1"}, "610.57.04"},
		{"lib32 only", true, []string{"lib32-nvidia-utils-610.60.01-1", "libnvidia-container-1.20.0-1"}, ""},
		{"no package", true, nil, ""},
		{"no module", false, []string{"nvidia-utils-610.57.04-1"}, ""},
	}
	for _, c := range cases {
		env, dir := tempEnv(t)
		if c.module {
			put(t, dir, "root/sys/module/nvidia/version", "610.57.04\n")
		}
		for _, p := range c.pkgs {
			put(t, dir, "root/var/lib/pacman/local/"+p+"/desc", "")
		}
		rb := ReadReboot(env, "x", "")
		loaded := ""
		if c.module {
			loaded = "610.57.04"
		}
		if rb.NVIDIALoaded != loaded || rb.NVIDIAInstalled != c.installed {
			t.Errorf("%s: loaded %q installed %q", c.name, rb.NVIDIALoaded, rb.NVIDIAInstalled)
		}
	}
	for dir, want := range map[string]bool{"nvidia-utils-610.57.04-1": true, "nvidia-550xx-utils-550.1-2": true,
		"lib32-nvidia-utils-610.57.04-1": false, "nvidia-settings-610.57.04-1": false, "nvidia-utils": false} {
		if IsNVIDIAUtils(dir) != want {
			t.Errorf("IsNVIDIAUtils(%q) = %v", dir, !want)
		}
	}
}
