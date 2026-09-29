package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePmon(t *testing.T) {
	out := `# gpu         pid   type     sm    mem    enc    dec    jpg    ofa    command
# Idx           #    C/G      %      %      %      %      %      %    name
    0        200     G      7      3      -      -      -      -    chromium
    0        201   C+G      5      2      -      -      -      -    chromium
    1        201     C      4      0      -      -      -      -    chromium
    0        300     G      -      -      -      -      -      -    kitty
`
	m := parsePmon(out)
	if m[200] != 7 || m[201] != 9 || m[300] != 0 || len(m) != 3 {
		t.Fatalf("pmon = %v", m)
	}
}

// fdinfo escribe un fd de DRM (o de otra cosa) en un /proc temporal. En el
// FS de un fixture, un enlace es un archivo "<ruta>.link".
func fdinfo(t *testing.T, root string, pid, fd int, target, info string) {
	t.Helper()
	d := filepath.Join(root, "proc", itoa(pid))
	os.MkdirAll(filepath.Join(d, "fd"), 0o755)
	os.MkdirAll(filepath.Join(d, "fdinfo"), 0o755)
	os.WriteFile(filepath.Join(d, "fd", itoa(fd)+".link"), []byte(target), 0o644)
	os.WriteFile(filepath.Join(d, "fdinfo", itoa(fd)), []byte(info), 0o644)
}

func TestDrmBusy(t *testing.T) {
	env := tempApps(t)
	root := env.FS.Root
	amd := "pos:\t0\nflags:\t02100002\ndrm-driver:\tamdgpu\ndrm-pdev:\t0000:03:00.0\ndrm-client-id:\t12\n" +
		"drm-engine-gfx:\t3000000 ns\ndrm-engine-compute:\t1000000 ns\ndrm-engine-capacity-gfx:\t1\n"
	fdinfo(t, root, 200, 5, "/dev/dri/renderD128", amd)
	fdinfo(t, root, 200, 7, "/dev/dri/renderD128", amd) // el mismo cliente por otro fd
	fdinfo(t, root, 200, 6, "/dev/null", "drm-client-id:\t99\ndrm-engine-gfx:\t9 ns\n")
	fdinfo(t, root, 201, 3, "/dev/dri/card1", "drm-pdev:\t0000:03:00.0\ndrm-client-id:\t13\ndrm-engine-render:\t500000 ns\n")
	got := drmBusy(env.FS, []int{200, 201, 300}, nil)
	if got[200] != 4000000 || got[201] != 500000 || got[300] != 0 {
		t.Fatalf("busy = %v", got)
	}
}

// Un fd sobre una GPU dormida no se cuenta: su fdinfo ni se lee, aunque
// exista y tenga un cliente propio.
func TestDrmBusySkipsSleepingGPU(t *testing.T) {
	env := tempApps(t)
	root := env.FS.Root
	os.MkdirAll(filepath.Join(root, "sys/class/drm/renderD128"), 0o755)
	os.WriteFile(filepath.Join(root, "sys/class/drm/renderD128/device.link"), []byte("../../../0000:03:00.0"), 0o644)
	os.MkdirAll(filepath.Join(root, "sys/class/drm/renderD129"), 0o755)
	os.WriteFile(filepath.Join(root, "sys/class/drm/renderD129/device.link"), []byte("../../../0000:01:00.0"), 0o644)
	fdinfo(t, root, 200, 5, "/dev/dri/renderD128", "drm-pdev:\t0000:03:00.0\ndrm-client-id:\t12\ndrm-engine-gfx:\t3000 ns\n")
	fdinfo(t, root, 200, 8, "/dev/dri/renderD129", "drm-pdev:\t0000:01:00.0\ndrm-client-id:\t40\ndrm-engine-gfx:\t7000 ns\n")
	// renderD130 no tiene enlace en /sys: se lee como antes.
	fdinfo(t, root, 201, 3, "/dev/dri/renderD130", "drm-pdev:\t0000:05:00.0\ndrm-client-id:\t50\ndrm-engine-gfx:\t100 ns\n")
	got := drmBusy(env.FS, []int{200, 201}, map[string]bool{"0000:01:00.0": true})
	if got[200] != 3000 || got[201] != 100 {
		t.Fatalf("busy = %v, want 200:3000 (renderD129 skipped), 201:100", got)
	}
	if got := drmBusy(env.FS, []int{200}, nil); got[200] != 10000 {
		t.Fatalf("without skip busy = %v, want 10000", got)
	}
}
