package maint

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
)

const newLog = `[2026-09-17T22:56:22-0300] [PACMAN] Running 'pacman -Syu'
[2026-09-17T22:56:23-0300] [PACMAN] starting full system upgrade
[2026-09-17T22:57:01-0300] [ALPM] upgraded a (1-1 -> 2-1)
[2026-09-17T22:57:09-0300] [ALPM] transaction completed
[2026-09-22T20:41:31-0300] [PACMAN] Running 'pacman -Syu --noconfirm'
[2026-09-22T20:41:32-0300] [PACMAN] starting full system upgrade
[2026-09-22T20:41:34-0300] [ALPM] transaction started
[2026-09-22T20:41:34-0300] [ALPM] upgraded pkg1 (4.29.9-1 -> 4.30.0-1)
[2026-09-22T20:41:34-0300] [ALPM] upgraded pkg2 (2026.9.9-1 -> 2026.9.12-1)
[2026-09-22T20:41:34-0300] [ALPM] transaction completed
[2026-09-22T20:41:52-0300] [PACMAN] Running 'pacman -U /home/user/.cache/yay/x.pkg.tar.zst'
[2026-09-22T20:41:52-0300] [ALPM] upgraded pkg3 (2.0.2-1 -> 2.1.0-1)
[2026-09-22T20:42:25-0300] [ALPM] transaction completed
`

func TestPacmanLogFormats(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, "root"+PacmanLog, newLog)
	var l LogReader
	r := &collect.Report{Lang: "en"}
	up := l.Read(env, r)
	if up == nil || len(r.List) != 0 {
		t.Fatalf("up = %+v, unavailable %+v", up, r.List)
	}
	if !up.LastFull.Equal(time.Date(2026, 9, 22, 23, 41, 32, 0, time.UTC)) || up.FullPackages != 2 ||
		!up.LastChange.Equal(time.Date(2026, 9, 22, 23, 42, 25, 0, time.UTC)) || up.LastFull.Location() != time.UTC {
		t.Errorf("new format = %+v", up)
	}
	// El formato viejo, sin segundos ni zona: hora local.
	put(t, dir, "root"+PacmanLog, "[2019-03-01 10:00] [PACMAN] starting full system upgrade\n"+
		"[2019-03-01 10:01] [ALPM] upgraded x (1 -> 2)\n[2019-03-01 10:01] [ALPM] upgraded y (1 -> 2)\n"+
		"[2019-03-01 10:02] [ALPM] transaction completed\n")
	old := l.Read(env, r)
	if !old.LastFull.Equal(time.Date(2019, 3, 1, 10, 0, 0, 0, time.Local)) || old.FullPackages != 2 ||
		!old.LastChange.Equal(time.Date(2019, 3, 1, 10, 2, 0, 0, time.Local)) {
		t.Errorf("old format = %+v", old)
	}
	// Una actualización completa sin paquetes (ya estaba todo al día).
	put(t, dir, "root"+PacmanLog, "[2026-09-20T10:00:00-0300] [PACMAN] starting full system upgrade\n"+
		"[2026-09-20T10:00:05-0300] [PACMAN] Running 'pacman -S foo'\n[2026-09-20T10:00:06-0300] [ALPM] installed foo (1-1)\n"+
		"[2026-09-20T10:00:06-0300] [ALPM] transaction completed\n")
	if zero := l.Read(env, r); zero.LastFull.IsZero() || zero.FullPackages != 0 {
		t.Errorf("0 packages = %+v", zero)
	}
	// Sin la línea: instalación nueva.
	put(t, dir, "root"+PacmanLog, "[2026-09-20T10:00:06-0300] [ALPM] installed foo (1-1)\n[2026-09-20T10:00:06-0300] [ALPM] transaction completed\n")
	if none := l.Read(env, r); !none.LastFull.IsZero() || none.LastChange.IsZero() {
		t.Errorf("no full upgrade = %+v", none)
	}
}

