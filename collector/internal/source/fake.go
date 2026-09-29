package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// FakeRunner lee la salida de Dir/<clave>.txt, con clave = nombre y args
// unidos con "_" (sin "/" ni espacios; si pasa de 200 bytes se recorta y se
// le agrega un hash). Si existe Dir/<clave>.exit, su número
// es el código de salida. Si el .txt no existe → exec.ErrNotFound.
type FakeRunner struct {
	Dir   string
	mu    sync.Mutex
	Calls []string
}

// Key es el nombre de archivo (sin .txt) de un comando en un fixture.
func Key(name string, args ...string) string {
	k := strings.Join(append([]string{name}, args...), "_")
	k = strings.NewReplacer("/", "-", " ", "-").Replace(k)
	if len(k) > 200 {
		sum := sha256.Sum256([]byte(k))
		k = k[:150] + "-" + hex.EncodeToString(sum[:6])
	}
	return k
}

func (r *FakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	k := Key(name, args...)
	r.mu.Lock()
	r.Calls = append(r.Calls, k)
	r.mu.Unlock()
	out, err := os.ReadFile(filepath.Join(r.Dir, k+".txt"))
	if err != nil {
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	if code, err := os.ReadFile(filepath.Join(r.Dir, k+".exit")); err == nil {
		n, _ := strconv.Atoi(strings.TrimSpace(string(code)))
		return out, &ExitCode{Code: n}
	}
	return out, nil
}

// Stream devuelve el mismo archivo que Run, entero, como si el comando
// hubiera emitido esas líneas y terminado.
func (r *FakeRunner) Stream(ctx context.Context, name string, args ...string) (io.ReadCloser, error) {
	out, err := r.Run(ctx, name, args...)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(out)), nil
}

// Count devuelve cuántas llamadas hubo a un comando.
func (r *FakeRunner) Count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.Calls {
		if c == name || strings.HasPrefix(c, name+"_") {
			n++
		}
	}
	return n
}

// ExitCode es el error de un comando falso que salió con código distinto de 0.
type ExitCode struct{ Code int }

func (e *ExitCode) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// ExitStatus devuelve el código de salida de un error de Runner (o -1).
func ExitStatus(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	var fe *ExitCode
	if errors.As(err, &fe) {
		return fe.Code
	}
	return -1
}

// FakeBus responde desde un JSON: {"system": {...}, "session": {...}} con las
// claves "<dest> <path> <iface> <prop>", "objects <dest> <path>" y
// "call <dest> <path> <method> <args…>".
type FakeBus struct {
	System, Session map[string]json.RawMessage
	mu              sync.Mutex
	calls           []string // claves pedidas, respondidas o no (Called)
}

var errNoKey = errors.New("fake bus: no such key")

func LoadFakeBus(path string) (*FakeBus, error) {
	b := &FakeBus{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	var raw struct {
		System  map[string]json.RawMessage `json:"system"`
		Session map[string]json.RawMessage `json:"session"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	b.System, b.Session = raw.System, raw.Session
	return b, nil
}

func (b *FakeBus) get(system bool, key string, dst any) error {
	b.mu.Lock()
	b.calls = append(b.calls, key)
	b.mu.Unlock()
	m := b.Session
	if system {
		m = b.System
	}
	v, ok := m[key]
	if !ok {
		return errNoKey
	}
	return json.Unmarshal(v, dst)
}

// Called cuenta los pedidos (propiedades, objetos y llamadas) cuya clave
// contiene sub: un test ve así si se habló con un servicio.
func (b *FakeBus) Called(sub string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, k := range b.calls {
		if strings.Contains(k, sub) {
			n++
		}
	}
	return n
}

func (b *FakeBus) Property(system bool, dest, path, iface, prop string) (any, error) {
	var v any
	err := b.get(system, strings.Join([]string{dest, path, iface, prop}, " "), &v)
	return v, err
}

func (b *FakeBus) Objects(system bool, dest, path string) (map[string]map[string]map[string]any, error) {
	var v map[string]map[string]map[string]any
	err := b.get(system, "objects "+dest+" "+path, &v)
	return v, err
}

func (b *FakeBus) Call(system bool, dest, path, method string, args ...any) ([]any, error) {
	parts := []string{"call", dest, path, method}
	for _, a := range args {
		parts = append(parts, fmt.Sprint(a))
	}
	var v []any
	err := b.get(system, strings.Join(parts, " "), &v)
	return v, err
}
