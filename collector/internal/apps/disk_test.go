package apps

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestParseIO(t *testing.T) {
	full := "rchar: 48900000000\nwchar: 3000000\nsyscr: 100\nsyscw: 50\n" +
		"read_bytes: 420000000\nwrite_bytes: 8000000\ncancelled_write_bytes: 500000"
	c, ok := parseIO(full)
	if !ok || c.read != 420000000 || c.write != 7500000 {
		t.Fatalf("parsed = %+v, %v", c, ok)
	}
	// Sin cancelled_write_bytes: 0.
	if c, ok := parseIO("read_bytes: 10\nwrite_bytes: 20"); !ok || c.read != 10 || c.write != 20 {
		t.Fatalf("without cancelled = %+v, %v", c, ok)
	}
	// Más cancelado que escrito: 0, nunca un uint64 desbordado.
	if c, ok := parseIO("read_bytes: 1\nwrite_bytes: 5\ncancelled_write_bytes: 9"); !ok || c.write != 0 {
		t.Fatalf("more cancelled than written = %+v, %v", c, ok)
	}
	for _, bad := range []string{
		"",
		"rchar: 5\nwchar: 6",
		"read_bytes: 1",
		"write_bytes: 1",
		"read_bytes: x\nwrite_bytes: 2",
		"read_bytes: -1\nwrite_bytes: 2",
		"read_bytes: 1\nwrite_bytes: 2\ncancelled_write_bytes: ?",
	} {
		if c, ok := parseIO(bad); ok {
			t.Errorf("%q parsed as %+v", bad, c)
		}
	}
}

func TestDelta(t *testing.T) {
	if d := delta(10, 4); d != 6 {
		t.Fatalf("delta(10, 4) = %d", d)
	}
	if d := delta(4, 10); d != 0 {
		t.Fatalf("a counter that went down = %d, want 0", d)
	}
}

func TestReadIOFixture(t *testing.T) {
	env := source.Fixture(t, "apps")
	s, err := readIO(env.FS, 200)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := parseIO(s); !ok || c.read != 420000000 || c.write != 8000000 {
		t.Fatalf("200 = %+v, %v", c, ok)
	}
	// 203 escribió 2 MB y canceló 0.5 MB.
	s, _ = readIO(env.FS, 203)
	if c, ok := parseIO(s); !ok || c.write != 1500000 {
		t.Fatalf("203 = %+v, %v", c, ok)
	}
	// quickshell (150) no tiene io: como un proceso que no deja leerlo.
	if _, err := readIO(env.FS, 150); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("150: %v", err)
	}
}

const chromiumApp = "app-org.chromium.Chromium-200.scope"

