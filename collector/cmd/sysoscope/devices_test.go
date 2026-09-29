package main

import (
	"errors"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/view"
)

// stubDevices reemplaza audio.Run y bluetooth.Run: audio.default y
// bt.forget fallan, lo demás responde "ok <id>". El stream las llama desde
// goroutines: calls va con mutex.
func stubDevices(t *testing.T) *[]runCall {
	t.Helper()
	var mu sync.Mutex
	var calls []runCall
	stub := func(fail string) func(source.Env, string, string, string) (string, error) {
		return func(_ source.Env, id, target, input string) (string, error) {
			mu.Lock()
			calls = append(calls, runCall{id, target, input})
			mu.Unlock()
			if id == fail {
				return "", errors.New("refused " + id)
			}
			return "ok " + id, nil
		}
	}
	oldA, oldB := audioRun, btRun
	audioRun, btRun = stub("audio.default"), stub("bt.forget")
	t.Cleanup(func() { audioRun, btRun = oldA, oldB })
	return &calls
}

// Las de audio llevan fila (y el dato de v); bt.power y bt.pair no.
func TestActionCommandDevices(t *testing.T) {
	calls := stubDevices(t)
	power := stubPower(t)
	a, _, out, errb := newApp(t, "")
	if code := a.run([]string{"action", "audio.volume", "alsa_output.x", "55"}); code != 0 || out.String() != "ok audio.volume\n" {
		t.Fatalf("volume: exit %d, out %q", code, out.String())
	}
	if code := a.run([]string{"action", "bt.power"}); code != 0 {
		t.Fatalf("power: exit %d", code)
	}
	if code := a.run([]string{"action", "bt.pair", "extra"}); code != 0 {
		t.Fatalf("pair: exit %d", code)
	}
	if code := a.run([]string{"action", "bt.forget", "02:00:00:00:00:0A"}); code != 1 || errb.String() != "refused bt.forget\n" {
		t.Fatalf("forget: exit %d, err %q", code, errb.String())
	}
	want := []runCall{{"audio.volume", "alsa_output.x", "55"}, {"bt.power", "", ""}, {"bt.pair", "", "extra"}, {"bt.forget", "02:00:00:00:00:0A", ""}}
	if !slices.Equal(*calls, want) || len(*power) != 0 {
		t.Fatalf("calls %+v, power calls %+v", *calls, *power)
	}
	for _, id := range []string{"audio.nope", "bt.nope"} {
		if code := a.run([]string{"action", id}); code != 2 {
			t.Errorf("%s: exit %d", id, code)
		}
	}
	if len(*calls) != 4 {
		t.Fatal("an unknown audio.* or bt.* id must be refused before running")
	}
}

func TestLazylazyDevicesAction(t *testing.T) {
	calls := stubDevices(t)
	a, _, _, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "action", "audio.mute", "alsa_output.x"}); code != 0 {
		t.Fatalf("mute: exit %d", code)
	}
	if code := a.run([]string{"lazylazy", "action", "bt.power"}); code != 0 {
		t.Fatalf("power: exit %d", code)
	}
	if !slices.Equal(*calls, []runCall{{"audio.mute", "alsa_output.x", ""}, {"bt.power", "", ""}}) {
		t.Fatalf("calls = %+v", *calls)
	}
}

// En el stream cada resultado sale en una línea y el engine relee lo suyo.
func TestStreamDevicesActions(t *testing.T) {
	stubDevices(t)
	a, fake, _, _ := newApp(t, "")
	inW, lines, done := pipeStream(t, a)
	io.WriteString(inW, `action audio.mute ["alsa_output.x",""]`+"\n")
	io.WriteString(inW, `action bt.connect ["02:00:00:00:00:0B",""]`+"\n")
	got := waitResults(t, lines, 2)
	inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
	ids := []string{got[0].ID, got[1].ID}
	slices.Sort(ids)
	if !got[0].OK || !got[1].OK || !slices.Equal(ids, []string{"audio.mute", "bt.connect"}) {
		t.Fatalf("results = %+v", got)
	}
	if fake.audioRefresh != 1 || fake.btRefresh != 1 || fake.powerRefresh != 0 || fake.wifiRefresh != 0 {
		t.Fatalf("refresh: audio %d, bt %d, power %d, wifi %d", fake.audioRefresh, fake.btRefresh, fake.powerRefresh, fake.wifiRefresh)
	}
	// audio.mute no cambia qué salidas ofrece Omarchy: RefreshAudio reusa
	// la disponibilidad en caché.
	if !slices.Equal(fake.audioResetAvail, []bool{false}) {
		t.Fatalf("audioResetAvail = %v, want [false] for audio.mute", fake.audioResetAvail)
	}
}

