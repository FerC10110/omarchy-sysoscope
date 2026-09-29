package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const (
	analog = "alsa_output.pci-0000_0c_00.3.analog-stereo"
	hdmi   = "alsa_output.pci-0000_42_00.1.hdmi-stereo"
	mic    = "alsa_input.pci-0000_0c_00.3.analog-stereo"
)

// stubEnv reemplaza getenv: lo que no está en vars es "".
func stubEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	old := getenv
	getenv = func(k string) string { return vars[k] }
	t.Cleanup(func() { getenv = old })
}

// actionEnv es el audio real de threadripper (dos salidas, una entrada) con
// un runner sobre una copia de sus comandos (devuelve su directorio) y un FS
// vacío; scripts dice si están los omarchy-audio-*-set-default en el PATH
// (/usr/bin del FS).
func actionEnv(t *testing.T, scripts bool) (source.Env, string) {
	t.Helper()
	env := source.Fixture(t, "threadripper")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(env.Run.(*source.FakeRunner).Dir)); err != nil {
		t.Fatal(err)
	}
	env.Run = &source.FakeRunner{Dir: dir}
	root := t.TempDir()
	env.FS = source.FS{Root: root}
	stubEnv(t, map[string]string{"PATH": "/usr/bin"})
	if scripts {
		for _, s := range []string{"omarchy-audio-output-set-default", "omarchy-audio-input-set-default"} {
			p := filepath.Join(root, "usr/bin", s)
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
		}
	}
	return env, dir
}

func readCmd(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, source.Key(name, args...)+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func putExit(t *testing.T, dir string, code int, name string, args ...string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, source.Key(name, args...)+".exit"), []byte(strconv.Itoa(code)), 0o644)
}

// changes son las órdenes que cambian algo (no las lecturas).
func changes(run *source.FakeRunner) []string {
	var out []string
	for _, c := range run.Calls {
		if !strings.HasPrefix(c, "pactl_--format=json") && c != availScript {
			out = append(out, c)
		}
	}
	return out
}

func actionsLog(t *testing.T, env source.Env) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(env.State, "actions.log"))
	return string(b)
}

// hookRunner es un FakeRunner que, después de ciertas órdenes, cambia los
// archivos del directorio (lo que la orden hizo de verdad).
type hookRunner struct {
	*source.FakeRunner
	after map[string]func()
}

func (h *hookRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := h.FakeRunner.Run(ctx, name, args...)
	if f := h.after[source.Key(name, args...)]; f != nil {
		f()
	}
	return out, err
}

// deadlineRunner anota cuánto le queda al plazo y responde que venció.
type deadlineRunner struct{ left time.Duration }

func (r *deadlineRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	d, _ := ctx.Deadline()
	r.left = time.Until(d)
	return nil, context.DeadlineExceeded
}

func TestKnown(t *testing.T) {
	for _, id := range []string{"audio.default", "audio.mute", "audio.up", "audio.down", "audio.volume"} {
		if !Known(id) {
			t.Errorf("Known(%q)", id)
		}
	}
	if Known("audio.nope") || Known("bt.power") {
		t.Fatal("Known")
	}
}

func TestMute(t *testing.T) {
	env, dir := actionEnv(t, true)
	run := env.Run.(*source.FakeRunner)
	put(t, dir, "", "pactl", "set-sink-mute", analog, "toggle")
	put(t, dir, "", "pactl", "set-source-mute", mic, "toggle")
	if out, err := Run(env, "audio.mute", analog, ""); err != nil || out != "Muted ALC1220 Analog" {
		t.Fatalf("output: out %q err %v", out, err)
	}
	if out, err := Run(env, "audio.mute", mic, ""); err != nil || out != "Muted ALC1220 Analog" {
		t.Fatalf("input: out %q err %v", out, err)
	}
	want := []string{source.Key("pactl", "set-sink-mute", analog, "toggle"), source.Key("pactl", "set-source-mute", mic, "toggle")}
	if !slices.Equal(changes(run), want) {
		t.Fatalf("changes = %q", changes(run))
	}
	// Sin la fila ni el texto: el nombre de pactl lleva la serie USB o la MAC.
	if l := actionsLog(t, env); !strings.Contains(l, `audio.mute "" "" ok`) || strings.Contains(l, analog) {
		t.Fatalf("actions.log = %q", l)
	}
	// Silenciada: la misma orden, y el texto lo dice al revés.
	sinks := strings.Replace(readCmd(t, dir, "pactl", sinksArgs...), `"mute":false`, `"mute":true`, 1)
	put(t, dir, sinks, "pactl", sinksArgs...)
	if out, _ := Run(env, "audio.mute", analog, ""); out != "Unmuted ALC1220 Analog" {
		t.Fatalf("out %q", out)
	}
}

