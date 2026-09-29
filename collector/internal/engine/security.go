package engine

import (
	"slices"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/security"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/system"
)

// securityTick lee lo de Seguridad. Con lo lento, también con la sección
// cerrada (el Resumen avisa con ellos): los puertos que escuchan, los
// nombres de /etc/passwd, el firewall (sus archivos solo si cambiaron) y el
// cifrado. Con Seguridad abierta, además, los puertos cada everyNet y en la
// primera muestra con la sección recién abierta (e.privacy es nil hasta el
// primer recorrido), y, en un solo recorrido de /proc/<pid>/fd, la cámara y
// el programa de cada socket propio nuevo (Owners recuerda los ya buscados).
func (e *Engine) securityTick(m *model.Memory, slow, netSlow bool) {
	env, open := e.env, e.opts.Focus == "security"
	if netSlow {
		// Las direcciones de las interfaces se reescriben en su lugar cada
		// everyNet (network.Sampler): el alcance de cada puerto depende de
		// ellas. Rearmar cuesta ~25 µs, una vez cada 5 muestras.
		e.secDirty = true
	}
	if slow || (open && (netSlow || e.privacy == nil)) {
		r := &collect.Report{Lang: env.Lang}
		socks := security.ReadSockets(env, r)
		if (socks == nil) != (e.secSocks == nil) || !slices.Equal(socks, e.secSocks) {
			e.secDirty = true
		}
		e.secSocks = socks
		e.unSec = r.List
	}
	if slow {
		e.secDirty = true // usuarios, firewall, cifrado e interfaces
		e.secUsers = security.Users(env)
		e.secFw = e.fwR.Read(env, e.units)
		var fss []model.Filesystem
		if e.storage != nil {
			fss = e.storage.Filesystems
		}
		e.secVols, e.secSwaps = security.Crypt(env, fss, m.Swaps)
	}
	if open && (netSlow || e.privacy == nil) {
		walks := e.owners.Walks()
		cams := e.owners.Resolve(env.FS, security.OwnInodes(e.secSocks, env.User.UID), system.HasCamera(env))
		e.privacy = system.PrivacyWith(env, cams)
		if e.owners.Walks() != walks {
			e.secDirty = true // pudo encontrar programas nuevos
		}
	}
}

// securityState arma Snapshot.Security con lo último leído; los programas
// de los sockets propios, solo con la sección abierta. Solo cuando cambió
// algo de lo que depende (lo marca securityTick: puertos, lo lento, las
// direcciones de las interfaces cada everyNet, programas nuevos; o cambiaron
// el foco o el slice de interfaces): si no, devuelve el de la muestra
// anterior, que nadie modifica después de armado.
func (e *Engine) securityState() *model.Security {
	var ifaces []model.Iface
	if e.net != nil {
		ifaces = e.net.Interfaces
	}
	open := e.opts.Focus == "security"
	if e.secState != nil && !e.secDirty && open == e.secOpen && sameSlice(ifaces, e.secIfaces) {
		return e.secState
	}
	var program func(uint64) (string, bool)
	if open {
		program = e.owners.Program
	}
	ls := security.Listening(e.secSocks, ifaces, e.secUsers, e.env.User.UID, program)
	security.ApplyFirewall(ls, e.secFw)
	e.secState = &model.Security{Listening: ls, Firewall: e.secFw, Volumes: e.secVols, Swaps: e.secSwaps}
	e.secDirty, e.secOpen, e.secIfaces = false, open, ifaces
	e.secBuilds++
	return e.secState
}

// sameSlice dice si a y b son el mismo slice (no si tienen lo mismo): las
// interfaces se reemplazan enteras cuando se releen.
func sameSlice[T any](a, b []T) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}
