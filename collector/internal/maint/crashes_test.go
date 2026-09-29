package maint

import (
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var dumpKey = "cmd/" + source.Key("coredumpctl", CoredumpArgs...)

// realDumps son 12 volcados reales de esta máquina (del más viejo al más
// nuevo, como los da coredumpctl), con el home reemplazado.
const realDumps = `[{"time":1788813896492430,"pid":18697,"uid":1000,"gid":1000,"sig":5,"corefile":"missing","exe":"/usr/lib/chromium/chromium","size":null},` +
	`{"time":1788815686219246,"pid":104088,"uid":1000,"gid":1000,"sig":11,"corefile":"missing","exe":"/usr/lib/localsearch-3","size":null},` +
	`{"time":1789069567536051,"pid":14452,"uid":1000,"gid":1000,"sig":4,"corefile":"missing","exe":"/usr/lib/chromium/chromium","size":null},` +
	`{"time":1789069723370414,"pid":2824,"uid":1000,"gid":1000,"sig":5,"corefile":"missing","exe":"/usr/lib/chromium/chromium","size":null},` +
	`{"time":1789247509822034,"pid":32320,"uid":1000,"gid":1000,"sig":6,"corefile":"missing","exe":"/usr/bin/nautilus","size":null},` +
	`{"time":1789311775916159,"pid":2877,"uid":1000,"gid":1000,"sig":5,"corefile":"present","exe":"/usr/lib/chromium/chromium","size":53083508},` +
	`{"time":1789341257433923,"pid":3874,"uid":1000,"gid":1000,"sig":5,"corefile":"present","exe":"/usr/lib/chromium/chromium","size":30166581},` +
	`{"time":1789430636242452,"pid":151425,"uid":960,"gid":960,"sig":6,"corefile":"inaccessible","exe":"/usr/lib/ollama/llama-server","size":null},` +
	`{"time":1789677734910827,"pid":1310369,"uid":1000,"gid":1000,"sig":4,"corefile":"present","exe":"/prog1","size":101054178},` +
	`{"time":1789677736672959,"pid":1310465,"uid":1000,"gid":1000,"sig":4,"corefile":"present","exe":"/prog1","size":100847504},` +
	`{"time":1790285644078658,"pid":263520,"uid":1000,"gid":1000,"sig":4,"corefile":"present","exe":"/usr/lib/chromium/chromium","size":180645979},` +
	`{"time":1790285649587597,"pid":263789,"uid":1000,"gid":1000,"sig":4,"corefile":"present","exe":"/usr/lib/chromium/chromium","size":24299594}]`

func TestReadCrashes(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, dumpKey+".txt", realDumps+"\n")
	put(t, dir, "root/etc/passwd", "root:x:0:0::/root:/usr/bin/bash\nollama:x:960:960::/var/lib/ollama:/usr/bin/nologin\nbroken\n")
	r := &collect.Report{Lang: "en"}
	c := ReadCrashes(env, r)
	if c == nil || c.Total != 12 || len(c.List) != 10 || len(r.List) != 0 {
		t.Fatalf("crashes = %+v, unavailable %+v", c, r.List)
	}
	want0 := model.Crash{Time: time.UnixMicro(1790285649587597).UTC(), PID: 263789, Program: "chromium", Signal: 4, SignalName: "SIGILL",
		UID: 1000, User: "user", Stored: true}
	if c.List[0] != want0 {
		t.Errorf("newest = %+v", c.List[0])
	}
	if o := c.List[4]; o.Program != "llama-server" || o.User != "ollama" || o.Stored || o.SignalName != "SIGABRT" || o.PID != 151425 {
		t.Errorf("ollama = %+v", o)
	}
	if o := c.List[2]; o.Program != "prog1" || o.PID != 1310465 {
		t.Errorf("prog1 = %+v", o)
	}
	if c.List[9].PID != 14452 || c.List[5].SignalName != "SIGTRAP" {
		t.Errorf("oldest shown = %+v, sig5 %+v", c.List[9], c.List[5])
	}
	if n := runner(env).Count("coredumpctl"); n != 1 {
		t.Errorf("coredumpctl ran %d times", n)
	}
}

