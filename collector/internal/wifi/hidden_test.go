package wifi

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

type ttyCall struct {
	name     string
	args     []string
	deadline time.Duration
}

// stubTTY reemplaza la corrida de nmcli en la terminal: anota lo que se
// corrió y, si stderr no es "", lo escribe como nmcli y sale con error.
func stubTTY(t *testing.T, stderr string) *[]ttyCall {
	t.Helper()
	var calls []ttyCall
	old := ttyRun
	ttyRun = func(ctx context.Context, errOut io.Writer, name string, args ...string) error {
		dl, _ := ctx.Deadline()
		calls = append(calls, ttyCall{name, args, time.Until(dl)})
		if stderr == "" {
			return nil
		}
		io.WriteString(errOut, stderr)
		return exec.Command("false").Run()
	}
	t.Cleanup(func() { ttyRun = old })
	return &calls
}

func actionsLog(t *testing.T, env source.Env) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(env.State, "actions.log"))
	return string(b)
}

// El nombre tipeado llega a nmcli como un solo argumento (nunca por un
// shell), con --ask (nmcli pide la clave en la terminal) y el tiempo de
// wifi-wait; el registro no lo guarda.
func TestConnectHiddenHostileSSID(t *testing.T) {
	calls := stubTTY(t, "")
	env := tempWifi(t)
	var out strings.Builder
	ssid := `$(reboot); "x"`
	if err := ConnectHidden(context.Background(), env, "wlp3s0", hiddenBSSID, strings.NewReader(ssid+"\n"), &out, false); err != nil {
		t.Fatal(err)
	}
	// Sin "bssid": fijaría el perfil guardado a un solo punto de acceso (un
	// router de doble banda o un mesh no podría pasarse a otra radio).
	want := []string{"--ask", "device", "wifi", "connect", ssid, "hidden", "yes", "ifname", "wlp3s0"}
	if len(*calls) != 1 || (*calls)[0].name != "nmcli" || !slices.Equal((*calls)[0].args, want) {
		t.Fatalf("calls = %+v", *calls)
	}
	if d := (*calls)[0].deadline; d < waitTimeout-time.Minute || d > waitTimeout {
		t.Fatalf("deadline in %v, want about %v", d, waitTimeout)
	}
	o := out.String()
	if !strings.Contains(o, "Hidden network on wlp3s0 · BSSID "+hiddenBSSID+" · 5 GHz · 62%\n") ||
		!strings.Contains(o, "Network name (SSID): ") || !strings.HasSuffix(o, "Connected to the hidden network "+hiddenBSSID+"\n") ||
		strings.Contains(o, "Press Enter") {
		t.Fatalf("output:\n%s", o)
	}
	log := actionsLog(t, env)
	if log != "2026-09-23T12:00:00Z wifi.connect_hidden \"\" \"\" ok\n" {
		t.Fatalf("log:\n%s", log)
	}
	if len(acted(env)) != 0 {
		t.Fatalf("acted %q", acted(env))
	}
}

// Sin nombre (o sin entrada) se cancela: nada corre y queda "cancelled".
func TestConnectHiddenCancel(t *testing.T) {
	calls := stubTTY(t, "")
	env := tempWifi(t)
	for _, in := range []string{"\n", "", "   \r\n"} {
		var out strings.Builder
		err := ConnectHidden(context.Background(), env, "wlp3s0", hiddenBSSID, strings.NewReader(in), &out, true)
		if ce := (*CancelError)(nil); !errors.As(err, &ce) || err.Error() != "cancelled" {
			t.Fatalf("%q: %v", in, err)
		}
		if !strings.HasSuffix(out.String(), "Cancelled\n") || strings.Contains(out.String(), "Press Enter") {
			t.Fatalf("%q: output:\n%s", in, out.String())
		}
	}
	want := strings.Repeat("2026-09-23T12:00:00Z wifi.connect_hidden \"\" \"\" error\n", 3)
	if len(*calls) != 0 || actionsLog(t, env) != want {
		t.Fatalf("calls %+v, log:\n%s", *calls, actionsLog(t, env))
	}
}

