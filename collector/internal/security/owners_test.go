package security

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// procFS es un /proc de fixture: los fds son archivos .link (como los
// escribe la captura). 9999 no tiene fd legible (un proceso ajeno).
func procFS(t *testing.T) source.FS {
	return fsWith(t, map[string]string{
		"/proc/2210/comm":       "kdeconnectd\n",
		"/proc/2210/fd/0.link":  "/dev/null\n",
		"/proc/2210/fd/23.link": "socket:[27057]\n",
		"/proc/2210/fd/24.link": "socket:[27056]\n",
		"/proc/3120/comm":       "chromium\n",
		"/proc/3120/fd/40.link": "socket:[1839287]\n",
		"/proc/3120/fd/7.link":  "/dev/video0\n",
		"/proc/9999/comm":       "other\n",
		"/proc/self/comm":       "x\n",
	})
}

func TestScanFDs(t *testing.T) {
	fs := procFS(t)
	dirs := 0
	found, cams := scanFDs(fs, map[uint64]bool{27057: true, 1839287: true}, false, &dirs)
	if found[27057] != (Proc{PID: 2210, Comm: "kdeconnectd", FD: "23"}) || found[1839287] != (Proc{PID: 3120, Comm: "chromium", FD: "40"}) ||
		cams != nil || dirs != 2 {
		t.Fatalf("found %+v, cams %v, %d fd dirs", found, cams, dirs)
	}
	// Se corta apenas aparece todo lo buscado: 27057 está en el primero.
	dirs = 0
	if found, _ := scanFDs(fs, map[uint64]bool{27057: true}, false, &dirs); len(found) != 1 || dirs != 1 {
		t.Errorf("early stop: found %+v after %d fd dirs", found, dirs)
	}
	// Sin nada que buscar ni cámara, no se recorre.
	dirs = 0
	if found, cams := scanFDs(fs, nil, false, &dirs); len(found) != 0 || cams != nil || dirs != 0 {
		t.Errorf("nothing to find: %+v %v, %d fd dirs", found, cams, dirs)
	}
	// Con cámara se recorre todo y el pid con /dev/video queda una vez.
	dirs = 0
	if _, cams := scanFDs(fs, map[uint64]bool{27057: true}, true, &dirs); !reflect.DeepEqual(cams, []int{3120}) || dirs != 2 {
		t.Errorf("camera: %v, %d fd dirs", cams, dirs)
	}
}

// En el sistema real los fds son symlinks.
func TestScanFDsSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proc/10/fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "proc/10/comm"), []byte("sshd\n"), 0o644)
	if err := os.Symlink("socket:[5]", filepath.Join(root, "proc/10/fd/3")); err != nil {
		t.Fatal(err)
	}
	found, _ := ScanFDs(source.FS{Root: root}, map[uint64]bool{5: true}, false)
	if found[5] != (Proc{PID: 10, Comm: "sshd", FD: "3"}) {
		t.Fatalf("found %+v", found)
	}
}

func TestOwnersCache(t *testing.T) {
	fs := procFS(t)
	var o Owners
	o.Resolve(fs, []uint64{27057, 1839287}, false)
	if p, ok := o.Program(27057); !ok || p != "kdeconnectd" || o.Walks() != 1 {
		t.Fatalf("program %q %v, walks %d", p, ok, o.Walks())
	}
	o.Resolve(fs, []uint64{27057, 1839287}, false)
	if o.Walks() != 1 {
		t.Fatal("the second read must not walk /proc (nothing new)")
	}
	o.Resolve(fs, []uint64{27057, 1839287, 555}, false) // 555: en ningún proceso
	if _, ok := o.Program(555); ok || o.Walks() != 2 {
		t.Fatalf("a new socket: walks %d", o.Walks())
	}
	o.Resolve(fs, []uint64{27057, 1839287, 555}, false)
	if o.Walks() != 2 {
		t.Fatal("a socket whose process was not found must not be searched again")
	}
	o.Resolve(fs, []uint64{27057}, false) // se cerraron dos
	if _, ok := o.Program(1839287); ok || o.Walks() != 2 {
		t.Fatal("a closed socket must leave the cache without a walk")
	}
	if cams := o.Resolve(fs, []uint64{27057}, true); !reflect.DeepEqual(cams, []int{3120}) || o.Walks() != 3 {
		t.Fatalf("with a camera the walk always runs: %v, walks %d", cams, o.Walks())
	}
}

func TestOwnInodes(t *testing.T) {
	a := netip.MustParseAddr("::")
	socks := []Socket{{"tcp", a, 1, 1000, 10}, {"tcp", a, 2, 0, 11}, {"udp", a, 3, 1000, 12}}
	if got := OwnInodes(socks, 1000); !reflect.DeepEqual(got, []uint64{10, 12}) {
		t.Fatalf("own = %v", got)
	}
}