func TestParseCrashesUnknowns(t *testing.T) {
	c, err := parseCrashes([]byte(`[{"time":1,"pid":5,"uid":4242,"sig":99,"corefile":"none","exe":"","size":null}]`),
		map[uint32]string{1000: "user"}, "en")
	if err != nil || len(c.List) != 1 {
		t.Fatalf("parse = %+v, %v", c, err)
	}
	if got := c.List[0]; got.Program != "Unknown" || got.User != "4242" || got.SignalName != "" || got.Signal != 99 || got.Stored {
		t.Errorf("unknowns = %+v", got)
	}
	if c, err := parseCrashes([]byte("  \n"), nil, "en"); err != nil || c.Total != 0 || c.List == nil {
		t.Errorf("blank = %+v, %v", c, err)
	}
	if _, err := parseCrashes([]byte("not json"), nil, "en"); err == nil {
		t.Error("garbage parsed")
	}
}

// Review Focus 2: sin volcados coredumpctl sale ≠0 y no escribe nada.
func TestReadCrashesNoDumps(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, dumpKey+".txt", "")
	put(t, dir, dumpKey+".exit", "1\n")
	r := &collect.Report{Lang: "en"}
	if c := ReadCrashes(env, r); c == nil || c.Total != 0 || c.List == nil || len(r.List) != 0 {
		t.Fatalf("fixture exit 1 = %+v, %+v", c, r.List)
	}
	env.Run = &errRunner{err: &exec.ExitError{Stderr: []byte("No coredumps found.\n")}}
	if c := ReadCrashes(env, r); c == nil || c.Total != 0 || len(r.List) != 0 {
		t.Fatalf("no coredumps found = %+v, %+v", c, r.List)
	}
	env.Run = &errRunner{err: &exec.ExitError{Stderr: []byte("Failed to acquire bus: Permission denied\n")}}
	if c := ReadCrashes(env, r); c != nil || len(r.List) != 1 || r.List[0].What != "maintenance.health.crashes" ||
		r.List[0].Message != "could not read: Failed to acquire bus: Permission denied" {
		t.Fatalf("other error = %+v, %+v", c, r.List)
	}
	bare, _ := tempEnv(t)
	r = &collect.Report{Lang: "en"}
	if c := ReadCrashes(bare, r); c != nil || r.List[0].Reason != "missing_tool" || r.List[0].Message != "requires coredumpctl" {
		t.Fatalf("missing = %+v, %+v", c, r.List)
	}
	put(t, dir, dumpKey+".txt", "garbage")
	if err := removeFile(dir, dumpKey+".exit"); err != nil {
		t.Fatal(err)
	}
	env, _ = tempEnvAt(t, dir)
	r = &collect.Report{Lang: "en"}
	if c := ReadCrashes(env, r); c != nil || len(r.List) != 1 || r.List[0].Reason != "error" {
		t.Fatalf("garbage = %+v, %+v", c, r.List)
	}
}

func TestUserNames(t *testing.T) {
	env, dir := tempEnv(t)
	if got := UserNames(env); !reflect.DeepEqual(got, map[uint32]string{1000: "user"}) {
		t.Errorf("without passwd = %v", got)
	}
	put(t, dir, "root/etc/passwd", "root:x:0:0::/root:/usr/bin/bash\njoe:x:1000:1000::/home/joe:/bin/bash\navahi:x:972:972::/:/usr/bin/nologin\n:x:5:5\nbad:x:nope:1\n")
	got := UserNames(env)
	if got[0] != "root" || got[972] != "avahi" || got[1000] != "user" || len(got) != 3 {
		t.Errorf("names = %v (the Env's own name wins)", got)
	}
}
