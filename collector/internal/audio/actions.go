package audio

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/actlog"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/wifi"
)

// actionTimeout: lazylazy corta una acción en segundo plano a los 5 s. Es el
// plazo de toda la acción: la lectura fresca, la orden y la confirmación.
const actionTimeout = 4 * time.Second

// step es lo que suben o bajan + y -, en puntos de %.
const step = 5

var ids = []string{"audio.default", "audio.mute", "audio.up", "audio.down", "audio.volume"}

// getenv lee el entorno (los tests la reemplazan).
var getenv = os.Getenv

// addrRe: una dirección Bluetooth. lazylazy no tiene hide, así que cualquier
// tecla de audio puede llegar con la fila de un dispositivo Bluetooth.
var addrRe = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)

// mu serializa las acciones de audio: mantener + o - manda ~25 teclas por
// segundo, cada una en su propia goroutine (ver stream.go). Sin esto, dos
// lecturas frescas en paralelo parten del mismo volumen de partida y una
// pisa el paso de la otra; con el mutex, cada acción termina (lectura,
// orden y confirmación) antes de que empiece la siguiente.
var mu sync.Mutex

// Known dice si id es una acción de audio. Todas son de fila (el Name de
// la salida o la entrada).
func Known(id string) bool { return slices.Contains(ids, id) }

// Run corre la acción de audio id sobre target (el nombre de pactl) con
// input (el volumen de audio.volume) y la anota en actions.log. Devuelve el
// texto para la barra de estado.
func Run(env source.Env, id, target, input string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	out, err := run(env, id, target, input)
	actlog.Write(env, id, target, input, out, err)
	return out, err
}

func run(env source.Env, id, target, input string) (string, error) {
	l := env.Lang
	if !Known(id) {
		return "", errors.New(i18n.F(l, "unknown action %q", id))
	}
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()
	// Se lee ahora: la salida pudo desenchufarse o cambiar desde la muestra.
	// Solo audio.default necesita el script de disponibilidad de Omarchy
	// (no se puede hacer predeterminada una salida escondida); las demás
	// buscan la fila por nombre exacto, visible o no, y así no lo corren en
	// cada tecla.
	var r Reader
	snd, err := r.read(ctx, env, id == "audio.default")
	if err != nil {
		return "", errors.New(wifi.Message(l, "pactl", nil, err))
	}
	n, sink := find(snd, target)
	if n == nil {
		if addrRe.MatchString(target) {
			return "", errors.New(i18n.F(l, "%s is not an audio device", target))
		}
		return "", errors.New(i18n.F(l, "no audio device %q (it may have been unplugged)", target))
	}
	name := wifi.Printable(n.Label)
	switch id {
	case "audio.default":
		return setDefault(ctx, env, n, sink)
	case "audio.mute":
		if err := command(ctx, env, "pactl", "set-"+kind(sink)+"-mute", n.Name, "toggle"); err != nil {
			return "", err
		}
		if n.Muted {
			return i18n.F(l, "Unmuted %s", name), nil
		}
		return i18n.F(l, "Muted %s", name), nil
	case "audio.up":
		if n.Volume >= 100 {
			return "", errors.New(i18n.F(l, "%s is already at 100%%", name))
		}
		return setVolume(ctx, env, n, sink, min(n.Volume+step, 100))
	case "audio.down":
		if n.Volume <= 0 {
			return "", errors.New(i18n.F(l, "%s is already at 0%%", name))
		}
		return setVolume(ctx, env, n, sink, min(max(n.Volume-step, 0), 100))
	case "audio.volume":
		v, err := strconv.Atoi(strings.TrimSpace(input))
		if err != nil || v < 0 || v > 100 {
			return "", errors.New(i18n.T(l, "the volume is a number from 0 to 100"))
		}
		return setVolume(ctx, env, n, sink, v)
	default:
		return "", errors.New(i18n.F(l, "unknown action %q", id))
	}
}

// find busca target entre las salidas (sink = true) y las entradas.
func find(snd *model.Sound, target string) (*model.AudioNode, bool) {
	for i := range snd.Outputs {
		if snd.Outputs[i].Name == target {
			return &snd.Outputs[i], true
		}
	}
	for i := range snd.Inputs {
		if snd.Inputs[i].Name == target {
			return &snd.Inputs[i], false
		}
	}
	return nil, false
}

func kind(sink bool) string {
	if sink {
		return "sink"
	}
	return "source"
}

// setVolume fija el volumen en v % con un valor absoluto, como las teclas
// de volumen de Omarchy (un "-5%" suelto pactl lo tomaría como una opción).
func setVolume(ctx context.Context, env source.Env, n *model.AudioNode, sink bool, v int) (string, error) {
	if err := command(ctx, env, "pactl", "set-"+kind(sink)+"-volume", n.Name, strconv.Itoa(v)+"%"); err != nil {
		return "", err
	}
	return i18n.F(env.Lang, "Volume of %s: %d%%", wifi.Printable(n.Label), v), nil
}

// setDefault la hace predeterminada con el script de Omarchy (también mueve
// lo que suena o graba) o, sin él, con pactl. El script siempre sale 0: se
// relee la predeterminada para saber si cambió.
func setDefault(ctx context.Context, env source.Env, n *model.AudioNode, sink bool) (string, error) {
	l := env.Lang
	name := wifi.Printable(n.Label)
	if n.Default {
		if sink {
			return "", errors.New(i18n.F(l, "%s is already the default output", name))
		}
		return "", errors.New(i18n.F(l, "%s is already the default input", name))
	}
	script := "omarchy-audio-output-set-default"
	if !sink {
		script = "omarchy-audio-input-set-default"
	}
	moves := isID(n.ID) && source.InPath(env.FS, getenv("PATH"), script)
	var err error
	if moves {
		err = command(ctx, env, script, n.ID, n.Name)
	} else {
		err = command(ctx, env, "pactl", "set-default-"+kind(sink), n.Name)
	}
	if err != nil {
		return "", err
	}
	// El script siempre sale 0: la relectura de la predeterminada es la única
	// prueba de que cambió. Si esa lectura falla o no es JSON, no hay cómo
	// confirmarlo: es un error, no un éxito sin confirmar.
	out, rerr := env.Run.Run(ctx, "pactl", "--format=json", "info")
	if rerr != nil {
		return "", errors.New(wifi.Message(l, "pactl", out, rerr))
	}
	var def defaults
	if json.Unmarshal(out, &def) != nil {
		return "", errors.New(i18n.F(l, "could not make %s the default", name))
	}
	now := def.Source
	if sink {
		now = def.Sink
	}
	if now != n.Name {
		return "", errors.New(i18n.F(l, "could not make %s the default", name))
	}
	switch {
	case sink && moves:
		return i18n.F(l, "Default output: %s", name), nil
	case sink:
		return i18n.F(l, "Default output: %s (playing streams were not moved: %s is missing)", name, script), nil
	case moves:
		return i18n.F(l, "Default input: %s", name), nil
	}
	return i18n.F(l, "Default input: %s (recording streams were not moved: %s is missing)", name, script), nil
}

// isID: el object.id de PipeWire es un número.
func isID(s string) bool {
	_, err := strconv.Atoi(s)
	return s != "" && err == nil
}

// command corre una orden dentro del plazo de la acción y devuelve su error
// legible.
func command(ctx context.Context, env source.Env, name string, args ...string) error {
	out, err := env.Run.Run(ctx, name, args...)
	if err != nil {
		return errors.New(wifi.Message(env.Lang, name, out, err))
	}
	return nil
}