// bumpTicks suma un tick de CPU (utime, campo 14) a un proceso del fixture:
// un proceso que hizo E/S usó CPU, y el sampler no relee el io de un proceso
// cuyos ticks no cambiaron. Sin stat (pid inexistente) no hace nada.
func bumpTicks(t *testing.T, env source.Env, pid int) {
	t.Helper()
	p := env.FS.Real("/proc/" + strconv.Itoa(pid) + "/stat")
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	f := strings.Fields(string(b))
	u, err := strconv.ParseUint(f[13], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	setStat(t, env, pid, 14, strconv.FormatUint(u+1, 10))
}

// swapIO pone el segundo juego de /proc/<pid>/io del fixture
// (fixtures/apps/io.2/<pid>): lo que cada proceso leyó y escribió 10 s después.
func swapIO(t *testing.T, env source.Env) {
	t.Helper()
	pids := swapIOOnly(t, env)
	for _, pid := range pids {
		bumpTicks(t, env, pid)
	}
}

// swapIOOnly hace lo mismo sin tocar los ticks; devuelve los pids cambiados.
func swapIOOnly(t *testing.T, env source.Env) []int {
	t.Helper()
	var pids []int
	dir := filepath.Join(source.FixturesDir(t), "apps", "io.2")
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(env.FS.Real("/proc/"+e.Name()+"/io"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// stubIO envuelve la lectura de /proc/<pid>/io: fail da el error de un pid
// (nil = leerlo del fixture). Devuelve cuántas lecturas hubo.
func stubIO(t *testing.T, fail func(pid int) error) *int {
	t.Helper()
	n := 0
	old := readIO
	readIO = func(fs source.FS, pid int) (string, error) {
		n++
		if fail != nil {
			if err := fail(pid); err != nil {
				return "", err
			}
		}
		return old(fs, pid)
	}
	t.Cleanup(func() { readIO = old })
	return &n
}

// setStat cambia el campo n (contado desde 1, como en proc(5)) de un proceso
// del fixture cuyo comm no tiene espacios.
func setStat(t *testing.T, env source.Env, pid, field int, v string) {
	t.Helper()
	p := env.FS.Real("/proc/" + strconv.Itoa(pid) + "/stat")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(string(b))
	f[field-1] = v
	if err := os.WriteFile(p, []byte(strings.Join(f, " ")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setStart cambia el arranque (campo 22) de un proceso del fixture cuyo comm
// no tiene espacios: el mismo pid pasa a ser otro proceso.
func setStart(t *testing.T, env source.Env, pid int, start uint64) {
	t.Helper()
	setStat(t, env, pid, 22, strconv.FormatUint(start, 10))
}

// writeIO escribe /proc/<pid>/io con lo leído y escrito en total y le suma
// un tick de CPU al proceso.
func writeIO(t *testing.T, env source.Env, pid int, read, write uint64) {
	t.Helper()
	writeIOOnly(t, env, pid, read, write)
	bumpTicks(t, env, pid)
}

// writeIOOnly hace lo mismo sin tocar los ticks: recoger un hijo suma a
// cutime, no a utime+stime, así que el padre puede quedar con los mismos ticks.
func writeIOOnly(t *testing.T, env source.Env, pid int, read, write uint64) {
	t.Helper()
	s := "read_bytes: " + strconv.FormatUint(read, 10) + "\nwrite_bytes: " +
		strconv.FormatUint(write, 10) + "\ncancelled_write_bytes: 0\n"
	if err := os.WriteFile(env.FS.Real("/proc/"+strconv.Itoa(pid)+"/io"), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// addProc agrega un proceso propio al /proc de la copia del fixture.
func addProc(t *testing.T, env source.Env, pid int, comm string, ppid int, start uint64, cgroup string) {
	t.Helper()
	dir := env.FS.Real("/proc/" + strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stat := strconv.Itoa(pid) + " (" + comm + ") S " + strconv.Itoa(ppid) +
		" 0 0 0 -1 0 0 0 0 0 10 5 0 0 20 0 1 0 " + strconv.FormatUint(start, 10) + " 0 2000\n"
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte("0::"+cgroup+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// removeProc hace que un proceso termine y su padre lo recoja.
func removeProc(t *testing.T, env source.Env, pid int) {
	t.Helper()
	if err := os.RemoveAll(env.FS.Real("/proc/" + strconv.Itoa(pid))); err != nil {
		t.Fatal(err)
	}
}

// diskReads toma dos lecturas con Disk sobre una copia del fixture, 10 s
// entre ellas; between (si no es nil) corre entre las dos.
func diskReads(t *testing.T, between func(env source.Env)) (*model.Apps, *collect.Report) {
	t.Helper()
	env := tempApps(t)
	now := clock(&env)
	s := &Sampler{UID: uid(), Disk: true}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	if between != nil {
		between(env)
	}
	*now = t0.Add(10 * time.Second)
	r := &collect.Report{Lang: "en"}
	return s.Sample(env, nil, 4, false, r), r
}

func unavailReason(r *collect.Report, what string) string {
	for _, u := range r.List {
		if u.What == what {
			return u.Reason
		}
	}
	return ""
}

func TestSampleDiskRate(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	s := &Sampler{UID: uid(), Disk: true}
	a := s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	if c := appOf(a, chromiumApp); c.DiskReadBps != nil || c.DiskWriteBps != nil {
		t.Fatal("the first read has a disk rate")
	}
	swapIO(t, env)
	*now = t0.Add(10 * time.Second)
	r := &collect.Report{Lang: "en"}
	a = s.Sample(env, nil, 4, false, r)
	// chromium: 200 y 201 leyeron 1 MB cada uno; 200 escribió 0.2 MB y 203
	// 0.3 MB con 0.1 MB cancelado. El rchar de 200 subió 50 GB y no cuenta.
	c := appOf(a, chromiumApp)
	if c.DiskReadBps == nil || *c.DiskReadBps != 200000 || c.DiskWriteBps == nil || *c.DiskWriteBps != 40000 {
		t.Fatalf("chromium disk = %v / %v, want 200000 / 40000", c.DiskReadBps, c.DiskWriteBps)
	}
	if st := appOf(a, "comm:syncthing"); st.DiskReadBps == nil || *st.DiskReadBps != 0 || st.DiskWriteBps == nil || *st.DiskWriteBps != 500000 {
		t.Fatalf("syncthing disk = %v / %v", st.DiskReadBps, st.DiskWriteBps)
	}
	// kitty no hizo nada: hay dato y es 0 (no nil).
	if k := appOf(a, "kitty-300-0.scope"); k.DiskReadBps == nil || *k.DiskReadBps != 0 || *k.DiskWriteBps != 0 {
		t.Fatalf("kitty disk = %v / %v", k.DiskReadBps, k.DiskWriteBps)
	}
	if unavail(r, "apps.disk") != "" {
		t.Fatalf("apps.disk = %q", unavail(r, "apps.disk"))
	}
}

// steam cancela más de lo que escribe: la escritura neta baja y la tasa es 0,
// nunca negativa ni un número enorme.
func TestSampleDiskNegativeIsZero(t *testing.T) {
	a, _ := diskReads(t, func(env source.Env) { swapIO(t, env) })
	st := appOf(a, "comm:steam")
	if st.DiskWriteBps == nil || *st.DiskWriteBps != 0 || st.DiskReadBps == nil || *st.DiskReadBps != 0 {
		t.Fatalf("steam disk = %v / %v, want 0 / 0", st.DiskReadBps, st.DiskWriteBps)
	}
}

// Un pid reusado es otro proceso: sus contadores no se restan de los del
// anterior (sería un pico falso). El proceso nuevo no tiene lectura anterior,
// así que no aporta ni da dato: syncthing, que no tiene otro proceso, queda
// sin dato ("—") en este intervalo, no con 0.
func TestSampleDiskPIDReused(t *testing.T) {
	a, _ := diskReads(t, func(env source.Env) {
		setStart(t, env, 500, 5000)
		writeIO(t, env, 500, 900000000000, 900000000000)
	})
	st := appOf(a, "comm:syncthing")
	if st.ID == "" {
		t.Fatal("syncthing is missing")
	}
	if st.DiskReadBps != nil || st.DiskWriteBps != nil {
		t.Fatalf("syncthing after its pid was reused = %v / %v, want no data", st.DiskReadBps, st.DiskWriteBps)
	}
}

// Un proceso nuevo (sin lectura anterior) no aporta nada y no hace que su
// app tenga dato: una app cuyos procesos legibles son todos nuevos queda sin
// dato ("—") en su primer intervalo, no en 0; una app con alguno viejo suma
// solo los viejos.
func TestSampleDiskNewProcess(t *testing.T) {
	const kitty = "/user.slice/user-1000.slice/user@1000.service/app.slice/app-graphical.slice/kitty-300-0.scope"
	a, _ := diskReads(t, func(env source.Env) {
		// Una app nueva y un proceso nuevo en kitty.
		addProc(t, env, 900, "newapp", 1, 9000, "/user.slice/user-1000.slice/user@1000.service/app.slice/app-newapp-900.scope")
		writeIO(t, env, 900, 5000000000, 5000000000)
		addProc(t, env, 305, "cp", 301, 3050, kitty)
		writeIO(t, env, 305, 7000000000, 7000000000)
	})
	n := appOf(a, "app-newapp-900.scope")
	if n.ID == "" {
		t.Fatal("the new app is missing")
	}
	if n.DiskReadBps != nil || n.DiskWriteBps != nil {
		t.Fatalf("new app = %v / %v, want no data", n.DiskReadBps, n.DiskWriteBps)
	}
	if k := appOf(a, "kitty-300-0.scope"); k.Procs != 3 || k.DiskReadBps == nil || *k.DiskReadBps != 0 || *k.DiskWriteBps != 0 {
		t.Fatalf("kitty with a new cp = %d procs, %v / %v, want 3, 0 / 0", k.Procs, k.DiskReadBps, k.DiskWriteBps)
	}
}

// /proc/<pid>/io suma lo de los hijos que el proceso ya recogió (wait). Un
// cp de 2 GB en kitty cuenta en el cp mientras vive; cuando termina y bash
// lo recoge, el contador de bash salta 2 GB, pero eso ya se contó: kitty no
// muestra un pico de GB/s.
func TestSampleDiskReapedChildNoSpike(t *testing.T) {
	const kitty = "/user.slice/user-1000.slice/user@1000.service/app.slice/app-graphical.slice/kitty-300-0.scope"
	env := tempApps(t)
	now := clock(&env)
	addProc(t, env, 305, "cp", 301, 3050, kitty)
	writeIO(t, env, 305, 1000000000, 1000000000)
	writeIO(t, env, 301, 0, 0)
	s := &Sampler{UID: uid(), Disk: true}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})

	// Mientras vive, lo del cp cuenta: 1 GB en 10 s.
	writeIO(t, env, 305, 2000000000, 2000000000)
	*now = t0.Add(10 * time.Second)
	k := appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), "kitty-300-0.scope")
	if k.DiskReadBps == nil || *k.DiskReadBps != 100000000 || *k.DiskWriteBps != 100000000 {
		t.Fatalf("kitty while cp runs = %v / %v, want 100000000 / 100000000", k.DiskReadBps, k.DiskWriteBps)
	}

	// cp terminó (su último tramo: 0.1 MB leído, 0.05 MB escrito) y bash lo
	// recogió: el io de bash trae todo lo del cp.
	removeProc(t, env, 305)
	writeIO(t, env, 301, 2000000000+100000, 2000000000+50000)
	*now = t0.Add(20 * time.Second)
	k = appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), "kitty-300-0.scope")
	if k.DiskReadBps == nil || *k.DiskReadBps != 10000 || *k.DiskWriteBps != 5000 {
		t.Fatalf("kitty after bash reaped cp = %v / %v, want 10000 / 5000", k.DiskReadBps, k.DiskWriteBps)
	}
}

// Una app que se cierra y la recoge un proceso de la sesión (systemd --user,
// subreaper): su I/O de toda la vida pasa al contador de systemd, que no
// muestra un pico por eso.
func TestSampleDiskReapedBySessionNoSpike(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	addProc(t, env, 90, "systemd", 1, 90, "/user.slice/user-1000.slice/user@1000.service/init.scope")
	writeIO(t, env, 90, 1000000, 1000000)
	setStat(t, env, 500, 4, "90") // syncthing es hijo de systemd --user
	s := &Sampler{UID: uid(), Disk: true}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})

	// syncthing (7 MB leído y 90 MB escrito en su vida) se cerró; systemd
	// lo recogió y además leyó 20 kB y escribió 10 kB.
	removeProc(t, env, 500)
	writeIO(t, env, 90, 1000000+7000000+20000, 1000000+90000000+10000)
	*now = t0.Add(10 * time.Second)
	a := s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	sd := appOf(a, "comm:systemd")
	if sd.ID == "" {
		t.Fatal("systemd is missing")
	}
	if sd.DiskReadBps == nil || *sd.DiskReadBps != 2000 || *sd.DiskWriteBps != 1000 {
		t.Fatalf("systemd after reaping syncthing = %v / %v, want 2000 / 1000", sd.DiskReadBps, sd.DiskWriteBps)
	}
	if st := appOf(a, "comm:syncthing"); st.ID != "" {
		t.Fatal("syncthing is still listed")
	}
}

// Recoger un hijo suma a cutime, no a utime+stime: bash puede recoger el cp
// sin que cambien sus ticks. Aun así se lee (tiene una resta pendiente); si
// se arrastrara su contador viejo, la resta se perdería y todo el I/O del cp
// aparecería como un pico en la próxima lectura real de bash.
func TestSampleDiskReapedChildSameTicksNoSpike(t *testing.T) {
	const kitty = "/user.slice/user-1000.slice/user@1000.service/app.slice/app-graphical.slice/kitty-300-0.scope"
	env := tempApps(t)
	now := clock(&env)
	addProc(t, env, 305, "cp", 301, 3050, kitty)
	writeIO(t, env, 305, 1000000000, 1000000000)
	writeIO(t, env, 301, 0, 0)
	s := &Sampler{UID: uid(), Disk: true}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})

	writeIO(t, env, 305, 2000000000, 2000000000)
	*now = t0.Add(10 * time.Second)
	k := appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), "kitty-300-0.scope")
	if k.DiskReadBps == nil || *k.DiskReadBps != 100000000 || *k.DiskWriteBps != 100000000 {
		t.Fatalf("kitty while cp runs = %s / %s, want 100000000 / 100000000", bps(k.DiskReadBps), bps(k.DiskWriteBps))
	}

	// bash recoge el cp sin gastar un tick.
	removeProc(t, env, 305)
	writeIOOnly(t, env, 301, 2000000000+100000, 2000000000+50000)
	*now = t0.Add(20 * time.Second)
	k = appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), "kitty-300-0.scope")
	if k.DiskReadBps == nil || *k.DiskReadBps != 10000 || *k.DiskWriteBps != 5000 {
		t.Fatalf("kitty after bash reaped cp with the same ticks = %s / %s, want 10000 / 5000", bps(k.DiskReadBps), bps(k.DiskWriteBps))
	}

	// bash vuelve a usar CPU sin hacer E/S: nada que mostrar.
	bumpTicks(t, env, 301)
	*now = t0.Add(30 * time.Second)
	k = appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), "kitty-300-0.scope")
	if k.DiskReadBps == nil || *k.DiskReadBps != 0 || *k.DiskWriteBps != 0 {
		t.Fatalf("kitty at bash's next tick = %s / %s, want 0 / 0", bps(k.DiskReadBps), bps(k.DiskWriteBps))
	}
}

