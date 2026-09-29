package source

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestHyprPath(t *testing.T) {
	env := map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "abc", "XDG_RUNTIME_DIR": "/run/user/1000"}
	get := func(k string) string { return env[k] }
	if p := HyprPath(get); p != "/run/user/1000/hypr/abc/.socket.sock" {
		t.Fatalf("path = %q", p)
	}
	env["HYPRLAND_INSTANCE_SIGNATURE"] = "../x"
	if p := HyprPath(get); p != "" {
		t.Fatalf("a signature with a slash = %q", p)
	}
	env["HYPRLAND_INSTANCE_SIGNATURE"] = ""
	if p := HyprPath(get); p != "" {
		t.Fatalf("without Hyprland = %q", p)
	}
}

func TestHyprSocketRoundTrip(t *testing.T) {
	// Corto a propósito: la ruta de un socket unix admite unos 108 bytes.
	dir, err := os.MkdirTemp("", "hy")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		c.Write(append([]byte("got "), buf[:n]...))
		c.Close()
	}()
	out, err := HyprSocket{Path: path}.Request("j/clients")
	if err != nil || string(out) != "got j/clients" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

func TestHyprSocketWithoutHyprland(t *testing.T) {
	if _, err := (HyprSocket{}).Request("j/clients"); !errors.Is(err, ErrNoHyprland) {
		t.Fatalf("no path: %v", err)
	}
	if _, err := (HyprSocket{Path: "/nonexistent/.socket.sock"}).Request("j/clients"); !errors.Is(err, ErrNoHyprland) {
		t.Fatalf("no socket: %v", err)
	}
}

func TestFakeHypr(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "j-clients.txt"), []byte("[]"), 0o644)
	h := &FakeHypr{Dir: dir}
	if out, err := h.Request("j/clients"); err != nil || string(out) != "[]" {
		t.Fatalf("clients = %q, %v", out, err)
	}
	expr := `hl.dsp.window.close({window="address:0x1"})`
	if out, err := h.Request("dispatch " + expr); err != nil || string(out) != "ok" {
		t.Fatalf("dispatch = %q, %v", out, err)
	}
	if _, err := h.Request("j/monitors"); !errors.Is(err, ErrNoHyprland) {
		t.Fatalf("missing file: %v", err)
	}
	if d := h.Dispatches(); len(d) != 1 || d[0] != expr {
		t.Fatalf("dispatches = %q", d)
	}
}

func TestFixtureHasHypr(t *testing.T) {
	if _, ok := Fixture(t, "minimal").Hypr.(*FakeHypr); !ok {
		t.Fatal("the fixture Env has no FakeHypr")
	}
}

func TestFSOwner(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f"), nil, 0o644)
	uid, err := FS{Root: dir}.Owner("/f")
	if err != nil || uid != uint32(os.Getuid()) {
		t.Fatalf("owner = %d, %v", uid, err)
	}
	if _, err := (FS{Root: dir}).Owner("/nope"); err == nil {
		t.Fatal("a missing file has no owner")
	}
}

func TestHyprLockPath(t *testing.T) {
	env := map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "abc", "XDG_RUNTIME_DIR": "/run/user/1000"}
	get := func(k string) string { return env[k] }
	if p := HyprLockPath(get); p != "/run/user/1000/hypr/abc/hyprland.lock" {
		t.Fatalf("path = %q", p)
	}
	env["HYPRLAND_INSTANCE_SIGNATURE"] = "../x"
	if p := HyprLockPath(get); p != "" {
		t.Fatalf("a signature with a slash = %q", p)
	}
	env["HYPRLAND_INSTANCE_SIGNATURE"] = ""
	if p := HyprLockPath(get); p != "" {
		t.Fatalf("without Hyprland = %q", p)
	}
}
