package view

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// at es la hora de un __REALTIME_TIMESTAMP como la muestra la vista.
func at(us int64) string { return time.UnixMicro(us).Local().Format("2006-01-02 15:04") }

func TestMaintenanceHealthView(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	v, err := Build("maintenance", s, Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if h := v.Blocks[0]; len(h.Blocks) != 4 || h.Blocks[3].Label != "Errors this boot" || h.Blocks[3].Text != "32" || h.Blocks[3].Level != "" {
		t.Fatalf("hero = %+v", h)
	}
	g := findGroup(v.Blocks, "Health")
	if g.Type != "group" || v.Blocks[len(v.Blocks)-1].Title != "Health" {
		t.Fatalf("health must be the last group: %+v", v.Blocks[len(v.Blocks)-1])
	}
	if got := kvValue(g.Blocks, "*", "Errors this boot"); got != "32 (kernel 23, wpa_supplicant 4, systemd-coredump 2)" {
		t.Errorf("errors = %q", got)
	}
	if got := kvValue(g.Blocks, "*", "Killed for lack of memory (since boot)"); got != "kernel 0 · systemd-oomd 0" {
		t.Errorf("oom = %q", got)
	}
	errs := findTable(g.Blocks, "Recent errors")
	rs := tableRows(errs)
	if len(rs) != 5 || !reflect.DeepEqual(rs[0].Cells, []string{at(1790151920000000), "kernel", "message 32"}) ||
		rs[4].Cells[1] != "systemd-coredump" || errs.Selectable || !reflect.DeepEqual(errs.Columns, []string{"Time", "Source", "Message"}) {
		t.Errorf("recent errors = %+v", errs)
	}
	cr := findTable(g.Blocks, "Crashes in the last 7 days (2)")
	crs := tableRows(cr)
	if len(crs) != 2 || !reflect.DeepEqual(crs[0].Cells, []string{at(1789677736672959), "1310465", "prog1", "SIGILL", "user"}) ||
		cr.Selectable || !reflect.DeepEqual(cr.Columns, []string{"Date", "PID", "Program", "Signal", "User"}) {
		t.Errorf("crashes = %+v", cr)
	}
	all := strings.Join(textsOf(g.Blocks), "\n")
	if !strings.Contains(all, "To see why one crashed: coredumpctl info <PID>") || strings.Contains(all, "Only your own messages") {
		t.Errorf("texts:\n%s", all)
	}
	if findTable(g.Blocks, "Killed for lack of memory").Type != "" {
		t.Error("no kills: no table")
	}
	walk(v.Blocks, func(b Block) {
		if b.Level == "warn" {
			t.Errorf("errors and crashes never warn: %+v", b)
		}
	})
	noEmptyValues(t, v)
}

func TestMaintenanceHealthCases(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	build := func() (*View, Block) {
		t.Helper()
		v, err := Build("maintenance", s, Options{Lang: "en"})
		if err != nil {
			t.Fatal(err)
		}
		return v, findGroup(v.Blocks, "Health")
	}
	h := s.Maintenance.Health
	h.Errors.Count, h.Errors.Capped, h.Errors.Partial = 1000, true, true
	h.Crashes = &model.Crashes{List: []model.Crash{}}
	base := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	h.OOM = &model.OOMList{Oomd: 1, List: []model.OOMKill{
		{Time: base.Add(time.Hour), Program: "firefox", By: "systemd-oomd"}, {Time: base, Program: "Web Content", By: "kernel"}}}
	one := uint64(1)
	s.Memory.OOMKills = &one
	v, g := build()
	if v.Blocks[0].Blocks[3].Text != "1000+" || !strings.HasPrefix(kvValue(g.Blocks, "*", "Errors this boot"), "1000+ (kernel 23") ||
		kvValue(g.Blocks, "*", "Killed for lack of memory (since boot)") != "kernel 1 · systemd-oomd not visible" {
		t.Errorf("capped (partial: systemd-oomd is not claimed): hero %+v, group %+v", v.Blocks[0].Blocks[3], g)
	}
	all := strings.Join(textsOf(g.Blocks), "\n")
	if !strings.Contains(all, "Only your own messages (join the wheel group to see the system's)") ||
		!strings.Contains(all, "No crashes in the last 7 days") || strings.Contains(all, "coredumpctl info") {
		t.Errorf("texts:\n%s", all)
	}
	kills := tableRows(findTable(g.Blocks, "Killed for lack of memory"))
	if len(kills) != 2 || !reflect.DeepEqual(kills[0].Cells, []string{base.Add(time.Hour).Local().Format("2006-01-02 15:04"), "firefox", "systemd-oomd"}) ||
		kills[1].Cells[2] != "kernel" {
		t.Errorf("kills = %+v", kills)
	}
	// Todavía sin leer.
	s.Maintenance.Health = nil
	v, g = build()
	if v.Blocks[0].Blocks[3].Text != "—" || !strings.Contains(strings.Join(textsOf(g.Blocks), "\n"), "Reading the journal…") {
		t.Errorf("not read yet: %+v / %+v", v.Blocks[0], g)
	}
	// Sin journalctl: el motivo, sin "Reading…".
	s.Unavailable = append(s.Unavailable, model.Unavailable{What: "maintenance.health.errors", Reason: "missing_tool", Message: "requires journalctl"})
	_, g = build()
	if all := strings.Join(textsOf(g.Blocks), "\n"); !strings.Contains(all, "Errors this boot: not available (requires journalctl)") || strings.Contains(all, "Reading") {
		t.Errorf("missing journalctl:\n%s", all)
	}
}

func TestMaintenanceHealthSpanish(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	v, err := Build("maintenance", s, Options{Lang: "es"})
	if err != nil {
		t.Fatal(err)
	}
	g := findGroup(v.Blocks, "Salud")
	if v.Blocks[0].Blocks[3].Label != "Errores en este arranque" || len(tableRows(findTable(g.Blocks, "Errores recientes"))) != 5 ||
		len(tableRows(findTable(g.Blocks, "Programas que fallaron en los últimos 7 días (2)"))) != 2 ||
		kvValue(g.Blocks, "*", "Errores en este arranque") == "" {
		t.Errorf("es = %+v", g)
	}
}

// Controller ruling (revisión de la tarea 5): el motor puede publicar
// Maintenance.Health con solo los OOM leídos (Memoria se abrió antes que
// Mantenimiento hiciera su primera lectura completa): Errors y Crashes
// quedan nil, sin ningún unavailable maintenance.health.*. La vista debe
// tratarlo como "todavía sin leer" (el mismo texto que Health == nil), no
// como cero errores ni como una falla. Lo de OOM que sí se leyó se muestra.
func TestMaintenanceHealthOOMOnly(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	base := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	s.Maintenance.Health = &model.Health{OOM: &model.OOMList{Oomd: 1, List: []model.OOMKill{
		{Time: base, Program: "firefox", By: "systemd-oomd"}}}}
	one := uint64(1)
	s.Memory.OOMKills = &one
	build := func() (*View, Block) {
		t.Helper()
		v, err := Build("maintenance", s, Options{Lang: "en"})
		if err != nil {
			t.Fatal(err)
		}
		return v, findGroup(v.Blocks, "Health")
	}
	v, g := build()
	if v.Blocks[0].Blocks[3].Text != "—" {
		t.Errorf("hero must not show 0 errors while unread: %+v", v.Blocks[0].Blocks[3])
	}
	if got := kvValue(g.Blocks, "*", "Errors this boot"); got != "" {
		t.Errorf("errors row must not show before it is read, got %q", got)
	}
	if all := strings.Join(textsOf(g.Blocks), "\n"); !strings.Contains(all, "Reading the journal…") {
		t.Errorf("errors/crashes not read yet must show the placeholder:\n%s", all)
	}
	if got := kvValue(g.Blocks, "*", "Killed for lack of memory (since boot)"); got != "kernel 1 · systemd-oomd 1" {
		t.Errorf("oom must still show: %q", got)
	}
	kills := tableRows(findTable(g.Blocks, "Killed for lack of memory"))
	if len(kills) != 1 || kills[0].Cells[1] != "firefox" {
		t.Errorf("oom kills must still show: %+v", kills)
	}
	walk(v.Blocks, func(b Block) {
		if b.Level == "warn" {
			t.Errorf("not read yet is not a warning: %+v", b)
		}
	})
	// Con un unavailable de errores: el motivo, sin "Reading…".
	s.Unavailable = append(s.Unavailable, model.Unavailable{What: "maintenance.health.errors", Reason: "missing_tool", Message: "requires journalctl"})
	_, g = build()
	if all := strings.Join(textsOf(g.Blocks), "\n"); !strings.Contains(all, "Errors this boot: not available (requires journalctl)") || strings.Contains(all, "Reading the journal…") {
		t.Errorf("missing journalctl must show the reason, not the placeholder:\n%s", all)
	}
}

// ReadOOM puede devolver la lista de systemd-oomd sola (el kernel falló):
// se muestra igual, con el motivo.
func TestMaintenanceHealthOOMKernelFailed(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	h := s.Maintenance.Health
	h.OOM = &model.OOMList{Oomd: 1, List: []model.OOMKill{{Program: "firefox", By: "systemd-oomd"}}}
	s.Unavailable = append(s.Unavailable, model.Unavailable{What: "maintenance.health.oom", Reason: "missing_tool", Message: "requires journalctl"})
	v, err := Build("maintenance", s, Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	g := findGroup(v.Blocks, "Health")
	kills := tableRows(findTable(g.Blocks, "Killed for lack of memory"))
	if len(kills) != 1 || kills[0].Cells[1] != "firefox" || kills[0].Cells[2] != "systemd-oomd" {
		t.Errorf("systemd-oomd kills must still show: %+v", kills)
	}
	if all := strings.Join(textsOf(g.Blocks), "\n"); !strings.Contains(all, "Killed for lack of memory: not available (requires journalctl)") {
		t.Errorf("kernel read failure reason must show:\n%s", all)
	}
}

func TestMemoryOOM(t *testing.T) {
	s, _ := snap(t, "threadripper")
	build := func(lang string) *View {
		t.Helper()
		v, err := Build("memory", s, Options{Lang: lang})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	badge := func() int {
		for _, sec := range Sections(s, Options{Lang: "en"}) {
			if sec.ID == "memory" {
				return sec.Badge
			}
		}
		return -1
	}
	v := build("en")
	if got := kvValue(v.Blocks, "Breakdown", "Killed for lack of memory (since boot)"); got != "0" || badge() != 0 || len(memoryWarnings(s, "en")) != 0 {
		t.Fatalf("no kills: row %q, badge %d", got, badge())
	}
	two := uint64(2)
	s.Memory.OOMKills = &two
	s.Maintenance = &model.Maintenance{Health: &model.Health{OOM: &model.OOMList{Oomd: 1, List: []model.OOMKill{
		{Program: "firefox", By: "systemd-oomd"}, {Program: "stress-ng", By: "kernel"},
		{Program: "Web Content", By: "kernel"}, {Program: "stress-ng", By: "kernel"}}}}}
	v = build("en")
	if got := kvValue(v.Blocks, "Breakdown", "Killed for lack of memory (since boot)"); got != "2 (stress-ng, Web Content) · 1 by systemd-oomd (firefox)" {
		t.Errorf("row = %q", got)
	}
	const warn = "2 processes were killed for lack of memory since boot"
	if b := body(v)[0]; b.Type != "text" || b.Text != warn || b.Level != "warn" {
		t.Errorf("first block = %+v", b)
	}
	found := false
	for _, w := range Warnings(s, Options{Lang: "en"}) {
		found = found || (w.Text == warn && w.Level == "warn" && w.Link == "memory")
	}
	if !found || badge() != 1 {
		t.Errorf("summary warning %v, badge %d", found, badge())
	}
	if b := body(build("es"))[0]; b.Text != "2 procesos terminados por falta de memoria desde el arranque" {
		t.Errorf("es = %q", b.Text)
	}
	one := uint64(1)
	s.Memory.OOMKills = &one
	if w := memoryWarnings(s, "en"); len(w) != 1 || w[0].Text != "1 process was killed for lack of memory since boot" {
		t.Errorf("singular = %+v", w)
	}
	// Sin nombres todavía (Memoria recién abierta): solo la cuenta.
	s.Maintenance = &model.Maintenance{}
	if got := kvValue(build("en").Blocks, "Breakdown", "Killed for lack of memory (since boot)"); got != "1" {
		t.Errorf("without names = %q", got)
	}
	// Sin /proc/vmstat: sin fila y sin aviso.
	s.Memory.OOMKills = nil
	v = build("en")
	if kvValue(v.Blocks, "Breakdown", "Killed for lack of memory (since boot)") != "" || badge() != 0 {
		t.Error("no oom_kill: no row, no badge")
	}
	noEmptyValues(t, v)
}

// M3: con varias partes de la salud sin leer se ve un texto por parte, cada
// uno con su parte delante (antes solo el primer motivo, sin decir de qué).
func TestMaintenanceHealthUnavailableParts(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	s.Maintenance.Health = &model.Health{OOM: &model.OOMList{List: []model.OOMKill{}}}
	s.Unavailable = append(s.Unavailable,
		model.Unavailable{What: "maintenance.health.errors", Reason: "missing_tool", Message: "requires journalctl"},
		model.Unavailable{What: "maintenance.health.crashes", Reason: "missing_tool", Message: "requires coredumpctl"})
	for _, h := range []*model.Health{s.Maintenance.Health, nil} {
		s.Maintenance.Health = h
		v, err := Build("maintenance", s, Options{Lang: "en"})
		if err != nil {
			t.Fatal(err)
		}
		all := strings.Join(textsOf(findGroup(v.Blocks, "Health").Blocks), "\n")
		if !strings.Contains(all, "Errors this boot: not available (requires journalctl)") ||
			!strings.Contains(all, "Crashes: not available (requires coredumpctl)") || strings.Contains(all, "Reading") {
			t.Errorf("health %v:\n%s", h != nil, all)
		}
	}
	v, err := Build("maintenance", s, Options{Lang: "es"})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(textsOf(findGroup(v.Blocks, "Salud").Blocks), "\n")
	if !strings.Contains(all, "Errores en este arranque: no disponible (requires journalctl)") ||
		!strings.Contains(all, "Programas que fallaron: no disponible (requires coredumpctl)") {
		t.Errorf("es:\n%s", all)
	}
}

// M4: con el journal parcial (el usuario no ve el del sistema) los OOM no
// afirman "systemd-oomd 0": sus muertes están en el journal del sistema.
// La nota va también con solo los OOM leídos.
func TestHealthPartialJournalOOM(t *testing.T) {
	const note = "Only your own messages (join the wheel group to see the system's)"
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	s.Maintenance.Health = &model.Health{OOM: &model.OOMList{Partial: true, List: []model.OOMKill{}}}
	v, err := Build("maintenance", s, Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	g := findGroup(v.Blocks, "Health")
	if got := kvValue(g.Blocks, "*", "Killed for lack of memory (since boot)"); got != "kernel 0 · systemd-oomd not visible" {
		t.Errorf("oom row = %q", got)
	}
	if all := strings.Join(textsOf(g.Blocks), "\n"); !strings.Contains(all, note) {
		t.Errorf("partial note with only the OOM read:\n%s", all)
	}
	// Memoria: la fila no dice nada de systemd-oomd como si fuera cero.
	two := uint64(2)
	s.Memory.OOMKills = &two
	v, err = Build("memory", s, Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if got := kvValue(v.Blocks, "Breakdown", "Killed for lack of memory (since boot)"); got != "2 · systemd-oomd not visible" {
		t.Errorf("memory row = %q", got)
	}
}

// Las palabras en castellano: "Terminados" (no "Cerrados"), "Quién" y
// "Fecha" donde la celda lleva fecha y hora.
func TestHealthSpanishWording(t *testing.T) {
	s := maintSnap(t, source.Fixture(t, "threadripper"))
	one := uint64(1)
	s.Memory.OOMKills = &one
	s.Maintenance.Health.OOM = &model.OOMList{List: []model.OOMKill{{Program: "stress-ng", By: "kernel"}}}
	v, err := Build("maintenance", s, Options{Lang: "es"})
	if err != nil {
		t.Fatal(err)
	}
	g := findGroup(v.Blocks, "Salud")
	if k := findTable(g.Blocks, "Terminados por falta de memoria"); !reflect.DeepEqual(k.Columns, []string{"Fecha", "Programa", "Quién"}) {
		t.Errorf("kills table = %+v", k)
	}
	if c := findTable(g.Blocks, "Programas que fallaron en los últimos 7 días (2)"); len(c.Columns) == 0 || c.Columns[0] != "Fecha" {
		t.Errorf("crashes table = %+v", c.Columns)
	}
	if kvValue(g.Blocks, "*", "Terminados por falta de memoria (desde el arranque)") == "" {
		t.Error("row label")
	}
	if w := memoryWarnings(s, "es"); len(w) != 1 || w[0].Text != "1 proceso terminado por falta de memoria desde el arranque" {
		t.Errorf("warning = %+v", w)
	}
}
