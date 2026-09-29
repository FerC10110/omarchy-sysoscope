package view

import (
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

func table(v *View, title string) *Block {
	var out *Block
	walk(v.Blocks, func(b Block) {
		if b.Type == "table" && b.Title == title && out == nil {
			out = &b
		}
	})
	return out
}

func texts(v *View) map[string]string {
	out := map[string]string{}
	walk(v.Blocks, func(b Block) {
		if b.Type == "text" {
			out[b.Text] = b.Level
		}
	})
	return out
}

func TestAboutView(t *testing.T) {
	v := build(t, "about", "threadripper", Options{})
	if kvValue(v.Blocks, "System", "Omarchy") != "4.0.4-1" {
		t.Errorf("System kv = %+v", v.Blocks[0])
	}
	if f := table(v, "Failed services"); f == nil || len(f.Rows.([]Row)) != 1 {
		t.Errorf("failed services = %+v", f)
	}
	if len(v.Actions) != 1 || v.Actions[0].ID != "firmware.check" || v.Actions[0].Key != "f" {
		t.Errorf("actions = %+v", v.Actions)
	}
	noEmptyValues(t, v)
}

func TestDevicesView(t *testing.T) {
	v := build(t, "devices", "threadripper", Options{})
	usb := table(v, "USB")
	if usb == nil || len(usb.Rows.([]Row)) != 2 {
		t.Fatalf("usb = %+v", usb)
	}
	joined := ""
	for _, r := range usb.Rows.([]Row) {
		joined += strings.Join(r.Cells, " ") + "\n"
	}
	if !strings.Contains(joined, "Logitech") || !strings.Contains(joined, "AURA") {
		t.Errorf("usb rows = %s", joined)
	}
	m := table(v, "Monitors")
	if m == nil || len(m.Rows.([]Row)) != 1 || m.Rows.([]Row)[0].Cells[1] != "DELL S2721DGF" {
		t.Errorf("monitors = %+v", m)
	}
	if table(v, "PCI") == nil {
		t.Error("no PCI table")
	}
	noEmptyValues(t, v)
}

func TestSecurityView(t *testing.T) {
	s, _ := snap(t, "threadripper")
	v, _ := Build("security", s, Options{Lang: "en"})
	if _, ok := texts(v)["Checking camera and microphone…"]; !ok {
		t.Errorf("texts = %v", texts(v))
	}
	s.Privacy = &model.Privacy{Camera: []string{}, Mic: []string{"obs"}}
	v, _ = Build("security", s, Options{Lang: "en"})
	tx := texts(v)
	if tx["Camera not in use"] != "ok" || tx["Microphone in use by: obs"] != "warn" {
		t.Errorf("texts = %v", tx)
	}
	s.Privacy = &model.Privacy{Camera: []string{}, Mic: []string{}, MicActive: true}
	v, _ = Build("security", s, Options{Lang: "en"})
	if tx := texts(v); tx["Microphone in use"] != "warn" {
		t.Errorf("mic active without a named app: texts = %v", tx)
	}
	vt := table(v, "Vulnerabilities")
	if vt == nil || len(vt.Rows.([]Row)) != len(s.CPU.Vulnerabilities) || len(s.CPU.Vulnerabilities) == 0 {
		t.Errorf("vulnerabilities = %+v", vt)
	}
	simd := false
	for _, b := range v.Blocks {
		if b.Type == "kv" && b.Title == "SIMD" && len(kvRows(b)) > 0 {
			simd = true
		}
	}
	if !simd {
		t.Error("no SIMD kv")
	}
	noEmptyValues(t, v)
}
