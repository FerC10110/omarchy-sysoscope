package engine

import (
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// El audio (pactl) corre solo con la vista Dispositivos abierta, en cada
// muestra; el script de disponibilidad, solo si cambian las salidas. Fuera
// de la vista, cero pactl y sin Sound, salvo la muestra que sigue a
// RefreshAudio.
func TestAudioOnlyWithDevices(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	run := runs(env)
	e := newEngine(t, env, "")
	if s := e.Tick(); s.System.Sound != nil || run.Count("pactl") != 0 {
		t.Fatalf("closed: sound %+v, pactl %d", s.System.Sound, run.Count("pactl"))
	}
	e.SetFocus("devices")
	s := e.Tick()
	if s.System.Sound == nil || len(s.System.Sound.Outputs) != 2 || len(s.System.Sound.Inputs) != 1 || run.Count("pactl") != 3 {
		t.Fatalf("devices: sound %+v, pactl %d", s.System.Sound, run.Count("pactl"))
	}
	e.Tick()
	if run.Count("pactl") != 6 || run.Count("omarchy-audio-sink-availability") != 1 {
		t.Fatalf("second tick: %q", run.Calls)
	}
	e.SetFocus("")
	if s := e.Tick(); s.System.Sound != nil || run.Count("pactl") != 6 {
		t.Fatal("closed again: no pactl and no sound")
	}
	// RefreshAudio(false) (mute, volumen…): lee todo de nuevo, pero reusa la
	// disponibilidad en caché (las salidas y sus puertos no cambiaron).
	e.RefreshAudio(false)
	if s := e.Tick(); s.System.Sound == nil || run.Count("pactl") != 9 || run.Count("omarchy-audio-sink-availability") != 1 {
		t.Fatal("RefreshAudio(false): one full read, cached availability")
	}
	if s := e.Tick(); s.System.Sound != nil {
		t.Fatal("RefreshAudio reads once")
	}
	e.SetFocus("")
	// RefreshAudio(true) (audio.default): fuerza que el script vuelva a
	// correr, aunque las salidas y sus puertos sigan iguales.
	e.RefreshAudio(true)
	if s := e.Tick(); s.System.Sound == nil || run.Count("omarchy-audio-sink-availability") != 2 {
		t.Fatal("RefreshAudio(true): forces the availability script again")
	}
}

// Sin pactl (el fixture laptop no tiene comandos): sin Sound y con
// "system.sound" entre los faltantes, solo mientras la vista está abierta.
func TestAudioUnavailable(t *testing.T) {
	has := func(us []string, what string) bool {
		for _, u := range us {
			if u == what {
				return true
			}
		}
		return false
	}
	e := newEngine(t, source.Fixture(t, "laptop"), "devices")
	s := e.Tick()
	var whats []string
	for _, u := range s.Unavailable {
		whats = append(whats, u.What)
	}
	if s.System.Sound != nil || !has(whats, "system.sound") {
		t.Fatalf("sound %+v, unavailable %q", s.System.Sound, whats)
	}
	e.SetFocus("")
	s = e.Tick()
	for _, u := range s.Unavailable {
		if u.What == "system.sound" {
			t.Fatal("with the view closed nothing about audio is reported")
		}
	}
}
