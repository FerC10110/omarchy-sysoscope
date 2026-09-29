package audio

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

var (
	sinksArgs   = []string{"--format=json", "list", "sinks"}
	sourcesArgs = []string{"--format=json", "list", "sources"}
	infoArgs    = []string{"--format=json", "info"}
)

// pnode arma un elemento de `pactl --format=json list sinks|sources` con lo
// que lee el colector; vols son los canales, en crudo (65536 = 100 %).
func pnode(name, desc, nick, class, id string, mute bool, vols ...int64) map[string]any {
	chans := []string{"front-left", "front-right", "rear-left", "rear-right"}
	vol := map[string]any{}
	for i, v := range vols {
		vol[chans[i]] = map[string]any{"value": v}
	}
	props := map[string]any{"object.id": id, "device.class": class}
	if nick != "" {
		props["node.nick"] = nick
	}
	return map[string]any{"name": name, "description": desc, "mute": mute, "volume": vol, "properties": props}
}

// withPorts pone los puertos (nombre, disponibilidad) de un elemento.
func withPorts(n map[string]any, ports ...[2]string) map[string]any {
	var ps []map[string]any
	for _, p := range ports {
		ps = append(ps, map[string]any{"name": p[0], "availability": p[1]})
	}
	n["ports"] = ps
	return n
}

// put escribe en dir la salida falsa de una orden: out es un texto o algo
// que se pasa a JSON.
func put(t *testing.T, dir string, out any, name string, args ...string) {
	t.Helper()
	b, ok := out.(string)
	if !ok {
		j, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		b = string(j)
	}
	if err := os.WriteFile(filepath.Join(dir, source.Key(name, args...)+".txt"), []byte(b), 0o644); err != nil {
		t.Fatal(err)
	}
}

// soundEnv es un Env con un runner vacío en un directorio temporal (que
// devuelve) y un reloj que el test mueve.
func soundEnv(t *testing.T, now *time.Time) (source.Env, string) {
	t.Helper()
	dir := t.TempDir()
	return source.Env{Run: &source.FakeRunner{Dir: dir}, Now: func() time.Time { return *now }, Lang: "en", State: t.TempDir()}, dir
}

func read(t *testing.T, r *Reader, env source.Env) (*model.Sound, *collect.Report) {
	t.Helper()
	rep := &collect.Report{Lang: "en"}
	return r.Read(context.Background(), env, rep), rep
}

// La salida real de pactl en la máquina del autor: dos salidas (la
// analógica al 80 %, predeterminada, y el HDMI al 40 %), una entrada y dos
// monitores que no cuentan.
func TestReadThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	var r Reader
	s, rep := read(t, &r, env)
	want := &model.Sound{
		Outputs: []model.AudioNode{
			{ID: "56", Name: "alsa_output.pci-0000_0c_00.3.analog-stereo", Label: "ALC1220 Analog", Volume: 80, Default: true},
			{ID: "88", Name: "alsa_output.pci-0000_42_00.1.hdmi-stereo", Label: "S34J55x", Volume: 40},
		},
		Inputs: []model.AudioNode{
			{ID: "57", Name: "alsa_input.pci-0000_0c_00.3.analog-stereo", Label: "ALC1220 Analog", Volume: 100, Default: true},
		},
	}
	if !reflect.DeepEqual(s, want) || len(rep.List) != 0 {
		t.Fatalf("sound = %+v, unavailable %+v", s, rep.List)
	}
	run := env.Run.(*source.FakeRunner)
	if run.Count("pactl") != 3 || run.Count(availScript) != 1 {
		t.Fatalf("calls = %q", run.Calls)
	}
	// Mismas salidas y puertos: el script no se vuelve a correr.
	read(t, &r, env)
	if run.Count("pactl") != 6 || run.Count(availScript) != 1 {
		t.Fatalf("second read calls = %q", run.Calls)
	}
}