// Lo mismo con una app que se cierra y la recoge systemd --user sin que
// cambien sus ticks.
func TestSampleDiskReapedBySessionSameTicksNoSpike(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	addProc(t, env, 90, "systemd", 1, 90, "/user.slice/user-1000.slice/user@1000.service/init.scope")
	writeIO(t, env, 90, 1000000, 1000000)
	setStat(t, env, 500, 4, "90") // syncthing es hijo de systemd --user
	s := &Sampler{UID: uid(), Disk: true}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})

	removeProc(t, env, 500)
	writeIOOnly(t, env, 90, 1000000+7000000+20000, 1000000+90000000+10000)
	*now = t0.Add(10 * time.Second)
	sd := appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), "comm:systemd")
	if sd.DiskReadBps == nil || *sd.DiskReadBps != 2000 || *sd.DiskWriteBps != 1000 {
		t.Fatalf("systemd after reaping syncthing with the same ticks = %s / %s, want 2000 / 1000", bps(sd.DiskReadBps), bps(sd.DiskWriteBps))
	}

	bumpTicks(t, env, 90)
	*now = t0.Add(20 * time.Second)
	sd = appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), "comm:systemd")
	if sd.DiskReadBps == nil || *sd.DiskReadBps != 0 || *sd.DiskWriteBps != 0 {
		t.Fatalf("systemd at its next tick = %s / %s, want 0 / 0", bps(sd.DiskReadBps), bps(sd.DiskWriteBps))
	}
}