// ±5 sobre el promedio, fijado con N% como las teclas de volumen de Omarchy
// (un "-5%" suelto pactl lo leería como una opción), con tope 100 y piso 0;
// en el tope o el piso, error sin correr nada.
func TestVolumeSteps(t *testing.T) {
	env, dir := actionEnv(t, true)
	run := env.Run.(*source.FakeRunner)
	for _, c := range []struct{ id, target, vol, out string }{
		{"audio.up", analog, "85%", "Volume of ALC1220 Analog: 85%"},
		{"audio.down", analog, "75%", "Volume of ALC1220 Analog: 75%"},
		{"audio.down", hdmi, "35%", "Volume of S34J55x: 35%"},
	} {
		put(t, dir, "", "pactl", "set-sink-volume", c.target, c.vol)
		if out, err := Run(env, c.id, c.target, ""); err != nil || out != c.out {
			t.Fatalf("%s %s: out %q err %v", c.id, c.target, out, err)
		}
	}
	n := len(changes(run))
	if _, err := Run(env, "audio.up", mic, ""); err == nil || err.Error() != "ALC1220 Analog is already at 100%" || len(changes(run)) != n {
		t.Fatalf("input at 100: %v", err)
	}
	// 98 → 100; en 0, bajar no hace nada.
	put(t, dir, []any{
		pnode(analog, "Analog", "ALC1220 Analog", "sound", "56", false, 64225, 64225),
		pnode(hdmi, "HDMI", "S34J55x", "sound", "88", false, 0, 0),
	}, "pactl", sinksArgs...)
	put(t, dir, "", "pactl", "set-sink-volume", analog, "100%")
	if out, err := Run(env, "audio.up", analog, ""); err != nil || out != "Volume of ALC1220 Analog: 100%" {
		t.Fatalf("98 up: out %q err %v", out, err)
	}
	if _, err := Run(env, "audio.down", hdmi, ""); err == nil || err.Error() != "S34J55x is already at 0%" {
		t.Fatalf("0 down: %v", err)
	}
	// Al 150 %: subir no hace nada, bajar va a 100.
	put(t, dir, []any{pnode(analog, "Analog", "ALC1220 Analog", "sound", "56", false, 98304, 98304)}, "pactl", sinksArgs...)
	if _, err := Run(env, "audio.up", analog, ""); err == nil || err.Error() != "ALC1220 Analog is already at 100%" {
		t.Fatalf("150 up: %v", err)
	}
	if out, err := Run(env, "audio.down", analog, ""); err != nil || out != "Volume of ALC1220 Analog: 100%" {
		t.Fatalf("150 down: out %q err %v", out, err)
	}
}

func TestVolumeInput(t *testing.T) {
	env, dir := actionEnv(t, true)
	run := env.Run.(*source.FakeRunner)
	put(t, dir, "", "pactl", "set-source-volume", mic, "55%")
	if out, err := Run(env, "audio.volume", mic, " 55 "); err != nil || out != "Volume of ALC1220 Analog: 55%" {
		t.Fatalf("out %q err %v", out, err)
	}
	for _, in := range []string{"101", "-1", "abc", "", "5.5"} {
		if _, err := Run(env, "audio.volume", analog, in); err == nil || err.Error() != "the volume is a number from 0 to 100" {
			t.Errorf("%q: %v", in, err)
		}
	}
	if len(changes(run)) != 1 {
		t.Fatalf("changes = %q", changes(run))
	}
}

