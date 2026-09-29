package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Con la sección cerrada: puertos, firewall y cifrado sí; programas no, y
// ningún recorrido de /proc/<pid>/fd.
func TestSecurityClosed(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	e := newEngine(t, env, "")
	s := e.Tick()
	e.Tick()
	sec := s.Security
	if sec == nil || len(sec.Listening) != 14 {
		t.Fatalf("security = %+v", sec)
	}
	first := sec.Listening[0]
	if first.Proto != "tcp" || first.Addr != "::" || first.Port != 1716 || first.Scope != "all" || first.Firewall != "open" ||
		!reflect.DeepEqual(first.Owners, []model.ListenerOwner{{UID: 1000, User: "user", Own: true}}) {
		t.Errorf("first = %+v", first)
	}
	// mDNS multicast (udp/5353) pasa por la excepción de before.rules que el
	// controller falló en la Task 3 (commit d0c9772): queda "limited" con su
	// origen, no "blocked" como decía el brief original (escrito antes de
	// esa revisión).
	if l := sec.Listening[2]; l.Addr != "0.0.0.0" || l.Port != 5353 || l.Firewall != "limited" || l.From != "mDNS (multicast)" || l.Owners[0].User != "avahi" {
		t.Errorf("avahi = %+v", l)
	}
	fw := sec.Firewall
	if fw == nil || fw.Tool != "ufw" || fw.State != "on" || !fw.Active || fw.Policy != "DROP" || !fw.IPv6 || len(fw.Rules) != 11 {
		t.Errorf("firewall = %+v", fw)
	}
	wantV := []model.CryptVolume{
		{Device: "/dev/nvme1n1p1", Mounts: []string{"/boot"}},
		{Device: "/dev/mapper/root", Mounts: []string{"/", "/home", "/var/cache/pacman/pkg", "/var/log"}, Encrypted: true, Type: "LUKS2", On: "nvme1n1p2"},
	}
	wantS := []model.SwapCrypt{
		{Name: "/swap/swapfile", Kind: "file", State: "encrypted", Type: "LUKS2", On: "nvme1n1p2"},
		{Name: "/dev/zram0", Kind: "zram", State: "memory"},
	}
	if !reflect.DeepEqual(sec.Volumes, wantV) || !reflect.DeepEqual(sec.Swaps, wantS) {
		t.Errorf("volumes %+v\nswaps %+v", sec.Volumes, sec.Swaps)
	}
	if s.Privacy != nil || e.owners.Walks() != 0 {
		t.Error("the fd walk must not run with Security closed")
	}
	// El reloj y el firewall: una sola llamada a systemd1 por vuelta lenta.
	if n := env.Bus.(*source.FakeBus).Called("ListUnitsByNames"); n != 1 {
		t.Errorf("ListUnitsByNames = %d calls, want 1", n)
	}
}

func TestSecurityOpenFindsPrograms(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	e := newEngine(t, env, "security")
	var s *model.Snapshot
	for range 6 {
		s = e.Tick()
	}
	ls := s.Security.Listening
	prog := func(i int) string { return ls[i].Owners[0].Program }
	if prog(0) != "prog2" || prog(1) != "prog2" || prog(7) != "prog3" || ls[6].Owners[0].User != "root" {
		t.Fatalf("listeners = %+v", ls)
	}
	if n := e.owners.Walks(); n != 1 {
		t.Fatalf("fd walks = %d after 6 samples, want 1 (the sockets did not change)", n)
	}
	if s.Privacy == nil {
		t.Error("the same walk must fill Privacy")
	}
	e.SetFocus("")
	if s := e.Tick(); s.Security.Listening[0].Owners[0].Program != "" {
		t.Error("programs only with Security open")
	}
}