// bps muestra una tasa de disco en un mensaje de error.
func bps(p *float64) string {
	if p == nil {
		return "nil"
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

// Un proceso propio que no deja leer su io (no volcable, setuid) no quita el
// dato de su app: se suman los demás. Una app sin ningún io legible queda sin
// dato, no en 0.
func TestSampleDiskUnreadable(t *testing.T) {
	stubIO(t, func(pid int) error {
		if pid == 201 {
			return os.ErrPermission
		}
		return nil
	})
	a, r := diskReads(t, func(env source.Env) { swapIO(t, env) })
	if c := appOf(a, chromiumApp); c.DiskReadBps == nil || *c.DiskReadBps != 100000 || *c.DiskWriteBps != 40000 {
		t.Fatalf("chromium without 201 = %v / %v, want 100000 / 40000", c.DiskReadBps, c.DiskWriteBps)
	}
	// quickshell (150) no tiene /proc/150/io en el fixture.
	if q := appOf(a, "comm:quickshell"); q.DiskReadBps != nil || q.DiskWriteBps != nil {
		t.Fatalf("quickshell disk = %v / %v, want no data", q.DiskReadBps, q.DiskWriteBps)
	}
	if unavail(r, "apps.disk") != "" {
		t.Fatal("a note while most processes were readable")
	}
}

// Si no se puede leer el io de ningún proceso (kernel sin contabilidad de
// I/O, un LSM que no deja), todas quedan sin dato y una línea dice por qué.
func TestSampleDiskNoneReadable(t *testing.T) {
	for _, c := range []struct {
		err    error
		reason string
	}{{os.ErrPermission, "permission"}, {os.ErrNotExist, "not_present"}} {
		t.Run(c.reason, func(t *testing.T) {
			stubIO(t, func(int) error { return c.err })
			a, r := diskReads(t, nil)
			for _, x := range a.List {
				if x.DiskReadBps != nil || x.DiskWriteBps != nil {
					t.Fatalf("%s has disk data", x.ID)
				}
			}
			if msg := unavail(r, "apps.disk"); msg != "the disk use of each process could not be read" || unavailReason(r, "apps.disk") != c.reason {
				t.Fatalf("apps.disk = %q (%s)", msg, unavailReason(r, "apps.disk"))
			}
		})
	}
}

// Sin Disk (los tops de CPU, Memoria, GPU y Red) no se lee ningún io; al
// prenderlo, la primera lectura no tiene base.
func TestSampleNoDiskWithoutFlag(t *testing.T) {
	n := stubIO(t, nil)
	env := tempApps(t)
	now := clock(&env)
	s := &Sampler{UID: uid()}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	swapIO(t, env)
	*now = t0.Add(10 * time.Second)
	a := s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	if *n != 0 {
		t.Fatalf("%d io reads without Disk", *n)
	}
	for _, x := range a.List {
		if x.DiskReadBps != nil || x.DiskWriteBps != nil {
			t.Fatalf("%s has disk data without Disk", x.ID)
		}
	}
	s.Disk = true
	*now = t0.Add(12 * time.Second)
	a = s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	if *n != 16 {
		t.Fatalf("%d io reads with Disk, want 16 (one per own process)", *n)
	}
	if c := appOf(a, chromiumApp); c.DiskReadBps != nil {
		t.Fatal("the first read with Disk has a rate")
	}
	// Apagar y volver a prender: la base de antes ya no sirve.
	s.Disk = false
	*now = t0.Add(14 * time.Second)
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	s.Disk = true
	*now = t0.Add(16 * time.Second)
	if c := appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), chromiumApp); c.DiskReadBps != nil {
		t.Fatal("a disk rate over a read without Disk")
	}
}

