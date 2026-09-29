package apps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var t0 = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

// clock pone un reloj movible en env.
func clock(env *source.Env) *time.Time {
	now := t0
	env.Now = func() time.Time { return now }
	return &now
}

func appOf(a *model.Apps, id string) model.App {
	for _, x := range a.List {
		if x.ID == id {
			return x
		}
	}
	return model.App{}
}

func unavail(r *collect.Report, what string) string {
	for _, u := range r.List {
		if u.What == what {
			return u.Message
		}
	}
	return ""
}

func TestSampleFirstRead(t *testing.T) {
	env := tempApps(t)
	clock(&env)
	s := &Sampler{UID: uid()}
	a := s.Sample(env, nil, 4, true, &collect.Report{Lang: "en"})
	if len(a.List) != 10 {
		t.Fatalf("%d apps", len(a.List))
	}
	// Con ventana primero, por nombre; sin ventana, sin CPU todavía, por memoria.
	for i, id := range []string{"app-org.chromium.Chromium-200.scope", "kitty-300-0.scope", "comm:steam", "comm:quickshell"} {
		if a.List[i].ID != id {
			t.Fatalf("List[%d] = %s, want %s", i, a.List[i].ID, id)
		}
	}
	c := a.List[0]
	if len(c.Windows) != 2 || c.Procs != 4 || c.MemB != 260000*pageSize || c.CPUPct != 0 || c.NetRxBps != nil || c.GPUPct != nil {
		t.Fatalf("chromium = %+v", c)
	}
	if len(a.Monitors) != 2 || a.TotalMemB == 0 {
		t.Fatalf("monitors %v, total %d", a.Monitors, a.TotalMemB)
	}
	if !appOf(a, "comm:Hyprland").Protected || appOf(a, "comm:syncthing").Protected {
		t.Fatal("protection is wrong")
	}
}

func TestSampleCPU(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	s := &Sampler{UID: uid()}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	setCPU(t, env, 200, 10400) // +400 ticks = 4 s de CPU
	*now = t0.Add(10 * time.Second)
	a := s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	// 4 s en 10 s son 40 % de un núcleo: 10 % de 4.
	if got := appOf(a, "app-org.chromium.Chromium-200.scope").CPUPct; got != 10 {
		t.Fatalf("chromium CPU = %v, want 10", got)
	}
	if a.List[0].ID != "app-org.chromium.Chromium-200.scope" || a.TotalCPUPct != 10 {
		t.Fatalf("first %s, total %v", a.List[0].ID, a.TotalCPUPct)
	}
}

func TestSampleNet(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	s := &Sampler{UID: uid()}
	s.Sample(env, nil, 4, true, &collect.Report{Lang: "en"})
	key := source.Key("ss", SSArgs...)
	writeCmd(t, env, key, "ESTAB 0 0 192.168.1.10:39172 203.0.113.54:443 users:((\"chromium\",pid=201,fd=48))\n"+
		"\t bytes_acked:107913 bytes_received:94376\n"+
		"ESTAB 0 0 192.168.1.10:22000 198.51.100.7:22000 users:((\"syncthing\",pid=500,fd=12))\n\t bytes_acked:5000 bytes_received:70000\n")
	*now = t0.Add(10 * time.Second)
	a := s.Sample(env, nil, 4, true, &collect.Report{Lang: "en"})
	c := appOf(a, "app-org.chromium.Chromium-200.scope")
	if c.NetRxBps == nil || *c.NetRxBps != 100 || *c.NetTxBps != 100 {
		t.Fatalf("chromium net = %v / %v", c.NetRxBps, c.NetTxBps)
	}
	if n := appOf(a, "comm:syncthing").NetRxBps; n == nil || *n != 0 {
		t.Fatalf("syncthing net = %v", n)
	}
	// Entre lecturas de ss se conserva la última tasa.
	*now = t0.Add(12 * time.Second)
	a = s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	if c := appOf(a, "app-org.chromium.Chromium-200.scope"); c.NetRxBps == nil || *c.NetRxBps != 100 {
		t.Fatal("the rate was lost between ss reads")
	}
}

