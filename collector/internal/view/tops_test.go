package view

import (
	"strings"
	"testing"
)

func topOf(t *testing.T, id string) Block {
	t.Helper()
	v, err := Build(id, appsSnap(), Options{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	return findGroup(v.Blocks, "Apps using the most")
}

func blockTexts(bs []Block) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Text)
	}
	return out
}

func TestTopCPU(t *testing.T) {
	g := topOf(t, "cpu")
	if got := strings.Join(blockTexts(g.Blocks), " | "); got != "Chromium · 12% | syncthing · 9% | Hyprland · 5%" {
		t.Fatalf("top = %q", got)
	}
	for _, b := range g.Blocks {
		if b.Link != "apps" {
			t.Fatalf("%q links to %q", b.Text, b.Link)
		}
	}
}

func TestTopMemory(t *testing.T) {
	if got := blockTexts(topOf(t, "memory").Blocks); len(got) != 3 || got[0] != "Chromium · 1.0 GiB" || got[1] != "Hyprland · 300.0 MiB" {
		t.Fatalf("top = %q", got)
	}
}

func TestTopGPUOnlyAppsWithAValue(t *testing.T) {
	if got := blockTexts(topOf(t, "gpu").Blocks); len(got) != 1 || got[0] != "Chromium · 3%" {
		t.Fatalf("top = %q", got)
	}
}

func TestTopNetworkNotesTCP(t *testing.T) {
	got := blockTexts(topOf(t, "network").Blocks)
	if len(got) != 2 || got[0] != "Chromium · 1.1 kB/s" || !strings.Contains(got[1], "TCP only") {
		t.Fatalf("top = %q", got)
	}
}

func TestTopGPUUnavailable(t *testing.T) {
	s := appsSnap()
	s.Apps.List[0].GPUPct = nil
	s.Unavailable = append(s.Unavailable, unavailable("apps.gpu", "the GPU is asleep; it is not woken up to read it"))
	v, _ := Build("gpu", s, Options{Lang: "en"})
	g := findGroup(v.Blocks, "Apps using the most")
	if len(g.Blocks) != 1 || !strings.Contains(g.Blocks[0].Text, "asleep") {
		t.Fatalf("group = %+v", g)
	}
}

func TestTopWithoutApps(t *testing.T) {
	s := appsSnap()
	s.Apps = nil
	for _, id := range []string{"cpu", "memory", "gpu", "network"} {
		v, _ := Build(id, s, Options{Lang: "en"})
		if g := findGroup(v.Blocks, "Apps using the most"); g.Type != "" {
			t.Fatalf("%s has a top without apps", id)
		}
	}
}
