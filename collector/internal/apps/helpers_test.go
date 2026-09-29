package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func uid() uint32 { return uint32(os.Getuid()) }

// tempApps copia el fixture apps a un directorio temporal (root, cmd y hypr),
// para poder cambiar /proc o las salidas entre lecturas sin tocar el fixture.
func tempApps(t *testing.T) source.Env {
	t.Helper()
	env := source.Fixture(t, "apps")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Dir(env.FS.Root))); err != nil {
		t.Fatal(err)
	}
	env.FS.Root = filepath.Join(dir, "root")
	env.Run = &source.FakeRunner{Dir: filepath.Join(dir, "cmd")}
	env.Hypr = &source.FakeHypr{Dir: filepath.Join(dir, "hypr")}
	return env
}

// setCPU cambia utime (campo 14) de un proceso del fixture cuyo comm no
// tiene espacios.
func setCPU(t *testing.T, env source.Env, pid int, utime uint64) {
	t.Helper()
	p := env.FS.Real(fmt.Sprintf("/proc/%d/stat", pid))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(string(b))
	f[13] = strconv.FormatUint(utime, 10)
	if err := os.WriteFile(p, []byte(strings.Join(f, " ")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeCmd reemplaza la salida falsa de un comando (clave de source.Key).
func writeCmd(t *testing.T, env source.Env, key, text string) {
	t.Helper()
	dir := env.Run.(*source.FakeRunner).Dir
	if err := os.WriteFile(filepath.Join(dir, key+".txt"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureWins son los procesos con ventana del fixture apps, con su clase.
var fixtureWins = map[int]string{200: "chromium", 300: "kitty", 600: "steam"}

func itoa(n int) string { return strconv.Itoa(n) }

// TestMain prohíbe mandar señales reales: los pids del fixture pueden existir
// en la máquina que corre los tests.
func TestMain(m *testing.M) {
	sendSignal = func(pid int, sig syscall.Signal) error {
		panic(fmt.Sprintf("real signal %v to %d in a test", sig, pid))
	}
	sleep = func(time.Duration) {}
	os.Exit(m.Run())
}

var sigNames = map[syscall.Signal]string{syscall.SIGSTOP: "STOP", syscall.SIGCONT: "CONT", syscall.SIGTERM: "TERM", syscall.SIGKILL: "KILL"}

// stubSignals anota las señales ("600 STOP"); onSig, si no es nil, simula su efecto.
func stubSignals(t *testing.T, onSig func(pid int, sig syscall.Signal)) *[]string {
	t.Helper()
	var got []string
	old := sendSignal
	sendSignal = func(pid int, sig syscall.Signal) error {
		got = append(got, fmt.Sprintf("%d %s", pid, sigNames[sig]))
		if onSig != nil {
			onSig(pid, sig)
		}
		return nil
	}
	t.Cleanup(func() { sendSignal = old })
	return &got
}

func groupByID(t *testing.T, env source.Env, id string) group {
	t.Helper()
	ps := readProcs(env.FS, uid(), map[int]meta{})
	for _, g := range groupProcs(ps, fixtureWins, nil) {
		if g.id == id {
			return g
		}
	}
	t.Fatalf("no group %s", id)
	return group{}
}

// cgFreeze crea el cgroup.freeze (en "0") del cgroup de cada proceso de g
// (una app puede tener procesos en más de un scope) y devuelve la ruta del
// primero.
func cgFreeze(t *testing.T, env source.Env, g group) string {
	t.Helper()
	var first string
	for _, p := range g.procs {
		f := env.FS.Real("/sys/fs/cgroup" + p.cgroup + "/cgroup.freeze")
		if first == "" {
			first = f
		}
		if _, err := os.Stat(f); err == nil {
			continue
		}
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte("0"), 0o644)
	}
	return first
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}