// El error de nmcli (su primera línea) queda en el registro, sin el nombre
// tipeado aunque nmcli lo repita.
func TestConnectHiddenNmcliError(t *testing.T) {
	stubTTY(t, "Error: No network with SSID 'Mi Escondida' found.\n")
	env := tempWifi(t)
	var out strings.Builder
	err := ConnectHidden(context.Background(), env, "wlp3s0", hiddenBSSID, strings.NewReader("Mi Escondida\n"), &out, false)
	if err == nil || err.Error() != "No network with SSID '(hidden network)' found." {
		t.Fatalf("%v", err)
	}
	log := actionsLog(t, env)
	if strings.Contains(log, "Mi Escondida") || log != "2026-09-23T12:00:00Z wifi.connect_hidden \"\" \"\" error\n" {
		t.Fatalf("log:\n%s", log)
	}
	if !strings.HasSuffix(out.String(), "the hidden network "+hiddenBSSID+" was not connected\n") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// Con --hold (la ventana propia del panel) espera Enter antes de volver;
// sin --hold (lazylazy) vuelve apenas termina nmcli.
func TestConnectHiddenHold(t *testing.T) {
	for _, fail := range []string{"", "Error: Secrets were required, but not provided.\n"} {
		stubTTY(t, fail)
		env := tempWifi(t)
		r, w := io.Pipe()
		var out strings.Builder
		done := make(chan error, 1)
		go func() { done <- ConnectHidden(context.Background(), env, "wlp3s0", hiddenBSSID, r, &out, true) }()
		go io.WriteString(w, "Escondida\n")
		select {
		case <-done:
			t.Fatalf("returned without Enter (fail %q)", fail)
		case <-time.After(100 * time.Millisecond):
		}
		go io.WriteString(w, "\n")
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("still waiting after Enter")
		}
		if !strings.HasSuffix(out.String(), "Press Enter to close") {
			t.Fatalf("output:\n%s", out.String())
		}
	}
	stubTTY(t, "")
	env := tempWifi(t)
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- ConnectHidden(context.Background(), env, "wlp3s0", hiddenBSSID, r, io.Discard, false) }()
	go io.WriteString(w, "Escondida\n")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("without --hold it waited")
	}
}

// Una placa o un BSSID que no son válidos: error y nada corre. Un BSSID en
// minúsculas se normaliza. Si la red ya no se ve, igual pregunta (sin datos
// de banda ni señal).
func TestConnectHiddenArgs(t *testing.T) {
	calls := stubTTY(t, "")
	env := tempWifi(t)
	for _, c := range [][2]string{{"", hiddenBSSID}, {"wlp3s0", "nope"}, {"wlp3s0", ""}} {
		if err := ConnectHidden(context.Background(), env, c[0], c[1], strings.NewReader("x\n"), io.Discard, false); err == nil {
			t.Fatalf("%q: no error", c)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("calls %+v", *calls)
	}
	var out strings.Builder
	if err := ConnectHidden(context.Background(), env, "wlp3s0", "aa:bb:cc:00:00:77", strings.NewReader("x\n"), &out, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Hidden network on wlp3s0 · BSSID AA:BB:CC:00:00:77\n") || len(*calls) != 1 {
		t.Fatalf("output:\n%s calls %+v", out.String(), *calls)
	}
}

func TestConnectHiddenSpanish(t *testing.T) {
	stubTTY(t, "")
	env := tempWifi(t)
	env.Lang = "es"
	var out strings.Builder
	ConnectHidden(context.Background(), env, "wlp3s0", hiddenBSSID, strings.NewReader("Escondida\n\n"), &out, true)
	for _, s := range []string{"Red oculta en wlp3s0", "Nombre de la red (SSID): ", "Conectado a la red oculta", "Enter para cerrar"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("missing %q in:\n%s", s, out.String())
		}
	}
}

