package maint

import (
	"bytes"
	"io"
	"os"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// PacmanLog es el registro de pacman (legible por todos).
const PacmanLog = "/var/log/pacman.log"

const (
	tailBytes = 256 << 10 // la última actualización casi siempre está acá
	maxBytes  = 64 << 20  // tope si hay que retroceder más
)

// chunkBytes es el bloque de scanBack; var (no const) para que los tests lo
// achiquen y puedan construir un borde de bloque exacto.
var chunkBytes int64 = 256 << 10

// LogReader lee pacman.log solo cuando cambió su mtime o su tamaño. El
// valor cero está listo para usar.
type LogReader struct {
	seen bool
	mod  time.Time
	size int64
	last *model.Upgrade
	err  error
}

// Read devuelve la última actualización completa, sus paquetes y el último
// cambio de paquetes. nil si el archivo no se pudo leer (y lo registra en r
// como maintenance.upgrade, también cuando se reusa la lectura anterior);
// un Upgrade vacío si no hay registros.
func (l *LogReader) Read(env source.Env, r *collect.Report) *model.Upgrade {
	fi, err := os.Stat(env.FS.Real(PacmanLog))
	if err != nil {
		l.seen, l.last, l.err = false, nil, err
		r.FromErr("maintenance.upgrade", "", err)
		return nil
	}
	if !l.seen || !fi.ModTime().Equal(l.mod) || fi.Size() != l.size {
		l.seen, l.mod, l.size = true, fi.ModTime(), fi.Size()
		l.last, l.err = readUpgrade(env.FS.Real(PacmanLog), fi.Size())
	}
	if l.err != nil {
		r.FromErr("maintenance.upgrade", "", l.err)
		return nil
	}
	return l.last
}

// readUpgrade lee los últimos 256 KiB; si ahí no está la línea de
// actualización completa, retrocede con scanBack (hasta 64 MiB en total,
// sin leerlos de una sola vez).
func readUpgrade(p string, size int64) (*model.Upgrade, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n := min(tailBytes, size)
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, size-n); err != nil && err != io.EOF {
		return nil, err
	}
	cut := n < size
	up, found := parseLog(buf, cut)
	if !found && cut {
		if buf, cut, err = scanBack(f, size); err != nil {
			return nil, err
		}
		full, _ := parseLog(buf, cut)
		up = full
	}
	return &up, nil
}

// scanBack retrocede desde el final del archivo en bloques de chunkBytes
// (hasta maxBytes en total) buscando "starting full system upgrade". En
// cada bloque nuevo descarta la primera línea si quedó partida (como hace
// parseLog con cut) y recién ahí busca: así una línea repartida entre dos
// bloques no se pierde ni se toma por encontrada a medias. Se detiene en el
// primer bloque donde, ya recortado, aparece completa.
func scanBack(r io.ReaderAt, size int64) ([]byte, bool, error) {
	marker := []byte("[PACMAN] starting full system upgrade")
	window := min(size, maxBytes)
	buf := make([]byte, window)
	var read int64
	for read < window {
		step := min(chunkBytes, window-read)
		start := window - read - step
		if _, err := r.ReadAt(buf[start:start+step], size-read-step); err != nil && err != io.EOF {
			return nil, false, err
		}
		read += step
		cand, cut := buf[window-read:], read < size
		if cut {
			if i := bytes.IndexByte(cand, '\n'); i >= 0 {
				cand = cand[i+1:]
			} else {
				continue // ninguna línea completa todavía: seguir retrocediendo
			}
		}
		if bytes.Contains(cand, marker) {
			return cand, false, nil // ya recortada la línea partida: cut no aplica de nuevo
		}
	}
	return buf, read < size, nil
}

// parseLog recorre las líneas en orden: cada "starting full system upgrade"
// reinicia la cuenta de "upgraded", que termina en el próximo "[PACMAN]
// Running". cut dice que la primera línea puede estar cortada (se saltea).
// found dice si hubo una línea de actualización completa.
func parseLog(data []byte, cut bool) (model.Upgrade, bool) {
	var up model.Upgrade
	if cut {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			data = nil
		}
	}
	found, counting := false, false
	for _, line := range strings.Split(string(data), "\n") {
		end := strings.Index(line, "] ")
		if !strings.HasPrefix(line, "[") || end < 0 {
			continue
		}
		t, ok := logTime(line[1:end])
		if !ok {
			continue
		}
		msg := line[end+2:]
		switch {
		case msg == "[PACMAN] starting full system upgrade":
			up.LastFull, up.FullPackages, found, counting = t, 0, true, true
		case strings.HasPrefix(msg, "[PACMAN] Running "):
			counting = false
		case counting && strings.HasPrefix(msg, "[ALPM] upgraded "):
			up.FullPackages++
		case msg == "[ALPM] transaction completed":
			up.LastChange = t
		}
	}
	return up, found
}

// ParseLogTime expone logTime: interpreta la fecha de una línea de
// pacman.log (nueva o vieja) y devuelve el instante en UTC. La usa la
// captura de fixtures para reescribir el formato nuevo sin la zona horaria
// del dueño de la máquina (ver fixture.pacmanTS).
func ParseLogTime(s string) (time.Time, bool) { return logTime(s) }

// logTime entiende "2026-09-22T20:41:32-0300" y el formato viejo
// "2019-03-01 10:00" (hora local). Devuelve UTC.
func logTime(s string) (time.Time, bool) {
	if t, err := time.Parse("2006-01-02T15:04:05-0700", s); err == nil {
		return t.UTC(), true
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}