// Review Focus 1: basura, vacío, faltante.
func TestPacmanLogGarbage(t *testing.T) {
	env, dir := tempEnv(t)
	for _, content := range []string{"", "\x00\x01garbage\n[not a date] [PACMAN] starting full system upgrade\n[",
		"[2026-09-22T20:41:32-0300]\n] [PACMAN] starting full system upgrade", strings.Repeat("x", 1000)} {
		put(t, dir, "root"+PacmanLog, content)
		var l LogReader
		r := &collect.Report{Lang: "en"}
		up := l.Read(env, r)
		if up == nil || !up.LastFull.IsZero() || !up.LastChange.IsZero() || up.FullPackages != 0 || len(r.List) != 0 {
			t.Errorf("%q = %+v %+v", content, up, r.List)
		}
	}
	missing, _ := tempEnv(t)
	var l LogReader
	r := &collect.Report{Lang: "en"}
	if up := l.Read(missing, r); up != nil || len(r.List) != 1 || r.List[0].What != "maintenance.upgrade" || r.List[0].Reason != "not_present" {
		t.Errorf("missing = %+v %+v", up, r.List)
	}
	// La lectura cacheada vuelve a explicar el hueco en cada muestra.
	r2 := &collect.Report{Lang: "en"}
	if up := l.Read(missing, r2); up != nil || len(r2.List) != 1 {
		t.Errorf("missing again = %+v %+v", up, r2.List)
	}
}

// La línea queda antes de los últimos 256 KiB: se lee el archivo entero.
func TestPacmanLogTailMiss(t *testing.T) {
	env, dir := tempEnv(t)
	var b strings.Builder
	b.WriteString("[2026-09-01T10:00:00-0300] [PACMAN] starting full system upgrade\n[2026-09-01T10:00:01-0300] [ALPM] upgraded a (1 -> 2)\n")
	b.WriteString("[2026-09-01T10:00:02-0300] [PACMAN] Running 'pacman -S filler'\n")
	for b.Len() < 300<<10 {
		b.WriteString("[2026-09-02T10:00:00-0300] [ALPM] installed filler-package-with-a-long-name (1.0.0-1)\n")
	}
	b.WriteString("[2026-09-03T10:00:00-0300] [ALPM] transaction completed\n")
	put(t, dir, "root"+PacmanLog, b.String())
	var l LogReader
	up := l.Read(env, &collect.Report{Lang: "en"})
	if !up.LastFull.Equal(time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)) || up.FullPackages != 1 ||
		!up.LastChange.Equal(time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("tail miss = %+v", up)
	}
}

// Si la línea de actualización completa queda antes de los últimos 256 KiB
// pero la de "transaction completed" también (Task 6 review): el último
// cambio no debe perderse al releer el archivo entero.
func TestPacmanLogTailMissKeepsLastChange(t *testing.T) {
	env, dir := tempEnv(t)
	var b strings.Builder
	b.WriteString("[2026-09-01T10:00:00-0300] [PACMAN] starting full system upgrade\n[2026-09-01T10:00:01-0300] [ALPM] upgraded a (1 -> 2)\n")
	b.WriteString("[2026-09-01T10:00:02-0300] [PACMAN] Running 'pacman -S filler'\n")
	b.WriteString("[2026-09-01T10:00:03-0300] [ALPM] transaction completed\n")
	for b.Len() < 300<<10 {
		b.WriteString("[2026-09-02T10:00:00-0300] [ALPM] installed filler-package-with-a-long-name (1.0.0-1)\n")
	}
	put(t, dir, "root"+PacmanLog, b.String())
	var l LogReader
	up := l.Read(env, &collect.Report{Lang: "en"})
	if up.LastChange.IsZero() {
		t.Fatalf("LastChange perdido: %+v", up)
	}
	if !up.LastChange.Equal(time.Date(2026, 9, 1, 13, 0, 3, 0, time.UTC)) {
		t.Fatalf("LastChange = %v, want 2026-09-01T13:00:03Z", up.LastChange)
	}
}

// countingReaderAt cuenta cuántos bytes se pidieron con ReadAt, para probar
// que scanBack no lee de más (Task 6 review: cost).
type countingReaderAt struct {
	data []byte
	read int64
}