// Una salida que Omarchy marca con 0 no se muestra; si cambia un puerto (se
// enchufa el HDMI), el script se vuelve a correr. Sin el script, todas.
func TestAvailability(t *testing.T) {
	now := source.FixtureNow
	env, dir := soundEnv(t, &now)
	hdmi := withPorts(pnode("hdmi", "HDMI", "Monitor", "sound", "7", false, 65536, 65536), [2]string{"hdmi-output-0", "not available"})
	analog := pnode("analog", "Analog", "", "sound", "5", false, 32768, 32768)
	put(t, dir, []any{analog, hdmi}, "pactl", sinksArgs...)
	put(t, dir, []any{}, "pactl", sourcesArgs...)
	put(t, dir, map[string]string{"default_sink_name": "analog"}, "pactl", infoArgs...)
	put(t, dir, "analog\t1\nhdmi\t0\n", availScript)
	var r Reader
	s, _ := read(t, &r, env)
	if len(s.Outputs) != 1 || s.Outputs[0].Name != "analog" || s.Outputs[0].Label != "Analog" || !s.Outputs[0].Default ||
		s.Outputs[0].Volume != 50 || len(s.Inputs) != 0 {
		t.Fatalf("sound = %+v", s)
	}
	put(t, dir, "analog\t1\nhdmi\t1\n", availScript)
	if s, _ = read(t, &r, env); len(s.Outputs) != 1 {
		t.Fatal("same outputs and ports: the script must not run again")
	}
	withPorts(hdmi, [2]string{"hdmi-output-0", "available"})
	put(t, dir, []any{analog, hdmi}, "pactl", sinksArgs...)
	if s, _ = read(t, &r, env); len(s.Outputs) != 2 || s.Outputs[1].Label != "Monitor" {
		t.Fatalf("after plugging the HDMI cable: %+v", s)
	}
	if n := env.Run.(*source.FakeRunner).Count(availScript); n != 2 {
		t.Fatalf("the script ran %d times", n)
	}
	os.Remove(filepath.Join(dir, availScript+".txt"))
	r.Reset()
	if s, _ = read(t, &r, env); len(s.Outputs) != 2 {
		t.Fatalf("without the script: %+v", s)
	}
}

// Las entradas sin monitores (por device.class o por el nombre), el volumen
// promedio de canales desparejos, el silencio y la etiqueta (node.nick, si
// no la descripción, si no el nombre).
func TestInputsAndVolumes(t *testing.T) {
	now := source.FixtureNow
	env, dir := soundEnv(t, &now)
	put(t, dir, []any{}, "pactl", sinksArgs...)
	put(t, dir, []any{
		pnode("out.monitor", "Monitor of Out", "", "", "3", false, 65536, 65536),
		pnode("hdmi.mon", "Monitor of HDMI", "", "monitor", "4", false, 65536, 65536),
		pnode("mic", "USB Mic", "Yeti", "sound", "9", true, 65536, 32768),
		pnode("quiet", "", "", "sound", "10", false),
	}, "pactl", sourcesArgs...)
	put(t, dir, map[string]string{"default_source_name": "mic"}, "pactl", infoArgs...)
	var r Reader
	s, _ := read(t, &r, env)
	want := []model.AudioNode{
		{ID: "9", Name: "mic", Label: "Yeti", Volume: 75, Muted: true, Default: true},
		{ID: "10", Name: "quiet", Label: "quiet", Volume: 0},
	}
	if !reflect.DeepEqual(s.Inputs, want) || len(s.Outputs) != 0 {
		t.Fatalf("inputs = %+v", s.Inputs)
	}
}

// Sin pactl: nil y "system.sound" (requiere pactl); no se vuelve a probar
// hasta pasados 10 s, pero el faltante sigue en cada muestra. Una respuesta
// que no es JSON también es una falla.
func TestReadFailureBacksOff(t *testing.T) {
	now := source.FixtureNow
	env, dir := soundEnv(t, &now)
	run := env.Run.(*source.FakeRunner)
	var r Reader
	s, rep := read(t, &r, env)
	if s != nil || len(rep.List) != 1 || rep.List[0].What != "system.sound" || rep.List[0].Message != "requires pactl" {
		t.Fatalf("sound %+v, unavailable %+v", s, rep.List)
	}
	now = now.Add(5 * time.Second)
	if s, rep = read(t, &r, env); s != nil || len(rep.List) != 1 || run.Count("pactl") != 1 {
		t.Fatalf("within 10 s: sound %+v, unavailable %+v, calls %q", s, rep.List, run.Calls)
	}
	put(t, dir, []any{}, "pactl", sinksArgs...)
	put(t, dir, []any{}, "pactl", sourcesArgs...)
	put(t, dir, map[string]string{}, "pactl", infoArgs...)
	now = now.Add(5 * time.Second)
	if s, rep = read(t, &r, env); s == nil || len(rep.List) != 0 || s.Outputs == nil || s.Inputs == nil {
		t.Fatalf("after 10 s: sound %+v, unavailable %+v", s, rep.List)
	}
	put(t, dir, "Connection failure: Connection refused", "pactl", sinksArgs...)
	r.Reset()
	if s, rep = read(t, &r, env); s != nil || len(rep.List) != 1 || rep.List[0].Reason != "error" {
		t.Fatalf("bad JSON: %+v %+v", s, rep.List)
	}
}
