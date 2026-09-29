package wifi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/actlog"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// ttyRun corre nmcli en la terminal de este proceso (los tests la
// reemplazan: nada corre de verdad).
var ttyRun = runTTY

// hiddenTimeout es lo máximo que espera a nmcli (los tests lo acortan).
var hiddenTimeout = waitTimeout

// CancelError: el usuario canceló (sin nombre o con Ctrl-C).
type CancelError struct{ Text string }

func (e *CancelError) Error() string { return e.Text }

// ConnectHidden es `sysoscope wifi-hidden`: pide el nombre (SSID) de la red
// oculta con ese BSSID leyendo una línea de in, y corre `nmcli --ask device
// wifi connect SSID hidden yes ifname PLACA` en la terminal, que pide la
// clave ahí mismo: ni el nombre ni la clave se anotan ni pasan por un shell.
// El BSSID solo identifica la fila: pasárselo a nmcli fijaría el perfil
// guardado a ese punto de acceso, y una red de doble banda o un mesh no
// podría pasarse (ni autoconectarse) a sus otras radios. Sin nombre, o con
// ctx cancelado (Ctrl-C), se cancela (*CancelError) sin correr nada más.
// Anota el resultado en actions.log como wifi.connect_hidden. Con hold (una
// ventana propia) espera Enter antes de volver, salvo si se canceló.
func ConnectHidden(ctx context.Context, env source.Env, iface, bssid string, in io.Reader, out io.Writer, hold bool) error {
	l := env.Lang
	const id = "wifi.connect_hidden"
	bssid = strings.ToUpper(strings.TrimSpace(bssid))
	tg := iface + ":hidden:" + bssid
	if iface == "" || strings.Contains(iface, ":") || !IsBSSID(bssid) {
		err := errors.New(i18n.F(l, "unknown network %q", tg))
		actlog.Write(env, id, tg, "", "", err)
		return err
	}
	fmt.Fprintln(out, hiddenContext(env, iface, bssid))
	fmt.Fprint(out, i18n.T(l, "Network name (SSID): "))
	r := bufio.NewReader(in)
	cancelled := func() error {
		err := &CancelError{i18n.T(l, "cancelled")}
		actlog.Write(env, id, tg, "", "", err)
		fmt.Fprintln(out, "\n"+i18n.T(l, "Cancelled"))
		return err
	}
	line, ok := readLine(ctx, r)
	ssid := strings.TrimRight(line, "\r\n")
	if !ok || strings.TrimSpace(ssid) == "" {
		return cancelled()
	}
	tctx, cancel := context.WithTimeout(ctx, hiddenTimeout)
	defer cancel()
	var stderr bytes.Buffer
	err := ttyRun(tctx, &stderr, "nmcli", "--ask", "device", "wifi", "connect", ssid, "hidden", "yes", "ifname", iface)
	switch {
	case err != nil && ctx.Err() != nil: // Ctrl-C mientras nmcli pedía la clave
		return cancelled()
	case err != nil && tctx.Err() != nil:
		err = errors.New(i18n.T(l, "timed out"))
		actlog.Write(env, id, tg, "", "", err)
		fmt.Fprintln(out, i18n.F(l, "the hidden network %s was not connected", bssid))
	case err != nil:
		// nmcli puede repetir el nombre ("No network with SSID '…'"): no
		// se anota.
		msg := bytes.ReplaceAll(stderr.Bytes(), []byte(ssid), []byte(i18n.T(l, "(hidden network)")))
		err = errors.New(Message(l, "nmcli", msg, err))
		actlog.Write(env, id, tg, "", "", err)
		fmt.Fprintln(out, i18n.F(l, "the hidden network %s was not connected", bssid))
	default:
		res := i18n.F(l, "Connected to the hidden network %s", bssid)
		actlog.Write(env, id, tg, "", res, nil)
		fmt.Fprintln(out, res)
	}
	if hold {
		fmt.Fprint(out, "\n"+i18n.T(l, "Press Enter to close"))
		readLine(ctx, r)
	}
	return err
}

// readLine lee una línea de r o vuelve con false si ctx termina antes: el
// proceso atrapa SIGINT (signal.NotifyContext), así que Ctrl-C llega como
// ctx y no cortaría una lectura bloqueada. La gorutina queda leyendo hasta
// que llegue algo o termine el proceso.
func readLine(ctx context.Context, r *bufio.Reader) (string, bool) {
	ch := make(chan string, 1)
	go func() {
		line, _ := r.ReadString('\n')
		ch <- line
	}()
	select {
	case line := <-ch:
		return line, true
	case <-ctx.Done():
		return "", false
	}
}

// hiddenContext es la línea que encabeza la pregunta: la placa, el BSSID y,
// si la red se ve en una lectura nueva, su banda y su señal.
func hiddenContext(env source.Env, iface, bssid string) string {
	parts := []string{i18n.F(env.Lang, "Hidden network on %s", iface), "BSSID " + bssid}
	if sc, err := current(env); err == nil {
		for _, n := range sc.Ifaces[iface].Networks {
			if n.Hidden && n.BSSID == bssid {
				if n.Band != "" {
					parts = append(parts, n.Band+" GHz")
				}
				parts = append(parts, fmt.Sprintf("%d%%", n.Signal))
				break
			}
		}
	}
	return strings.Join(parts, " · ")
}

// runTTY corre name con la terminal de este proceso (stdin y stdout) y su
// stderr, que también se copia a errOut para anotar el error. Lo que se
// tipea para nmcli (la clave) va directo a él. Si ctx termina, manda
// SIGTERM (no SIGKILL: nmcli pudo apagar el eco para pedir la clave y así
// lo restaura) y solo mata después de ttyWaitDelay.
func runTTY(ctx context.Context, errOut io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, errOut)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = ttyWaitDelay
	return cmd.Run()
}

// ttyWaitDelay es lo que runTTY espera después del SIGTERM.
const ttyWaitDelay = 5 * time.Second
