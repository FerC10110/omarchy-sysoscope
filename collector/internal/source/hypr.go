package source

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrNoHyprland: no hay un Hyprland al que preguntarle (otra sesión, un TTY).
var ErrNoHyprland = errors.New("Hyprland is not running")

// Hypr habla con el socket de pedidos de Hyprland, el mismo que usa hyprctl:
// "j/clients", "j/monitors", "j/activeworkspace", "dispatch <expresión Lua>".
type Hypr interface {
	Request(cmd string) ([]byte, error)
}

// HyprPath es la ruta del socket de la sesión actual, o "" sin Hyprland.
func HyprPath(getenv func(string) string) string {
	sig, run := getenv("HYPRLAND_INSTANCE_SIGNATURE"), getenv("XDG_RUNTIME_DIR")
	if sig == "" || run == "" || strings.ContainsRune(sig, '/') {
		return ""
	}
	return filepath.Join(run, "hypr", sig, ".socket.sock")
}

// HyprLockPath es el hyprland.lock de la sesión actual (su primera línea es
// el pid de Hyprland), o "" sin Hyprland.
func HyprLockPath(getenv func(string) string) string {
	sock := HyprPath(getenv)
	if sock == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sock), "hyprland.lock")
}

// HyprSocket manda un pedido por conexión, como hyprctl, sin crear procesos.
type HyprSocket struct {
	Path    string
	Timeout time.Duration // 0 = 1 s
}

func (h HyprSocket) Request(cmd string) ([]byte, error) {
	if h.Path == "" {
		return nil, ErrNoHyprland
	}
	t := h.Timeout
	if t <= 0 {
		t = time.Second
	}
	c, err := net.DialTimeout("unix", h.Path, t)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil, ErrNoHyprland
		}
		return nil, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(t))
	if _, err := io.WriteString(c, cmd); err != nil {
		return nil, err
	}
	return io.ReadAll(c)
}

// FakeHypr responde desde Dir/<Key(cmd)>.txt y anota los pedidos. Un
// "dispatch" sin archivo responde "ok" (como Hyprland); otro pedido sin
// archivo es ErrNoHyprland.
type FakeHypr struct {
	Dir   string
	mu    sync.Mutex
	Calls []string
}

func (f *FakeHypr) Request(cmd string) ([]byte, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, cmd)
	f.mu.Unlock()
	if out, err := os.ReadFile(filepath.Join(f.Dir, Key(cmd)+".txt")); err == nil {
		return out, nil
	}
	if strings.HasPrefix(cmd, "dispatch ") {
		return []byte("ok"), nil
	}
	return nil, ErrNoHyprland
}

// Dispatches devuelve las expresiones despachadas, en orden.
func (f *FakeHypr) Dispatches() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.Calls {
		if e, ok := strings.CutPrefix(c, "dispatch "); ok {
			out = append(out, e)
		}
	}
	return out
}