// El corredor real: `true` y `false` son lo único que un test corre de
// verdad; el stderr se copia para anotarlo.
func TestRunTTY(t *testing.T) {
	var buf strings.Builder
	if err := runTTY(context.Background(), &buf, "true"); err != nil {
		t.Fatal(err)
	}
	if err := runTTY(context.Background(), &buf, "sh", "-c", "echo boom >&2; exit 3"); err == nil || buf.String() != "boom\n" {
		t.Fatalf("%v, stderr %q", err, buf.String())
	}
}

// Ctrl-C (el contexto del proceso, que atrapa SIGINT) en la pregunta del
// nombre cancela: nada corre, queda "cancelled" y no espera Enter.
func TestConnectHiddenInterruptAtPrompt(t *testing.T) {
	calls := stubTTY(t, "")
	env := tempWifi(t)
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var out strings.Builder
	done := make(chan error, 1)
	go func() { done <- ConnectHidden(ctx, env, "wlp3s0", hiddenBSSID, r, &out, true) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl-C did not cancel the prompt")
	}
	if ce := (*CancelError)(nil); !errors.As(err, &ce) || len(*calls) != 0 || strings.Contains(out.String(), "Press Enter") {
		t.Fatalf("%v, calls %+v, output:\n%s", err, *calls, out.String())
	}
	if !strings.HasSuffix(actionsLog(t, env), `wifi.connect_hidden "" "" error`+"\n") {
		t.Fatalf("log:\n%s", actionsLog(t, env))
	}
}

// ctxTTY reemplaza nmcli por uno que espera hasta que el contexto termina.
func ctxTTY(t *testing.T) {
	t.Helper()
	old := ttyRun
	ttyRun = func(ctx context.Context, errOut io.Writer, name string, args ...string) error {
		<-ctx.Done()
		return errors.New("signal: terminated")
	}
	t.Cleanup(func() { ttyRun = old })
}

// Ctrl-C mientras nmcli pide la clave: cancelado, sin esperar Enter.
func TestConnectHiddenInterruptDuringNmcli(t *testing.T) {
	ctxTTY(t)
	env := tempWifi(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	var out strings.Builder
	err := ConnectHidden(ctx, env, "wlp3s0", hiddenBSSID, strings.NewReader("Escondida\n\n"), &out, true)
	if ce := (*CancelError)(nil); !errors.As(err, &ce) || strings.Contains(out.String(), "Press Enter") {
		t.Fatalf("%v, output:\n%s", err, out.String())
	}
}

// Se acabó el tiempo: "timed out" (traducido), no "signal: terminated".
func TestConnectHiddenTimeout(t *testing.T) {
	ctxTTY(t)
	old := hiddenTimeout
	hiddenTimeout = 30 * time.Millisecond
	t.Cleanup(func() { hiddenTimeout = old })
	env := tempWifi(t)
	env.Lang = "es"
	err := ConnectHidden(context.Background(), env, "wlp3s0", hiddenBSSID, strings.NewReader("Escondida\n"), io.Discard, false)
	if err == nil || err.Error() != "no respondió a tiempo" || !strings.HasSuffix(actionsLog(t, env), `wifi.connect_hidden "" "" error`+"\n") {
		t.Fatalf("%v, log:\n%s", err, actionsLog(t, env))
	}
}

// Al cortar por tiempo, el corredor real manda SIGTERM (nmcli restaura la
// terminal) en vez de SIGKILL.
func TestRunTTYTerminates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := runTTY(ctx, io.Discard, "sh", "-c", `trap "exit 7" TERM; sleep 5 >/dev/null 2>&1 & wait`)
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("%v, want exit 7 from the TERM trap", err)
	}
}
