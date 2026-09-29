package view

import (
	"fmt"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// IDs de las secciones, en el orden de la barra lateral.
var IDs = []string{"summary", "cpu", "memory", "gpu", "storage", "network", "sensors", "power", "about", "maintenance", "apps", "devices", "security"}

// builders arma cada vista; las tareas de cada grupo de secciones las registran.
var builders = map[string]func(s *model.Snapshot, o Options) []Block{}

// actions devuelve las acciones de una vista.
var actions = map[string]func(s *model.Snapshot, o Options) []Action{}

// tops agrega bloques al final de una vista: el top de apps (tops.go).
var tops = map[string]func(s *model.Snapshot, o Options) []Block{}

func title(id, lang string) string {
	switch id {
	case "summary":
		return i18n.T(lang, "Summary")
	case "cpu":
		return i18n.T(lang, "CPU")
	case "memory":
		return i18n.T(lang, "Memory")
	case "gpu":
		return i18n.T(lang, "GPU")
	case "storage":
		return i18n.T(lang, "Disks")
	case "network":
		return i18n.T(lang, "Network")
	case "sensors":
		return i18n.T(lang, "Sensors")
	case "power":
		return i18n.T(lang, "Power")
	case "about":
		return i18n.T(lang, "About")
	case "maintenance":
		return i18n.T(lang, "Maintenance")
	case "apps":
		return i18n.T(lang, "Applications")
	case "devices":
		return i18n.T(lang, "Devices")
	case "security":
		return i18n.T(lang, "Security")
	}
	return id
}

func group(id string) string {
	switch id {
	case "summary":
		return ""
	case "about", "maintenance", "apps", "devices", "security":
		return "system"
	}
	return "components"
}

// Sections devuelve la barra lateral con los badges de lo que requiere atención.
func Sections(s *model.Snapshot, o Options) []Section {
	out := make([]Section, 0, len(IDs))
	for _, id := range IDs {
		out = append(out, Section{ID: id, Title: title(id, o.Lang), Group: group(id), Badge: badge(id, s, o)})
	}
	return out
}

func badge(id string, s *model.Snapshot, o Options) int {
	n := 0
	switch id {
	case "summary":
		if warnings != nil {
			n = len(warnings(s, o))
		}
	case "storage":
		if s.Storage != nil {
			for _, d := range s.Storage.Disks {
				if diskAttention(d.Smart) {
					n++
				}
			}
		}
	case "about":
		if s.System != nil {
			n += len(s.System.FailedUnits)
		}
		n += firmwareUpdates(s)
		if clockWarning(s, o.Lang) != "" {
			n++
		}
	case "security":
		if s.CPU != nil {
			for _, v := range s.CPU.Vulnerabilities {
				if v.State == "vulnerable" {
					n++
				}
			}
		}
	}
	for _, a := range attention {
		if a.section == id {
			n += len(a.warn(s, o.Lang))
		}
	}
	return n
}

// diskAttention: SMART dice que falla, trae advertencias, o hay sectores malos
// o errores de medio (el primer aviso de un disco que se degrada).
func diskAttention(m *model.Smart) bool {
	return m != nil && (m.Failing || len(m.Warnings) > 0 || (m.BadSectors != nil && *m.BadSectors > 0) ||
		(m.MediaErrors != nil && *m.MediaErrors > 0))
}

// warnings la conecta la vista del Resumen.
var warnings func(s *model.Snapshot, o Options) []Block

// attention son los avisos de las secciones de las tandas 4–8: cada
// entrada da los avisos de su sección (Links con level y link ya puestos)
// a partir de lo que se lee también con la sección cerrada. Warnings los
// suma al Resumen, en este orden, y badge cuenta los de cada sección: una
// sola fuente de verdad, como clockWarning.
var attention = []struct {
	section string
	warn    func(s *model.Snapshot, lang string) []Block
}{
	{"maintenance", maintenanceWarnings},
	{"memory", memoryWarnings},
	{"security", securityWarnings},
	{"devices", devicesWarnings},
}

func firmwareUpdates(s *model.Snapshot) int {
	n := 0
	if s.Firmware != nil {
		for _, d := range s.Firmware.Devices {
			if d.Available != "" {
				n++
			}
		}
	}
	return n
}

// Build arma la vista id. Un id desconocido es un error.
func Build(id string, s *model.Snapshot, o Options) (*View, error) {
	b, ok := builders[id]
	if !ok {
		return nil, fmt.Errorf("unknown section %q", id)
	}
	v := &View{Schema: model.Schema, ID: id, Title: title(id, o.Lang), Blocks: b(s, o), Actions: []Action{}}
	if v.Blocks == nil {
		v.Blocks = []Block{}
	}
	if t, ok := tops[id]; ok {
		v.Blocks = append(v.Blocks, t(s, o)...)
	}
	if h, ok := heroes[id]; ok {
		if hb := h(s, o); hb != nil {
			v.Blocks = append([]Block{*hb}, v.Blocks...)
		}
	}
	if a, ok := actions[id]; ok {
		v.Actions = a(s, o)
	}
	return v, nil
}

// Unavailable devuelve "not available: <mensaje>" para el primer faltante cuyo
// What empieza con prefix, o nil si no hay.
func Unavailable(s *model.Snapshot, prefix string) *Block {
	lang := langOf(s)
	for _, u := range s.Unavailable {
		if u.What == prefix || strings.HasPrefix(u.What, prefix+".") {
			b := Text(i18n.F(lang, "not available: %s", u.Message), "warn")
			return &b
		}
	}
	return nil
}

// langOf es el idioma del snapshot (sus mensajes ya vienen traducidos).
func langOf(s *model.Snapshot) string {
	if s.Lang == "" {
		return "en"
	}
	return s.Lang
}