func TestSampleNvidia(t *testing.T) {
	env := tempApps(t)
	clock(&env)
	s := &Sampler{UID: uid()}
	a := s.Sample(env, []model.GPU{{Vendor: "nvidia"}}, 4, true, &collect.Report{Lang: "en"})
	if g := appOf(a, "app-org.chromium.Chromium-200.scope").GPUPct; g == nil || *g != 12 {
		t.Fatalf("chromium GPU = %v", g)
	}
	if g := appOf(a, "comm:syncthing").GPUPct; g == nil || *g != 0 {
		t.Fatalf("syncthing GPU = %v", g)
	}
}

func TestSampleSleepingGPU(t *testing.T) {
	env := tempApps(t)
	clock(&env)
	s := &Sampler{UID: uid()}
	r := &collect.Report{Lang: "en"}
	a := s.Sample(env, []model.GPU{{Vendor: "nvidia", Suspended: true}}, 4, true, r)
	if n := env.Run.(*source.FakeRunner).Count(source.Key("nvidia-smi", PmonArgs...)); n != 0 {
		t.Fatal("nvidia-smi woke a sleeping GPU")
	}
	if !strings.Contains(unavail(r, "apps.gpu"), "asleep") || a.List[0].GPUPct != nil {
		t.Fatalf("apps.gpu = %q, GPU = %v", unavail(r, "apps.gpu"), a.List[0].GPUPct)
	}
}

func TestSampleWithoutHyprland(t *testing.T) {
	env := tempApps(t)
	clock(&env)
	env.Hypr = &source.FakeHypr{Dir: t.TempDir()}
	s := &Sampler{UID: uid()}
	r := &collect.Report{Lang: "en"}
	a := s.Sample(env, nil, 4, true, r)
	for _, x := range a.List {
		if len(x.Windows) > 0 {
			t.Fatalf("%s has windows", x.ID)
		}
	}
	if unavail(r, "apps.windows") != "Hyprland is not running" {
		t.Fatalf("apps.windows = %q", unavail(r, "apps.windows"))
	}
}

func TestSampleKeepsSlowUnavailable(t *testing.T) {
	env := tempApps(t)
	clock(&env)
	env.Run = &source.FakeRunner{Dir: t.TempDir()} // sin ss
	s := &Sampler{UID: uid()}
	s.Sample(env, nil, 4, true, &collect.Report{Lang: "en"})
	r := &collect.Report{Lang: "en"}
	a := s.Sample(env, nil, 4, false, r)
	if unavail(r, "apps.net") == "" || a.List[0].NetRxBps != nil {
		t.Fatal("apps.net was not kept between ss reads")
	}
}

