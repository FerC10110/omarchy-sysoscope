package maint

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var (
	kernKey = "cmd/" + source.Key("journalctl", KernelOOMArgs...)
	oomdKey = "cmd/" + source.Key("journalctl", OomdArgs...)
)

const (
	kernelWeb    = "Out of memory: Killed process 48213 (Web Content) total-vm:11534336kB, anon-rss:6291456kB, file-rss:0kB, shmem-rss:0kB, UID:1000 pgtables:14336kB oom_score_adj:167"
	kernelCgroup = "Memory cgroup out of memory: Killed process 5120 (stress-ng) total-vm:2097152kB, anon-rss:1048576kB, file-rss:0kB, shmem-rss:0kB, UID:1000 pgtables:2100kB oom_score_adj:0"
	// La línea real del driver NVIDIA de esta máquina: no es una muerte.
	kernelNVIDIA = "NVRM: nvAssertOkFailedNoLog: Assertion failed: Out of memory [NV_ERR_NO_MEMORY] (0x00000051) returned from pReuseMappingDb->pMapCb(pReuseMappingDb->pGlobalCtx, pAllocCtx, range, cachingFlags, &token, _reusemappingdbAddMappingCallback) @ mapping_reuse.c:273"
	// Las dos formas de systemd-oomd (src/oom/oomd-util.c) y una que no es una muerte.
	oomdPressure = "Killed /user.slice/user-1000.slice/user@1000.service/app.slice/app-org.mozilla.firefox-4242.scope due to memory pressure for /user.slice/user-1000.slice/user@1000.service being 83.21% > 50.00% for > 20s with reclaim activity"
	oomdSwap     = "Killed /user.slice/user-1000.slice/user@1000.service/app.slice/app-Hyprland-kitty@3f2a.service due to memory used (16320000000) / total (16640000000) and swap used (8480000000) / total (8590000000) being more than 90.00%"
	oomdNot      = "Considered 12 cgroups for killing, top candidates were:"
)

// Review Focus 3: NVIDIA y "Considered…" nunca cuentan; el OOM de un
// cgroup sí.
func TestParseKills(t *testing.T) {
	for msg, want := range map[string]killLine{
		kernelWeb:    {by: "kernel", pid: "48213", program: "Web Content"},
		kernelCgroup: {by: "kernel", pid: "5120", program: "stress-ng"},
		oomdPressure: {by: "systemd-oomd", program: "firefox", unit: "/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.mozilla.firefox-4242.scope"},
		oomdSwap:     {by: "systemd-oomd", program: "kitty", unit: "/user.slice/user-1000.slice/user@1000.service/app.slice/app-Hyprland-kitty@3f2a.service"},
		"Killed /user.slice/app-Hyprland-12345.scope due to memory pressure": {by: "systemd-oomd", program: "app-Hyprland-12345.scope",
			unit: "/user.slice/app-Hyprland-12345.scope"},
	} {
		if got, ok := parseKill(msg); !ok || got != want {
			t.Errorf("parseKill(%.40q) = %+v, %v; want %+v", msg, got, ok, want)
		}
	}
	for _, msg := range []string{kernelNVIDIA, oomdNot, "Killed", "", "Out of memory: Killed process x"} {
		if k, ok := parseKill(msg); ok {
			t.Errorf("%.40q is not a kill: %+v", msg, k)
		}
	}
	for msg, want := range map[string]string{
		kernelWeb:    "Out of memory: Killed process 48213 (Web Content)",
		kernelCgroup: "Out of memory: Killed process 5120 (stress-ng)",
		oomdSwap:     "Killed /user.slice/user-1000.slice/user@1000.service/app.slice/app-Hyprland-kitty@3f2a.service",
	} {
		got, ok := OOMMessage(msg)
		if !ok || got != want {
			t.Errorf("OOMMessage = %q, %v; want %q", got, ok, want)
		}
		if again, ok := OOMMessage(got); !ok || again != want {
			t.Errorf("the reduced message must parse again: %q", again)
		}
	}
	if _, ok := OOMMessage(kernelNVIDIA); ok {
		t.Error("NVIDIA reduced")
	}
}

// Review Focus 4: el journal del kernel (~49 ms) solo si oom_kill > 0.
func TestReadOOMKernelOnlyWithKills(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, oomdKey+".txt", jline(map[string]any{"MESSAGE": oomdNot, "__REALTIME_TIMESTAMP": "1790159000000000"})+
		jline(map[string]any{"MESSAGE": oomdPressure, "__REALTIME_TIMESTAMP": "1790162000000000"}))
	put(t, dir, kernKey+".txt", jline(map[string]any{"MESSAGE": kernelWeb, "__REALTIME_TIMESTAMP": "1790160000000000"})+
		jline(map[string]any{"MESSAGE": kernelNVIDIA, "__REALTIME_TIMESTAMP": "1790160500000000"})+
		jline(map[string]any{"MESSAGE": kernelCgroup, "__REALTIME_TIMESTAMP": "1790161000000000"}))
	r := &collect.Report{Lang: "en"}
	l := ReadOOM(env, 0, r)
	if l == nil || l.Oomd != 1 || len(l.List) != 1 || l.List[0].Program != "firefox" || l.List[0].By != "systemd-oomd" {
		t.Fatalf("oom_kill 0 = %+v", l)
	}
	if n := runner(env).Count(source.Key("journalctl", KernelOOMArgs...)); n != 0 {
		t.Fatalf("the kernel's journal ran %d times with oom_kill 0", n)
	}
	l = ReadOOM(env, 2, r)
	want := []model.OOMKill{
		{Time: time.UnixMicro(1790162000000000).UTC(), Program: "firefox", By: "systemd-oomd"},
		{Time: time.UnixMicro(1790161000000000).UTC(), Program: "stress-ng", By: "kernel"},
		{Time: time.UnixMicro(1790160000000000).UTC(), Program: "Web Content", By: "kernel"},
	}
	if l == nil || l.Oomd != 1 || !reflect.DeepEqual(l.List, want) || len(r.List) != 0 {
		t.Fatalf("oom_kill 2 = %+v, %+v", l, r.List)
	}
	if n := runner(env).Count(source.Key("journalctl", KernelOOMArgs...)); n != 1 {
		t.Fatalf("the kernel's journal ran %d times with oom_kill 2", n)
	}
}