// d con los scripts de Omarchy (mueven lo que suena o graba): la orden
// exacta, y el resultado se confirma releyendo la predeterminada (el script
// siempre sale 0).
func TestDefault(t *testing.T) {
	env, dir := actionEnv(t, true)
	fake := env.Run.(*source.FakeRunner)
	put(t, dir, []any{
		pnode(mic, "Family 17h", "ALC1220 Analog", "sound", "57", false, 65536, 65536),
		pnode("alsa_input.usb-mic", "USB Mic", "Yeti", "sound", "99", false, 65536),
	}, "pactl", sourcesArgs...)
	info := readCmd(t, dir, "pactl", infoArgs...)
	out := source.Key("omarchy-audio-output-set-default", "88", hdmi)
	in := source.Key("omarchy-audio-input-set-default", "99", "alsa_input.usb-mic")
	put(t, dir, "", "omarchy-audio-output-set-default", "88", hdmi)
	put(t, dir, "", "omarchy-audio-input-set-default", "99", "alsa_input.usb-mic")
	env.Run = &hookRunner{FakeRunner: fake, after: map[string]func(){
		out: func() {
			info = strings.Replace(info, `"default_sink_name":"`+analog, `"default_sink_name":"`+hdmi, 1)
			put(t, dir, info, "pactl", infoArgs...)
		},
		in: func() {
			info = strings.Replace(info, `"default_source_name":"`+mic, `"default_source_name":"alsa_input.usb-mic`, 1)
			put(t, dir, info, "pactl", infoArgs...)
		},
	}}
	if got, err := Run(env, "audio.default", hdmi, ""); err != nil || got != "Default output: S34J55x" {
		t.Fatalf("output: out %q err %v", got, err)
	}
	if got, err := Run(env, "audio.default", "alsa_input.usb-mic", ""); err != nil || got != "Default input: Yeti" {
		t.Fatalf("input: out %q err %v", got, err)
	}
	if !slices.Equal(changes(fake), []string{out, in}) {
		t.Fatalf("changes = %q", changes(fake))
	}
}

func TestDefaultNotConfirmed(t *testing.T) {
	env, dir := actionEnv(t, true)
	put(t, dir, "", "omarchy-audio-output-set-default", "88", hdmi)
	if _, err := Run(env, "audio.default", hdmi, ""); err == nil || err.Error() != "could not make S34J55x the default" {
		t.Fatalf("err %v", err)
	}
}

// Sin los scripts de Omarchy: pactl set-default-sink, y se dice que lo que
// suena no se movió.
func TestDefaultFallback(t *testing.T) {
	env, dir := actionEnv(t, false)
	fake := env.Run.(*source.FakeRunner)
	key := source.Key("pactl", "set-default-sink", hdmi)
	put(t, dir, "", "pactl", "set-default-sink", hdmi)
	info := readCmd(t, dir, "pactl", infoArgs...)
	env.Run = &hookRunner{FakeRunner: fake, after: map[string]func(){key: func() {
		put(t, dir, strings.Replace(info, `"default_sink_name":"`+analog, `"default_sink_name":"`+hdmi, 1), "pactl", infoArgs...)
	}}}
	if out, err := Run(env, "audio.default", hdmi, ""); err != nil ||
		out != "Default output: S34J55x (playing streams were not moved: omarchy-audio-output-set-default is missing)" {
		t.Fatalf("out %q err %v", out, err)
	}
	if !slices.Equal(changes(fake), []string{key}) {
		t.Fatalf("changes = %q", changes(fake))
	}
}

// El script siempre sale 0: si la relectura de la predeterminada (la única
// prueba de que cambió) falla, no se puede confirmar nada, así que es un
// error, no un éxito no confirmado.
func TestDefaultConfirmReadFails(t *testing.T) {
	env, dir := actionEnv(t, true)
	fake := env.Run.(*source.FakeRunner)
	key := source.Key("omarchy-audio-output-set-default", "88", hdmi)
	put(t, dir, "", "omarchy-audio-output-set-default", "88", hdmi)
	env.Run = &hookRunner{FakeRunner: fake, after: map[string]func(){key: func() {
		put(t, dir, "Connection failure: Connection refused\n", "pactl", infoArgs...)
		putExit(t, dir, 1, "pactl", infoArgs...)
	}}}
	if _, err := Run(env, "audio.default", hdmi, ""); err == nil || err.Error() != "Connection failure: Connection refused" {
		t.Fatalf("err = %v", err)
	}
}

