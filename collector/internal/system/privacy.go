package system

import (
	"slices"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/security"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Privacy dice qué procesos usan la cámara y el micrófono, con su propio
// recorrido de /proc/<pid>/fd (solo si hay algún video4linux). El engine
// usa PrivacyWith: el mismo recorrido resuelve también los sockets de
// Seguridad (security.Owners).
func Privacy(env source.Env) *model.Privacy {
	var cams []int
	if HasCamera(env) {
		_, cams = security.ScanFDs(env.FS, nil, true)
	}
	return PrivacyWith(env, cams)
}

// HasCamera dice si hay algún dispositivo video4linux: sin él no hace falta
// buscar quién usa la cámara.
func HasCamera(env source.Env) bool {
	devs, _ := env.FS.Glob("/sys/class/video4linux/video*")
	return len(devs) > 0
}

// PrivacyWith arma Privacy con los pids que tienen una cámara abierta (de
// un recorrido ya hecho). El micrófono sale de las capturas ALSA en curso
// (pocos archivos).
func PrivacyWith(env source.Env, camera []int) *model.Privacy {
	p := &model.Privacy{Camera: []string{}, Mic: []string{}}
	subs, _ := env.FS.Glob("/proc/asound/card*/pcm*c/sub*/status")
	for _, f := range subs {
		lines, err := env.FS.Lines(f)
		if err != nil || !slices.Contains(lines, "state: RUNNING") {
			continue
		}
		for _, l := range lines {
			if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "owner_pid" {
				p.MicActive = true
				if c := comm(env, strings.TrimSpace(v)); !audioServers[c] {
					p.Mic = append(p.Mic, c)
				}
			}
		}
	}
	for _, pid := range camera {
		p.Camera = append(p.Camera, comm(env, strconv.Itoa(pid)))
	}
	for _, l := range []*[]string{&p.Camera, &p.Mic} {
		slices.Sort(*l)
		*l = slices.Compact(*l)
	}
	return p
}

// audioServers tienen la captura ALSA en nombre de otra app: nombrarlos
// diría "pipewire" en vez de quién graba.
var audioServers = map[string]bool{"pipewire": true, "pipewire-pulse": true, "wireplumber": true, "pulseaudio": true}

func comm(env source.Env, pid string) string {
	if c, err := env.FS.Read("/proc/" + pid + "/comm"); err == nil && c != "" {
		return c
	}
	return "unknown"
}
