// Package apps agrupa los procesos del usuario en aplicaciones, les suma el
// consumo (CPU, memoria, GPU, red TCP), les pega sus ventanas de Hyprland y
// corre las acciones de la sección Aplicaciones sobre ventanas y apps.
package apps

import (
	"os"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// clkTck son los ticks por segundo de /proc/<pid>/stat (USER_HZ: 100 en Linux).
const clkTck = 100

var pageSize = uint64(os.Getpagesize())

// pkey identifica un proceso: un pid se recicla, un pid con su arranque no.
type pkey struct {
	pid   int
	start uint64
}

type proc struct {
	pkey
	ppid   int
	comm   string
	cpu    uint64 // utime+stime, en ticks
	rssB   uint64
	cgroup string // "/user.slice/…/app-….scope"
}

// meta es lo que no cambia en la vida de un proceso; se lee una vez.
type meta struct {
	start  uint64
	own    bool
	cgroup string
}

// parseStat lee /proc/<pid>/stat. comm va entre el primer "(" y el último ")"
// porque puede tener espacios y paréntesis.
func parseStat(s string) (proc, bool) {
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return proc{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(s[:open]))
	if err != nil {
		return proc{}, false
	}
	f := strings.Fields(s[end+1:])
	// f[0] es el campo 3 (el estado): el campo n está en f[n-3].
	if len(f) < 22 {
		return proc{}, false
	}
	n := func(field int) uint64 { v, _ := strconv.ParseUint(f[field-3], 10, 64); return v }
	ppid, _ := strconv.Atoi(f[1])
	return proc{pkey: pkey{pid: pid, start: n(22)}, ppid: ppid, comm: s[open+1 : end],
		cpu: n(14) + n(15), rssB: n(24) * pageSize}, true
}

// readProcs devuelve los procesos de uid. cache guarda por pid si es del
// usuario y su cgroup: un proceso ajeno no se vuelve a leer mientras viva.
func readProcs(fs source.FS, uid uint32, cache map[int]meta) []proc {
	dirs, _ := fs.Dir("/proc")
	alive := make(map[int]bool, len(dirs))
	var out []proc
	for _, d := range dirs {
		pid, err := strconv.Atoi(d)
		if err != nil {
			continue
		}
		alive[pid] = true
		m, cached := cache[pid]
		if cached && !m.own {
			continue
		}
		s, err := fs.Read("/proc/" + d + "/stat")
		if err != nil {
			continue
		}
		p, ok := parseStat(s)
		if !ok || p.pid != pid {
			continue
		}
		if !cached || m.start != p.start {
			m = meta{start: p.start}
			if o, err := fs.Owner("/proc/" + d); err == nil && o == uid {
				m.own = true
				m.cgroup = cgroupOf(fs, pid)
			}
			cache[pid] = m
		}
		if !m.own {
			continue
		}
		p.cgroup = m.cgroup
		out = append(out, p)
	}
	for pid := range cache {
		if !alive[pid] {
			delete(cache, pid)
		}
	}
	return out
}

// cgroupOf es la ruta de cgroup v2 (la línea "0::…"), o "".
func cgroupOf(fs source.FS, pid int) string {
	ls, _ := fs.Lines("/proc/" + strconv.Itoa(pid) + "/cgroup")
	for _, l := range ls {
		if strings.HasPrefix(l, "0::") {
			return l[3:]
		}
	}
	return ""
}

// startOf es el arranque del proceso pid (campo 22), o 0 si ya no está.
func startOf(fs source.FS, pid int) uint64 {
	s, err := fs.Read("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	p, ok := parseStat(s)
	if !ok {
		return 0
	}
	return p.start
}
