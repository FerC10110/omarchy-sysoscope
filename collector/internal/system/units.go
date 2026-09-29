package system

import "github.com/FerC10110/omarchy-sysoscope/collector/internal/source"

// WatchedUnits son las unidades que se piden a systemd1 en una sola llamada
// con lo lento: la del reloj (timesyncd) y las del firewall (Seguridad):
// ufw, firewalld, nftables e iptables/ip6tables (las unidades de
// iptables-restore que trae el paquete iptables de Arch).
var WatchedUnits = []string{ntpUnit, "ufw.service", "firewalld.service", "nftables.service", "iptables.service", "ip6tables.service"}

// Units es el ActiveState ("active", "inactive", "activating", …) de cada
// unidad de WatchedUnits que systemd tiene cargada: sin las que no existen
// ni las enmascaradas. nil: systemd1 no respondió.
type Units map[string]string

// ListUnits hace la llamada ListUnitsByNames a systemd1 (que siempre corre:
// no activa nada). Es de solo consulta.
func ListUnits(env source.Env) Units {
	body, err := env.Bus.Call(true, systemd, systemdPath, "org.freedesktop.systemd1.Manager.ListUnitsByNames", WatchedUnits)
	if err != nil || len(body) == 0 {
		return nil
	}
	u := Units{}
	list, _ := body[0].([]any)
	for _, it := range list {
		f, ok := it.([]any)
		if !ok || len(f) < 4 {
			continue
		}
		name, _ := f[0].(string)
		load, _ := f[2].(string)
		active, _ := f[3].(string)
		if name != "" && load == "loaded" {
			u[name] = active
		}
	}
	return u
}
