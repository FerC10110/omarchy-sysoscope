package system

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func privacyRoot(t *testing.T, camera bool) string {
	t.Helper()
	root := t.TempDir()
	put := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(rel, target string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
	}
	put("proc/asound/card0/pcm0c/sub0/status", "state: RUNNING\nowner_pid   : 4242\ntrigger_time: 1.0\n")
	put("proc/asound/card0/pcm0c/sub1/status", "closed\n")
	put("proc/asound/card0/pcm0p/sub0/status", "state: RUNNING\nowner_pid   : 7\n") // reproducción, no captura
	put("proc/4242/comm", "obs\n")
	put("proc/99/comm", "firefox\n")
	put("proc/100/comm", "bash\n")
	link("proc/99/fd/7", "/dev/video0")
	link("proc/99/fd/8", "/dev/video0")
	link("proc/100/fd/3", "/dev/null")
	if camera {
		put("sys/class/video4linux/video0/name", "Integrated Camera\n")
	}
	return root
}

func TestPrivacy(t *testing.T) {
	p := Privacy(source.Env{FS: source.FS{Root: privacyRoot(t, true)}})
	if !reflect.DeepEqual(p.Camera, []string{"firefox"}) || !reflect.DeepEqual(p.Mic, []string{"obs"}) {
		t.Fatalf("privacy = %+v", p)
	}
}

// Sin cámaras no se recorre /proc: el fd a /dev/video0 no se ve.
func TestPrivacyNoCamera(t *testing.T) {
	p := Privacy(source.Env{FS: source.FS{Root: privacyRoot(t, false)}})
	if p.Camera == nil || len(p.Camera) != 0 {
		t.Fatalf("camera = %#v, want an empty list", p.Camera)
	}
	if !reflect.DeepEqual(p.Mic, []string{"obs"}) {
		t.Fatalf("mic = %v", p.Mic)
	}
}

// En Omarchy la captura ALSA suele ser de PipeWire, no de la app que graba:
// el servidor de audio no se nombra, pero el micrófono sigue en uso.
func TestPrivacyAudioServerIsNotTheOwner(t *testing.T) {
	root := privacyRoot(t, false)
	os.MkdirAll(filepath.Join(root, "proc/asound/card1/pcm0c/sub0"), 0o755)
	os.WriteFile(filepath.Join(root, "proc/asound/card1/pcm0c/sub0/status"), []byte("state: RUNNING\nowner_pid   : 555\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "proc/555"), 0o755)
	os.WriteFile(filepath.Join(root, "proc/555/comm"), []byte("pipewire\n"), 0o644)
	p := Privacy(source.Env{FS: source.FS{Root: root}})
	if !reflect.DeepEqual(p.Mic, []string{"obs"}) || !p.MicActive {
		t.Fatalf("privacy = %+v", p)
	}
	os.Remove(filepath.Join(root, "proc/asound/card0/pcm0c/sub0/status"))
	p = Privacy(source.Env{FS: source.FS{Root: root}})
	if len(p.Mic) != 0 || !p.MicActive {
		t.Fatalf("only pipewire capturing: %+v", p)
	}
}

// El engine pasa los pids con cámara del recorrido compartido con los
// sockets (security.ScanFDs).
func TestPrivacyWith(t *testing.T) {
	env := source.Env{FS: source.FS{Root: privacyRoot(t, true)}}
	if p := PrivacyWith(env, []int{99, 99, 100}); !reflect.DeepEqual(p.Camera, []string{"bash", "firefox"}) || !reflect.DeepEqual(p.Mic, []string{"obs"}) {
		t.Fatalf("privacy = %+v", p)
	}
	if p := PrivacyWith(env, nil); p.Camera == nil || len(p.Camera) != 0 {
		t.Fatalf("camera = %#v, want an empty list", p.Camera)
	}
	if !HasCamera(env) || HasCamera(source.Env{FS: source.FS{Root: privacyRoot(t, false)}}) {
		t.Error("HasCamera")
	}
}