func TestSampleResetForgetsDisk(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	s := &Sampler{UID: uid(), Disk: true}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	s.Reset()
	swapIO(t, env)
	*now = t0.Add(10 * time.Second)
	if c := appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), chromiumApp); c.DiskReadBps != nil {
		t.Fatal("a disk rate right after Reset")
	}
}

// DropDisk olvida solo la base del disco (al salir de Aplicaciones hacia una
// vista sin apps): la CPU sigue teniendo tasa en la lectura siguiente.
func TestSampleDropDiskKeepsCPU(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	s := &Sampler{UID: uid(), Disk: true}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	s.DropDisk()
	swapIO(t, env)
	setCPU(t, env, 200, 10400)
	*now = t0.Add(10 * time.Second)
	a := s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	c := appOf(a, chromiumApp)
	if c.DiskReadBps != nil || c.DiskWriteBps != nil {
		t.Fatalf("a disk rate right after DropDisk = %v / %v", c.DiskReadBps, c.DiskWriteBps)
	}
	if c.CPUPct == 0 {
		t.Fatal("DropDisk forgot the CPU base")
	}
	// La lectura siguiente ya tiene base.
	*now = t0.Add(12 * time.Second)
	if c := appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), chromiumApp); c.DiskReadBps == nil {
		t.Fatal("no disk rate on the read after DropDisk")
	}
}

