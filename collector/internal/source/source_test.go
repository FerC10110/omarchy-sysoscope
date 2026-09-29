package source

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFSRootAndHelpers(t *testing.T) {
	root := t.TempDir()
	write(t, root, "proc/loadavg", "3.37 1.84 1.34 1/2788 1161473\n")
	write(t, root, "sys/x/num", " 42\n")
	write(t, root, "sys/x/float", "24.24\n")
	write(t, root, "sys/class/hwmon/hwmon10/name", "ath10k_hwmon\n")
	write(t, root, "sys/class/hwmon/hwmon2/name", "k10temp\n")
	if err := os.Symlink("hwmon2", filepath.Join(root, "sys/class/hwmon/link")); err != nil {
		t.Fatal(err)
	}
	fs := FS{Root: root}

	got, err := fs.Read("/proc/loadavg")
	if err != nil || got != "3.37 1.84 1.34 1/2788 1161473" {
		t.Fatalf("Read = %q, %v", got, err)
	}
	if n, err := fs.ReadInt("/sys/x/num"); err != nil || n != 42 {
		t.Fatalf("ReadInt = %d, %v", n, err)
	}
	if f, err := fs.ReadFloat("/sys/x/float"); err != nil || f != 24.24 {
		t.Fatalf("ReadFloat = %v, %v", f, err)
	}
	g, err := fs.Glob("/sys/class/hwmon/hwmon*")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/sys/class/hwmon/hwmon10", "/sys/class/hwmon/hwmon2"}
	if !reflect.DeepEqual(g, want) {
		t.Fatalf("Glob = %v, want %v", g, want)
	}
	if !fs.Exists("/sys/class/hwmon/link/name") || fs.Exists("/nope") {
		t.Fatal("Exists wrong")
	}
	if l, err := fs.Link("/sys/class/hwmon/link"); err != nil || l != "hwmon2" {
		t.Fatalf("Link = %q, %v", l, err)
	}
	d, err := fs.Dir("/sys/class/hwmon")
	if err != nil || !reflect.DeepEqual(d, []string{"hwmon10", "hwmon2", "link"}) {
		t.Fatalf("Dir = %v, %v", d, err)
	}
	lines, err := fs.Lines("/proc/loadavg")
	if err != nil || len(lines) != 1 {
		t.Fatalf("Lines = %v, %v", lines, err)
	}
	if _, err := fs.Read("/missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestFSNoRoot(t *testing.T) {
	if (FS{}).path("/proc/stat") != "/proc/stat" {
		t.Fatal("empty root must keep the path")
	}
}

func TestFakeRunner(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "cmd/nvidia-smi_--query-gpu=name_--format=csv.txt", "name\nRTX\n")
	r := &FakeRunner{Dir: filepath.Join(dir, "cmd")}
	out, err := r.Run(context.Background(), "nvidia-smi", "--query-gpu=name", "--format=csv")
	if err != nil || string(out) != "name\nRTX\n" {
		t.Fatalf("Run = %q, %v", out, err)
	}
	if _, err := r.Run(context.Background(), "inxi", "-m"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing cmd err = %v", err)
	}
	if r.Count("nvidia-smi") != 1 || r.Count("inxi") != 1 {
		t.Fatalf("counts = %v", r.Calls)
	}
}

func TestFakeBus(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "dbus.json", `{"system":{"net.hadess.PowerProfiles /net/hadess/PowerProfiles net.hadess.PowerProfiles ActiveProfile":"balanced"}}`)
	b, err := LoadFakeBus(filepath.Join(dir, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := b.Property(true, "net.hadess.PowerProfiles", "/net/hadess/PowerProfiles", "net.hadess.PowerProfiles", "ActiveProfile")
	if err != nil || v != "balanced" {
		t.Fatalf("Property = %v, %v", v, err)
	}
	if _, err := b.Property(false, "a", "/b", "c", "d"); err == nil {
		t.Fatal("absent key must error")
	}
}

func TestExecRunnerTimeout(t *testing.T) {
	start := time.Now()
	_, err := ExecRunner{Timeout: 50 * time.Millisecond}.Run(context.Background(), "sleep", "5")
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if _, err := (ExecRunner{Timeout: time.Second}).Run(context.Background(), "no-such-binary-xyz"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing binary err = %v", err)
	}
}

func TestFakeRunnerStream(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "cmd/nvidia-smi_-lms_100.txt", "a\nb\n")
	r := &FakeRunner{Dir: filepath.Join(dir, "cmd")}
	rc, err := r.Stream(context.Background(), "nvidia-smi", "-lms", "100")
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(rc)
	var got []string
	for sc.Scan() {
		got = append(got, sc.Text())
	}
	rc.Close()
	if !reflect.DeepEqual(got, []string{"a", "b"}) || r.Count("nvidia-smi") != 1 {
		t.Fatalf("Stream = %v, calls %v", got, r.Calls)
	}
	if _, err := r.Stream(context.Background(), "nope"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing stream err = %v", err)
	}
}

