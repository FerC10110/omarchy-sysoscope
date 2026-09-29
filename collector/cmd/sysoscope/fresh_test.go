package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// view cpu sin stream arma un engine y toma dos muestras: la segunda tiene
// que volver a leer las apps, o no hay CPU por app y la vista queda sin
// "Apps using the most".
func TestViewCPUHasTops(t *testing.T) {
	env := source.Fixture(t, "apps")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Dir(env.FS.Root))); err != nil {
		t.Fatal(err)
	}
	env.FS.Root = filepath.Join(dir, "root")
	env.Run = &source.FakeRunner{Dir: filepath.Join(dir, "cmd")}
	env.Hypr = &source.FakeHypr{Dir: filepath.Join(dir, "hypr")}
	os.WriteFile(env.FS.Real("/proc/cpuinfo"), []byte("processor\t: 0\nprocessor\t: 1\nprocessor\t: 2\nprocessor\t: 3\n"), 0o644)
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	var out, errb bytes.Buffer
	a := &app{env: env, stdin: strings.NewReader(""), stdout: &out, stderr: &errb,
		newEngine: func(env source.Env, o engine.Options) sampler { return engine.New(env, o) },
		// Entre las dos muestras, chromium usa 1 s de CPU.
		sleep: func(d time.Duration) {
			now = now.Add(d)
			p := env.FS.Real("/proc/200/stat")
			b, _ := os.ReadFile(p)
			f := strings.Fields(string(b))
			u, _ := strconv.Atoi(f[13])
			f[13] = strconv.Itoa(u + 100)
			os.WriteFile(p, []byte(strings.Join(f, " ")+"\n"), 0o644)
		},
	}
	if code := a.run([]string{"view", "cpu"}); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "Apps using the most") || !strings.Contains(out.String(), "Chromium") {
		t.Fatalf("view cpu has no tops: %s", out.String())
	}
}

// view apps sin stream toma dos muestras con foco apps: la segunda trae el
// disco por app (entre las dos, el fixture pasa al juego io.2).
func TestViewAppsHasDisk(t *testing.T) {
	env := source.Fixture(t, "apps")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Dir(env.FS.Root))); err != nil {
		t.Fatal(err)
	}
	env.FS.Root = filepath.Join(dir, "root")
	env.Run = &source.FakeRunner{Dir: filepath.Join(dir, "cmd")}
	env.Hypr = &source.FakeHypr{Dir: filepath.Join(dir, "hypr")}
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	io2 := filepath.Join(source.FixturesDir(t), "apps", "io.2")
	var out, errb bytes.Buffer
	a := &app{env: env, stdin: strings.NewReader(""), stdout: &out, stderr: &errb,
		newEngine: func(env source.Env, o engine.Options) sampler { return engine.New(env, o) },
		sleep: func(d time.Duration) {
			now = now.Add(d)
			es, _ := os.ReadDir(io2)
			for _, e := range es {
				b, _ := os.ReadFile(filepath.Join(io2, e.Name()))
				os.WriteFile(env.FS.Real("/proc/"+e.Name()+"/io"), b, 0o644)
				// Un proceso que hizo E/S usó CPU: sin un tick más, el sampler
				// no relee el io de un proceso que no cambió.
				p := env.FS.Real("/proc/" + e.Name() + "/stat")
				if sb, err := os.ReadFile(p); err == nil {
					f := strings.Fields(string(sb))
					u, _ := strconv.Atoi(f[13])
					f[13] = strconv.Itoa(u + 1)
					os.WriteFile(p, []byte(strings.Join(f, " ")+"\n"), 0o644)
				}
			}
		},
	}
	if code := a.run([]string{"view", "apps"}); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	// En 250 ms chromium leyó 2 MB y escribió 0.4 MB.
	for _, want := range []string{"Disk R 8.0 MB/s W 1.6 MB/s", "Disk per app counts what reaches the disk"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("view apps has no %q: %s", want, out.String())
		}
	}
}