// Si la relectura de la predeterminada no es JSON, tampoco se puede
// confirmar: error, igual que si no hubiera cambiado.
func TestDefaultConfirmReadJunk(t *testing.T) {
	env, dir := actionEnv(t, true)
	fake := env.Run.(*source.FakeRunner)
	key := source.Key("omarchy-audio-output-set-default", "88", hdmi)
	put(t, dir, "", "omarchy-audio-output-set-default", "88", hdmi)
	env.Run = &hookRunner{FakeRunner: fake, after: map[string]func(){key: func() {
		put(t, dir, "not json", "pactl", infoArgs...)
	}}}
	if _, err := Run(env, "audio.default", hdmi, ""); err == nil || err.Error() != "could not make S34J55x the default" {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultAlready(t *testing.T) {
	env, _ := actionEnv(t, true)
	run := env.Run.(*source.FakeRunner)
	if _, err := Run(env, "audio.default", analog, ""); err == nil || err.Error() != "ALC1220 Analog is already the default output" {
		t.Fatalf("output: %v", err)
	}
	if _, err := Run(env, "audio.default", mic, ""); err == nil || err.Error() != "ALC1220 Analog is already the default input" {
		t.Fatalf("input: %v", err)
	}
	if len(changes(run)) != 0 {
		t.Fatalf("changes = %q", changes(run))
	}
}

// La fila ya no está (se desenchufó), es un monitor o es una salida que
// Omarchy esconde: error legible y ninguna orden.
func TestTargetGone(t *testing.T) {
	env, dir := actionEnv(t, true)
	run := env.Run.(*source.FakeRunner)
	for _, tg := range []string{"bluez_output.gone", "", analog + ".monitor"} {
		if _, err := Run(env, "audio.mute", tg, ""); err == nil || err.Error() != fmt.Sprintf("no audio device %q (it may have been unplugged)", tg) {
			t.Errorf("%q: %v", tg, err)
		}
	}
	put(t, dir, analog+"\t1\n"+hdmi+"\t0\n", availScript)
	if _, err := Run(env, "audio.default", hdmi, ""); err == nil || !strings.HasPrefix(err.Error(), "no audio device") {
		t.Fatalf("hidden output: %v", err)
	}
	if _, err := Run(env, "audio.nope", analog, ""); err == nil || err.Error() != `unknown action "audio.nope"` {
		t.Fatalf("unknown: %v", err)
	}
	if len(changes(run)) != 0 {
		t.Fatalf("changes = %q", changes(run))
	}
}

// lazylazy no tiene hide: cualquier tecla de audio puede llegar con la fila
// de un dispositivo Bluetooth (su dirección). El motivo lo dice, distinto
// del de una salida desenchufada.
func TestTargetIsBluetoothAddress(t *testing.T) {
	env, _ := actionEnv(t, true)
	run := env.Run.(*source.FakeRunner)
	addr := "02:00:00:00:00:0A"
	for _, id := range []string{"audio.mute", "audio.up", "audio.down", "audio.volume", "audio.default"} {
		if _, err := Run(env, id, addr, "50"); err == nil || err.Error() != addr+" is not an audio device" {
			t.Errorf("%s: %v", id, err)
		}
	}
	if len(changes(run)) != 0 {
		t.Fatalf("changes = %q", changes(run))
	}
	env.Lang = "es"
	if _, err := Run(env, "audio.mute", addr, ""); err == nil || err.Error() != addr+" no es un dispositivo de audio" {
		t.Fatalf("es: %v", err)
	}
}

// Solo audio.default corre el script de disponibilidad de Omarchy: mute,
// +/- y v buscan la fila por nombre exacto sea o no visible, así una tecla
// mantenida (+ o -, ~25/s) no lo corre en cada repetición.
func TestAvailabilityRunsOnlyForDefault(t *testing.T) {
	env, dir := actionEnv(t, true)
	run := env.Run.(*source.FakeRunner)
	put(t, dir, "", "pactl", "set-sink-mute", analog, "toggle")
	put(t, dir, "", "pactl", "set-sink-volume", analog, "85%")
	put(t, dir, "", "pactl", "set-sink-volume", analog, "75%")
	put(t, dir, "", "pactl", "set-sink-volume", analog, "80%")
	for _, c := range []struct{ id, in string }{
		{"audio.mute", ""}, {"audio.up", ""}, {"audio.down", ""}, {"audio.volume", "80"},
	} {
		if _, err := Run(env, c.id, analog, c.in); err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
	}
	if n := run.Count(availScript); n != 0 {
		t.Fatalf("%s ran %d times for row actions, want 0", availScript, n)
	}
	info := readCmd(t, dir, "pactl", infoArgs...)
	key := source.Key("omarchy-audio-output-set-default", "88", hdmi)
	put(t, dir, "", "omarchy-audio-output-set-default", "88", hdmi)
	env.Run = &hookRunner{FakeRunner: run, after: map[string]func(){key: func() {
		put(t, dir, strings.Replace(info, `"default_sink_name":"`+analog, `"default_sink_name":"`+hdmi, 1), "pactl", infoArgs...)
	}}}
	if _, err := Run(env, "audio.default", hdmi, ""); err != nil {
		t.Fatalf("default: %v", err)
	}
	if n := run.Count(availScript); n != 1 {
		t.Fatalf("%s ran %d times for audio.default, want 1", availScript, n)
	}
}

// concurRunner arma "list sinks" con el volumen actual (lo que dejó la
// última orden) y tarda un poco en responder: agranda la ventana de una
// carrera entre dos Run concurrentes si no estuvieran serializados.
type concurRunner struct {
	mu  sync.Mutex
	vol int // % que devuelve la próxima "list sinks"
}

func (r *concurRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	switch {
	case name == "pactl" && slices.Equal(args, sinksArgs):
		r.mu.Lock()
		v := r.vol
		r.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		b, _ := json.Marshal([]any{pnode(analog, "Analog", "ALC1220 Analog", "sound", "56", false, int64(v)*65536/100)})
		return b, nil
	case name == "pactl" && slices.Equal(args, sourcesArgs):
		return []byte("[]"), nil
	case name == "pactl" && slices.Equal(args, infoArgs):
		return []byte(`{"default_sink_name":"other"}`), nil
	case name == "pactl" && len(args) == 3 && args[0] == "set-sink-volume" && args[1] == analog:
		v, err := strconv.Atoi(strings.TrimSuffix(args[2], "%"))
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.vol = v
		r.mu.Unlock()
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected command: %s %q", name, args)
}

// Mantener + manda una tecla por repetición (~25/s), cada una en su propia
// goroutine (stream.go). Sin serializar, dos Run concurrentes leen el mismo
// volumen de partida y una pisa el paso de la otra (40 → 45 dos veces, en
// vez de 40 → 45 → 50). El mutex de Run hace que cada acción entera
// (lectura, orden, y en audio.default la confirmación) termine antes de
// que empiece la siguiente.
func TestConcurrentRunSerializes(t *testing.T) {
	r := &concurRunner{vol: 40}
	env := source.Env{Run: r, Lang: "en", State: t.TempDir(), Now: time.Now}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := Run(env, "audio.up", analog, ""); err != nil {
				t.Errorf("up: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if r.vol != 50 {
		t.Fatalf("vol = %d, want 50 (40 -> 45 -> 50, una acción después de la otra)", r.vol)
	}
}

func TestPactlFails(t *testing.T) {
	env, dir := actionEnv(t, true)
	os.Remove(filepath.Join(dir, source.Key("pactl", sinksArgs...)+".txt"))
	if _, err := Run(env, "audio.mute", analog, ""); err == nil || err.Error() != "requires pactl" {
		t.Fatalf("without pactl: %v", err)
	}
	env, dir = actionEnv(t, true)
	put(t, dir, "Failure: No such entity\n", "pactl", "set-sink-mute", analog, "toggle")
	putExit(t, dir, 1, "pactl", "set-sink-mute", analog, "toggle")
	if _, err := Run(env, "audio.mute", analog, ""); err == nil || err.Error() != "Failure: No such entity" {
		t.Fatalf("pactl error: %v", err)
	}
	if l := actionsLog(t, env); !strings.HasSuffix(l, `audio.mute "" "" error`+"\n") {
		t.Fatalf("actions.log = %q", l)
	}
}

func TestActionTimeout(t *testing.T) {
	env, _ := actionEnv(t, true)
	r := &deadlineRunner{}
	env.Run = r
	if _, err := Run(env, "audio.mute", analog, ""); err == nil || err.Error() != "timed out" {
		t.Fatalf("err %v", err)
	}
	if r.left <= 3*time.Second || r.left > 4*time.Second {
		t.Fatalf("deadline in %v, want 4 s", r.left)
	}
}

func TestAudioActionsSpanish(t *testing.T) {
	env, dir := actionEnv(t, true)
	env.Lang = "es"
	put(t, dir, "", "pactl", "set-sink-mute", analog, "toggle")
	if out, _ := Run(env, "audio.mute", analog, ""); out != "Silenciado: ALC1220 Analog" {
		t.Fatalf("out %q", out)
	}
	if _, err := Run(env, "audio.volume", analog, "x"); err == nil || err.Error() != "el volumen es un número de 0 a 100" {
		t.Fatalf("err %v", err)
	}
	if _, err := Run(env, "audio.default", analog, ""); err == nil || err.Error() != "ALC1220 Analog ya es la salida predeterminada" {
		t.Fatalf("err %v", err)
	}
}