func TestExecRunnerStreamKillsOnClose(t *testing.T) {
	rc, err := ExecRunner{}.Stream(context.Background(), "sh", "-c", "echo one; sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(rc)
	if !sc.Scan() || sc.Text() != "one" {
		t.Fatalf("first line = %q", sc.Text())
	}
	start := time.Now()
	if err := rc.Close(); time.Since(start) > 2*time.Second {
		t.Fatalf("Close took %v (%v)", time.Since(start), err)
	}
}

// Los fixtures no pueden traer symlinks (el Market y `omarchy plugin add` los
// rechazan): un enlace se guarda como un archivo "<nombre>.link" con su destino.
func TestFixtureLinkFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sys/bus/pci/devices/0000:42:00.0/driver.link", "drivers/nvidia\n")
	fs := FS{Root: root}
	if l, err := fs.Link("/sys/bus/pci/devices/0000:42:00.0/driver"); err != nil || l != "drivers/nvidia" {
		t.Fatalf("Link = %q, %v", l, err)
	}
	if _, err := fs.Link("/sys/bus/pci/devices/0000:42:00.0/none"); err == nil {
		t.Fatal("a missing link must fail")
	}
}

// El estado del reloj del kernel (adjtimex) de un fixture es el archivo
// "timex" con la palabra de estado; sin archivo, error.
func TestFixtureTimex(t *testing.T) {
	st, err := Fixture(t, "threadripper").Timex()
	if err != nil || st&0x0040 != 0 { // STA_UNSYNC apagado: sincronizado
		t.Fatalf("threadripper timex = %#x, %v", st, err)
	}
	if _, err := Fixture(t, "laptop").Timex(); err == nil {
		t.Fatal("laptop has no timex file: must fail")
	}
}

func TestInPath(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "usr/bin"), 0o755)
	os.WriteFile(filepath.Join(root, "usr/bin/tool"), nil, 0o755)
	fs := FS{Root: root}
	if !InPath(fs, "/nowhere:/usr/bin", "tool") || InPath(fs, "/usr/bin", "other") || InPath(fs, "usr/bin", "tool") || InPath(fs, "", "tool") {
		t.Fatal("InPath")
	}
}

func TestFakeBusCountsCalls(t *testing.T) {
	b := &FakeBus{System: map[string]json.RawMessage{"call a.b /x a.b.M root": json.RawMessage(`[1]`)}}
	if _, err := b.Call(true, "a.b", "/x", "a.b.M", "root"); err != nil {
		t.Fatal(err)
	}
	b.Call(true, "a.b", "/x", "a.b.M", "nope") // sin respuesta: cuenta igual
	b.Property(true, "c.d", "/y", "c.d", "P")
	if n := b.Called("a.b"); n != 2 {
		t.Errorf("a.b calls = %d, want 2", n)
	}
	if n := b.Called("c.d"); n != 1 {
		t.Errorf("c.d calls = %d, want 1", n)
	}
	if n := b.Called("org.opensuse.Snapper"); n != 0 {
		t.Errorf("snapper calls = %d", n)
	}
}

func TestFixtureIdentityAndCopy(t *testing.T) {
	env := Fixture(t, "threadripper")
	if env.User.UID != 1000 || env.User.Name != "user" || env.HyprLock != FixtureHyprLock {
		t.Fatalf("user = %+v, lock = %q", env.User, env.HyprLock)
	}
	cp := FixtureCopy(t, "threadripper")
	if cp.FS.Root == env.FS.Root || !cp.FS.Exists("/proc/sys/kernel/osrelease") {
		t.Fatalf("copy root = %q", cp.FS.Root)
	}
	write(t, cp.FS.Root, "etc/only-in-the-copy", "x")
	if env.FS.Exists("/etc/only-in-the-copy") {
		t.Fatal("FixtureCopy wrote into collector/fixtures")
	}
	if _, ok := cp.Run.(*FakeRunner); !ok || cp.Cache == "" || cp.State == "" {
		t.Fatalf("copy env = %+v", cp)
	}
}

func TestCurrentUser(t *testing.T) {
	u := CurrentUser()
	if u.UID != uint32(os.Getuid()) || len(u.GIDs) == 0 || u.GIDs[0] != uint32(os.Getgid()) {
		t.Fatalf("user = %+v", u)
	}
}
