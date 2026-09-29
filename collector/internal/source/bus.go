package source

import (
	"context"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Bus es lo mínimo de D-Bus que usa el colector.
type Bus interface {
	// Property lee una propiedad. system=false usa el bus de sesión.
	Property(system bool, dest, path, iface, prop string) (any, error)
	// Objects es org.freedesktop.DBus.ObjectManager.GetManagedObjects:
	// path → interface → propiedad → valor (ya convertido a tipos Go).
	Objects(system bool, dest, path string) (map[string]map[string]map[string]any, error)
	// Call llama un método y devuelve el Body.
	Call(system bool, dest, path, method string, args ...any) ([]any, error)
}

// DBus implementa Bus con godbus; abre las conexiones al primer uso y las
// reutiliza. Si no hay bus, cada llamada devuelve el error de conexión.
// Cada llamada espera como mucho Timeout (0 = 2 s): un servicio colgado no
// puede trabar la muestra.
type DBus struct {
	Timeout time.Duration
	mu      sync.Mutex
	sys     *dbus.Conn
	session *dbus.Conn
}

func (b *DBus) conn(system bool) (*dbus.Conn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if system {
		if b.sys == nil {
			c, err := dbus.ConnectSystemBus()
			if err != nil {
				return nil, err
			}
			b.sys = c
		}
		return b.sys, nil
	}
	if b.session == nil {
		c, err := dbus.ConnectSessionBus()
		if err != nil {
			return nil, err
		}
		b.session = c
	}
	return b.session, nil
}

func (b *DBus) ctx() (context.Context, context.CancelFunc) {
	t := b.Timeout
	if t <= 0 {
		t = 2 * time.Second
	}
	return context.WithTimeout(context.Background(), t)
}

func (b *DBus) Property(system bool, dest, path, iface, prop string) (any, error) {
	c, err := b.conn(system)
	if err != nil {
		return nil, err
	}
	ctx, cancel := b.ctx()
	defer cancel()
	var v dbus.Variant
	err = c.Object(dest, dbus.ObjectPath(path)).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, iface, prop).Store(&v)
	if err != nil {
		return nil, err
	}
	return plain(v), nil
}

func (b *DBus) Objects(system bool, dest, path string) (map[string]map[string]map[string]any, error) {
	c, err := b.conn(system)
	if err != nil {
		return nil, err
	}
	var raw map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	ctx, cancel := b.ctx()
	defer cancel()
	if err := c.Object(dest, dbus.ObjectPath(path)).CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&raw); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]map[string]any, len(raw))
	for p, ifaces := range raw {
		m := make(map[string]map[string]any, len(ifaces))
		for iface, props := range ifaces {
			pm := make(map[string]any, len(props))
			for k, v := range props {
				pm[k] = plain(v)
			}
			m[iface] = pm
		}
		out[string(p)] = m
	}
	return out, nil
}

func (b *DBus) Call(system bool, dest, path, method string, args ...any) ([]any, error) {
	c, err := b.conn(system)
	if err != nil {
		return nil, err
	}
	ctx, cancel := b.ctx()
	defer cancel()
	call := c.Object(dest, dbus.ObjectPath(path)).CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return nil, call.Err
	}
	out := make([]any, len(call.Body))
	for i, v := range call.Body {
		out[i] = plain(v)
	}
	return out, nil
}

// plain convierte valores de godbus en tipos Go simples (sin Variant ni
// ObjectPath), para que el código de los colectores y el FakeBus hablen igual.
func plain(v any) any {
	switch x := v.(type) {
	case dbus.Variant:
		return plain(x.Value())
	case dbus.ObjectPath:
		return string(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = e
		}
		return out
	case []map[string]dbus.Variant:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	case map[string]dbus.Variant:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = plain(e)
		}
		return out
	case [][]any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	}
	return v
}