func (r *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n := copy(p, r.data[off:])
	r.read += int64(n)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// Una línea de actualización completa partida justo entre dos bloques de
// scanBack no debe perderse.
func TestPacmanLogScanBackSplitLine(t *testing.T) {
	old := chunkBytes
	chunkBytes = 100
	t.Cleanup(func() { chunkBytes = old })
	marker := "[2026-09-01T10:00:00-0300] [PACMAN] starting full system upgrade\n"
	filler := strings.Repeat("y", 50) // deja el borde del primer bloque (100 B desde el final) a mitad del marcador
	data := []byte(marker + filler)
	r := &countingReaderAt{data: data}
	buf, cut, err := scanBack(r, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	up, found := parseLog(buf, cut)
	if !found || !up.LastFull.Equal(time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("marcador partido no encontrado: found=%v up=%+v", found, up)
	}
}

// Un marcador varios bloques de 256 KiB atrás del final (más allá de la
// cola que ya intentó el tail-read) se encuentra igual, y scanBack no
// necesita llegar a leer todo el archivo para dar con él.
func TestPacmanLogScanBackSeveralChunksBack(t *testing.T) {
	fillerLine := "[2026-09-02T10:00:00-0300] [ALPM] installed filler-package-with-a-long-name (1.0.0-1)\n"
	var head strings.Builder
	for head.Len() < 700<<10 { // varios bloques de 256 KiB antes del marcador
		head.WriteString(fillerLine)
	}
	marker := "[2026-09-01T10:00:00-0300] [PACMAN] starting full system upgrade\n" +
		"[2026-09-01T10:00:01-0300] [ALPM] upgraded a (1 -> 2)\n[2026-09-01T10:00:02-0300] [ALPM] transaction completed\n"
	var tail strings.Builder
	for tail.Len() < 300<<10 { // pasa la cola de 256 KiB (tailBytes): no se encuentra sin retroceder
		tail.WriteString(fillerLine)
	}
	data := []byte(head.String() + marker + tail.String())
	r := &countingReaderAt{data: data}
	buf, cut, err := scanBack(r, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	up, found := parseLog(buf, cut)
	if !found || up.FullPackages != 1 || !up.LastChange.Equal(time.Date(2026, 9, 1, 13, 0, 2, 0, time.UTC)) {
		t.Fatalf("marcador varios bloques atrás no encontrado: found=%v up=%+v", found, up)
	}
	if r.read >= int64(len(data)) {
		t.Errorf("scanBack leyó %d bytes de %d: no paró apenas encontró el marcador, sin llegar al principio del archivo", r.read, len(data))
	}
}

// Sin la línea en absoluto (hasta el tope maxBytes), scanBack no debe
// intentar leer más allá del archivo.
func TestPacmanLogScanBackNotFound(t *testing.T) {
	old := chunkBytes
	chunkBytes = 100
	t.Cleanup(func() { chunkBytes = old })
	data := []byte(strings.Repeat("y", 250))
	r := &countingReaderAt{data: data}
	buf, cut, err := scanBack(r, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if _, found := parseLog(buf, cut); found {
		t.Fatalf("no debería encontrar nada en pura basura")
	}
	if cut {
		t.Errorf("cut = true, want false: se leyó el archivo entero (más chico que maxBytes)")
	}
}

// Solo se relee si cambió el mtime o el tamaño.
func TestPacmanLogCachedByMtime(t *testing.T) {
	env, dir := tempEnv(t)
	p := filepath.Join(dir, "root", PacmanLog)
	put(t, dir, "root"+PacmanLog, "[2026-09-20T10:00:00-0300] [PACMAN] starting full system upgrade\n")
	stamp := time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC)
	os.Chtimes(p, stamp, stamp)
	var l LogReader
	first := l.Read(env, &collect.Report{Lang: "en"})
	// Mismo tamaño y mismo mtime: no se relee (la fecha nueva no aparece).
	put(t, dir, "root"+PacmanLog, "[2026-09-21T10:00:00-0300] [PACMAN] starting full system upgrade\n")
	os.Chtimes(p, stamp, stamp)
	if again := l.Read(env, &collect.Report{Lang: "en"}); !again.LastFull.Equal(first.LastFull) {
		t.Fatalf("re-read without a change: %v", again.LastFull)
	}
	os.Chtimes(p, stamp.Add(time.Minute), stamp.Add(time.Minute))
	if changed := l.Read(env, &collect.Report{Lang: "en"}); changed.LastFull.Day() != 21 {
		t.Fatalf("mtime changed, not re-read: %v", changed.LastFull)
	}
}
