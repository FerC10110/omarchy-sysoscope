package fixture

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/bluetooth"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/storage"
)

// exitErrRunner corre como el FakeRunner que envuelve, salvo para un comando
// (key = source.Key): ahí da out=nil y err, como ExecRunner cuando un
// comando de verdad sale con error sin escribir nada a stdout (algo que el
// .exit de un fixture no puede expresar, porque no lleva stderr).
type exitErrRunner struct {
	*source.FakeRunner
	key string
	err error
}

func (r *exitErrRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if source.Key(name, args...) == r.key {
		return nil, r.err
	}
	return r.FakeRunner.Run(ctx, name, args...)
}

// netHexOf arma el hex de /proc/net (palabras de 32 bits little-endian, ver
// Anonymizer.NetHex) para una dirección, del mismo largo que usaría el
// kernel: 4 bytes para v4, 16 para v6 (o v4 mapeada en v6, con v6=true).
func netHexOf(t *testing.T, s string, v6 bool) string {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("bad ip %q", s)
	}
	var b []byte
	if v6 {
		b = ip.To16()
	} else {
		b = ip.To4()
	}
	if b == nil {
		t.Fatalf("%q doesn't fit v6=%v", s, v6)
	}
	out := make([]byte, len(b))
	for i := 0; i < len(b); i += 4 {
		out[i], out[i+1], out[i+2], out[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	return strings.ToUpper(hex.EncodeToString(out))
}

func TestAnonymize(t *testing.T) {
	a := NewAnonymizer("joe", "/home/joe", "zenith")
	a.Serial("FAKEDISK0000001")
	a.Serial("WD-FAKE0001")
	cases := []struct{ in, want string }{
		{"link/ether 70:4d:7b:12:34:56 brd", "link/ether 02:00:00:00:00:01 brd"},
		{"70:4D:7B:12:34:56 again", "02:00:00:00:00:01 again"},
		{"lo 00:00:00:00:00:00", "lo 00:00:00:00:00:00"},
		{`"serial": "FAKEDISK0000001"`, `"serial": "SERIAL1"`},
		{"/drives/WDC_WD120EFGX_68CPHN0_WD_FAKE0001", "/drives/WDC_WD120EFGX_68CPHN0_SERIAL2"},
		{"\tSSID: MiCasa 5G\n", "\tSSID: wifi1\n"},
		{"/home/joe/.config and joe", "/home/user/.config and user"},
		{"/run/media/joe/USB on zenith, attester joey", "/run/media/user/USB on host, attester joey"},
		{"vbios 94.02.42.00.2B stays", "vbios 94.02.42.00.2B stays"},
		{"0b3bffe4-e0e2-4d94-ba1b-4046a42e9e62", "00000000-0000-0000-0000-000000000001"},
	}
	for _, c := range cases {
		if got := a.Text(c.in); got != c.want {
			t.Errorf("Text(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Una interfaz nombrada con su MAC (esquema de udev en/wl/wwx<mac>)
	// pierde la MAC pero conserva su prefijo de 2 letras, con el mismo
	// pseudónimo sin importar si está sola, en una ruta (después de "/") o
	// en un campo compuesto (después de "_", como "in_<iface>" de ufw).
	fresh := NewAnonymizer("joe", "/home/joe", "zenith")
	for _, c := range []struct{ in, want string }{
		{"enx00e04c680001", "enx020000000001"},
		{"/sys/class/net/enx00e04c680001/type", "/sys/class/net/enx020000000001/type"},
		{"in_enx00e04c680001", "in_enx020000000001"},
		{"wlx1234567890ab", "wlx020000000002"},
	} {
		if got := fresh.Text(c.in); got != c.want {
			t.Errorf("Text(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, c := range [][2]string{
		{"nameserver 192.168.100.1\nnameserver 8.8.8.8\n", "nameserver 192.0.2.1\nnameserver 192.0.2.2\n"},
		{"nameserver 127.0.0.53\n", "nameserver 127.0.0.53\n"},
		{"nameserver fe80::1c2b:3aff:fe4d:5e6f\n", "nameserver 2001:db8::1\n"},
		{"nameserver fe80::1%enp5s0\n", "nameserver 2001:db8::2%enp5s0\n"},
		{"search corp.secret.lan home\n", "search example.com\n"},
	} {
		if got := a.IPs(c[0]); got != c[1] {
			t.Errorf("IPs(%q) = %q, want %q", c[0], got, c[1])
		}
	}
	if got := a.RouteHex("0164A8C0"); got != "010200C0" {
		t.Errorf("route hex = %q, want 192.0.2.1 little-endian", got)
	}
	if a.RouteHex("00000000") != "00000000" {
		t.Error("zero route must stay")
	}
	for _, c := range [][2]string{
		{"0100007F", "0100007F"}, // loopback
		{"00000000", "00000000"},
		{"FB0000E0", "FB0000E0"}, // multicast: su alcance no cambia
		{"097100CB", "030200C0"}, // 203.0.113.9 → 192.0.2.3 (el tercero de este test)
		{"00000000000000000000000001000000", "00000000000000000000000001000000"}, // ::1
		{"000080FE00000000FF3A2B1C6F5E4DFE", "000080FE000000000000000001000000"}, // fe80::1c2b:3aff:fe4d:5e6f → fe80::1
		{"000002FF0000000000000000FB000000", "000002FF0000000000000000FB000000"}, // ff02::fb
		{"F804012A000000000000000001000000", "B80D0120000000000000000003000000"}, // 2a01:4f8::1 → 2001:db8::3
		{"ZZ", "ZZ"},
		// IPv4 link-local (169.254/16): conserva el rango, no pasa a 192.0.2.N.
		{netHexOf(t, "169.254.5.9", false), netHexOf(t, "169.254.0.1", false)},
		// v4 mapeada en v6 (::ffff:a.b.c.d): la misma 203.0.113.9 de este
		// test (ya en 192.0.2.3 arriba), con la familia v4 (no 2001:db8::N)
		// y la estructura de v4 mapeada.
		{netHexOf(t, "::ffff:203.0.113.9", true), netHexOf(t, "::ffff:192.0.2.3", true)},
	} {
		if got := a.NetHex(c[0]); got != c[1] {
			t.Errorf("NetHex(%s) = %s, want %s", c[0], got, c[1])
		}
	}
}

func TestCaptureRoundTrip(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	out := t.TempDir()
	if err := Capture(src, out, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"root/proc/stat", "root/sys/class/hwmon/hwmon2/temp2_input", "dbus.json", "cmd/" + source.Key("inxi", "-m", "-c", "0") + ".txt"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	capEnv := func(dir string) source.Env {
		bus, err := source.LoadFakeBus(filepath.Join(dir, "dbus.json"))
		if err != nil {
			t.Fatal(err)
		}
		return source.Env{FS: source.FS{Root: filepath.Join(dir, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(dir, "cmd")},
			Bus: bus, Now: func() time.Time { return source.FixtureNow }, Timex: source.FileTimex(filepath.Join(dir, "timex")),
			Lang: "en", Home: "/home/user", State: t.TempDir(), Cache: t.TempDir()}
	}
	st := func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }
	want := engine.New(src, engine.Options{Interval: 2 * time.Second, Statfs: st}).Tick()
	got := engine.New(capEnv(out), engine.Options{Interval: 2 * time.Second, Statfs: st}).Tick()
	if got.CPU.Model != want.CPU.Model || *got.CPU.TempC != *want.CPU.TempC || len(got.Storage.Disks) != len(want.Storage.Disks) ||
		got.GPUs[0].Name != want.GPUs[0].Name || len(got.Memory.Modules) != 3 || got.System.Omarchy != "4.0.4-1" || got.Power.Profile != "balanced" {
		t.Fatalf("captured fixture differs: cpu %q disks %d gpu %q omarchy %q profile %q",
			got.CPU.Model, len(got.Storage.Disks), got.GPUs[0].Name, got.System.Omarchy, got.Power.Profile)
	}
	if got.Storage.Disks[4].Smart == nil || len(got.System.FailedUnits) != 1 {
		t.Fatalf("dbus not captured: smart %+v units %+v", got.Storage.Disks[4].Smart, got.System.FailedUnits)
	}
	if got.System.Time == nil || *got.System.Time != *want.System.Time || !want.System.Time.Synced || !want.System.Time.NTP {
		t.Fatalf("clock not captured: %+v, want %+v", got.System.Time, want.System.Time)
	}
	// El reloj se captura sin timedate1 (se activa por D-Bus): enlace de
	// /etc/localtime, estado de adjtimex y la unidad de timesyncd.
	if raw, _ := os.ReadFile(filepath.Join(out, "dbus.json")); strings.Contains(string(raw), "timedate1") {
		t.Error("capture must not ask timedate1")
	}
	if l, _ := os.ReadFile(filepath.Join(out, "root/etc/localtime.link")); string(l) != "/usr/share/zoneinfo/America/Argentina/Buenos_Aires\n" {
		t.Errorf("localtime.link = %q", l)
	}
	if tx, _ := os.ReadFile(filepath.Join(out, "timex")); string(tx) != "8193\n" {
		t.Errorf("timex = %q", tx)
	}

	// El audio, con la vista Dispositivos: pactl reducido a lo que se lee.
	sound := func(env source.Env) *model.Sound {
		return engine.New(env, engine.Options{Interval: 2 * time.Second, Focus: "devices", Statfs: st}).Tick().System.Sound
	}
	if w, g := sound(src), sound(capEnv(out)); w == nil || !reflect.DeepEqual(g, w) {
		t.Fatalf("audio not captured: %+v, want %+v", g, w)
	}
	info, _ := os.ReadFile(filepath.Join(out, "cmd", source.Key("pactl", "--format=json", "info")+".txt"))
	if strings.Contains(string(info), "user_name") || !strings.Contains(string(info), `"default_sink_name"`) {
		t.Errorf("pactl info = %s", info)
	}

	anon := t.TempDir()
	tzDir := t.TempDir() // con /etc/timezone, que también dice dónde vive
	if err := os.CopyFS(tzDir, os.DirFS(filepath.Dir(src.FS.Root))); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tzDir, "root/etc/timezone"), []byte("America/Argentina/Buenos_Aires\n"), 0o644)
	src = capEnv(tzDir)
	if err := Capture(src, anon, true); err != nil {
		t.Fatal(err)
	}
	host, _ := os.ReadFile(filepath.Join(anon, "root/etc/hostname"))
	if strings.TrimSpace(string(host)) != "host" {
		t.Errorf("hostname = %q", host)
	}
	lsblk, _ := os.ReadFile(filepath.Join(anon, "cmd", source.Key("lsblk", "-J", "-b", "-o", "NAME,KNAME,PATH,TYPE,SIZE,MODEL,SERIAL,TRAN,ROTA,FSTYPE,MOUNTPOINTS,PKNAME,LABEL")+".txt"))
	if !strings.Contains(string(lsblk), `"SERIAL1"`) {
		t.Errorf("serials not anonymized in lsblk")
	}
	a := engine.New(capEnv(anon), engine.Options{Interval: 2 * time.Second, Statfs: st}).Tick()
	if len(a.Storage.Disks) != 6 || a.Storage.Disks[4].Smart == nil {
		t.Fatalf("anonymized capture broke SMART matching: %+v", a.Storage.Disks)
	}
	if a.System.Time == nil || a.System.Time.Zone != "UTC" || !a.System.Time.NTP || !a.System.Time.Synced || !a.System.Time.CanNTP {
		t.Fatalf("anonymized time zone = %+v", a.System.Time)
	}
	filepath.WalkDir(anon, func(p string, d os.DirEntry, err error) error {
		if data, _ := os.ReadFile(p); !d.IsDir() && strings.Contains(string(data), "Buenos_Aires") {
			t.Errorf("%s leaks the time zone", p)
		}
		return nil
	})
}

func TestCaptureAnonymizeLeaksNothing(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Dir(src.FS.Root))); err != nil {
		t.Fatal(err)
	}
	put := func(rel, s string) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.Chmod(p, 0o644)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("USER", "joe")
	put("root/etc/hostname", "myhostbox\n")
	put("root/sys/class/power_supply/hid-aa:bb:cc:dd:ee:f1-battery/type", "Battery\n")
	put("root/sys/class/power_supply/hid-aa:bb:cc:dd:ee:f1-battery/model_name", "joe mouse on myhostbox\n")
	put("root/etc/resolv.conf", "search corp.secret.lan\nnameserver fe80::1%enp5s0\n")
	put("root/var/lib/pacman/local/secretpkg-1.0-1/desc", "")
	put("root/var/lib/flatpak/app/com.secretcorp.App/.keep", "")
	put("cmd/pacman_-Qqm.txt", "secret-aur-tool\n")
	put("root/proc/net/tcp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"+
		"   0: 097100CB:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 777 1 0000000000000000 100 0 0 10 0\n"+
		"   1: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 779 1 0000000000000000 100 0 0 10 0\n"+
		// Un socket de un tercer humano de la máquina (uid 1001, no quien
		// corre la captura): su nombre real (alice) no debe sobrevivir en el
		// passwd reducido, y su proceso (comm/fd) no se captura (OwnInodes lo
		// filtra: solo se listean sockets propios).
		"   2: 00000000:1F91 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1001        0 781 1 0000000000000000 100 0 0 10 0\n")
	put("root/proc/net/udp6", "  sl  local_address remote_address st\n"+
		" 1: 000080FE00000000FF3A2B1C6F5E4DFE:0222 00000000000000000000000000000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 778 2 0000000000000000 0\n")
	put("root/etc/ufw/user.rules", "*filter\n### tuple ### allow tcp 22 0.0.0.0/0 any 203.0.113.9 in comment=6a6f652773207365637265742070630a\n"+
		"-A ufw-user-input -p tcp --dport 22 -s 203.0.113.9 -j ACCEPT\n"+
		// Una tupla de perfil de aplicación (9 campos, con app dst): el
		// nombre de la app lleva el usuario como sufijo (joe-syncthing), para
		// probar que también pasa por Text (I2), no solo direcciones y
		// comentario.
		"### tuple ### allow tcp 8384 0.0.0.0/0 any 0.0.0.0/0 joe-syncthing - in\nCOMMIT\n")
	put("root/sys/block/dm-0/dm/uuid", "CRYPT-LUKS2-9f3a2b1c0d4e5f60718293a4b5c6d7e8-root\n")
	put("root/etc/passwd", "root:x:0:0::/root:/bin/bash\njoe:x:1000:1000:Julian Secret:/home/joe:/bin/zsh\nalice:x:1001:1001:Alice Secret:/home/alice:/bin/bash\n")
	// Un socket propio (uid 1000, inode 779, arriba en tcp) cuyo dueño se
	// llama como si fuera un secreto: Seguridad también debe pasar el comm
	// por Prog (progN), no dejarlo tal cual.
	put("root/proc/9999/comm", "secret-listener\n")
	put("root/proc/9999/fd/5.link", "socket:[779]\n")
	// Segunda línea: SYSLOG_IDENTIFIER y _COMM son también nombres de
	// programa, no solo el MESSAGE (que ya sale "message N").
	put("cmd/"+source.Key("journalctl", maint.JournalErrArgs...)+".txt",
		`{"MESSAGE":"secret token of joe on myhostbox","PRIORITY":"3","SYSLOG_IDENTIFIER":"kernel","_BOOT_ID":"abc","__CURSOR":"s=1;i=2","__REALTIME_TIMESTAMP":"1790150060000000"}`+"\n"+
			`{"MESSAGE":"another secret leak","PRIORITY":"4","SYSLOG_IDENTIFIER":"secret-daemon","_COMM":"secret-daemon","__REALTIME_TIMESTAMP":"1790150070000000"}`+"\n")
	// Segunda fila: el volcado de otro usuario (alice, no quien corre la
	// captura) con un exe cuyo programa también se llama "secret…". Tercera
	// y cuarta: el secreto vive solo en el directorio (no en el programa),
	// para probar que el exe se reduce a "/" + progN, sin el camino.
	put("cmd/"+source.Key("coredumpctl", maint.CoredumpArgs...)+".txt",
		`[{"time":1789677734910827,"pid":7,"uid":1000,"gid":1000,"sig":11,"corefile":"present","exe":"/home/joe/.local/bin/mytool","size":null,"boot_id":"abc","hostname":"myhostbox"},`+
			`{"time":1789677734910900,"pid":8,"uid":1001,"gid":1001,"sig":6,"corefile":"present","exe":"/home/alice/.local/bin/secret-thing","size":null,"boot_id":"abc","hostname":"myhostbox"},`+
			`{"time":1789677734910950,"pid":9,"uid":1000,"gid":1000,"sig":4,"corefile":"present","exe":"/opt/secretcorp/bin/x","size":null,"boot_id":"abc","hostname":"myhostbox"},`+
			`{"time":1789677734911000,"pid":10,"uid":1001,"gid":1001,"sig":5,"corefile":"present","exe":"/home/alice/secretcorp/tool","size":null,"boot_id":"abc","hostname":"myhostbox"}]`+"\n")
	put("root/var/log/pacman.log", "[2026-09-22T20:41:31-0300] [PACMAN] Running 'pacman -U /home/joe/.cache/yay/secret-tool/x.pkg.tar.zst'\n"+
		"[2026-09-22T20:41:32-0300] [PACMAN] starting full system upgrade\n"+
		"[2026-09-22T20:41:34-0300] [ALPM] upgraded secret-tool (1.0-1 -> 1.1-1)\n"+
		"[2026-09-22T20:41:34-0300] [ALPM-SCRIPTLET] joe's secret hook output\n"+
		"[2026-09-22T20:41:34-0300] [ALPM] transaction completed\n")
	put("root/home/joe/.local/state/omarchy/restart-walker-required", "")
	put("cmd/"+source.Key("pactl", "--format=json", "list", "sinks")+".txt",
		`[{"name":"bluez_output.AA_BB_CC_DD_EE_F3.1","description":"Julian's AirPods","mute":false,`+
			`"volume":{"front-left":{"value":65536}},"properties":{"object.id":"70","node.nick":"Julian's AirPods",`+
			`"device.class":"sound","api.bluez5.address":"AA:BB:CC:DD:EE:F3"},"ports":[]}]`)
	put("cmd/"+source.Key("pactl", "--format=json", "info")+".txt",
		`{"user_name":"joe","host_name":"myhostbox","cookie":"ab:cd","default_sink_name":"bluez_output.AA_BB_CC_DD_EE_F3.1","default_source_name":"x"}`)
	raw, _ := os.ReadFile(filepath.Join(dir, "dbus.json"))
	var bus map[string]map[string]any
	json.Unmarshal(raw, &bus)
	bus["system"]["call org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus.NameHasOwner org.bluez"] = []any{true}
	bus["system"]["objects org.bluez /"] = map[string]any{"/org/bluez/hci0/dev_AA_BB_CC_DD_EE_F2": map[string]any{
		"org.bluez.Device1": map[string]any{"Name": "Julian's iPhone", "Address": "AA:BB:CC:DD:EE:F2", "Connected": true}}}
	b, _ := json.Marshal(bus)
	put("dbus.json", string(b))

	fb, err := source.LoadFakeBus(filepath.Join(dir, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := source.Env{FS: source.FS{Root: filepath.Join(dir, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(dir, "cmd")},
		Bus: fb, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/joe",
		User: source.User{UID: 1000, Name: "joe"}}
	out := t.TempDir()
	if err := Capture(env, out, true); err != nil {
		t.Fatal(err)
	}
	leaks := regexp.MustCompile(`(?i)aa:bb:cc:dd:ee|AA_BB_CC_DD_EE|secret|Julian|\bjoe\b|\balice\b|myhostbox|fe80::1|-0300|203\.0\.113\.9|097100CB|FF3A2B1C|6a6f652773|9f3a2b1c0d4e`)
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if m := leaks.FindString(p); m != "" {
			t.Errorf("path leaks %q: %s", m, p)
		}
		if !d.IsDir() {
			data, _ := os.ReadFile(p)
			if m := leaks.FindString(string(data)); m != "" {
				t.Errorf("%s leaks %q", p, m)
			}
		}
		return nil
	})
	pkgs, _ := os.ReadDir(filepath.Join(out, "root/var/lib/pacman/local"))
	if len(pkgs) != 5 {
		t.Errorf("package count must survive anonymization: %d dirs", len(pkgs))
	}
	if _, err := os.Stat(filepath.Join(out, "root/var/lib/pacman/local/omarchy-4.0.4-1")); err != nil {
		t.Error("the omarchy package must keep its name (the collector reads its version)")
	}
	log, _ := os.ReadFile(filepath.Join(out, "root/var/log/pacman.log"))
	// Las fechas quedan en UTC (+0000), como el resto de la captura
	// anonimizada: no la zona horaria (-0300) del dueño de la máquina.
	want := "[2026-09-22T23:41:31+0000] [PACMAN] Running 'pacman'\n[2026-09-22T23:41:32+0000] [PACMAN] starting full system upgrade\n" +
		"[2026-09-22T23:41:34+0000] [ALPM] upgraded pkg1\n[2026-09-22T23:41:34+0000] [ALPM] transaction completed\n"
	if string(log) != want {
		t.Errorf("pacman.log = %q", log)
	}
	if _, err := os.Stat(filepath.Join(out, "root/var/lib/pacman/local/nvidia-utils-610.57.04-1")); err != nil {
		t.Error("the NVIDIA utils package must keep its name (its version is compared with the loaded driver)")
	}
	if _, err := os.Stat(filepath.Join(out, "root/home/user/.local/state/omarchy/restart-walker-required")); err != nil {
		t.Error("Omarchy's restart markers must be captured, under the anonymized home")
	}

	errs, _ := os.ReadFile(filepath.Join(out, "cmd", source.Key("journalctl", maint.JournalErrArgs...)+".txt"))
	// "kernel" (la fuente literal) no cambia; secret-daemon, sí, y da el
	// mismo prog1 en SYSLOG_IDENTIFIER y _COMM (mismo nombre, mismo progN).
	if string(errs) != `{"MESSAGE":"message 1","PRIORITY":"3","SYSLOG_IDENTIFIER":"kernel","__REALTIME_TIMESTAMP":"1790150060000000"}`+"\n"+
		`{"MESSAGE":"message 2","PRIORITY":"4","SYSLOG_IDENTIFIER":"prog1","_COMM":"prog1","__REALTIME_TIMESTAMP":"1790150070000000"}`+"\n" {
		t.Errorf("journal errors = %q", errs)
	}
	dumps, _ := os.ReadFile(filepath.Join(out, "cmd", source.Key("coredumpctl", maint.CoredumpArgs...)+".txt"))
	// gid no se guarda (no se lee); el exe entero se reduce a "/" + progN
	// (coredumpctl solo lee path.Base, ver maint/crashes.go): ni el home de
	// otro usuario ni un directorio de proyecto sobreviven.
	if string(dumps) != `[{"corefile":"present","exe":"/prog2","pid":7,"sig":11,"size":null,"time":1789677734910827,"uid":1000},`+
		`{"corefile":"present","exe":"/prog3","pid":8,"sig":6,"size":null,"time":1789677734910900,"uid":1001},`+
		`{"corefile":"present","exe":"/prog4","pid":9,"sig":4,"size":null,"time":1789677734910950,"uid":1000},`+
		`{"corefile":"present","exe":"/prog5","pid":10,"sig":5,"size":null,"time":1789677734911000,"uid":1001}]`+"\n" {
		t.Errorf("coredumps = %q", dumps)
	}

	// Seguridad: user.rules queda solo con las tuplas (direcciones y app
	// anonimizadas, comentario como "rule N"); el uuid de dm-0 (LUKS) da un
	// id estable con el mismo prefijo/estructura.
	rules, _ := os.ReadFile(filepath.Join(out, "root/etc/ufw/user.rules"))
	if !strings.Contains(string(rules), "comment=72756c652031") || strings.Contains(string(rules), "-A ") {
		t.Errorf("user.rules = %q", rules)
	}
	// El nombre de un perfil de aplicación es tan propio como un nombre de
	// programa: pasa por Prog (progN), no solo por Text, así que el usuario
	// que arrastraba como prefijo tampoco sobrevive.
	if !regexp.MustCompile(`### tuple ### allow tcp 8384 0\.0\.0\.0/0 any 0\.0\.0\.0/0 prog[0-9]+ - in`).MatchString(string(rules)) ||
		strings.Contains(string(rules), "syncthing") {
		t.Errorf("user.rules app profile = %q, want the app name replaced by progN", rules)
	}
	// El passwd reducido: el humano que corre la captura queda "user"; otro
	// humano de la máquina (uid 1001, alice) no sigue con su nombre real.
	passwd, _ := os.ReadFile(filepath.Join(out, "root/etc/passwd"))
	if !regexp.MustCompile(`(?m)^user:x:1000:$`).Match(passwd) || !regexp.MustCompile(`(?m)^user[0-9]+:x:1001:$`).Match(passwd) {
		t.Errorf("passwd = %q, want uid 1000 as \"user\" and uid 1001 as a stable userN", passwd)
	}
	if u, _ := os.ReadFile(filepath.Join(out, "root/sys/block/dm-0/dm/uuid")); string(u) != "CRYPT-LUKS2-00000000000000000000000000000001-root\n" {
		t.Errorf("dm uuid = %q", u)
	}
	// El comm de un socket propio pasa por Prog (progN), no por Text: "secret
	// listener" no sobrevive como nombre de programa reconocible.
	if c9999, _ := os.ReadFile(filepath.Join(out, "root/proc/9999/comm")); !regexp.MustCompile(`^prog[0-9]+\n$`).Match(c9999) {
		t.Errorf("proc/9999/comm = %q, want progN (Prog, not Text)", c9999)
	}
}

// La salud sobrevive a una captura: el engine sobre la captura lee lo mismo.
// Con oom_kill > 0 se captura también el journal del kernel, reducido.
func TestCaptureHealth(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	out := t.TempDir()
	if err := Capture(src, out, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"root/proc/vmstat", "cmd/" + source.Key("journalctl", maint.JournalErrArgs...) + ".txt",
		"cmd/" + source.Key("coredumpctl", maint.CoredumpArgs...) + ".txt", "cmd/" + source.Key("journalctl", maint.OomdArgs...) + ".txt"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "cmd", source.Key("journalctl", maint.KernelOOMArgs...)+".txt")); err == nil {
		t.Error("the kernel's journal must not run with oom_kill 0")
	}
	bus, err := source.LoadFakeBus(filepath.Join(out, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	st := func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }
	env2 := source.Env{FS: source.FS{Root: filepath.Join(out, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(out, "cmd")},
		Bus: bus, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/user",
		State: t.TempDir(), Cache: t.TempDir(), User: source.FixtureUser, HyprLock: source.FixtureHyprLock}
	want := engine.New(src, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "maintenance"}).Tick().Maintenance.Health
	got := engine.New(env2, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "maintenance"}).Tick().Maintenance.Health
	if got == nil || want == nil || !reflect.DeepEqual(got.Errors, want.Errors) || !reflect.DeepEqual(got.Crashes, want.Crashes) ||
		!reflect.DeepEqual(got.OOM, want.OOM) {
		t.Fatalf("health differs: got %+v, want %+v", got, want)
	}

	// Con muertes: el journal del kernel y el de systemd-oomd, reducidos y
	// con el programa cambiado por progN al anonimizar (secret-app, y el
	// tramo final de la unidad de systemd-oomd); el conteo sobrevive igual
	// (mismos Errors.Count, Crashes.Total y largo de la lista de OOM que sin
	// anonimizar).
	cp := source.FixtureCopy(t, "threadripper")
	dir := filepath.Dir(cp.FS.Root)
	if err := os.WriteFile(filepath.Join(dir, "root/proc/vmstat"), []byte("oom_kill 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", source.Key("journalctl", maint.KernelOOMArgs...)+".txt"),
		[]byte(`{"MESSAGE":"Out of memory: Killed process 48213 (secret-app) total-vm:11534336kB, anon-rss:6291456kB, UID:1000 pgtables:14336kB oom_score_adj:167","__CURSOR":"s=9","__REALTIME_TIMESTAMP":"1790160000000000"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Segunda línea: un camino anidado, con el secreto en un tramo padre (no
	// en el último): endurece que se pseudonimiza todo lo que sigue a los
	// .slice del principio como una sola unidad, no solo el último tramo.
	if err := os.WriteFile(filepath.Join(dir, "cmd", source.Key("journalctl", maint.OomdArgs...)+".txt"),
		[]byte(`{"MESSAGE":"Killed /user.slice/user-1000.slice/user@1000.service/app.slice/app-flatpak-com.secretcorp.App-1234.scope due to memory pressure for /user.slice/user-1000.slice being 87.89% > 50.00% for > 20s with reclaim activity","__REALTIME_TIMESTAMP":"1790159000000000"}`+"\n"+
			`{"MESSAGE":"Killed /user.slice/user-1000.slice/user@1000.service/app.slice/app-flatpak-com.secretcorp.App-5678.scope/nested/leaf due to memory pressure for /user.slice/user-1000.slice being 91.00% > 50.00% for > 20s with reclaim activity","__REALTIME_TIMESTAMP":"1790159500000000"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	readHealth := func(d string) *model.Health {
		bus, err := source.LoadFakeBus(filepath.Join(d, "dbus.json"))
		if err != nil {
			t.Fatal(err)
		}
		e := source.Env{FS: source.FS{Root: filepath.Join(d, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(d, "cmd")},
			Bus: bus, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/user",
			State: t.TempDir(), Cache: t.TempDir(), User: source.FixtureUser, HyprLock: source.FixtureHyprLock}
		return engine.New(e, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "maintenance"}).Tick().Maintenance.Health
	}

	plain := t.TempDir()
	if err := Capture(cp, plain, false); err != nil {
		t.Fatal(err)
	}
	anon := t.TempDir()
	t.Setenv("USER", "joe")
	if err := Capture(cp, anon, true); err != nil {
		t.Fatal(err)
	}

	// El progN exacto depende de qué otros nombres consumió el resto de la
	// captura (los del propio fixture): solo importa la forma y que ya no
	// diga "secret-app" ni "secretcorp".
	k, err := os.ReadFile(filepath.Join(anon, "cmd", source.Key("journalctl", maint.KernelOOMArgs...)+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\{"MESSAGE":"Out of memory: Killed process 48213 \(prog[0-9]+\)","__REALTIME_TIMESTAMP":"1790160000000000"\}` + "\n$").Match(k) {
		t.Fatalf("kernel journal = %q", k)
	}
	oomd, err := os.ReadFile(filepath.Join(anon, "cmd", source.Key("journalctl", maint.OomdArgs...)+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	// Los .slice del principio (user.slice, user-1000.slice: no identifican
	// una app) sobreviven; todo lo demás —incluido "app.slice" y cualquier
	// tramo anidado después del scope— queda en un solo progN por línea.
	oomdLines := strings.Split(strings.TrimRight(string(oomd), "\n"), "\n")
	oomdRe := regexp.MustCompile(`^\{"MESSAGE":"Killed /user\.slice/user-1000\.slice/prog[0-9]+","__REALTIME_TIMESTAMP":"(1790159000000000|1790159500000000)"\}$`)
	if len(oomdLines) != 2 {
		t.Fatalf("oomd journal lines = %d: %q", len(oomdLines), oomd)
	}
	for _, l := range oomdLines {
		if !oomdRe.MatchString(l) {
			t.Errorf("oomd journal line = %q", l)
		}
	}

	// Ningún archivo de la salud (ni el resto de la captura) deja pasar
	// "secret".
	leaks := regexp.MustCompile(`(?i)secret`)
	filepath.WalkDir(anon, func(p string, d os.DirEntry, err error) error {
		if !d.IsDir() {
			data, _ := os.ReadFile(p)
			if m := leaks.FindString(string(data)); m != "" {
				t.Errorf("%s leaks %q", p, m)
			}
		}
		return nil
	})

	plainHealth, anonHealth := readHealth(plain), readHealth(anon)
	if plainHealth == nil || anonHealth == nil || plainHealth.Errors == nil || anonHealth.Errors == nil ||
		plainHealth.Errors.Count != anonHealth.Errors.Count ||
		len(plainHealth.Errors.Sources) != len(anonHealth.Errors.Sources) ||
		plainHealth.Crashes == nil || anonHealth.Crashes == nil ||
		plainHealth.Crashes.Total != anonHealth.Crashes.Total || plainHealth.OOM == nil || anonHealth.OOM == nil ||
		len(plainHealth.OOM.List) != len(anonHealth.OOM.List) {
		t.Fatalf("counts differ: plain %+v, anon %+v", plainHealth, anonHealth)
	}
}

// Sin volcados coredumpctl sale 1 sin escribir nada: se guarda igual, para
// que el fixture no parezca "sin coredumpctl". Este caso llega con out ya
// no-nil (un .txt vacío existente, como cualquier fixture): el criterio de
// abajo (maint.NoEntries) no interviene, ver TestCaptureNoEntriesGatesStorage.
func TestCaptureEmptyCoredumps(t *testing.T) {
	cp := source.FixtureCopy(t, "threadripper")
	dir := filepath.Dir(cp.FS.Root)
	key := source.Key("coredumpctl", maint.CoredumpArgs...)
	if err := os.WriteFile(filepath.Join(dir, "cmd", key+".txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", key+".exit"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := Capture(cp, out, false); err != nil {
		t.Fatal(err)
	}
	if code, err := os.ReadFile(filepath.Join(out, "cmd", key+".exit")); err != nil || string(code) != "1\n" {
		t.Fatalf("exit = %q, %v", code, err)
	}
	if _, err := os.Stat(filepath.Join(out, "cmd", key+".txt")); err != nil {
		t.Fatal("the empty output must be kept")
	}
}

// Como en la máquina real (el comando no escribe nada a stdout: out llega
// nil, no un .txt vacío): journalctl/coredumpctl sin nada se guarda vacío
// solo cuando el motivo es "no hay" (el mismo criterio que maint.NoEntries,
// M4); con otro motivo real (p. ej. permiso denegado) no se guarda nada, así
// el replay lo ve unavailable, como en la máquina real.
func TestCaptureNoEntriesGatesStorage(t *testing.T) {
	key := source.Key("coredumpctl", maint.CoredumpArgs...)

	kept := source.FixtureCopy(t, "threadripper")
	kept.Run = &exitErrRunner{FakeRunner: kept.Run.(*source.FakeRunner), key: key, err: &exec.ExitError{Stderr: []byte("No coredumps found.\n")}}
	out := t.TempDir()
	if err := Capture(kept, out, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "cmd", key+".txt")); err != nil {
		t.Error("a 'no entries' failure must still be captured, empty")
	}

	dropped := source.FixtureCopy(t, "threadripper")
	dropped.Run = &exitErrRunner{FakeRunner: dropped.Run.(*source.FakeRunner), key: key, err: &exec.ExitError{Stderr: []byte("Permission denied\n")}}
	out2 := t.TempDir()
	if err := Capture(dropped, out2, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out2, "cmd", key+".txt")); err == nil {
		t.Error("a real failure must not be captured as an empty fixture")
	}
	if _, err := os.Stat(filepath.Join(out2, "cmd", key+".exit")); err == nil {
		t.Error("a real failure must not be captured as an empty fixture")
	}
}

// Un fixture capturado no trae symlinks (el Market los rechaza): los enlaces
// de driver quedan como archivos .link que FS.Link sabe leer.
func TestCaptureWritesNoSymlinks(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	out := t.TempDir()
	if err := Capture(src, out, false); err != nil {
		t.Fatal(err)
	}
	links := 0
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type()&os.ModeSymlink != 0 {
			t.Errorf("symlink in capture: %s", p)
		}
		if err == nil && strings.HasSuffix(p, "/driver.link") {
			links++
		}
		return nil
	})
	fs := source.FS{Root: filepath.Join(out, "root")}
	if l, err := fs.Link("/sys/bus/pci/devices/0000:42:00.0/driver"); err != nil || l != "drivers/nvidia" || links != 4 {
		t.Fatalf("driver link = %q, %v; %d .link files", l, err, links)
	}
}

// BlueZ: el adaptador, emparejado y confianza de cada dispositivo, y el
// rfkill; con anonymize, tampoco queda el Alias de un cercano.
func TestCaptureBluetooth(t *testing.T) {
	src := source.Fixture(t, "laptop")
	out := t.TempDir()
	if err := Capture(src, out, false); err != nil {
		t.Fatal(err)
	}
	bus, err := source.LoadFakeBus(filepath.Join(out, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := bluetooth.Read(src)
	got, err := bluetooth.Read(source.Env{FS: source.FS{Root: filepath.Join(out, "root")}, Bus: bus})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("captured %+v (%v), want %+v", got, err, want)
	}
	if soft, _ := os.ReadFile(filepath.Join(out, "root/sys/class/rfkill/rfkill0/soft")); string(soft) != "0\n" {
		t.Errorf("rfkill soft = %q", soft)
	}
	anon := t.TempDir()
	if err := Capture(src, anon, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(anon, "dbus.json"))
	for _, s := range []string{"Speaker", "WH-1000XM4", "Keyboard"} {
		if strings.Contains(string(raw), s) {
			t.Errorf("dbus.json leaks %q", s)
		}
	}
}

// Lo que lee Mantenimiento sobrevive a una captura: el engine sobre la
// captura da el mismo reinicio y la misma última actualización.
func TestCaptureMaintenance(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	out := t.TempDir()
	if err := Capture(src, out, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"root/var/log/pacman.log", "root/etc/pacman.conf", "root/sys/module/nvidia/version",
		"root/usr/lib/modules/7.2.5-3-omarchy/vmlinuz", "root/usr/lib/modules/7.2.3-arch1-3/vmlinuz",
		"root/etc/conf.d/snapper", "root/etc/snapper/configs/root",
		"root/usr/share/dbus-1/system-services/org.opensuse.Snapper.service",
		"root/var/cache/pacman/pkg/.keep", "root/home/user/.cache/yay/.keep", "root/var/lib/systemd/coredump/.keep",
		"root" + source.FixtureHyprLock, "root/proc/613755/exe.link", "dirsizes.json",
		"cmd/" + source.Key("journalctl", "--disk-usage") + ".txt"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	raw, err := os.ReadFile(filepath.Join(out, maint.SizesFile))
	if err != nil {
		t.Fatal(err)
	}
	var measured map[string]maint.DirSize
	if err := json.Unmarshal(raw, &measured); err != nil || len(measured) != 3 {
		t.Fatalf("dirsizes.json = %s (%v)", raw, err)
	}
	for _, p := range []string{"/var/cache/pacman/pkg", "/home/user/.cache/yay", "/var/lib/systemd/coredump"} {
		if _, ok := measured[p]; !ok {
			t.Errorf("dirsizes.json lacks %s", p)
		}
	}
	sizes, err := maint.LoadDirSizes(filepath.Join(out, maint.SizesFile))
	if err != nil {
		t.Fatal(err)
	}
	bus, err := source.LoadFakeBus(filepath.Join(out, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	env2 := source.Env{FS: source.FS{Root: filepath.Join(out, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(out, "cmd")},
		Bus: bus, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/user",
		State: t.TempDir(), Cache: t.TempDir(), User: source.FixtureUser, HyprLock: source.FixtureHyprLock}
	st := func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }
	want := engine.New(src, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "maintenance", DirSize: sizes}).Tick().Maintenance
	got := engine.New(env2, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "maintenance", DirSize: sizes}).Tick().Maintenance
	if !reflect.DeepEqual(got.Reboot, want.Reboot) || !reflect.DeepEqual(got.Upgrade, want.Upgrade) ||
		len(got.Caches) != len(want.Caches) || got.Snapshots == nil || got.Snapshots.Configs[0].Name != "root" {
		t.Fatalf("got %+v / %+v, want %+v / %+v", got.Reboot, got.Upgrade, want.Reboot, want.Upgrade)
	}
}

// Lo que lee Seguridad sobrevive a una captura: el engine sobre la captura
// da la misma Seguridad (con los programas, que salen de los fds).
func TestCaptureSecurity(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	out := t.TempDir()
	if err := Capture(src, out, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"root/proc/net/tcp", "root/proc/net/tcp6", "root/proc/net/udp", "root/proc/net/udp6",
		"root/proc/sys/net/ipv4/ip_local_port_range", "root/etc/ufw/ufw.conf", "root/etc/default/ufw", "root/etc/ufw/user.rules",
		"root/etc/ufw/user6.rules", "root/sys/block/dm-0/dm/uuid", "root/sys/block/dm-0/dm/name", "root/sys/block/dm-0/slaves/nvme1n1p2/.keep",
		"root/proc/2210/comm", "root/proc/2210/fd/23.link", "root/proc/2210/fd/24.link", "root/proc/3120/fd/40.link"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if pw, _ := os.ReadFile(filepath.Join(out, "root/etc/passwd")); string(pw) != "root:x:0:\navahi:x:967:\nsystemd-resolve:x:974:\nuser:x:1000:\n" {
		t.Errorf("passwd = %q", pw)
	}
	if raw, _ := os.ReadFile(filepath.Join(out, "dbus.json")); !strings.Contains(string(raw), "ufw.service firewalld.service nftables.service iptables.service ip6tables.service]") {
		t.Error("the units call must be captured with the firewall units")
	}
	bus, err := source.LoadFakeBus(filepath.Join(out, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	env2 := source.Env{FS: source.FS{Root: filepath.Join(out, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(out, "cmd")},
		Bus: bus, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/user",
		State: t.TempDir(), Cache: t.TempDir(), User: source.FixtureUser}
	st := func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }
	want := engine.New(src, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "security"}).Tick().Security
	got := engine.New(env2, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "security"}).Tick().Security
	if !reflect.DeepEqual(got, want) || len(got.Listening) != 14 || got.Listening[0].Owners[0].Program != "prog2" {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// pacman.log de una instalación vieja puede pesar más de 2 MiB (el tope
// general de la captura): Mantenimiento lo necesita igual, así que no se
// saltea sin más, ni anonimizado (la reducción es chica) ni sin anonimizar
// (se guarda la cola, alineada a una línea).
func TestCaptureLargePacmanLog(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Dir(src.FS.Root))); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	old := "[2019-03-01T10:00:00-0300] [PACMAN] starting full system upgrade\n" +
		"[2019-03-01T10:00:01-0300] [ALPM] upgraded old (1 -> 2)\n[2019-03-01T10:00:02-0300] [ALPM] transaction completed\n"
	b.WriteString(old)
	for b.Len() < 3<<20 { // más de 2 MiB
		b.WriteString("[2019-03-02T10:00:00-0300] [ALPM] installed filler-package-with-a-name (1.0.0-1)\n")
	}
	recent := "[2026-09-20T10:00:00-0300] [PACMAN] starting full system upgrade\n" +
		"[2026-09-20T10:00:01-0300] [ALPM] upgraded recent (1 -> 2)\n[2026-09-20T10:00:02-0300] [ALPM] transaction completed\n"
	b.WriteString(recent)
	if err := os.WriteFile(filepath.Join(dir, "root/var/log/pacman.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	fb, err := source.LoadFakeBus(filepath.Join(dir, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := source.Env{FS: source.FS{Root: filepath.Join(dir, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(dir, "cmd")},
		Bus: fb, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/user"}

	// Sin anonimizar: se guarda la cola (con la actualización reciente al
	// final), nunca el archivo entero, y alineada a una línea completa.
	out := t.TempDir()
	if err := Capture(env, out, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "root/var/log/pacman.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || len(got) >= b.Len() {
		t.Fatalf("pacman.log capturado sin recorte: %d bytes de %d", len(got), b.Len())
	}
	if !strings.HasSuffix(string(got), recent) {
		t.Errorf("pacman.log no tiene la actualización reciente: ...%s", got[len(got)-min(len(got), 200):])
	}
	if got[0] != '[' {
		t.Errorf("pacman.log no arranca alineado a una línea: %q…", got[:min(len(got), 40)])
	}

	// Anonimizado: pacmanLog lo reduce mucho, así que se lee entero (no se
	// pierde la actualización vieja, fuera de la cola de 2 MiB).
	anonOut := t.TempDir()
	if err := Capture(env, anonOut, true); err != nil {
		t.Fatal(err)
	}
	anonGot, err := os.ReadFile(filepath.Join(anonOut, "root/var/log/pacman.log"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(anonGot), "starting full system upgrade"); n != 2 {
		t.Errorf("pacman.log anonimizado = %d actualizaciones completas, want 2: %s", n, anonGot)
	}
}

// El exe de Hyprland puede vivir bajo el home (compilado a mano, o un
// AppImage en ~/.local/bin): su destino no debe llevar el home ni el
// nombre real del dueño de la máquina.
func TestCaptureAnonymizeHyprlandExe(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Dir(src.FS.Root))); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "root", source.FixtureHyprLock)
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("9999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exeLink := filepath.Join(dir, "root/proc/9999/exe.link")
	if err := os.MkdirAll(filepath.Dir(exeLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exeLink, []byte("/home/joe/.local/bin/Hyprland (deleted)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "joe")
	fb, err := source.LoadFakeBus(filepath.Join(dir, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := source.Env{FS: source.FS{Root: filepath.Join(dir, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(dir, "cmd")},
		Bus: fb, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/joe", HyprLock: source.FixtureHyprLock}
	out := t.TempDir()
	if err := Capture(env, out, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "root/proc/9999/exe.link"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/user/.local/bin/Hyprland (deleted)\n"; string(got) != want {
		t.Errorf("exe.link = %q, want %q", got, want)
	}
}

// /etc/pacman.conf puede tener un Server con credenciales (un mirror
// privado) en cualquier sección: --anonymize solo guarda lo que lee
// Mantenimiento (el CacheDir de [options]; ver maint.PacmanCacheDir), nunca
// el resto del archivo.
func TestCaptureAnonymizePacmanConf(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Dir(src.FS.Root))); err != nil {
		t.Fatal(err)
	}
	conf := "[options]\nHoldPkg = pacman glibc\nCacheDir = /data/pkg/\nArchitecture = auto\n\n" +
		"[core]\nInclude = /etc/pacman.d/mirrorlist\n\n" +
		"[private]\nServer = https://user:s3cr3t-token@mirror.example.com/$repo/os/$arch\n"
	if err := os.WriteFile(filepath.Join(dir, "root/etc/pacman.conf"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	fb, err := source.LoadFakeBus(filepath.Join(dir, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := source.Env{FS: source.FS{Root: filepath.Join(dir, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(dir, "cmd")},
		Bus: fb, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/user"}
	out := t.TempDir()
	if err := Capture(env, out, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "root/etc/pacman.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "s3cr3t-token") || strings.Contains(string(got), "Server") ||
		strings.Contains(string(got), "[private]") || strings.Contains(string(got), "[core]") {
		t.Errorf("pacman.conf keeps more than Mantenimiento needs: %s", got)
	}
	if want := "[options]\nCacheDir = /data/pkg/\n"; string(got) != want {
		t.Errorf("pacman.conf = %q, want %q", got, want)
	}
	if cd := maint.PacmanCacheDir(source.Env{FS: source.FS{Root: filepath.Join(out, "root")}}); cd != "/data/pkg" {
		t.Errorf("reduced pacman.conf broke PacmanCacheDir: %q", cd)
	}
}

// ALLOW_USERS y ALLOW_GROUPS de un config de snapper nombran a otros
// usuarios reales de la máquina: no sobreviven a --anonymize. Quien corre
// la captura sigue dando permiso sobre el fixture, como en la máquina real
// (ahí también es a quien lee env.User).
func TestCaptureAnonymizeSnapperAllow(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Dir(src.FS.Root))); err != nil {
		t.Fatal(err)
	}
	config := "SUBVOLUME=\"/\"\nFSTYPE=\"btrfs\"\nALLOW_USERS=\"joe alice bob\"\nALLOW_GROUPS=\"wheel staff\"\n" +
		"SYNC_ACL=\"no\"\nNUMBER_CLEANUP=\"yes\"\nNUMBER_LIMIT=\"5\"\n"
	if err := os.WriteFile(filepath.Join(dir, "root/etc/snapper/configs/root"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "joe")

	envAt := func(d string, user source.User) source.Env {
		bus, err := source.LoadFakeBus(filepath.Join(d, "dbus.json"))
		if err != nil {
			t.Fatal(err)
		}
		return source.Env{FS: source.FS{Root: filepath.Join(d, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(d, "cmd")},
			Bus: bus, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/user",
			State: t.TempDir(), Cache: t.TempDir(), User: user, HyprLock: source.FixtureHyprLock}
	}
	st := func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }
	opts := func() engine.Options {
		return engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "maintenance"}
	}

	// En la máquina real, quien corre el colector es "joe" (Env.User =
	// CurrentUser()), listado en ALLOW_USERS: permitido.
	before := engine.New(envAt(dir, source.User{UID: 1000, Name: "joe", GIDs: []uint32{1000}}), opts()).Tick().Maintenance
	if before.Snapshots == nil || !before.Snapshots.Configs[0].Allowed {
		t.Fatalf("setup: root config not allowed for joe: %+v", before.Snapshots)
	}

	out := t.TempDir()
	if err := Capture(envAt(dir, source.FixtureUser), out, true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "root/etc/snapper/configs/root"))
	if err != nil {
		t.Fatal(err)
	}
	if m := regexp.MustCompile(`(?i)\balice\b|\bbob\b|\bstaff\b`).FindString(string(raw)); m != "" {
		t.Errorf("snapper config leaks %q: %s", m, raw)
	}

	after := engine.New(envAt(out, source.FixtureUser), opts()).Tick().Maintenance
	if after.Snapshots == nil || after.Snapshots.Configs[0].Name != "root" ||
		after.Snapshots.Configs[0].Allowed != before.Snapshots.Configs[0].Allowed {
		t.Fatalf("round trip broke Allowed: got %+v, want same as %+v", after.Snapshots, before.Snapshots)
	}
}

// oomdUnit conserva solo los .slice genéricos del principio (-.slice,
// user.slice, system.slice, machine.slice, user-<n>.slice); desde el primer
// tramo que no lo es, todo es un solo progN. Un slice de plantilla
// (system-secretd.slice) no es genérico: nombra al servicio. Si el cgroup
// matado es él mismo genérico, la ruta queda igual, sin "/" al final.
func TestOomdUnitGenericSlices(t *testing.T) {
	c := &capture{anon: NewAnonymizer("joe", "/home/joe", "host")}
	cases := []struct{ in, re string }{
		{"/system.slice/system-secretd.slice/secretd@x.service", `^/system\.slice/prog[0-9]+$`},
		{"/user.slice/user-1000.slice/user@1000.service/app.slice/x.scope", `^/user\.slice/user-1000\.slice/prog[0-9]+$`},
		{"/machine.slice/secret-vm.scope", `^/machine\.slice/prog[0-9]+$`},
		{"/-.slice/secret.service", `^/-\.slice/prog[0-9]+$`},
		{"/user.slice/user-abc.slice/secret.scope", `^/user\.slice/prog[0-9]+$`},
		{"/user.slice/user-1000.slice", `^/user\.slice/user-1000\.slice$`},
		{"/user.slice", `^/user\.slice$`},
	}
	for _, tc := range cases {
		got := c.oomdUnit(tc.in)
		if !regexp.MustCompile(tc.re).MatchString(got) {
			t.Errorf("oomdUnit(%q) = %q, want %s", tc.in, got, tc.re)
		}
		if strings.Contains(got, "secret") {
			t.Errorf("oomdUnit(%q) = %q leaks", tc.in, got)
		}
	}
}

// Un exe vacío queda vacío (el replay muestra "Unknown", como sin anonimizar),
// no "/progN".
func TestExeProgramEmpty(t *testing.T) {
	c := &capture{anon: NewAnonymizer("joe", "/home/joe", "host")}
	if got := c.exeProgram(""); got != "" {
		t.Errorf("exeProgram(\"\") = %q, want \"\"", got)
	}
	if got := c.exeProgram("/usr/lib/secret/app"); !regexp.MustCompile(`^/prog[0-9]+$`).MatchString(got) {
		t.Errorf("exeProgram = %q", got)
	}
}

// dmUUID mantiene el prefijo de un dm cifrado o de LVM (para que el fixture
// siga distinguiendo uno del otro) pero cambia el id real por un contador
// estable; ambos casos llegan con el salto de línea final que trae sysfs.
func TestDmUUID(t *testing.T) {
	c := &capture{anon: NewAnonymizer("joe", "/home/joe", "host")}
	if got := c.dmUUID([]byte("CRYPT-LUKS2-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-root\n")); string(got) != "CRYPT-LUKS2-00000000000000000000000000000001-root\n" {
		t.Errorf("dmUUID(crypt) = %q", got)
	}
	got := c.dmUUID([]byte("LVM-nUvnDGDNVIQ8Q4L1blE6bBGSJz2gLLdV8G3s3wLB4rE5oT8UY9j6mLYSbeoAsdKl\n"))
	if string(got) != "LVM-00000000000000000000000000000002\n" {
		t.Errorf("dmUUID(lvm) = %q, want the LVM- prefix kept and the id replaced", got)
	}
	if strings.Contains(string(got), "nUvnDGDNVIQ8") {
		t.Error("dmUUID(lvm) leaks the real LVM id")
	}
}

// LVM escapa cada guion literal de un nombre doblándolo: un hostname con
// guion (host-name) queda "host--name" adentro de un dm/name de LVM
// (host--name-root), no en su forma lisa. Text debe reconocer las dos
// formas, y darle el mismo reemplazo tanto en dm/name como en cualquier
// otro archivo que mencione el mismo /dev/mapper/<nombre> (aquí, lsblk).
func TestCaptureAnonymizeLVMDoubleDashHostname(t *testing.T) {
	src := source.FixtureCopy(t, "threadripper")
	dir := filepath.Dir(src.FS.Root)
	if err := os.WriteFile(filepath.Join(dir, "root/etc/hostname"), []byte("host-name\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dmDir := filepath.Join(dir, "root/sys/block/dm-9/dm")
	if err := os.MkdirAll(dmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmDir, "uuid"), []byte("LVM-abcdefabcdefabcdefabcdefabcdefab\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmDir, "name"), []byte("host--name-root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lsblkKey := source.Key("lsblk", storage.LsblkArgs...)
	if err := os.WriteFile(filepath.Join(dir, "cmd", lsblkKey+".txt"),
		[]byte(`{"blockdevices":[{"name":"dm-9","path":"/dev/mapper/host--name-root","type":"lvm","mountpoints":["/"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "joe")
	out := t.TempDir()
	if err := Capture(src, out, true); err != nil {
		t.Fatal(err)
	}
	leaks := regexp.MustCompile(`host-name|host--name`)
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if m := leaks.FindString(p); m != "" {
			t.Errorf("path leaks %q: %s", m, p)
		}
		if !d.IsDir() {
			data, _ := os.ReadFile(p)
			if m := leaks.FindString(string(data)); m != "" {
				t.Errorf("%s leaks %q", p, m)
			}
		}
		return nil
	})
	nameOut, err := os.ReadFile(filepath.Join(out, "root/sys/block/dm-9/dm/name"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^host-(\S+)\n$`).FindStringSubmatch(string(nameOut))
	if m == nil {
		t.Fatalf("dm-9 name = %q, want host-<rest> (the doubled-dash hostname replaced by \"host\", the -root suffix kept)", nameOut)
	}
	lsblkOut, err := os.ReadFile(filepath.Join(out, "cmd", lsblkKey+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lsblkOut), "/dev/mapper/host-"+m[1]) {
		t.Errorf("lsblk output doesn't carry the same replacement as dm/name (host-%s): %s", m[1], lsblkOut)
	}
}

// ufwRules conserva el alcance de una dirección como Anonymizer.NetHex:
// multicast queda igual, y un origen link-local (v4 o v6) se remapea dentro
// de su mismo rango, no a un 192.0.2.N/2001:db8::N cualquiera.
func TestUfwRulesScope(t *testing.T) {
	c := &capture{anon: NewAnonymizer("joe", "/home/joe", "host")}
	in := "### tuple ### allow udp 5353 224.0.0.251 any fe80::abcd in\n" +
		"### tuple ### allow udp 1900 0.0.0.0/0 any 169.254.7.7 in\n"
	got := string(c.ufwRules([]byte(in)))
	if !strings.Contains(got, "224.0.0.251") {
		t.Errorf("ufwRules must keep a multicast destination unchanged: %q", got)
	}
	if strings.Contains(got, "fe80::abcd") || !strings.Contains(got, "fe80::1") {
		t.Errorf("ufwRules must remap a link-local v6 source within fe80::/10, not drop it or map it elsewhere: %q", got)
	}
	if strings.Contains(got, "169.254.7.7") || !strings.Contains(got, "169.254.0.1") {
		t.Errorf("ufwRules must remap a link-local v4 source within 169.254/16, not drop it or map it elsewhere: %q", got)
	}
}

// Una interfaz nombrada con su MAC (esquema de udev en/wl/wwx<mac>) es tan
// identificatoria como la MAC misma, y una WireGuard nombrada con el
// hostname (wg-<hostname>) lleva el nombre real de la máquina: con
// --anonymize ninguna de las dos debe sobrevivir, y el mismo nombre real
// debe dar el mismo pseudónimo en sysfs, en /proc/net/dev y en una regla de
// ufw (el campo "in_<iface>").
func TestCaptureAnonymizeInterfaceNames(t *testing.T) {
	src := source.FixtureCopy(t, "threadripper")
	dir := filepath.Dir(src.FS.Root)
	if err := os.WriteFile(filepath.Join(dir, "root/etc/hostname"), []byte("quokka\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	netDir := filepath.Join(dir, "root/sys/class/net/enx00e04c680001")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, v := range map[string]string{"type": "1\n", "operstate": "up\n", "flags": "4099\n", "address": "00:e0:4c:68:00:01\n", "speed": "1000\n"} {
		if err := os.WriteFile(filepath.Join(netDir, f), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dev := "Inter-|   Receive                                                |  Transmit\n" +
		" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed\n" +
		"enx00e04c680001:  100    1    0    0    0     0          0         0      200    2    0    0    0     0       0          0\n" +
		"   wg-quokka:  300    3    0    0    0     0          0         0      400    4    0    0    0     0       0          0\n"
	if err := os.WriteFile(filepath.Join(dir, "root/proc/net/dev"), []byte(dev), 0o644); err != nil {
		t.Fatal(err)
	}
	rules := "*filter\n:ufw-user-input - [0:0]\n### RULES ###\n\n" +
		"### tuple ### allow udp 53317 0.0.0.0/0 any 0.0.0.0/0 in_enx00e04c680001\n" +
		"-A ufw-user-input -i enx00e04c680001 -p udp --dport 53317 -j ACCEPT\n\n" +
		"### tuple ### allow tcp 51820 0.0.0.0/0 any 0.0.0.0/0 in_wg-quokka\n" +
		"-A ufw-user-input -i wg-quokka -p tcp --dport 51820 -j ACCEPT\n### END RULES ###\nCOMMIT\n"
	if err := os.WriteFile(filepath.Join(dir, "root/etc/ufw/user.rules"), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "joe")
	out := t.TempDir()
	if err := Capture(src, out, true); err != nil {
		t.Fatal(err)
	}
	leaks := regexp.MustCompile(`(?i)00e04c680001|00:e0:4c:68:00:01|\bquokka\b`)
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if m := leaks.FindString(p); m != "" {
			t.Errorf("path leaks %q: %s", m, p)
		}
		if !d.IsDir() {
			data, _ := os.ReadFile(p)
			if m := leaks.FindString(string(data)); m != "" {
				t.Errorf("%s leaks %q", p, m)
			}
		}
		return nil
	})
	sysfsDirs, err := os.ReadDir(filepath.Join(out, "root/sys/class/net"))
	if err != nil {
		t.Fatal(err)
	}
	var fakeIface string
	for _, e := range sysfsDirs {
		if regexp.MustCompile(`^enx02[0-9a-f]{10}$`).MatchString(e.Name()) {
			fakeIface = e.Name()
		}
	}
	if fakeIface == "" {
		t.Fatalf("no anonymized enx* interface directory under root/sys/class/net: %v", sysfsDirs)
	}
	devOut, err := os.ReadFile(filepath.Join(out, "root/proc/net/dev"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(devOut), fakeIface+":") {
		t.Errorf("proc/net/dev doesn't carry the same anonymized interface name (%s): %s", fakeIface, devOut)
	}
	rulesOut, err := os.ReadFile(filepath.Join(out, "root/etc/ufw/user.rules"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rulesOut), "in_"+fakeIface) {
		t.Errorf("user.rules doesn't carry the same anonymized interface name (%s): %s", fakeIface, rulesOut)
	}
	if !strings.Contains(string(rulesOut), "in_wg-host") {
		t.Errorf("user.rules doesn't anonymize wg-quokka's hostname: %s", rulesOut)
	}
}

// El WWID de un dm que no es CRYPT- ni LVM- (multipath, una partición de un
// LUKS con particiones, stratis) es tan identificatorio como el uuid de un
// CRYPT-/LVM-: con --anonymize no debe sobrevivir, ni en el propio archivo,
// ni en dm/name, ni en cualquier otro archivo que lo mencione (aquí, lsblk:
// una montura que lo usa) — y debe ser el mismo id en los tres.
func TestCaptureAnonymizeOtherDMIDs(t *testing.T) {
	src := source.FixtureCopy(t, "threadripper")
	dir := filepath.Dir(src.FS.Root)
	wwid := "3600508b400105e210000900000490000"
	name := "mpath-" + wwid
	dmDir := filepath.Join(dir, "root/sys/block/dm-9/dm")
	if err := os.MkdirAll(dmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmDir, "uuid"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmDir, "name"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lsblkKey := source.Key("lsblk", storage.LsblkArgs...)
	if err := os.WriteFile(filepath.Join(dir, "cmd", lsblkKey+".txt"),
		[]byte(`{"blockdevices":[{"name":"dm-9","path":"/dev/mapper/`+name+`","type":"mpath","mountpoints":["/data"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "joe")
	out := t.TempDir()
	if err := Capture(src, out, true); err != nil {
		t.Fatal(err)
	}
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if strings.Contains(p, wwid) {
			t.Errorf("path leaks the WWID: %s", p)
		}
		if !d.IsDir() {
			data, _ := os.ReadFile(p)
			if strings.Contains(string(data), wwid) {
				t.Errorf("%s leaks the WWID", p)
			}
		}
		return nil
	})
	uuidOut, err := os.ReadFile(filepath.Join(out, "root/sys/block/dm-9/dm/uuid"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^mpath-(\S+)\n$`).FindStringSubmatch(string(uuidOut))
	if m == nil {
		t.Fatalf("dm-9 uuid = %q, want mpath-<id> (prefix kept, id replaced)", uuidOut)
	}
	id := m[1]
	nameOut, err := os.ReadFile(filepath.Join(out, "root/sys/block/dm-9/dm/name"))
	if err != nil {
		t.Fatal(err)
	}
	if string(nameOut) != "mpath-"+id+"\n" {
		t.Errorf("dm-9 name = %q, want the same id as uuid (mpath-%s)", nameOut, id)
	}
	lsblkOut, err := os.ReadFile(filepath.Join(out, "cmd", lsblkKey+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lsblkOut), id) {
		t.Errorf("lsblk output doesn't carry the same id as dm/uuid (%s): %s", id, lsblkOut)
	}
}

// Quien corre la captura se identifica por uid (Env.User.UID), no por si su
// nombre coincide con $USER: aunque diverjan, el propio uid (una cuenta
// humana, >= 1000) sigue quedando "user" en el passwd reducido y no, por
// error, userN como si fuera otro humano de la máquina. Una captura como
// root (uid 0) no convierte a root en "user": root es cuenta de sistema.
func TestCaptureSecurityOwnUserByUID(t *testing.T) {
	src := source.FixtureCopy(t, "threadripper")
	dir := filepath.Dir(src.FS.Root)
	if err := os.WriteFile(filepath.Join(dir, "root/etc/passwd"),
		[]byte("root:x:0:0::/root:/bin/bash\nmorgan:x:1000:1000::/home/morgan:/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "someoneelse") // deliberadamente distinto del nombre real del dueño
	fb, err := source.LoadFakeBus(filepath.Join(dir, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := source.Env{FS: source.FS{Root: filepath.Join(dir, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(dir, "cmd")},
		Bus: fb, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/morgan",
		User: source.User{UID: 1000, Name: "morgan"}}
	out := t.TempDir()
	if err := Capture(env, out, true); err != nil {
		t.Fatal(err)
	}
	pw, err := os.ReadFile(filepath.Join(out, "root/etc/passwd"))
	if err != nil {
		t.Fatal(err)
	}
	if string(pw) != "root:x:0:\nuser:x:1000:\n" {
		t.Errorf("passwd = %q, want the capturing user (by uid) as \"user\" even though its name doesn't match $USER", pw)
	}

	env.User = source.User{UID: 0, Name: "root"}
	out = t.TempDir()
	if err := Capture(env, out, true); err != nil {
		t.Fatal(err)
	}
	if pw, _ := os.ReadFile(filepath.Join(out, "root/etc/passwd")); string(pw) != "root:x:0:\nuser1:x:1000:\n" {
		t.Errorf("root capture: passwd = %q, want root kept and morgan as another user", pw)
	}
}

// La captura de /proc/net/{tcp,udp}[6] guarda solo lo que Seguridad lee:
// TCP en LISTEN (0A) y UDP sin conectar (07), más la cabecera; las
// conexiones establecidas (con quién habla la máquina) no, con o sin
// --anonymize.
func TestCaptureProcNetOnlyListeners(t *testing.T) {
	src := source.FixtureCopy(t, "threadripper")
	for _, anon := range []bool{false, true} {
		out := t.TempDir()
		if err := Capture(src, out, anon); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"tcp", "tcp6", "udp", "udp6"} {
			data, err := os.ReadFile(filepath.Join(out, "root/proc/net", f))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			if !strings.Contains(lines[0], "local_address") {
				t.Errorf("%s (anon %v): no header: %q", f, anon, lines[0])
			}
			want := "0A"
			if strings.HasPrefix(f, "udp") {
				want = "07"
			}
			for _, l := range lines[1:] {
				if fs := strings.Fields(l); len(fs) < 4 || fs[3] != want {
					t.Errorf("%s (anon %v): kept %q", f, anon, l)
				}
			}
		}
	}
}

// El progN de cada comm no depende del orden (no determinístico) en que Go
// entrega el mapa de inodos de ScanFDs: security() los recorre en orden de
// inodo, así que capturas repetidas de la misma máquina dan exactamente los
// mismos archivos.
func TestCaptureSecurityDeterministicProgN(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	t.Setenv("USER", "joe")
	var first map[string]string
	for i := 0; i < 20; i++ {
		out := t.TempDir()
		if err := Capture(src, out, true); err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, p := range []string{"root/proc/2210/comm", "root/proc/3120/comm"} {
			data, err := os.ReadFile(filepath.Join(out, p))
			if err != nil {
				t.Fatal(err)
			}
			got[p] = string(data)
		}
		if first == nil {
			first = got
			continue
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d: comm assignments changed: got %v, want %v", i, got, first)
		}
	}
}

// La captura anonimizada da la misma forma de Seguridad que la simple: la
// misma cantidad de listeners, el mismo veredicto de firewall y la misma
// cantidad de dueños en cada uno (como conjunto, no por posición: el orden
// de un listener "addr"/"link" puede depender de su dirección, que cambia
// al anonimizar) y el mismo cifrado de volúmenes y swaps.
func TestCaptureSecurityAnonymized(t *testing.T) {
	src := source.Fixture(t, "threadripper")
	plain := t.TempDir()
	if err := Capture(src, plain, false); err != nil {
		t.Fatal(err)
	}
	anon := t.TempDir()
	t.Setenv("USER", "joe")
	if err := Capture(src, anon, true); err != nil {
		t.Fatal(err)
	}
	read := func(dir string) *model.Security {
		bus, err := source.LoadFakeBus(filepath.Join(dir, "dbus.json"))
		if err != nil {
			t.Fatal(err)
		}
		env := source.Env{FS: source.FS{Root: filepath.Join(dir, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(dir, "cmd")},
			Bus: bus, Now: func() time.Time { return source.FixtureNow }, Lang: "en", Home: "/home/user",
			State: t.TempDir(), Cache: t.TempDir(), User: source.FixtureUser}
		st := func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }
		return engine.New(env, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "security"}).Tick().Security
	}
	want, got := read(plain), read(anon)
	sig := func(ls []model.Listener) []string {
		out := make([]string, len(ls))
		for i, l := range ls {
			out[i] = strings.Join([]string{l.Proto, l.Scope, l.Firewall, l.From}, "|") + "|" + strings.Repeat("o", len(l.Owners))
		}
		sort.Strings(out)
		return out
	}
	if len(got.Listening) != len(want.Listening) {
		t.Fatalf("listener count differs: got %d, want %d", len(got.Listening), len(want.Listening))
	}
	if gs, ws := sig(got.Listening), sig(want.Listening); !reflect.DeepEqual(gs, ws) {
		t.Errorf("listener signatures differ:\ngot  %v\nwant %v", gs, ws)
	}
	if got.Firewall == nil || want.Firewall == nil || got.Firewall.State != want.Firewall.State || got.Firewall.Active != want.Firewall.Active {
		t.Errorf("firewall state differs: got %+v, want %+v", got.Firewall, want.Firewall)
	}
	if len(got.Volumes) != len(want.Volumes) || len(got.Swaps) != len(want.Swaps) {
		t.Fatalf("volumes/swaps count differs: got %d/%d, want %d/%d", len(got.Volumes), len(got.Swaps), len(want.Volumes), len(want.Swaps))
	}
	for i := range want.Volumes {
		if got.Volumes[i].Encrypted != want.Volumes[i].Encrypted || got.Volumes[i].Type != want.Volumes[i].Type {
			t.Errorf("volume %d differs: got %+v, want %+v", i, got.Volumes[i], want.Volumes[i])
		}
	}
	for i := range want.Swaps {
		if got.Swaps[i].State != want.Swaps[i].State || got.Swaps[i].Type != want.Swaps[i].Type {
			t.Errorf("swap %d differs: got %+v, want %+v", i, got.Swaps[i], want.Swaps[i])
		}
	}
}

// Lo que leen Energía y Dispositivos sobrevive a una captura anonimizada, y
// no se filtran el serial ni la descripción del monitor, los nombres de los
// workspaces, los nombres de los programas ni los motivos y títulos.
func TestCaptureEnergyScreens(t *testing.T) {
	t.Setenv("USER", "joe")
	src := source.FixtureCopy(t, "ultrawide")
	hypr := t.TempDir()
	mon := `[{"id":0,"name":"HDMI-A-1","description":"Samsung Electric Company S34J55x FAKE000001","serial":"FAKE000001",` +
		`"width":3440,"height":1440,"refreshRate":49.987,"scale":1,"vrr":false,"disabled":false,"currentFormat":"XRGB8888",` +
		`"x":0,"y":0,"activeWorkspace":{"id":3,"name":"tax-returns"},"specialWorkspace":{"id":0,"name":""},` +
		`"mirrorOf":"none","availableModes":["3440x1440@49.99Hz","1280x1024@75.03Hz"],"colorManagementPreset":"srgb"}]`
	cls := `[{"address":"0x1","class":"SecretPlayer","title":"Playing secret-movie.mkv","mapped":true,"inhibitingIdle":true,"workspace":{"id":3,"name":"tax-returns"}},` +
		`{"address":"0x2","class":"Other","title":"nope","mapped":true,"inhibitingIdle":false}]`
	for f, v := range map[string]string{"j-monitors.txt": mon, "j-clients.txt": cls} {
		if err := os.WriteFile(filepath.Join(hypr, f), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src.Hypr = &source.FakeHypr{Dir: hypr}
	stay := filepath.Join(src.FS.Root, "home/user/.local/state/omarchy/indicators/stay-awake")
	if err := os.MkdirAll(filepath.Dir(stay), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stay, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	bus := src.Bus.(*source.FakeBus)
	bus.System["call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager.ListInhibitors"] =
		json.RawMessage(`[[["sleep","SecretApp","Lock screen before suspend","delay",1000,1456],["idle","mpv","Playing secret-movie.mkv","block",1000,77],` +
			`["shutdown","kernel","Lock screen before suspend","delay",1000,1]]]`)

	out := t.TempDir()
	if err := Capture(src, out, true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "hypr", source.Key("j/monitors")+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"FAKE000001", "S34J55x", "Samsung", "description", "serial", "tax-returns"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("j-monitors filtra %q: %s", leak, raw)
		}
	}
	var ms []map[string]any
	if err := json.Unmarshal(raw, &ms); err != nil || len(ms) != 1 {
		t.Fatalf("j-monitors = %s (%v)", raw, err)
	}
	keys := []string{}
	for k := range ms[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	wantKeys := []string{"activeWorkspace", "availableModes", "colorManagementPreset", "currentFormat", "disabled", "height", "id", "mirrorOf",
		"name", "refreshRate", "scale", "specialWorkspace", "vrr", "width", "x", "y"}
	if !slices.Equal(keys, wantKeys) || !reflect.DeepEqual(ms[0]["activeWorkspace"], map[string]any{"id": 3.0}) {
		t.Errorf("j-monitors keys = %v, workspace %v", keys, ms[0]["activeWorkspace"])
	}
	rawC, err := os.ReadFile(filepath.Join(out, "hypr", source.Key("j/clients")+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"SecretPlayer", "secret-movie", "tax-returns", "Other", "nope"} {
		if strings.Contains(string(rawC), leak) {
			t.Errorf("j-clients filtra %q: %s", leak, rawC)
		}
	}
	if !strings.Contains(string(rawC), `"title 1"`) || !regexp.MustCompile(`"class":\s*"prog\d+"`).Match(rawC) {
		t.Errorf("j-clients = %s", rawC)
	}
	dbus, _ := os.ReadFile(filepath.Join(out, "dbus.json"))
	for _, leak := range []string{"SecretApp", "mpv", "secret-movie", "Lock screen"} {
		if strings.Contains(string(dbus), leak) {
			t.Errorf("dbus.json filtra %q", leak)
		}
	}
	var db struct {
		System map[string][][][]any `json:"system"`
	}
	json.Unmarshal(dbus, &db)
	list := db.System["call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager.ListInhibitors"]
	if len(list) != 1 || len(list[0]) != 3 {
		t.Fatalf("ListInhibitors = %s", dbus)
	}
	var whys, whos []string
	for _, f := range list[0] {
		whos, whys = append(whos, f[1].(string)), append(whys, f[2].(string))
	}
	if !slices.Equal(whys, []string{"reason 1", "reason 2", "reason 1"}) || whos[2] != "kernel" ||
		!regexp.MustCompile(`^prog\d+$`).MatchString(whos[0]) || !regexp.MustCompile(`^prog\d+$`).MatchString(whos[1]) || whos[0] == whos[1] {
		t.Errorf("who = %v, why = %v", whos, whys)
	}
	for _, p := range []string{"root/sys/power/mem_sleep", "root/sys/bus/usb/devices/1-5/version", "root/sys/bus/usb/devices/1-5/bDeviceClass",
		"root/sys/bus/usb/devices/1-5/power/wakeup", "root/sys/bus/usb/devices/1-5/1-5:1.0/bInterfaceClass",
		"root/sys/bus/usb/devices/1-5/port/peer.link", "root/sys/bus/pci/devices/0000:05:00.0/power/wakeup",
		"root/home/user/.local/state/omarchy/indicators/stay-awake"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	// El engine sobre la captura ve lo mismo (salvo los nombres de pci.ids,
	// que la captura no guarda).
	fb, err := source.LoadFakeBus(filepath.Join(out, "dbus.json"))
	if err != nil {
		t.Fatal(err)
	}
	env2 := source.Env{FS: source.FS{Root: filepath.Join(out, "root")}, Run: &source.FakeRunner{Dir: filepath.Join(out, "cmd")},
		Bus: fb, Hypr: &source.FakeHypr{Dir: filepath.Join(out, "hypr")}, Now: func() time.Time { return source.FixtureNow },
		Lang: "en", Home: "/home/user", State: t.TempDir(), Cache: t.TempDir(), User: source.FixtureUser, HyprLock: source.FixtureHyprLock}
	st := func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }
	want := engine.New(src, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "power"}).Tick()
	got := engine.New(env2, engine.Options{Interval: 2 * time.Second, Statfs: st, Focus: "power"}).Tick()
	paths := func(ws []model.WakeDevice) []string {
		var out []string
		for _, w := range ws {
			out = append(out, w.Kind+":"+w.Path)
		}
		return out
	}
	if got.Power.SleepMode != want.Power.SleepMode || !slices.Equal(paths(got.Power.Wakeup), paths(want.Power.Wakeup)) ||
		got.Power.Inhibit == nil || !got.Power.Inhibit.StayAwake || len(got.Power.Inhibit.Logind) != 3 ||
		len(got.Power.Inhibit.Windows) != 1 {
		t.Fatalf("power: got %+v, want %+v", got.Power, want.Power)
	}
	for i, w := range want.Power.Inhibit.Logind {
		g := got.Power.Inhibit.Logind[i]
		if !slices.Equal(g.What, w.What) || g.Mode != w.Mode || g.UID != w.UID || g.PID != w.PID {
			t.Errorf("inhibitor %d: got %+v, want %+v", i, g, w)
		}
	}
	if !reflect.DeepEqual(got.System.USB, want.System.USB) || got.System.Monitors[0].MaxVHz != 75 ||
		!reflect.DeepEqual(got.System.Monitors[0].Live, want.System.Monitors[0].Live) {
		t.Fatalf("devices: got %+v / %+v, want %+v / %+v", got.System.USB, got.System.Monitors, want.System.USB, want.System.Monitors)
	}
}

// El EDID anonimizado no lleva el texto 0xFE ni los bloques de extensión
// (pueden traer un serial); conserva nombre y rango y el checksum cierra.
func TestEDIDAnonymizeDropsUnspecifiedAndExtensions(t *testing.T) {
	e := make([]byte, 256)
	copy(e, []byte{0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0})
	e[126] = 1
	put := func(off int, tag byte, txt string) {
		e[off+3] = tag
		copy(e[off+5:off+18], []byte(txt+"\n"))
	}
	put(54, 0xfc, "PANEL")
	put(72, 0xfe, "PANELSER123")
	e[90+3], e[90+5], e[90+6] = 0xfd, 48, 75
	copy(e[128+20:], "EXTSERIAL999")
	sum := byte(0)
	for _, b := range e[:127] {
		sum += b
	}
	e[127] = -sum
	out := EDID(e)
	if len(out) != 128 || out[126] != 0 {
		t.Fatalf("len %d, ext %d", len(out), out[126])
	}
	if strings.Contains(string(out), "PANELSER123") || strings.Contains(string(out), "EXTSERIAL999") {
		t.Errorf("filtra el serial: %q", out)
	}
	var s byte
	for _, b := range out {
		s += b
	}
	if s != 0 {
		t.Errorf("checksum %d", s)
	}
	if !slices.Equal(out[54:72], e[54:72]) || !slices.Equal(out[90:108], e[90:108]) {
		t.Errorf("nombre o rango cambiaron")
	}
}
