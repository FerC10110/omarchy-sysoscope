package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/ondisk"
)

// staticCache es el inventario que no cambia durante un arranque. Se
// invalida cuando cambia el boot_id o el schema, y si no tiene la marca
// private (uno de antes, que podía guardar el hostname y las series de los
// monitores): se vuelve a leer todo y se reescribe sin ellos.
type staticCache struct {
	Schema      int                 `json:"schema"`
	Private     int                 `json:"private"` // cachePrivate: sin lo que identifica (ondisk.System)
	BootID      string              `json:"boot_id"`
	Lang        string              `json:"lang"` // los mensajes de Unavailable vienen traducidos
	CPU         *model.CPU          `json:"cpu"`
	Modules     *model.Memory       `json:"memory_modules"`
	GPUs        []model.GPU         `json:"gpus_static"`
	System      *model.System       `json:"system_static"`
	Unavailable []model.Unavailable `json:"unavailable,omitempty"`
}

func (e *Engine) bootID() string {
	id, _ := e.env.FS.Read("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(id)
}

func (e *Engine) loadCache(bootID string) *staticCache {
	if bootID == "" || e.env.Cache == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(e.env.Cache, "static.json"))
	if err != nil {
		return nil
	}
	var c staticCache
	if json.Unmarshal(data, &c) != nil || c.Schema != model.Schema || c.Private != cachePrivate || c.BootID != bootID || c.Lang != e.env.Lang ||
		c.CPU == nil || c.Modules == nil || c.System == nil {
		return nil
	}
	return &c
}

// cachePrivate es la versión de lo que el caché deja afuera.
const cachePrivate = 1

// saveCache guarda el inventario sin lo que identifica al equipo (el
// hostname y las series de los monitores: al cargarlo se releen con
// system.Identity).
func (e *Engine) saveCache(c *staticCache) {
	if c.BootID == "" || e.env.Cache == "" {
		return
	}
	d := *c
	d.Private, d.System = cachePrivate, ondisk.System(c.System)
	data, err := json.Marshal(&d)
	if err != nil {
		return
	}
	WriteAtomic(filepath.Join(e.env.Cache, "static.json"), data)
}

// WriteAtomic escribe con un temporal en el mismo directorio y rename.
func WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
