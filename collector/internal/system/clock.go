package system

import (
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
	"golang.org/x/sys/unix"
)

const (
	systemd     = "org.freedesktop.systemd1"
	systemdPath = "/org/freedesktop/systemd1"
	ntpUnit     = "systemd-timesyncd.service"
)

// clock arma la zona horaria y el NTP sin tocar org.freedesktop.timedate1:
// systemd-timedated se activa por D-Bus y se cierra a los ~30 s, así que
// leerlo cada minuto lo arrancaba cada vez (Starting/Started/Deactivated en
// el journal). Lee lo mismo que él: /etc/localtime, adjtimex y la unidad de
// systemd-timesyncd (de units, la llamada a systemd1 que comparte con el
// firewall). nil si ninguna de las tres fuentes dice nada: sin filas ni
// aviso.
func clock(env source.Env, units Units) *model.TimeSync {
	t := &model.TimeSync{Zone: zone(env)}
	synced, kernel := kernelSynced(env)
	t.Synced = synced
	t.CanNTP, t.NTP = timesyncd(env, units)
	if t.Zone == "" && !kernel && !t.CanNTP {
		return nil
	}
	return t
}

// zone es el destino de /etc/localtime después de ".../zoneinfo/", como lo
// calcula timedated. Si no es un enlace a zoneinfo, /etc/timezone; si no, "".
func zone(env source.Env) string {
	if l, err := env.FS.Link("/etc/localtime"); err == nil {
		if i := strings.Index(l, "zoneinfo/"); i >= 0 && (i == 0 || l[i-1] == '/') {
			if z := strings.Trim(l[i+len("zoneinfo/"):], "/"); z != "" {
				return z
			}
		}
	}
	z, _ := env.FS.Read("/etc/timezone")
	return strings.TrimSpace(z)
}

// kernelSynced lee el bit STA_UNSYNC del reloj del kernel (adjtimex de solo
// lectura), que es de donde timedated saca NTPSynchronized. ok=false si no
// se pudo leer.
func kernelSynced(env source.Env) (synced, ok bool) {
	if env.Timex == nil {
		return false, false
	}
	st, err := env.Timex()
	if err != nil {
		return false, false
	}
	return st&unix.STA_UNSYNC == 0, true
}

// timesyncd mira systemd-timesyncd.service como timedated: canNTP si la
// unidad está cargada (units solo trae las cargadas: ni not-found ni
// masked); ntp si está habilitada o activa. Sin systemd1: false, false.
func timesyncd(env source.Env, units Units) (canNTP, ntp bool) {
	active, ok := units[ntpUnit]
	if !ok {
		return false, false
	}
	state := ""
	if body, err := env.Bus.Call(true, systemd, systemdPath, "org.freedesktop.systemd1.Manager.GetUnitFileState", ntpUnit); err == nil && len(body) > 0 {
		state, _ = body[0].(string)
	}
	return true, state == "enabled" || state == "enabled-runtime" || active == "active" || active == "activating"
}
