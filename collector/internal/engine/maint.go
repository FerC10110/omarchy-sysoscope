package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/maint"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// maintJob dice qué lee una vuelta de maintTick; maintResult es lo leído.
type maintJob struct {
	caches, snaps bool
	health        bool   // errores, crashes y OOM (Mantenimiento abierta)
	oom           bool   // solo los OOM (Memoria abierta, o un oom_kill nuevo)
	kills         uint64 // oom_kill al empezar: el journal del kernel solo si es > 0
}

type maintResult struct {
	job       maintJob
	at        time.Time
	caches    []model.CacheDir
	unCaches  []model.Unavailable
	snapshots *model.Snapshots
	unSnaps   []model.Unavailable
	health    *model.Health
	unHealth  []model.Unavailable
}

// maintTick lee, en su gorutina en el stream (nunca dos a la vez; el
// resultado se aplica en la muestra siguiente) y en línea en un comando de
// una sola muestra:
//   - con Mantenimiento abierta: las cachés (al entrar, cada cachesEvery y
//     con refresh), los snapshots (al entrar y con refresh) y la salud
//     (cada healthEvery y con refresh);
//   - con Memoria abierta: solo los procesos matados por falta de memoria,
//     con el ritmo de la salud;
//   - con cualquiera de las dos, los OOM enseguida si oom_kill cambió (a lo
//     sumo cada oomKillEvery; una muestra sin oom_kill no es un cambio).
//
// Fuera de esas secciones no corre nada y se conserva lo último leído.
func (e *Engine) maintTick(now time.Time) {
	e.maintMu.Lock()
	e.applyMaint()
	open := e.opts.Focus == "maintenance"
	if (!open && e.opts.Focus != "memory") || e.maintRunning {
		e.maintMu.Unlock()
		return
	}
	// Una muestra sin oom_kill no es un cambio: se sigue con lo visto.
	kills := e.oomSeen
	if e.oomKills != nil {
		kills = *e.oomKills
	}
	job := maintJob{kills: kills}
	if open {
		job.caches = e.cachesDue || e.cachesAt.IsZero() || now.Sub(e.cachesAt) >= cachesEvery
		job.snaps = e.snapsDue || !e.snapsRead
		job.health = e.healthDue || e.healthAt.IsZero() || now.Sub(e.healthAt) >= healthEvery
	}
	// Una muerte nueva se lee enseguida, pero a lo sumo cada oomKillEvery:
	// un bucle de muertes no corre journalctl en cada muestra.
	killed := kills != e.oomSeen && now.Sub(e.oomAt) >= oomKillEvery
	job.oom = !job.health && (e.oomDue || e.oomAt.IsZero() || now.Sub(e.oomAt) >= healthEvery || killed)
	if !job.caches && !job.snaps && !job.health && !job.oom {
		e.maintMu.Unlock()
		return
	}
	// Solo se da por hecho lo que corre: un refresh pedido en Memoria sigue
	// pendiente para las cachés, los snapshots y la salud de Mantenimiento.
	if job.caches {
		e.cachesDue = false
	}
	if job.snaps {
		e.snapsDue = false
	}
	if job.health {
		e.healthDue = false
	}
	if job.health || job.oom {
		e.oomDue, e.oomSeen = false, kills
	}
	e.maintRunning = true
	e.maintMu.Unlock()

	env, size, btrfs := e.env, e.opts.DirSize, maint.RootBtrfs(e.storage)
	run := func() *maintResult {
		res := &maintResult{job: job, at: now}
		if job.caches {
			r := &collect.Report{Lang: env.Lang}
			res.caches, res.unCaches = maint.Caches(env, size, r), r.List
		}
		if job.snaps {
			r := &collect.Report{Lang: env.Lang}
			res.snapshots, res.unSnaps = maint.Snapper(env, btrfs, r), r.List
		}
		if job.health || job.oom {
			r := &collect.Report{Lang: env.Lang}
			res.health, res.unHealth = maint.ReadHealth(env, job.health, job.kills, r), r.List
		}
		return res
	}
	if e.opts.Stream {
		go func() {
			res := run()
			e.maintMu.Lock()
			e.maintRes, e.maintRunning = res, false
			e.maintMu.Unlock()
		}()
		return
	}
	res := run()
	e.maintMu.Lock()
	e.maintRes, e.maintRunning = res, false
	e.applyMaint()
	e.maintMu.Unlock()
}