// setUnit cambia el ActiveState de una unidad en el dbus.json de una copia
// del fixture y recarga el FakeBus.
func setUnit(t *testing.T, env *source.Env, unit, active string) {
	t.Helper()
	p := filepath.Join(filepath.Dir(env.FS.Root), "dbus.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var b map[string]map[string]any
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	for k, v := range b["system"] {
		if !strings.Contains(k, "ListUnitsByNames") {
			continue
		}
		for _, u := range v.([]any)[0].([]any) {
			if f := u.([]any); f[0] == unit {
				f[3] = active
			}
		}
	}
	out, _ := json.Marshal(b)
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatal(err)
	}
	fb, err := source.LoadFakeBus(p)
	if err != nil {
		t.Fatal(err)
	}
	env.Bus = fb
}

func TestSecurityFirewallNotLoaded(t *testing.T) {
	env := source.FixtureCopy(t, "threadripper")
	setUnit(t, &env, "ufw.service", "inactive")
	sec := newEngine(t, env, "").Tick().Security
	if sec.Firewall.State != "not_loaded" || sec.Firewall.Active {
		t.Fatalf("firewall = %+v", sec.Firewall)
	}
	if l := sec.Listening[0]; l.Firewall != "none" {
		t.Errorf("first = %+v", l)
	}
}

// Los archivos de ufw no cambian: se leen una vez aunque corra lo lento.
func TestSecurityFirewallFilesReadOnce(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	e := newEngine(t, env, "")
	for range 3 {
		e.Tick()
		now = now.Add(slowPeriod)
	}
	// Las 3 muestras fueron lentas: una llamada a systemd1 por cada una.
	if n := env.Bus.(*source.FakeBus).Called("ListUnitsByNames"); n != 3 {
		t.Fatalf("%d slow samples, want 3", n)
	}
	if n := e.fwR.Parses(); n != 1 {
		t.Fatalf("ufw files parsed %d times in 3 slow samples, want 1", n)
	}
}

func TestSecurityLaptop(t *testing.T) {
	s := newEngine(t, source.Fixture(t, "laptop"), "security").Tick()
	if s.Security == nil || s.Security.Listening != nil || s.Security.Firewall == nil || s.Security.Firewall.State != "none" {
		t.Fatalf("security = %+v", s.Security)
	}
	found := false
	for _, u := range s.Unavailable {
		found = found || u.What == "security.listening"
	}
	if !found {
		t.Errorf("unavailable = %+v", s.Unavailable)
	}
}

// Snapshot.Security se arma de nuevo solo cuando cambia algo de lo que
// depende (sockets, firewall, programas, foco, interfaces): una muestra
// común reusa el de la anterior.
func TestSecurityStateCached(t *testing.T) {
	env := source.FixtureCopy(t, "threadripper")
	e := newEngine(t, env, "security")
	first := e.Tick() // lenta: se arma
	if e.secBuilds != 1 {
		t.Fatalf("builds = %d after the slow sample, want 1", e.secBuilds)
	}
	for i := 1; i < 5; i++ {
		if s := e.Tick(); s.Security != first.Security {
			t.Fatalf("sample %d rebuilt Security with nothing changed", i)
		}
	}
	if e.secBuilds != 1 {
		t.Fatalf("builds = %d, want 1", e.secBuilds)
	}

	// Las direcciones de las interfaces se reescriben en su lugar cada
	// everyNet: la 5 arma de nuevo (el alcance depende de ellas).
	for i := 1; i < 5; i++ {
		e.Tick()
	}
	if e.secBuilds != 2 {
		t.Fatalf("builds = %d after the everyNet sample, want 2", e.secBuilds)
	}
	first = e.Tick() // 9: reusa el de la 5; esta es la referencia

	// Un socket nuevo en LISTEN: la próxima relectura de puertos lo arma.
	p := filepath.Join(env.FS.Root, "proc/net/tcp")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	line := "  99: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 77777 1 0000000000000000 100 0 0 10 5\n"
	if err := os.WriteFile(p, append(raw, line...), 0o644); err != nil {
		t.Fatal(err)
	}
	var s *model.Snapshot
	for range 5 {
		s = e.Tick()
	}
	if e.secBuilds != 3 || s.Security == first.Security || len(s.Security.Listening) != len(first.Security.Listening)+1 {
		t.Fatalf("builds = %d, listening %d → %d", e.secBuilds, len(first.Security.Listening), len(s.Security.Listening))
	}

	// Cerrar la sección cambia los dueños (sin programas): se arma de nuevo.
	e.SetFocus("")
	if s2 := e.Tick(); e.secBuilds != 4 || s2.Security == s.Security || s2.Security.Listening[0].Owners[0].Program != "" {
		t.Fatalf("builds = %d after closing Security", e.secBuilds)
	}
	e.Tick()
	if e.secBuilds != 4 {
		t.Fatalf("builds = %d, want 4", e.secBuilds)
	}

	// Una vuelta lenta (firewall, usuarios, cifrado, interfaces) siempre arma.
	e.RefreshSlow()
	e.Tick()
	if e.secBuilds != 5 {
		t.Fatalf("builds = %d after a slow sample, want 5", e.secBuilds)
	}
}

