package bluetooth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const (
	objectsKey = "objects org.bluez /"
	ownerKey   = "call org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus.NameHasOwner org.bluez"
)

// btBus es un bus falso con BlueZ: objs es el árbol de GetManagedObjects;
// owner, si no es nil, la respuesta de NameHasOwner("org.bluez").
func btBus(t *testing.T, objs map[string]any, owner *bool) *source.FakeBus {
	t.Helper()
	raw, err := json.Marshal(objs)
	if err != nil {
		t.Fatal(err)
	}
	b := &source.FakeBus{System: map[string]json.RawMessage{objectsKey: raw}}
	if owner != nil {
		b.System[ownerKey] = json.RawMessage(fmt.Sprintf("[%t]", *owner))
	}
	return b
}

func adapter(powered bool) map[string]any {
	return map[string]any{"org.bluez.Adapter1": map[string]any{"Powered": powered}}
}

func device(addr, name string, connected, paired bool) map[string]any {
	return map[string]any{"org.bluez.Device1": map[string]any{"Address": addr, "Name": name, "Connected": connected, "Paired": paired, "Trusted": paired}}
}

// rfkillRoot arma un /sys con un rfkill de Bluetooth (soft "0" o "1") y uno
// de Wi-Fi bloqueado, que no cuenta.
func rfkillRoot(t *testing.T, soft string) string {
	t.Helper()
	root := t.TempDir()
	for name, files := range map[string]map[string]string{
		"rfkill0": {"type": "bluetooth\n", "soft": soft + "\n", "hard": "0\n"},
		"rfkill1": {"type": "wlan\n", "soft": "1\n", "hard": "0\n"},
	} {
		dir := filepath.Join(root, "sys/class/rfkill", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f, c := range files {
			os.WriteFile(filepath.Join(dir, f), []byte(c), 0o644)
		}
	}
	return root
}

// stubEnv reemplaza getenv: lo que no está en vars es "".
func stubEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	old := getenv
	getenv = func(k string) string { return vars[k] }
	t.Cleanup(func() { getenv = old })
}

func TestReadLaptop(t *testing.T) {
	st, err := Read(source.Fixture(t, "laptop"))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Present || !st.Powered || st.Blocked || len(st.Devices) != 3 {
		t.Fatalf("state = %+v", st)
	}
	h, k, n := st.Devices[0], st.Devices[1], st.Devices[2]
	if h.Name != "WH-1000XM4" || !h.Connected || !h.Paired || !h.Trusted || h.Battery == nil || *h.Battery != 70 {
		t.Errorf("headphones = %+v", h)
	}
	if k.Name != "Keyboard K380" || k.Connected || !k.Paired || k.Battery != nil {
		t.Errorf("keyboard = %+v", k)
	}
	// Cercano: sin emparejar, al final, con el Alias (no tiene Name).
	if n.Name != "Speaker" || n.Paired || n.Connected || n.Address != "02:00:00:00:00:0C" {
		t.Errorf("nearby = %+v", n)
	}
}

func TestReadAdapterStates(t *testing.T) {
	yes, no := true, false
	env := source.Env{FS: source.FS{Root: rfkillRoot(t, "1")}, Bus: btBus(t, map[string]any{"/org/bluez/hci0": adapter(false)}, &yes)}
	st, err := Read(env)
	if err != nil || !st.Present || st.Powered || !st.Blocked || len(st.Devices) != 0 {
		t.Fatalf("off and blocked: %+v %v", st, err)
	}
	// Con dos adaptadores, alcanza con que uno esté encendido.
	env.FS = source.FS{Root: rfkillRoot(t, "0")}
	env.Bus = btBus(t, map[string]any{"/org/bluez/hci0": adapter(false), "/org/bluez/hci1": adapter(true)}, &yes)
	if st, _ = Read(env); !st.Powered || st.Blocked {
		t.Fatalf("two adapters: %+v", st)
	}
	// BlueZ sin adaptadores (el dongle se desenchufó): sin adaptador, sin error.
	env.Bus = btBus(t, map[string]any{}, &yes)
	if st, err = Read(env); err != nil || st.Present {
		t.Fatalf("no adapter: %+v %v", st, err)
	}
	// BlueZ no corre: ni se le pregunta (el bus igual respondería), así D-Bus
	// no intenta activarlo.
	env.Bus = btBus(t, map[string]any{"/org/bluez/hci0": adapter(true),
		"/org/bluez/hci0/dev_02_00_00_00_00_0A": device("02:00:00:00:00:0A", "X", true, true)}, &no)
	if st, err = Read(env); err == nil || st.Present || len(st.Devices) != 0 {
		t.Fatalf("not running: %+v %v", st, err)
	}
	// La pregunta falla (sin respuesta) o responde algo que no es un booleano:
	// tampoco se le habla a BlueZ, aunque el bus tenga sus objetos.
	objs := map[string]any{"/org/bluez/hci0": adapter(true)}
	env.Bus = btBus(t, objs, nil)
	if st, err = Read(env); err == nil || st.Present {
		t.Fatalf("owner error: %+v %v", st, err)
	}
	odd := btBus(t, objs, nil)
	odd.System[ownerKey] = json.RawMessage(`["yes"]`)
	env.Bus = odd
	if st, err = Read(env); err == nil || st.Present {
		t.Fatalf("odd owner answer: %+v %v", st, err)
	}
}

func TestSample(t *testing.T) {
	env := source.Fixture(t, "laptop")
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(env.FS.Root)); err != nil {
		t.Fatal(err)
	}
	env.FS = source.FS{Root: root}
	stubEnv(t, map[string]string{"PATH": "/usr/local/bin:/usr/bin"})
	tool := func(name string) {
		os.MkdirAll(filepath.Join(root, "usr/bin"), 0o755)
		os.WriteFile(filepath.Join(root, "usr/bin", name), []byte("#!/bin/sh\n"), 0o755)
	}
	s := &model.System{}
	r := &collect.Report{Lang: "en"}
	Sample(env, s, r)
	if len(s.Bluetooth) != 3 || !s.BTPresent || !s.BTPowered || s.BTBlocked || s.BTTools || len(r.List) != 0 {
		t.Fatalf("system = %+v, unavailable %+v", s, r.List)
	}
	tool(powerCmd)
	if Sample(env, s, r); s.BTTools {
		t.Fatal("omarchy-bluetooth-device is missing")
	}
	tool(deviceCmd)
	if Sample(env, s, r); !s.BTTools {
		t.Fatal("both Omarchy commands are in PATH")
	}
	// Sin BlueZ: como antes, sin dispositivos y "system.bluetooth" no disponible.
	env.Bus = &source.FakeBus{}
	Sample(env, s, r)
	if len(s.Bluetooth) != 0 || s.BTPresent || len(r.List) != 1 || r.List[0].What != "system.bluetooth" {
		t.Fatalf("without BlueZ: %+v, unavailable %+v", s, r.List)
	}
}