func TestReadOOMFailures(t *testing.T) {
	bare, dir := tempEnv(t)
	r := &collect.Report{Lang: "en"}
	if l := ReadOOM(bare, 3, r); l != nil || len(r.List) != 1 || r.List[0].What != "maintenance.health.oom" || r.List[0].Reason != "missing_tool" {
		t.Fatalf("without journalctl = %+v, %+v", l, r.List)
	}
	if n := runner(bare).Count(source.Key("journalctl", KernelOOMArgs...)); n != 0 {
		t.Error("the kernel's journal after oomd failed")
	}
	// oomd bien y el kernel sin entradas (sale 1 con -g sin coincidencias).
	put(t, dir, oomdKey+".txt", "")
	put(t, dir, kernKey+".txt", "")
	put(t, dir, kernKey+".exit", "1\n")
	r = &collect.Report{Lang: "en"}
	if l := ReadOOM(bare, 3, r); l == nil || l.Oomd != 0 || l.List == nil || len(l.List) != 0 || len(r.List) != 0 {
		t.Fatalf("no kernel entries = %+v, %+v", l, r.List)
	}
}

func TestReadHealth(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, errKey+".txt", jline(map[string]any{"MESSAGE": "boom", "SYSLOG_IDENTIFIER": "kernel", "PRIORITY": "3", "__REALTIME_TIMESTAMP": "1790150060000000"}))
	put(t, dir, dumpKey+".txt", "[]\n")
	put(t, dir, oomdKey+".txt", "")
	r := &collect.Report{Lang: "en"}
	h := ReadHealth(env, false, 0, r)
	fr := runner(env)
	if h == nil || h.Errors != nil || h.Crashes != nil || h.OOM == nil || fr.Count("coredumpctl") != 0 ||
		fr.Count(source.Key("journalctl", JournalErrArgs...)) != 0 {
		t.Fatalf("oom only = %+v (calls %v)", h, fr.Calls)
	}
	h = ReadHealth(env, true, 0, r)
	if h.Errors == nil || h.Errors.Count != 1 || h.Crashes == nil || h.Crashes.Total != 0 || h.OOM == nil || len(r.List) != 0 {
		t.Fatalf("full = %+v, %+v", h, r.List)
	}
}

// Los argumentos exactos (la clave de los fixtures y BINARY_PROVENANCE.md):
// -a para que un campo de más de 4096 bytes no llegue null (M2); -g
// '^Killed ' para que el -n 50 de systemd-oomd cuente solo muertes (M5).
func TestHealthArgs(t *testing.T) {
	if want := []string{"-b", "-p", "err", "-q", "--no-pager", "-a", "-o", "json", "-n", "1000",
		"--output-fields=MESSAGE,SYSLOG_IDENTIFIER,_COMM,PRIORITY,__REALTIME_TIMESTAMP"}; !reflect.DeepEqual(JournalErrArgs, want) {
		t.Errorf("JournalErrArgs = %q", JournalErrArgs)
	}
	if want := []string{"-b", "-q", "--no-pager", "-o", "json", "-n", "50",
		"--output-fields=MESSAGE,__REALTIME_TIMESTAMP", "-g", "^Killed ", "SYSLOG_IDENTIFIER=systemd-oomd"}; !reflect.DeepEqual(OomdArgs, want) {
		t.Errorf("OomdArgs = %q", OomdArgs)
	}
	if want := []string{"-b", "-k", "-q", "--no-pager", "-o", "json", "-n", "20",
		"--output-fields=MESSAGE,__REALTIME_TIMESTAMP", "-g", "of memory: Killed process"}; !reflect.DeepEqual(KernelOOMArgs, want) {
		t.Errorf("KernelOOMArgs = %q", KernelOOMArgs)
	}
}

// M7: el nombre del programa matado se recorta a 40 runas y pierde los
// caracteres de formato (U+202E invierte el texto que sigue).
func TestReadOOMLongProgram(t *testing.T) {
	env, dir := tempEnv(t)
	long := strings.Repeat("x", 60)
	put(t, dir, oomdKey+".txt", jline(map[string]any{"MESSAGE": "Killed /user.slice/" + long + ".scope due to memory pressure", "__REALTIME_TIMESTAMP": "1790162000000000"}))
	put(t, dir, kernKey+".txt", jline(map[string]any{"MESSAGE": "Out of memory: Killed process 1 (ev‮il) total-vm:1kB", "__REALTIME_TIMESTAMP": "1790160000000000"}))
	l := ReadOOM(env, 1, &collect.Report{Lang: "en"})
	if l == nil || len(l.List) != 2 {
		t.Fatalf("list = %+v", l)
	}
	if p := l.List[0].Program; utf8.RuneCountInString(p) != 40 || !strings.HasSuffix(p, "…") {
		t.Errorf("long program = %q", p)
	}
	if p := l.List[1].Program; p != "evil" {
		t.Errorf("bidi program = %q", p)
	}
}
