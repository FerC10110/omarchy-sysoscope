package view

import (
	"slices"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func TestAppsSectionIsListed(t *testing.T) {
	secs := Sections(appsSnap(), Options{Lang: "es"})
	i := slices.IndexFunc(secs, func(s Section) bool { return s.ID == "apps" })
	if i < 1 || secs[i-1].ID != "maintenance" || secs[i].Group != "system" || secs[i].Title != "Aplicaciones" {
		t.Fatalf("sections = %+v", secs)
	}
}

func TestAppsViewGroupsWindowsByApp(t *testing.T) {
	v, err := Build("apps", appsSnap(), Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Blocks[0].Type != "hero" {
		t.Fatalf("first block = %s", v.Blocks[0].Type)
	}
	g := findGroup(v.Blocks, "Chromium")
	if len(g.Blocks) != 2 || !strings.Contains(g.Blocks[0].Text, "12%") || !strings.Contains(g.Blocks[0].Text, "approx.") {
		t.Fatalf("Chromium group = %+v", g)
	}
	tb := g.Blocks[1]
	rows := tableRows(tb)
	if !tb.Selectable || len(rows) != 2 || rows[0].ID != "0xa1:20" || rows[1].ID != "0xa2" || rows[0].Cells[0] != "● GitHub" || rows[1].Cells[3] != "fullscreen" {
		t.Fatalf("Chromium table = %+v", tb)
	}
	k := tableRows(findGroup(v.Blocks, "kitty").Blocks[1])
	if len(k) != 1 || !slices.Equal(k[0].Flags, []string{"protected"}) || k[0].Cells[3] != "floating, pinned" {
		t.Fatalf("kitty rows = %+v", k)
	}
}

// windowlessRows son las filas de la tabla de apps sin ventana.
func windowlessRows(t *testing.T, s *model.Snapshot) []Row {
	t.Helper()
	v, err := Build("apps", s, Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range v.Blocks {
		if b.Type == "table" && b.Title == "Without a window: the heaviest" {
			return tableRows(b)
		}
	}
	return nil
}

func TestAppsViewTopWithoutWindow(t *testing.T) {
	rows := windowlessRows(t, appsSnap())
	if len(rows) != 4 || rows[0].ID != "app:comm:syncthing" || rows[1].ID != "app:comm:Hyprland" {
		t.Fatalf("rows = %+v", rows)
	}
	if !slices.Equal(rows[0].Flags, []string{"nowindow"}) || !slices.Equal(rows[1].Flags, []string{"protected", "nowindow"}) {
		t.Fatalf("flags = %v / %v", rows[0].Flags, rows[1].Flags)
	}
}

// Una app congelada sin ventana usa 0 % de CPU: igual queda en la tabla,
// después de las 3 que más usan, para poder reanudarla o cerrarla.
func TestAppsViewKeepsFrozenWithoutWindow(t *testing.T) {
	rows := windowlessRows(t, appsSnap())
	if len(rows) != 4 || rows[3].ID != "app:comm:make" || rows[3].Cells[0] != "❄ make" {
		t.Fatalf("rows = %+v", rows)
	}
	// Si ya está entre las 3, no se repite.
	s := appsSnap()
	s.Apps.List[5].CPUPct = 50
	s.Apps.List = append(s.Apps.List[:2], append([]model.App{s.Apps.List[5]}, s.Apps.List[2:5]...)...)
	rows = windowlessRows(t, s)
	if len(rows) != 3 || rows[0].ID != "app:comm:make" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestAppsViewAlwaysNotesTCP(t *testing.T) {
	v, _ := Build("apps", appsSnap(), Options{Lang: "en"})
	if last := v.Blocks[len(v.Blocks)-1]; !strings.Contains(last.Text, "TCP only") {
		t.Fatalf("last block = %+v", last)
	}
}

func TestAppsViewWithoutData(t *testing.T) {
	s := appsSnap()
	s.Apps = nil
	v, _ := Build("apps", s, Options{Lang: "en"})
	if len(v.Blocks) == 0 || v.Blocks[0].Text != "Reading the applications…" {
		t.Fatalf("blocks = %+v", v.Blocks)
	}
}

func TestAppsActions(t *testing.T) {
	v, _ := Build("apps", appsSnap(), Options{Lang: "en"})
	if len(v.Actions) != 20 {
		t.Fatalf("%d actions", len(v.Actions))
	}
	by := map[string]Action{}
	for _, a := range v.Actions {
		by[a.ID] = a
		if a.Target != "row" || a.Mode != "background" {
			t.Errorf("%s: target %q mode %q", a.ID, a.Target, a.Mode)
		}
	}
	if a := by["window.to_monitor"]; a.Input == nil || a.Input.Type != "choice" || !slices.Equal(a.Input.Options, []string{"DP-1", "HDMI-A-1"}) {
		t.Fatalf("to_monitor = %+v", a)
	}
	if a := by["window.to_workspace"]; a.Input == nil || a.Input.Type != "number" {
		t.Fatalf("to_workspace = %+v", a)
	}
	if a := by["window.resize"]; a.Input.Default != "70%" || len(a.Input.Options) != 3 {
		t.Fatalf("resize = %+v", a.Input)
	}
	if a := by["window.close"]; a.Confirm != "Close this window?" || !slices.Equal(a.Hide, []string{"nowindow"}) {
		t.Fatalf("close = %+v", a)
	}
	if a := by["app.kill"]; a.Key != "K" || !slices.Equal(a.Hide, []string{"protected"}) {
		t.Fatalf("kill = %+v", a)
	}
	if a := by["app.btop"]; len(a.Hide) != 0 {
		t.Fatalf("btop = %+v", a)
	}
	s := appsSnap()
	s.Apps.Monitors = s.Apps.Monitors[:1]
	v, _ = Build("apps", s, Options{Lang: "en"})
	if len(v.Actions) != 19 || slices.ContainsFunc(v.Actions, func(a Action) bool { return a.ID == "window.to_monitor" }) {
		t.Fatal("with one monitor there is nowhere to move a window")
	}
}

func TestAppsHero(t *testing.T) {
	v, _ := Build("apps", appsSnap(), Options{Lang: "en"})
	h := v.Blocks[0]
	if len(h.Blocks) != 4 || h.Blocks[0].Text != "2" || h.Blocks[1].Text != "3" || h.Lines[0] != "1 frozen" {
		t.Fatalf("hero = %+v", h)
	}
}

// withDisk le da disco a chromium (0.2 MB/s de lectura, 40 kB/s de
// escritura), a kitty (0 y 0) y a syncthing (solo escritura); el resto queda
// sin dato.
func withDisk() *model.Snapshot {
	s := appsSnap()
	set := func(i int, r, w float64) { s.Apps.List[i].DiskReadBps, s.Apps.List[i].DiskWriteBps = &r, &w }
	set(0, 200000, 40000) // Chromium
	set(1, 0, 0)          // kitty
	set(2, 0, 500000)     // syncthing
	return s
}

func TestAppsViewDiskLine(t *testing.T) {
	v, _ := Build("apps", withDisk(), Options{Lang: "en"})
	if line := findGroup(v.Blocks, "Chromium").Blocks[0].Text; !strings.Contains(line, "Disk R 200.0 kB/s W 40.0 kB/s") {
		t.Fatalf("Chromium line = %q", line)
	}
	// Con dato y las dos en 0, la línea no nombra el disco.
	if line := findGroup(v.Blocks, "kitty").Blocks[0].Text; strings.Contains(line, "Disk") {
		t.Fatalf("kitty line = %q", line)
	}
	// Sin dato tampoco.
	v, _ = Build("apps", appsSnap(), Options{Lang: "en"})
	if line := findGroup(v.Blocks, "Chromium").Blocks[0].Text; strings.Contains(line, "Disk") {
		t.Fatalf("Chromium line without data = %q", line)
	}
}

func TestAppsViewDiskColumn(t *testing.T) {
	v, _ := Build("apps", withDisk(), Options{Lang: "en"})
	var tb Block
	for _, b := range v.Blocks {
		if b.Type == "table" && b.Title == "Without a window: the heaviest" {
			tb = b
		}
	}
	if !slices.Equal(tb.Columns, []string{"App", "CPU", "Memory", "Disk", "Processes"}) {
		t.Fatalf("columns = %v", tb.Columns)
	}
	rows := tableRows(tb)
	// syncthing escribe 500 kB/s; Hyprland no tiene dato.
	if rows[0].ID != "app:comm:syncthing" || rows[0].Cells[3] != "500.0 kB/s" || rows[0].Cells[4] != "1" {
		t.Fatalf("syncthing row = %+v", rows[0])
	}
	if rows[1].ID != "app:comm:Hyprland" || rows[1].Cells[3] != "—" {
		t.Fatalf("Hyprland row = %+v", rows[1])
	}
	// Con dato en 0: "0 B/s", no "—".
	s := withDisk()
	s.Apps.List[2].DiskWriteBps = new(float64)
	if r := windowlessRows(t, s); r[0].Cells[3] != "0 B/s" {
		t.Fatalf("syncthing at 0 = %q", r[0].Cells[3])
	}
}

// La nota del disco va siempre en Aplicaciones, antes de la de TCP (que
// sigue última). La otra mitad: el top de Red reusa la de TCP y no debe
// arrastrar la del disco.
func TestAppsViewNotesDisk(t *testing.T) {
	v, _ := Build("apps", appsSnap(), Options{Lang: "en"})
	n := len(v.Blocks)
	if n < 2 || v.Blocks[n-2].Text != "Disk per app counts what reaches the disk; reads served from memory are not included." ||
		!strings.Contains(v.Blocks[n-1].Text, "TCP only") {
		t.Fatalf("last blocks = %+v", v.Blocks[max(0, n-2):])
	}
	for _, text := range blockTexts(topOf(t, "network").Blocks) {
		if strings.Contains(text, "Disk per app") {
			t.Fatalf("the Network top has the disk note: %q", text)
		}
	}
}

func TestAppsViewDiskUnavailable(t *testing.T) {
	s := appsSnap()
	s.Unavailable = []model.Unavailable{unavailable("apps.disk", "the disk use of each process could not be read")}
	v, _ := Build("apps", s, Options{Lang: "en"})
	found := false
	for _, b := range v.Blocks {
		found = found || (b.Level == "warn" && b.Text == "not available: the disk use of each process could not be read")
	}
	if !found {
		t.Fatalf("no apps.disk line in %+v", v.Blocks)
	}
}

func TestAppsViewDiskSpanish(t *testing.T) {
	s := withDisk()
	s.Lang = "es"
	v, _ := Build("apps", s, Options{Lang: "es"})
	if line := findGroup(v.Blocks, "Chromium").Blocks[0].Text; !strings.Contains(line, "Disco L 200.0 kB/s E 40.0 kB/s") {
		t.Fatalf("Chromium line = %q", line)
	}
	n := len(v.Blocks)
	if v.Blocks[n-2].Text != "El disco por app cuenta lo que llega al disco: no incluye lo que se lee desde la memoria." {
		t.Fatalf("disk note = %q", v.Blocks[n-2].Text)
	}
	for _, b := range v.Blocks {
		if b.Type == "table" && b.Title == "Sin ventana: lo que más consume" && !slices.Contains(b.Columns, "Disco") {
			t.Fatalf("columns = %v", b.Columns)
		}
	}
}
