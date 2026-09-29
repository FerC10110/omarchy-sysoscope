package power

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/actlog"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// actionTimeout: lazylazy corta una acción en segundo plano a los 5 s.
const actionTimeout = 4 * time.Second

var ids = []string{"power.profile", "power.profile_other"}

// Known dice si id es una acción de Energía. Todas son de la sección (sin
// fila): `action ID PERFIL`.
func Known(id string) bool { return slices.Contains(ids, id) }

// Run corre la acción de Energía id con input (el perfil) y la anota en
// actions.log. Devuelve el texto para la barra de estado.
func Run(env source.Env, id, target, input string) (string, error) {
	out, err := run(env, id, strings.TrimSpace(input))
	actlog.Write(env, id, target, input, out, err)
	return out, err
}

func run(env source.Env, id, input string) (string, error) {
	l := env.Lang
	if !Known(id) {
		return "", errors.New(i18n.F(l, "unknown action %q", id))
	}
	// Se lee ahora: el perfil, la lista y la corriente pudieron cambiar desde
	// la muestra.
	var p model.Power
	batteries(env, &p)
	profile(env, &p, &collect.Report{Lang: l})
	if len(p.Profiles) == 0 {
		return "", errors.New(i18n.T(l, "power-profiles-daemon is not available"))
	}
	if !slices.Contains(p.Profiles, input) {
		return "", errors.New(i18n.F(l, "unknown power profile %q; one of: %s", input, strings.Join(p.Profiles, ", ")))
	}
	src := Source(&p)
	if id == "power.profile_other" {
		return saveOther(env, &p, src, input)
	}
	if !p.SetScript {
		if err := command(env, "powerprofilesctl", "set", input); err != nil {
			return "", err
		}
		return i18n.F(l, "Power profile set to %s (not remembered: %s is missing)", input, setScript), nil
	}
	if err := command(env, setScript, src, input); err != nil {
		return "", err
	}
	if src == "battery" {
		return i18n.F(l, "Power profile set to %s (remembered on battery)", input), nil
	}
	return i18n.F(l, "Power profile set to %s (remembered while plugged in)", input), nil
}

// saveOther guarda el perfil de la otra fuente sin cambiar el activo:
// omarchy-powerprofiles-set siempre corre powerprofilesctl set, así que se
// escribe su archivo de estado directo. Sin el script, nadie aplicaría lo
// guardado.
func saveOther(env source.Env, p *model.Power, src, prof string) (string, error) {
	l := env.Lang
	switch {
	case !p.SetScript:
		return "", errors.New(i18n.F(l, "requires %s", setScript))
	case !HasBattery(p):
		return "", errors.New(i18n.T(l, "there is no battery"))
	}
	other := Other(src)
	if err := writeState(env, other, prof); err != nil {
		return "", errors.New(i18n.F(l, "could not save the profile: %v", err))
	}
	if other == "battery" {
		return i18n.F(l, "Saved %s for when it runs on battery", prof), nil
	}
	return i18n.F(l, "Saved %s for when it is plugged in", prof), nil
}

// writeState escribe "<perfil>\n" en <estado>/<fuente>: un temporal oculto
// en el mismo directorio y rename (nadie lee un archivo a medias).
func writeState(env source.Env, src, prof string) error {
	dir := env.FS.Real(StateDir(env))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+src+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.WriteString(prof + "\n")
	cerr := f.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp, 0o644)); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, src)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// command corre una orden corta (hasta actionTimeout) y devuelve su error legible.
func command(env source.Env, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()
	out, err := env.Run.Run(ctx, name, args...)
	if err != nil {
		return errors.New(message(env.Lang, name, out, err))
	}
	return nil
}

// message es el error de una orden en una línea: falta, no respondió a
// tiempo, o la primera línea no vacía de su stderr (o de su salida).
func message(l, name string, out []byte, err error) string {
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return i18n.F(l, "requires %s", name)
	case errors.Is(err, context.DeadlineExceeded):
		return i18n.T(l, "timed out")
	}
	text := out
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(bytes.TrimSpace(ee.Stderr)) > 0 {
		text = ee.Stderr
	}
	for _, line := range strings.Split(string(text), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}
