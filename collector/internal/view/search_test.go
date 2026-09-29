package view

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/engine"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// findHit devuelve el primer resultado igual a want, o false.
func findHit(hits []Hit, want Hit) bool {
	for _, h := range hits {
		if h == want {
			return true
		}
	}
	return false
}

func TestSearchFindsLabelValueAndCells(t *testing.T) {
	s, e := snap(t, "threadripper")
	o := Options{Lang: "en", Units: "c", Recent: e.Recent}

	// Un valor de kv: el modelo del procesador.
	hits := Search(s, o, "ryzen")
	if len(hits) == 0 || hits[0] != (Hit{Section: "cpu", SectionTitle: "CPU", Path: "Processor",
		Text: "Model: AMD Ryzen Threadripper 1920X 12-Core Processor"}) {
		t.Fatalf("ryzen = %+v", hits)
	}
	// Una etiqueta de kv.
	if h := Search(s, o, "MICROCODE"); !findHit(h, Hit{Section: "cpu", SectionTitle: "CPU", Path: "Processor", Text: "Microcode: 0x8001137"}) {
		t.Errorf("microcode = %+v", h)
	}
	// Una celda de una tabla que se puede elegir: trae el id de la fila.
	if h := Search(s, o, "powerd"); !findHit(h, Hit{Section: "about", SectionTitle: "About", Path: "Failed services",
		Text: "nvidia-powerd.service · system · NVIDIA Dynamic Boost Daemon", Row: "system:nvidia-powerd.service"}) {
		t.Errorf("powerd = %+v", h)
	}
	// Una tabla que no se elige: sin Row.
	if h := Search(s, o, "zram"); !findHit(h, Hit{Section: "memory", SectionTitle: "Memory", Path: "Swap devices",
		Text: "/dev/zram0 · zram (zstd, 2.0×) · 47.0 GiB · 0 B · 100"}) {
		t.Errorf("zram = %+v", h)
	}
	// La ruta junta los títulos de los bloques de arriba.
	if h := Search(s, o, "3666"); !findHit(h, Hit{Section: "storage", SectionTitle: "Disks",
		Path: "WDC WD120EFGX-68CPHN0 (sda) › Health", Text: "Power-on hours: 3666"}) {
		t.Errorf("3666 = %+v", h)
	}
	// El Resumen no se busca (repite las otras secciones).
	for _, h := range Search(s, o, "rtx 3080") {
		if h.Section == "summary" {
			t.Errorf("hit del Resumen: %+v", h)
		}
	}
}

func TestSearchFoldsAccents(t *testing.T) {
	s, _ := snap(t, "threadripper")
	o := Options{Lang: "es", Units: "c"}
	// "Mo\u0301dulos" ya es NFD (la "ó" como "o" más el diacrítico
	// combinante U+0301); esta otra usa el escape en el literal para que no
	// dependa de que el archivo se mantenga sin normalizar.
	for _, q := range []string{"MODULOS", "módulos", "Módulos", "Mo\u0301dulos"} {
		if h := Search(s, o, q); !findHit(h, Hit{Section: "memory", SectionTitle: "Memoria", Text: "Módulos"}) {
			t.Errorf("%q = %+v", q, h)
		}
	}
	if got := fold("Energía ÑANDÚ Über é"); got != "energia nandu uber e" {
		t.Errorf("fold = %q", got)
	}
	// "e\u0301" es NFD ("e" + el diacrítico combinante U+0301): ejercita
	// la rama de fold que saca los diacríticos combinantes (U+0300–U+036F).
	if got := fold("e\u0301"); got != "e" {
		t.Errorf("fold(NFD) = %q", got)
	}
}

func TestSearchShortQueryAndClip(t *testing.T) {
	s, _ := snap(t, "threadripper")
	for _, q := range []string{"", "a", "  a  ", "é"} {
		if h := Search(s, Options{Lang: "en"}, q); h == nil || len(h) != 0 {
			t.Errorf("%q = %#v, want []", q, h)
		}
	}
	h := Search(s, Options{Lang: "en"}, "retpolines")
	if len(h) == 0 || utf8.RuneCountInString(h[0].Text) != hitRunes || !strings.HasSuffix(h[0].Text, "…") {
		t.Fatalf("retpolines = %+v", h)
	}
}

func TestSearchCapsInOrder(t *testing.T) {
	s, _ := snap(t, "threadripper")
	old := builders["cpu"]
	t.Cleanup(func() { builders["cpu"] = old })
	builders["cpu"] = func(*model.Snapshot, Options) []Block {
		rows := make([]Row, 80)
		for i := range rows {
			rows[i] = Row{ID: fmt.Sprint(i), Cells: []string{fmt.Sprintf("fila %d", i)}}
		}
		return []Block{Table("T", []string{"c"}, rows, true)}
	}
	h := Search(s, Options{Lang: "en"}, "fila")
	if len(h) != searchMax || h[0].Text != "fila 0" || h[0].Row != "0" || h[49].Text != "fila 49" || h[0].Path != "T" {
		t.Fatalf("hits = %d: %+v … %+v", len(h), h[0], h[len(h)-1])
	}
}

func TestSearchSkipsBrokenSection(t *testing.T) {
	s, _ := snap(t, "threadripper")
	old := builders["gpu"]
	t.Cleanup(func() { builders["gpu"] = old })
	builders["gpu"] = func(*model.Snapshot, Options) []Block { panic("boom") }
	if h := Search(s, Options{Lang: "en"}, "ryzen"); len(h) == 0 || h[0].Section != "cpu" {
		t.Fatalf("hits = %+v", h)
	}
}

func TestSearchEmptySnapshot(t *testing.T) {
	if h := Search(nil, Options{Lang: "en"}, "cpu"); h == nil || len(h) != 0 {
		t.Errorf("nil = %#v", h)
	}
	_ = Search(&model.Snapshot{}, Options{Lang: "en"}, "cpu") // no entra en pánico
}

// BenchmarkSearch mide una búsqueda sobre el fixture threadripper (Task 5:
// tiene que quedar por debajo de 5 ms).
func BenchmarkSearch(b *testing.B) {
	env := source.Fixture(b, "threadripper")
	e := engine.New(env, engine.Options{Interval: 2 * time.Second, Statfs: fakeStatfs})
	defer e.Close()
	e.Tick()
	s := e.Tick()
	o := Options{Lang: "es", Units: "c", Recent: e.Recent}
	b.ResetTimer()
	for b.Loop() {
		Search(s, o, "ryzen")
	}
}