// Una app de varios procesos (800 → 801) se cierra entera en el mismo
// intervalo y la recoge systemd --user: lo de 801 llega a systemd (por 800 o
// porque 801 quedó huérfano bajo el subreaper). Se resta del ancestro vivo
// más cercano: systemd no muestra un pico.
func TestSampleDiskReapedTreeNoSpike(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	addProc(t, env, 90, "systemd", 1, 90, "/user.slice/user-1000.slice/user@1000.service/init.scope")
	writeIO(t, env, 90, 1000000, 1000000)
	setStat(t, env, 800, 4, "90") // obsidian es hijo de systemd --user
	writeIO(t, env, 800, 1000000, 400000)
	writeIO(t, env, 801, 3000000000, 2000000000)
	s := &Sampler{UID: uid(), Disk: true}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})

	removeProc(t, env, 801)
	removeProc(t, env, 800)
	writeIO(t, env, 90, 1000000+1000000+3000000000+20000, 1000000+400000+2000000000+10000)
	*now = t0.Add(10 * time.Second)
	sd := appOf(s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"}), "comm:systemd")
	if sd.DiskReadBps == nil || *sd.DiskReadBps != 2000 || *sd.DiskWriteBps != 1000 {
		t.Fatalf("systemd after reaping obsidian's tree = %v / %v, want 2000 / 1000", sd.DiskReadBps, sd.DiskWriteBps)
	}
}

