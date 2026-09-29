package security

import (
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Proc es el proceso que tiene abierto un socket: pid, nombre (comm) y el
// número de fd (la captura de fixtures lo usa para recrear el enlace).
type Proc struct {
	PID  int
	Comm string
	FD   string
}

// ScanFDs recorre una sola vez los fds de /proc/<pid>/fd que el usuario
// puede leer (los de sus procesos: los ajenos dan permiso denegado y se
// saltean). Devuelve el proceso de cada inodo de want y, si camera, los
// pids con un /dev/video* abierto. Sin cámara, se corta apenas aparecen
// todos los inodos buscados; sin nada que buscar, no recorre nada. En un
// fixture los fds son archivos "<n>.link" (FS.Link los lee).
func ScanFDs(fs source.FS, want map[uint64]bool, camera bool) (map[uint64]Proc, []int) {
	dirs := 0
	return scanFDs(fs, want, camera, &dirs)
}

// scanFDs es ScanFDs contando los directorios fd que abrió (tests).
func scanFDs(fs source.FS, want map[uint64]bool, camera bool, dirs *int) (map[uint64]Proc, []int) {
	found := map[uint64]Proc{}
	if len(want) == 0 && !camera {
		return found, nil
	}
	var cams []int
	pids, _ := fs.Dir("/proc")
	for _, d := range pids {
		pid, err := strconv.Atoi(d)
		if err != nil {
			continue
		}
		fds, err := fs.Dir("/proc/" + d + "/fd")
		if err != nil {
			continue
		}
		*dirs++
		cam := false
		for _, n := range fds {
			n = strings.TrimSuffix(n, ".link")
			l, err := fs.Link("/proc/" + d + "/fd/" + n)
			if err != nil {
				continue
			}
			if camera && strings.HasPrefix(l, "/dev/video") {
				cam = true
				continue
			}
			ino, ok := socketInode(l)
			if !ok || !want[ino] {
				continue
			}
			if _, dup := found[ino]; !dup {
				found[ino] = Proc{PID: pid, Comm: comm(fs, d), FD: n}
			}
		}
		if cam {
			cams = append(cams, pid)
		}
		if !camera && len(found) == len(want) {
			break
		}
	}
	return found, cams
}

// socketInode lee el destino de un fd de socket: "socket:[27057]".
func socketInode(l string) (uint64, bool) {
	s, ok := strings.CutPrefix(l, "socket:[")
	if !ok || !strings.HasSuffix(s, "]") {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSuffix(s, "]"), 10, 64)
	return n, err == nil
}

func comm(fs source.FS, pid string) string {
	if c, err := fs.Read("/proc/" + pid + "/comm"); err == nil && c != "" {
		return c
	}
	return "unknown"
}

// OwnInodes son los inodos de los sockets de uid: los únicos cuyo proceso
// se puede buscar sin root.
func OwnInodes(socks []Socket, uid uint32) []uint64 {
	var out []uint64
	for _, s := range socks {
		if s.UID == uid {
			out = append(out, s.Inode)
		}
	}
	return out
}

// Owners recuerda el proceso de cada socket propio ya buscado (también los
// que no aparecieron), para recorrer /proc solo cuando hay uno nuevo.
type Owners struct {
	known map[uint64]Proc // Proc{} = se buscó y no apareció
	walks int
}

// Resolve olvida los sockets que ya no están, busca los nuevos y, si
// camera, recorre todo para la cámara (Privacy): un solo paso por /proc.
// Devuelve los pids con una cámara abierta.
func (o *Owners) Resolve(fs source.FS, want []uint64, camera bool) []int {
	if o.known == nil {
		o.known = map[uint64]Proc{}
	}
	keep := make(map[uint64]bool, len(want))
	missing := map[uint64]bool{}
	for _, i := range want {
		keep[i] = true
		if _, ok := o.known[i]; !ok {
			missing[i] = true
		}
	}
	for i := range o.known {
		if !keep[i] {
			delete(o.known, i)
		}
	}
	if len(missing) == 0 && !camera {
		return nil
	}
	o.walks++
	found, cams := ScanFDs(fs, missing, camera)
	for i := range missing {
		o.known[i] = found[i]
	}
	return cams
}

// Program es el nombre del proceso de un socket propio, si se encontró.
func (o *Owners) Program(inode uint64) (string, bool) {
	p, ok := o.known[inode]
	if !ok || p.PID == 0 {
		return "", false
	}
	return p.Comm, true
}

// Walks cuenta los recorridos de /proc hechos.
func (o *Owners) Walks() int { return o.walks }