// Final pass (Task 6 review a): abrir Seguridad relee los puertos en la
// muestra siguiente, no con los de la última vuelta lenta (hasta 60 s).
func TestSecurityOpenRefreshesSockets(t *testing.T) {
	env := source.FixtureCopy(t, "threadripper")
	e := newEngine(t, env, "")
	n0 := len(e.Tick().Security.Listening) // lenta, tick 0
	addListen(t, env, "  99: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 77777 1 0000000000000000 100 0 0 10 5\n")
	if n := len(e.Tick().Security.Listening); n != n0 { // tick 1, cerrada: no relee
		t.Fatalf("closed: %d listeners, want %d", n, n0)
	}
	e.SetFocus("security")
	if n := len(e.Tick().Security.Listening); n != n0+1 { // tick 2: no es everyNet
		t.Fatalf("after opening: %d listeners, want %d", n, n0+1)
	}
}

// Task 10 review (d): un socket propio nuevo cuyo programa aparece en el
// recorrido de fds sale con su programa en la misma muestra; un cambio de
// dirección de una interfaz cambia el alcance en la siguiente relectura.
func TestSecurityStateNewProgramAndAddress(t *testing.T) {
	env := source.FixtureCopy(t, "threadripper")
	var v4 []string
	e := newEngine(t, env, "security")
	e.netS.Addrs = func(name string) ([]string, []string) {
		if name == "docker0" {
			return v4, nil
		}
		return nil, nil
	}
	s := e.Tick()
	dns := func(s *model.Snapshot) model.Listener {
		for _, l := range s.Security.Listening {
			if l.Addr == "172.17.0.1" && l.Proto == "tcp" {
				return l
			}
		}
		t.Fatal("no 172.17.0.1 tcp listener")
		return model.Listener{}
	}
	if l := dns(s); l.Iface != "" {
		t.Fatalf("before = %+v", l)
	}
	// Solo cambia una dirección (los puertos no): la relectura de
	// everyNet la ve igual.
	v4 = []string{"172.17.0.1/16"}
	for range 5 { // hasta la 5 (everyNet)
		s = e.Tick()
	}
	if l := dns(s); l.Iface != "docker0" {
		t.Errorf("after the address change = %+v", l)
	}
	builds := e.secBuilds

	addListen(t, env, "  99: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 77777 1 0000000000000000 100 0 0 10 5\n")
	dir := filepath.Join(env.FS.Root, "proc/4242")
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "comm"), []byte("prog9\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "fd/3.link"), []byte("socket:[77777]"), 0o644)
	for range 5 { // hasta la 10
		s = e.Tick()
	}
	found := false
	for _, l := range s.Security.Listening {
		if l.Port == 8080 && len(l.Owners) == 1 && l.Owners[0].Program == "prog9" {
			found = true
		}
	}
	if !found {
		t.Errorf("new own socket without its program: %+v", s.Security.Listening)
	}
	if e.secBuilds != builds+1 {
		t.Errorf("builds = %d, want %d", e.secBuilds, builds+1)
	}
}

func addListen(t *testing.T, env source.Env, line string) {
	t.Helper()
	p := filepath.Join(env.FS.Root, "proc/net/tcp")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(raw, line...), 0o644); err != nil {
		t.Fatal(err)
	}
}