// Un ciclo de ppid entre procesos muertos (pids reusados) no cuelga la
// búsqueda del ancestro vivo y no resta nada.
func TestReapedPPIDCycle(t *testing.T) {
	a, b, live := pkey{pid: 10, start: 1}, pkey{pid: 11, start: 1}, pkey{pid: 12, start: 1}
	last := map[pkey]ioBase{
		a:    {ioCount{5, 5}, 11},
		b:    {ioCount{7, 7}, 10},
		live: {ioCount{1, 1}, 1},
	}
	if got := reaped(last, map[pkey]bool{live: true}); len(got) != 0 {
		t.Fatalf("reaped = %v, want nothing", got)
	}
}

// Un proceso cuyos ticks de CPU no cambiaron no se relee: no aporta nada a la
// tasa (0) y la app sigue con dato. Sus contadores pasan como base, así que lo
// que acumule se ve en su próxima lectura real.
func TestSampleDiskSkipsIdleProcess(t *testing.T) {
	n := stubIO(t, nil)
	env := tempApps(t)
	now := clock(&env)
	s := &Sampler{UID: uid(), Disk: true}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	if *n != 16 {
		t.Fatalf("%d reads in the first sample, want 16", *n)
	}
	// El io cambia, pero los ticks no: no se lee (un proceso que no usó CPU
	// no pudo hacer E/S que valga la pena).
	swapIOOnly(t, env)
	*n = 0
	*now = t0.Add(10 * time.Second)
	a := s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	// Solo se lee el 150, que no tiene base (su io no existe en el fixture).
	if *n != 1 {
		t.Fatalf("%d reads of processes with the same ticks, want 1 (only 150, without a base)", *n)
	}
	c := appOf(a, chromiumApp)
	if c.DiskReadBps == nil || *c.DiskReadBps != 0 || *c.DiskWriteBps != 0 {
		t.Fatalf("chromium = %v / %v, want data with 0 / 0", c.DiskReadBps, c.DiskWriteBps)
	}
	// Ahora usan CPU: la lectura real trae todo lo acumulado desde la base
	// original (la del primer sample), que siguió como base mientras no se leían.
	for _, pid := range []int{200, 201, 202, 203} {
		bumpTicks(t, env, pid)
	}
	*n = 0
	*now = t0.Add(20 * time.Second)
	a = s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	if *n != 5 {
		t.Fatalf("%d reads, want 5 (the 4 whose ticks changed and 150)", *n)
	}
	c = appOf(a, chromiumApp)
	if c.DiskReadBps == nil || *c.DiskReadBps != 200000 || *c.DiskWriteBps != 40000 {
		t.Fatalf("chromium = %v / %v, want the accumulated 200000 / 40000", c.DiskReadBps, c.DiskWriteBps)
	}
}