// Una app sin scope toma el nombre de la clase de su ventana más reciente;
// las de scope conservan el nombre del scope.
func TestSampleNameFromWindowClass(t *testing.T) {
	env := tempApps(t)
	clock(&env)
	p := filepath.Join(env.Hypr.(*source.FakeHypr).Dir, source.Key("j/clients")+".txt")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	j := strings.Replace(string(b), `"class":"steam"`, `"class":"Steam"`, 1)
	if j == string(b) {
		t.Fatal("the fixture has no steam class")
	}
	if err := os.WriteFile(p, []byte(j), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Sampler{UID: uid()}
	a := s.Sample(env, nil, 4, true, &collect.Report{Lang: "en"})
	if n := appOf(a, "comm:steam").Name; n != "Steam" {
		t.Fatalf("steam name = %q, want Steam", n)
	}
	if n := appOf(a, "app-org.chromium.Chromium-200.scope").Name; n != "Chromium" {
		t.Fatalf("chromium name = %q, want Chromium", n)
	}
}

// drmFd le da al proceso 201 de chromium (con ventana) un cliente DRM.
func drmFd(t *testing.T, env source.Env, node string, ns int) {
	t.Helper()
	fdinfo(t, env.FS.Root, 201, 9, "/dev/dri/"+node,
		"drm-pdev:\t0000:03:00.0\ndrm-client-id:\t77\ndrm-engine-gfx:\t"+itoa(ns)+" ns\n")
}

// nouveau: una NVIDIA sin el driver propietario se mide por fdinfo.
func TestSampleNouveau(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	if err := os.Remove(env.FS.Real("/proc/driver/nvidia/version")); err != nil {
		t.Fatal(err)
	}
	gpus := []model.GPU{{Vendor: "nvidia"}}
	s := &Sampler{UID: uid()}
	drmFd(t, env, "renderD128", 0)
	s.Sample(env, gpus, 4, true, &collect.Report{Lang: "en"})
	drmFd(t, env, "renderD128", 1000000000) // 1 s de motor en 10 s
	*now = t0.Add(10 * time.Second)
	a := s.Sample(env, gpus, 4, true, &collect.Report{Lang: "en"})
	if g := appOf(a, "app-org.chromium.Chromium-200.scope").GPUPct; g == nil || *g <= 0 {
		t.Fatalf("chromium GPU = %v, want > 0", g)
	}
	if n := env.Run.(*source.FakeRunner).Count("nvidia-smi"); n != 0 {
		t.Fatalf("nvidia-smi ran %d times with nouveau", n)
	}
}

// Híbrida: la AMD despierta se mide; la NVIDIA dormida no se toca.
func TestSampleHybridSleepingNvidia(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	os.MkdirAll(env.FS.Real("/sys/class/drm/renderD128"), 0o755)
	os.WriteFile(env.FS.Real("/sys/class/drm/renderD128/device.link"), []byte("../../../0000:03:00.0"), 0o644)
	gpus := []model.GPU{{Vendor: "amd", PCI: "0000:03:00.0"}, {Vendor: "nvidia", PCI: "0000:01:00.0", Suspended: true}}
	s := &Sampler{UID: uid()}
	drmFd(t, env, "renderD128", 0)
	s.Sample(env, gpus, 4, true, &collect.Report{Lang: "en"})
	drmFd(t, env, "renderD128", 1000000000)
	*now = t0.Add(10 * time.Second)
	a := s.Sample(env, gpus, 4, true, &collect.Report{Lang: "en"})
	if g := appOf(a, "app-org.chromium.Chromium-200.scope").GPUPct; g == nil || *g <= 0 {
		t.Fatalf("chromium GPU = %v, want > 0", g)
	}
	if n := env.Run.(*source.FakeRunner).Count("nvidia-smi"); n != 0 {
		t.Fatalf("nvidia-smi ran %d times on a sleeping GPU", n)
	}
}

// Las apps con ventana van en orden estable (nombre, id): una que empieza a
// usar CPU no se mueve bajo el cursor. Las de sin ventana son un ranking.
func TestSampleWindowedOrderIgnoresCPU(t *testing.T) {
	env := tempApps(t)
	now := clock(&env)
	s := &Sampler{UID: uid()}
	s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	setCPU(t, env, 600, 100000) // steam
	setCPU(t, env, 500, 100000) // syncthing, sin ventana
	*now = t0.Add(10 * time.Second)
	a := s.Sample(env, nil, 4, false, &collect.Report{Lang: "en"})
	for i, id := range []string{"app-org.chromium.Chromium-200.scope", "kitty-300-0.scope", "comm:steam", "comm:syncthing"} {
		if a.List[i].ID != id {
			t.Fatalf("List[%d] = %s, want %s", i, a.List[i].ID, id)
		}
	}
	if appOf(a, "comm:steam").CPUPct == 0 {
		t.Fatal("steam got no CPU")
	}
}
