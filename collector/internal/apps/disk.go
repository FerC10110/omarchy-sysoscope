package apps

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// ioCount son los bytes que un proceso llevó al disco: read_bytes (lo que se
// leyó del dispositivo, no de la caché) y write_bytes − cancelled_write_bytes
// (lo que ensució en la caché para escribirse, menos lo que se descartó antes
// de escribirse). rchar y wchar no sirven: cuentan pipes, sockets y caché.
type ioCount struct{ read, write uint64 }

// parseIO lee /proc/<pid>/io. Sin read_bytes o write_bytes, o con un número
// que no se entiende, no hay dato. cancelled_write_bytes puede faltar (0); si
// supera a write_bytes, la escritura neta es 0.
func parseIO(s string) (ioCount, bool) {
	var c ioCount
	var cancelled uint64
	var hasR, hasW bool
	for _, l := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k != "read_bytes" && k != "write_bytes" && k != "cancelled_write_bytes" {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return ioCount{}, false
		}
		switch k {
		case "read_bytes":
			c.read, hasR = n, true
		case "write_bytes":
			c.write, hasW = n, true
		case "cancelled_write_bytes":
			cancelled = n
		}
	}
	if !hasR || !hasW {
		return ioCount{}, false
	}
	c.write = delta(c.write, cancelled)
	return c, true
}

// readIO lee /proc/<pid>/io. Es una variable para que los tests cuenten las
// lecturas y simulen un proceso que no deja leerlo.
var readIO = func(fs source.FS, pid int) (string, error) {
	return fs.Read("/proc/" + strconv.Itoa(pid) + "/io")
}

// delta es cur − prev, o 0 si el contador bajó (la escritura neta baja cuando
// se cancela más de lo que se escribe).
func delta(cur, prev uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}

// readDisk lee /proc/<pid>/io de cada proceso. Un proceso propio que no deja
// leerlo (se volvió no volcable después de leerlo: ssh-agent, gpg-agent…) o
// que ya terminó queda afuera. Si no se pudo leer el de ninguno, lo anota:
// sin eso la columna Disk quedaría toda en "—" sin explicación.
//
// Un proceso con base y con los mismos ticks de CPU (utime+stime) que en la
// muestra anterior no se relee (es lo que más cuesta): su contador anterior
// pasa como el de ahora, así que su diferencia es 0 y lo que acumule se ve en
// su próxima lectura real, que puede ser varias muestras después (los
// contadores son acumulados): todo cae en un solo intervalo, cuya tasa sale
// inflada. Un proceso nuevo, sin base o con ticks distintos se lee.
//
// Un proceso con una resta pendiente (sub: recogió hijos muertos, ver reaped)
// se lee siempre: recoger un hijo suma a cutime, no a utime+stime, así que sus
// ticks pueden no cambiar; si se arrastrara su contador viejo, la resta se
// perdería y todo el I/O del hijo saldría como un pico en su próxima lectura
// real.
func readDisk(fs source.FS, ps []proc, prevCPU map[pkey]uint64, base map[pkey]ioBase, sub map[pkey]ioCount, r *collect.Report) map[pkey]ioCount {
	out := make(map[pkey]ioCount, len(ps))
	var first error
	for _, p := range ps {
		_, pending := sub[p.pkey]
		if b, ok := base[p.pkey]; ok && !pending {
			if c, ok := prevCPU[p.pkey]; ok && c == p.cpu {
				out[p.pkey] = b.ioCount
				continue
			}
		}
		s, err := readIO(fs, p.pid)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if c, ok := parseIO(s); ok {
			out[p.pkey] = c
		}
	}
	if len(out) == 0 && len(ps) > 0 {
		reason := "error"
		switch {
		case errors.Is(first, os.ErrNotExist):
			reason = "not_present"
		case errors.Is(first, os.ErrPermission):
			reason = "permission"
		}
		r.Note("apps.disk", reason, i18n.T(r.Lang, "the disk use of each process could not be read"))
	}
	return out
}

// ioBase es lo que se guarda de cada proceso para la lectura siguiente: sus
// contadores y su padre.
type ioBase struct {
	ioCount
	ppid int
}

// diskBase arma la base de la lectura siguiente con los procesos legibles.
func diskBase(ps []proc, counts map[pkey]ioCount) map[pkey]ioBase {
	if counts == nil {
		return nil
	}
	out := make(map[pkey]ioBase, len(counts))
	for _, p := range ps {
		if c, ok := counts[p.pkey]; ok {
			out[p.pkey] = ioBase{c, p.ppid}
		}
	}
	return out
}

// reaped suma, para cada proceso vivo, lo último que se vio de los procesos
// que ya no están y lo tenían como ancestro más cercano vivo.
// /proc/<pid>/io incluye lo de los hijos que el proceso recogió (wait):
// cuando bash recoge un cp de 2 GB, su contador salta 2 GB que ya se
// contaron en el cp mientras vivía. Restar eso de la diferencia del padre
// evita un pico falso. Si el padre también terminó, se sube por la cadena de
// padres de la base (una app de varios procesos que se cierra entera llega
// toda a systemd --user). La resta puede quedar de más (tasa baja, saturada
// en 0, nunca un pico) cuando el kernel no le suma el hijo al padre: un hijo
// recogido solo (el padre ignora SIGCHLD o usa SA_NOCLDWAIT), o un huérfano
// que recoge un subreaper que no es su ancestro en la base. Límites
// conocidos: ese huérfano recogido por un subreaper en el mismo intervalo
// puede dar un pico de una muestra en el subreaper; un hijo que nunca estuvo en la base no se resta; un
// proceso vivo pero ilegible no se cuenta; y si el ancestro vivo no se puede
// leer en la muestra en que recoge, la resta se pierde (queda sin base, así
// que no aporta hasta su segunda lectura legible y no da un pico). Sin
// ancestro vivo en la base, no se hace nada. readDisk lee siempre a un
// proceso que aparece acá, aunque sus ticks no hayan cambiado.
func reaped(last map[pkey]ioBase, alive map[pkey]bool) map[pkey]ioCount {
	byPID := make(map[int]pkey, len(last))
	for k := range last {
		byPID[k.pid] = k
	}
	var out map[pkey]ioCount
	for k, b := range last {
		if alive[k] {
			continue
		}
		// Hasta len(last) saltos: un ciclo de ppid no lo cuelga.
		parent, ok := byPID[b.ppid]
		for n := 0; ok && !alive[parent] && n < len(last); n++ {
			parent, ok = byPID[last[parent].ppid]
		}
		if !ok || !alive[parent] {
			continue
		}
		if out == nil {
			out = map[pkey]ioCount{}
		}
		c := out[parent]
		c.read += b.read
		c.write += b.write
		out[parent] = c
	}
	return out
}
