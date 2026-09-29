package apps

import (
	"path"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// PmonArgs: una muestra de uso por proceso de nvidia-smi.
var PmonArgs = []string{"pmon", "-c", "1", "-s", "u"}

// parsePmon lee `nvidia-smi pmon -c 1 -s u`: el % de SM por pid, sumado
// entre GPUs ("-" es 0).
func parsePmon(out string) map[int]float64 {
	m := map[int]float64{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 4 || strings.HasPrefix(f[0], "#") {
			continue
		}
		pid, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		v, _ := strconv.ParseFloat(f[3], 64)
		m[pid] += v
	}
	return m
}

// drmBusy suma por pid el tiempo de motor de GPU (ns) de sus clientes DRM,
// del fdinfo de amdgpu, i915 o nouveau. Solo mira los fd que apuntan a
// /dev/dri; un cliente compartido entre fds o procesos cuenta una vez.
// skip son las direcciones PCI de las GPUs dormidas: sus fds se saltan antes
// de leer el fdinfo, para no despertarlas.
func drmBusy(fs source.FS, pids []int, skip map[string]bool) map[int]uint64 {
	out := map[int]uint64{}
	seen := map[string]bool{}
	for _, pid := range pids {
		base := "/proc/" + strconv.Itoa(pid)
		fds, _ := fs.Dir(base + "/fd")
		for _, fd := range fds {
			fd = strings.TrimSuffix(fd, ".link") // así se ven los enlaces en un fixture
			l, err := fs.Link(base + "/fd/" + fd)
			if err != nil || !strings.HasPrefix(l, "/dev/dri/") {
				continue
			}
			if len(skip) > 0 {
				node := strings.TrimPrefix(l, "/dev/dri/")
				if dev, err := fs.Link("/sys/class/drm/" + node + "/device"); err == nil && skip[path.Base(dev)] {
					continue
				}
			}
			ls, err := fs.Lines(base + "/fdinfo/" + fd)
			if err != nil {
				continue
			}
			var client, pdev string
			var ns uint64
			for _, l := range ls {
				k, v, ok := strings.Cut(l, ":")
				if !ok {
					continue
				}
				v = strings.TrimSpace(v)
				switch {
				case k == "drm-client-id":
					client = v
				case k == "drm-pdev":
					pdev = v
				case strings.HasPrefix(k, "drm-engine-") && strings.HasSuffix(v, " ns"):
					n, _ := strconv.ParseUint(strings.TrimSuffix(v, " ns"), 10, 64)
					ns += n
				}
			}
			id := pdev + "/" + client
			if client == "" || seen[id] {
				continue
			}
			seen[id] = true
			out[pid] += ns
		}
	}
	return out
}
