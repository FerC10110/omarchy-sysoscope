package maint

import (
	"context"
	"encoding/json"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// jline es una línea de journalctl -o json.
func jline(f map[string]any) string {
	b, _ := json.Marshal(f)
	return string(b) + "\n"
}

var errKey = "cmd/" + source.Key("journalctl", JournalErrArgs...)

// Review Focus 1: todas las formas en que llega un MESSAGE (las dos
// primeras son líneas reales de esta máquina).
func TestParseErrorsMessageForms(t *testing.T) {
	out := jline(map[string]any{"MESSAGE": "[drm:__nv_drm_gem_nvkms_map [nvidia_drm]] *ERROR* [nvidia-drm] [GPU ID 0x00004200] Failed to map NvKmsKapiMemory 0x000000009ab9cf53",
		"PRIORITY": "3", "SYSLOG_IDENTIFIER": "kernel", "__REALTIME_TIMESTAMP": "1790285630614458"}) +
		jline(map[string]any{"MESSAGE": "wlp4s0: nl80211: kernel reports: multicast RX registrations are not supported",
			"PRIORITY": "3", "SYSLOG_IDENTIFIER": "wpa_supplicant", "_COMM": "wpa_supplicant", "__REALTIME_TIMESTAMP": "1790449058237259"}) +
		"not json at all\n" +
		jline(map[string]any{"MESSAGE": []int{104, 195, 169, 108, 108, 111, 10, 119, 111, 114, 108, 100}, // "héllo\nworld"
			"PRIORITY": "3", "_COMM": "qml6", "__REALTIME_TIMESTAMP": "1790449100000000"}) +
		jline(map[string]any{"MESSAGE": []int{255, 254, 0, 1}, "PRIORITY": "2", "SYSLOG_IDENTIFIER": "bluetoothd", "__REALTIME_TIMESTAMP": "1790449200000000"}) +
		jline(map[string]any{"MESSAGE": []string{"first", "second"}, "PRIORITY": "3", "__REALTIME_TIMESTAMP": "1790449300000000"}) +
		jline(map[string]any{"MESSAGE": "line one\nline\ttwo \x1b[1mbold\x1b[0m", "PRIORITY": "3", "SYSLOG_IDENTIFIER": "sddm-helper", "__REALTIME_TIMESTAMP": "1790449400000000"}) +
		jline(map[string]any{"MESSAGE": strings.Repeat("é", 300), "SYSLOG_IDENTIFIER": "kernel", "PRIORITY": "3", "__REALTIME_TIMESTAMP": "bad"}) +
		"\n"
	je := parseErrors([]byte(out), "en")
	if je.Count != 7 || je.Capped || je.Partial {
		t.Fatalf("count = %d capped %v partial %v", je.Count, je.Capped, je.Partial)
	}
	wantSrc := []model.ErrorSource{{Name: "kernel", Count: 2}, {Name: "Unknown", Count: 1}, {Name: "bluetoothd", Count: 1},
		{Name: "qml6", Count: 1}, {Name: "sddm-helper", Count: 1}, {Name: "wpa_supplicant", Count: 1}}
	if !reflect.DeepEqual(je.Sources, wantSrc) {
		t.Errorf("sources = %+v", je.Sources)
	}
	if len(je.Recent) != 5 {
		t.Fatalf("recent = %+v", je.Recent)
	}
	long := je.Recent[0].Message
	if utf8.RuneCountInString(long) != 160 || !strings.HasSuffix(long, "…") || !je.Recent[0].Time.IsZero() {
		t.Errorf("long message = %d runes %q, time %v", utf8.RuneCountInString(long), long[len(long)-6:], je.Recent[0].Time)
	}
	if je.Recent[1].Message != "line one line two bold" || je.Recent[1].Source != "sddm-helper" {
		t.Errorf("control chars = %+v", je.Recent[1])
	}
	if je.Recent[2] != (model.JournalEntry{Time: time.UnixMicro(1790449300000000).UTC(), Source: "Unknown", Priority: 3, Message: "first"}) {
		t.Errorf("array of strings = %+v", je.Recent[2])
	}
	if je.Recent[3].Message != "(binary)" || je.Recent[3].Priority != 2 || je.Recent[3].Source != "bluetoothd" {
		t.Errorf("binary = %+v", je.Recent[3])
	}
	if je.Recent[4].Message != "héllo world" || je.Recent[4].Source != "qml6" || !je.Recent[4].Time.Equal(time.UnixMicro(1790449100000000)) {
		t.Errorf("bytes = %+v", je.Recent[4])
	}
	es := parseErrors([]byte(out), "es")
	if es.Recent[3].Message != "(binario)" || es.Recent[2].Source != "Desconocido" {
		t.Errorf("es = %+v", es.Recent)
	}
	if empty := parseErrors(nil, "en"); empty.Count != 0 || empty.Sources == nil || empty.Recent == nil {
		t.Errorf("empty = %+v", empty)
	}
}

func TestParseErrorsCap(t *testing.T) {
	line := jline(map[string]any{"MESSAGE": "x", "SYSLOG_IDENTIFIER": "kernel", "__REALTIME_TIMESTAMP": "1"})
	if je := parseErrors([]byte(strings.Repeat(line, 1000)), "en"); je.Count != 1000 || !je.Capped {
		t.Errorf("1000 = %d capped %v", je.Count, je.Capped)
	}
	if je := parseErrors([]byte(strings.Repeat(line, 999)), "en"); je.Count != 999 || je.Capped {
		t.Errorf("999 = %d capped %v", je.Count, je.Capped)
	}
}

func TestReadErrors(t *testing.T) {
	env, dir := tempEnv(t)
	put(t, dir, errKey+".txt", jline(map[string]any{"MESSAGE": "boom", "SYSLOG_IDENTIFIER": "kernel", "PRIORITY": "3", "__REALTIME_TIMESTAMP": "1790150060000000"}))
	r := &collect.Report{Lang: "en"}
	if je := ReadErrors(env, r); je == nil || je.Count != 1 || len(r.List) != 0 {
		t.Fatalf("errors = %+v, unavailable %+v", je, r.List)
	}
	// Sale ≠0 sin escribir nada (sin entradas): cero, sin aviso.
	put(t, dir, errKey+".txt", "")
	put(t, dir, errKey+".exit", "1\n")
	if je := ReadErrors(env, r); je == nil || je.Count != 0 || len(r.List) != 0 {
		t.Fatalf("no entries = %+v, %+v", je, r.List)
	}
	// Sin journalctl.
	bare, _ := tempEnv(t)
	r = &collect.Report{Lang: "en"}
	if je := ReadErrors(bare, r); je != nil || len(r.List) != 1 || r.List[0].What != "maintenance.health.errors" ||
		r.List[0].Reason != "missing_tool" || r.List[0].Message != "requires journalctl" {
		t.Fatalf("missing = %+v, %+v", je, r.List)
	}
	// Tope de 5 s y motivo de stderr.
	er := &errRunner{err: context.DeadlineExceeded}
	bare.Run = er
	r = &collect.Report{Lang: "en"}
	if je := ReadErrors(bare, r); je != nil || r.List[0].Reason != "timeout" || er.left > healthTimeout || er.left < 4*time.Second {
		t.Fatalf("timeout = %+v, deadline %v", r.List, er.left)
	}
	bare.Run = &errRunner{err: &exec.ExitError{Stderr: []byte("Failed to open journal: Permission denied\n")}}
	r = &collect.Report{Lang: "en"}
	if ReadErrors(bare, r); r.List[0].Message != "could not read: Failed to open journal: Permission denied" {
		t.Fatalf("stderr = %+v", r.List)
	}
}

func TestJournalPartial(t *testing.T) {
	env, dir := tempEnv(t)
	if JournalPartial(env) {
		t.Error("without /etc/group nothing is claimed")
	}
	put(t, dir, "root/etc/group", "root:x:0:\nwheel:x:998:ana\nusers:x:1000:\n")
	if !JournalPartial(env) {
		t.Error("user only in users: partial")
	}
	env.User.GIDs = []uint32{1000, 998}
	if JournalPartial(env) {
		t.Error("in wheel by gid: full")
	}
	env.User.GIDs = []uint32{1000}
	put(t, dir, "root/etc/group", "adm:x:4:user\nusers:x:1000:\n")
	if JournalPartial(env) {
		t.Error("in adm by name: full")
	}
	put(t, dir, "root/etc/group", "systemd-journal:x:190:\nusers:x:1000:\n")
	env.User.GIDs = []uint32{1000, 190}
	if JournalPartial(env) {
		t.Error("in systemd-journal: full")
	}
	env.User = source.User{UID: 0, Name: "root", GIDs: []uint32{0}}
	if JournalPartial(env) {
		t.Error("root sees everything")
	}
	// ReadErrors lo suma.
	env.User = source.FixtureUser
	put(t, dir, "root/etc/group", "users:x:1000:\n")
	put(t, dir, errKey+".txt", "")
	if je := ReadErrors(env, &collect.Report{Lang: "en"}); je == nil || !je.Partial {
		t.Errorf("partial = %+v", je)
	}
	// M4: también los OOM (Memoria los lee sin los errores).
	put(t, dir, oomdKey+".txt", "")
	if l := ReadOOM(env, 0, &collect.Report{Lang: "en"}); l == nil || !l.Partial {
		t.Errorf("oom partial = %+v", l)
	}
}

// M7: la fuente se recorta a 40 runas; la fuente y el mensaje pierden los
// caracteres de formato (U+202E, bidi) además de los de control.
func TestParseErrorsLongSourceAndBidi(t *testing.T) {
	out := jline(map[string]any{"MESSAGE": "a‮b​c", "SYSLOG_IDENTIFIER": strings.Repeat("s", 60), "__REALTIME_TIMESTAMP": "1"}) +
		jline(map[string]any{"MESSAGE": "m", "_COMM": "co‮mm" + strings.Repeat("é", 50), "__REALTIME_TIMESTAMP": "2"})
	je := parseErrors([]byte(out), "en")
	if len(je.Recent) != 2 {
		t.Fatalf("recent = %+v", je.Recent)
	}
	if s := je.Recent[1].Source; utf8.RuneCountInString(s) != 40 || !strings.HasSuffix(s, "…") {
		t.Errorf("long source = %q", s)
	}
	if m := je.Recent[1].Message; m != "abc" {
		t.Errorf("bidi message = %q", m)
	}
	if s := je.Recent[0].Source; utf8.RuneCountInString(s) != 40 || !strings.HasPrefix(s, "comm") {
		t.Errorf("comm source = %q", s)
	}
}