// applyMaint copia una lectura terminada. Se llama con maintMu tomado. Una
// lectura solo de OOM reemplaza los OOM y conserva errores y crashes.
func (e *Engine) applyMaint() {
	res := e.maintRes
	if res == nil {
		return
	}
	e.maintRes = nil
	if res.job.caches {
		e.caches, e.cachesAt, e.unCaches = res.caches, res.at, res.unCaches
	}
	if res.job.snaps {
		e.snaps, e.unSnaps, e.snapsRead = res.snapshots, res.unSnaps, true
	}
	switch {
	case res.job.health:
		e.health, e.unHealth = res.health, res.unHealth
		e.healthAt, e.oomAt = res.at, res.at
	case res.job.oom:
		h := model.Health{}
		if e.health != nil {
			h = *e.health
		}
		h.OOM = res.health.OOM
		e.health, e.oomAt = &h, res.at
		e.unHealth = append(withoutWhat(e.unHealth, "maintenance.health.oom"), res.unHealth...)
	}
}

// withoutWhat quita de list los faltantes de what.
func withoutWhat(list []model.Unavailable, what string) []model.Unavailable {
	var out []model.Unavailable
	for _, u := range list {
		if u.What != what {
			out = append(out, u)
		}
	}
	return out
}

// RefreshMaintenance hace que la próxima muestra con Mantenimiento abierta
// vuelva a medir las cachés y a leer los snapshots y la salud, y que la
// próxima con Memoria abierta relea los OOM (tecla r).
func (e *Engine) RefreshMaintenance() {
	e.maintMu.Lock()
	e.cachesDue, e.snapsDue, e.healthDue, e.oomDue = true, true, true, true
	e.maintMu.Unlock()
}

// CheckUpdates busca actualizaciones de los repos oficiales en segundo
// plano (checkupdates sale a internet; nunca el AUR). Una llamada mientras
// otra corre no hace nada; el resultado se guarda en updates.json y llega
// en la primera muestra después de que termina.
func (e *Engine) CheckUpdates() {
	e.updMu.Lock()
	defer e.updMu.Unlock()
	if e.updRunning {
		return
	}
	e.updRunning = true
	env, db := e.env, e.opts.CheckupDB
	if db == "" {
		db = maint.CheckupDB(os.Getenv, os.Getuid())
	}
	go func() {
		u := maint.CheckUpdates(env, db)
		SaveUpdates(env.Cache, u)
		e.updMu.Lock()
		e.updRes, e.updRunning = u, false
		e.updMu.Unlock()
	}()
}

// UpdatesRunning dice si hay una comprobación en curso.
func (e *Engine) UpdatesRunning() bool {
	e.updMu.Lock()
	defer e.updMu.Unlock()
	return e.updRunning
}

// SaveUpdates guarda una comprobación en <cache>/updates.json (la leen el
// stream del panel, el de lazylazy y el próximo arranque). Running y Stale
// no se guardan: se calculan.
func SaveUpdates(cache string, u *model.Updates) error {
	if cache == "" || u == nil {
		return nil
	}
	c := *u
	c.Running, c.Stale = false, false
	data, err := json.Marshal(&c)
	if err != nil {
		return err
	}
	return WriteAtomic(filepath.Join(cache, maint.UpdatesFile), data)
}

// loadUpdates relee updates.json si cambió su mtime: también lo escribe
// otro proceso (la acción de lazylazy o el stream del panel).
func (e *Engine) loadUpdates() {
	if e.env.Cache == "" {
		return
	}
	p := filepath.Join(e.env.Cache, maint.UpdatesFile)
	fi, err := os.Stat(p)
	if err != nil || fi.ModTime().Equal(e.updMod) {
		return
	}
	e.updMod = fi.ModTime()
	if u, err := maint.LoadUpdates(p); err == nil {
		e.updates = u
	}
}

// maintenance arma Snapshot.Maintenance con lo último de cada ritmo.
func (e *Engine) maintenance() *model.Maintenance {
	e.updMu.Lock()
	if e.updRes != nil {
		e.updates, e.updRes = e.updRes, nil
	}
	running := e.updRunning
	e.updMu.Unlock()
	m := &model.Maintenance{Reboot: e.reboot, Upgrade: e.upgrade, Caches: e.caches, CachesAt: e.cachesAt, Snapshots: e.snaps, Health: e.health}
	if e.updates != nil || running {
		u := model.Updates{Repo: []model.PkgUpdate{}}
		if e.updates != nil {
			u = *e.updates
		}
		u.Running, u.Stale = running, maint.Stale(&u, e.upgrade)
		m.Updates = &u
	}
	return m
}
