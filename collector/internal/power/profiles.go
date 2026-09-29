package power

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// setScript es el script de Omarchy que cambia el perfil y lo recuerda para
// la fuente (ac o battery); su servicio de batería lo reaplica al enchufar o
// desenchufar.
const setScript = "omarchy-powerprofiles-set"

// getenv lee el entorno (los tests la reemplazan).
var getenv = os.Getenv

// StateDir es el directorio donde Omarchy guarda el perfil de cada fuente,
// el mismo que usa omarchy-powerprofiles-set:
// ${OMARCHY_POWERPROFILES_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/omarchy/powerprofiles}.
// Es una ruta del sistema real (sin la raíz del FS).
func StateDir(env source.Env) string {
	if d := getenv("OMARCHY_POWERPROFILES_STATE_DIR"); d != "" {
		return d
	}
	state := getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(env.Home, ".local", "state")
	}
	return filepath.Join(state, "omarchy", "powerprofiles")
}

// saved lee el perfil guardado para src ("ac" o "battery"): la primera línea
// del archivo, recortada, o "" si no hay.
func saved(env source.Env, src string) string {
	s, err := env.FS.Read(filepath.Join(StateDir(env), src))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// hasCommand busca name en los directorios absolutos del PATH, a través del
// FS (así un fixture decide si está).
func hasCommand(env source.Env, name string) bool {
	for _, d := range filepath.SplitList(getenv("PATH")) {
		if filepath.IsAbs(d) && env.FS.Exists(filepath.Join(d, name)) {
			return true
		}
	}
	return false
}

// HasBattery dice si la máquina tiene batería propia (no la de un periférico).
func HasBattery(p *model.Power) bool {
	for _, b := range p.Batteries {
		if !b.Peripheral {
			return true
		}
	}
	return false
}

// Source es la fuente de ahora: "battery" si se sabe que está desenchufada;
// si no, "ac" (sin fuente Mains, como un escritorio).
func Source(p *model.Power) string {
	if p.OnAC != nil && !*p.OnAC {
		return "battery"
	}
	return "ac"
}

// Other es la otra fuente.
func Other(src string) string {
	if src == "battery" {
		return "ac"
	}
	return "battery"
}
