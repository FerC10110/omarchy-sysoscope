package source

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// slowPeer responde tarde a todo, como un servicio colgado.
type slowPeer struct{}

func (slowPeer) Sleep() *dbus.Error { time.Sleep(3 * time.Second); return nil }
func (slowPeer) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	time.Sleep(3 * time.Second)
	return dbus.MakeVariant(""), nil
}

// privateBus arranca un dbus-daemon propio y devuelve su dirección.
func privateBus(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon not installed")
	}
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--nopidfile", "--print-address")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(addr)
}

func TestDBusCallsTimeOut(t *testing.T) {
	addr := privateBus(t)
	srv, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	srv.Export(slowPeer{}, "/slow", "io.test.Slow")
	srv.Export(slowPeer{}, "/slow", "org.freedesktop.DBus.Properties")
	if _, err := srv.RequestName("io.test.Slow", 0); err != nil {
		t.Fatal(err)
	}
	cli, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	b := &DBus{session: cli, Timeout: 200 * time.Millisecond}
	defer cli.Close()

	start := time.Now()
	if _, err := b.Call(false, "io.test.Slow", "/slow", "io.test.Slow.Sleep"); err == nil {
		t.Fatal("a hung method must fail")
	}
	if _, err := b.Property(false, "io.test.Slow", "/slow", "io.test.Slow", "X"); err == nil {
		t.Fatal("a hung property must fail")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("two calls took %v; each must give up after the timeout", d)
	}
}