// audio.default sí hace que la próxima muestra vuelva a correr el script
// de disponibilidad de Omarchy (a diferencia de mute, +, - y v).
func TestStreamAudioDefaultResetsAvailability(t *testing.T) {
	stubDevices(t)
	a, fake, _, _ := newApp(t, "")
	inW, lines, done := pipeStream(t, a)
	io.WriteString(inW, `action audio.default ["alsa_output.x",""]`+"\n")
	waitResults(t, lines, 1)
	inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
	if fake.audioRefresh != 1 || !slices.Equal(fake.audioResetAvail, []bool{true}) {
		t.Fatalf("refresh %d, audioResetAvail %v, want 1 and [true]", fake.audioRefresh, fake.audioResetAvail)
	}
}

func TestBtWaitCommand(t *testing.T) {
	var got []string
	old := btWait
	btWait = func(_ source.Env, id, tg string, argv []string) error {
		got = append([]string{id, tg}, argv...)
		if tg == "bad" {
			return errors.New("could not")
		}
		return nil
	}
	t.Cleanup(func() { btWait = old })
	a, _, out, _ := newApp(t, "")
	args := []string{"bt-wait", "bt.connect", "02:00:00:00:00:0B", "--", "omarchy-bluetooth-device", "connect", "02:00:00:00:00:0B"}
	if code := a.run(args); code != 0 || out.Len() != 0 ||
		!slices.Equal(got, []string{"bt.connect", "02:00:00:00:00:0B", "omarchy-bluetooth-device", "connect", "02:00:00:00:00:0B"}) {
		t.Fatalf("exit %d, out %q, got %q", code, out.String(), got)
	}
	if code := a.run([]string{"bt-wait", "bt.power", "", "--", "omarchy-bluetooth-power", "on"}); code != 0 || got[1] != "" {
		t.Fatalf("power: exit %d, got %q", code, got)
	}
	if code := a.run([]string{"bt-wait", "bt.connect", "bad", "--", "x"}); code != 1 {
		t.Fatalf("failed: exit %d", code)
	}
	for _, bad := range [][]string{{"bt-wait", "bt.connect", "x"}, {"bt-wait", "a", "b", "c", "d"}} {
		if code := a.run(bad); code != 2 {
			t.Errorf("%q: exit %d", bad, code)
		}
	}
}

// Dispositivos tiene su foco (audio y Bluetooth en cada muestra) y
// latest.json nunca le sirve.
func TestDevicesFocus(t *testing.T) {
	if engineFocus("devices") != "devices" {
		t.Fatal("engineFocus")
	}
	s := &model.Snapshot{Apps: &model.Apps{List: []model.App{{ID: "x"}}}, System: &model.System{Sound: &model.Sound{}}}
	if latestServes(s, engineFocus("devices")) {
		t.Fatal("latest.json must not serve the Devices view")
	}
}

// lazylazy no tiene hide: de las v por fila queda una, sin dato por defecto
// (la vista original no se toca).
func TestForLazylazyDevices(t *testing.T) {
	in := func(d string) *view.Input { return &view.Input{Type: "number", Prompt: "Volume (0–100)", Default: d} }
	first := in("80")
	v := &view.View{Actions: []view.Action{
		{Key: "m", ID: "audio.mute", Target: "row", Mode: "background"},
		{Key: "v", ID: "audio.volume", Target: "row", Mode: "background", Input: first},
		{Key: "v", ID: "audio.volume", Target: "row", Mode: "background", Input: in("40")},
		{Key: "c", ID: "bt.connect", Target: "row", Mode: "background"},
	}}
	got := forLazylazy(v).Actions
	var keys []string
	for _, a := range got {
		keys = append(keys, a.Key)
	}
	if !slices.Equal(keys, []string{"m", "v", "c"}) || got[1].Input.Default != "" || got[1].Input.Type != "number" || got[1].Mode != "background" {
		t.Fatalf("actions = %+v", got)
	}
	if first.Default != "80" {
		t.Fatal("forLazylazy changed the original input")
	}
}
