package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

type runCall struct{ id, target, input string }

// stubApps reemplaza apps.Run: window.to_workspace funciona, lo demás falla.
// El stream la llama desde goroutines: calls va con mutex.
func stubApps(t *testing.T) *[]runCall {
	t.Helper()
	var mu sync.Mutex
	var calls []runCall
	old := appsRun
	appsRun = func(_ source.Env, id, target, input string) (string, error) {
		mu.Lock()
		calls = append(calls, runCall{id, target, input})
		mu.Unlock()
		if id == "window.to_workspace" {
			return "Move to workspace: " + target, nil
		}
		return "", errors.New("the window no longer exists")
	}
	t.Cleanup(func() { appsRun = old })
	return &calls
}

func TestLazylazyAppsAction(t *testing.T) {
	calls := stubApps(t)
	a, _, out, errb := newApp(t, "")
	if code := a.run([]string{"lazylazy", "action", "window.to_workspace", "0xa1", "4"}); code != 0 || out.String() != "Move to workspace: 0xa1\n" {
		t.Fatalf("exit %d, out %q", code, out.String())
	}
	if code := a.run([]string{"lazylazy", "action", "window.float", "0xdead"}); code != 1 || errb.String() != "the window no longer exists\n" {
		t.Fatalf("exit %d, err %q", code, errb.String())
	}
	if (*calls)[0] != (runCall{"window.to_workspace", "0xa1", "4"}) || (*calls)[1] != (runCall{"window.float", "0xdead", ""}) {
		t.Fatalf("calls = %+v", *calls)
	}
}

func TestActionCommand(t *testing.T) {
	calls := stubApps(t)
	a, _, out, _ := newApp(t, "")
	if code := a.run([]string{"action", "window.to_workspace", "app:comm:Web Content", "2"}); code != 0 || !strings.Contains(out.String(), "Web Content") {
		t.Fatalf("exit %d, out %q", code, out.String())
	}
	if code := a.run([]string{"action", "cpu.nope"}); code != 2 || len(*calls) != 1 {
		t.Fatal("an id outside window.*/app.* must be refused before running")
	}
}

func TestLazylazyAppsKeys(t *testing.T) {
	a, fake, out, _ := newApp(t, "")
	if code := a.run([]string{"lazylazy", "view", "apps"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(fake.focus) == 0 || fake.focus[0] != "apps" {
		t.Fatalf("focus = %v", fake.focus)
	}
	var v struct{ Actions []struct{ Key, ID string } }
	json.Unmarshal(out.Bytes(), &v)
	for _, x := range v.Actions {
		if x.Key == "enter" {
			t.Fatal("lazylazy reserves enter")
		}
		if x.ID == "window.focus" && x.Key != "g" {
			t.Fatalf("focus key = %q", x.Key)
		}
	}
}

// TestStreamActionResult usa pipes: el resultado llega en otra goroutine,
// después de que el stream ya leyó el comando.
func TestStreamActionResult(t *testing.T) {
	calls := stubApps(t)
	a, _, _, _ := newApp(t, "")
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	a.stdin, a.stdout = inR, outW
	done := make(chan int, 1)
	go func() { done <- a.run([]string{"stream", "--interval", "1h"}) }()
	go func() {
		io.WriteString(inW, `action window.to_workspace ["app:comm:Web Content","4"]`+"\n")
		io.WriteString(inW, `action window.float ["0xdead"]`+"\n")
	}()
	type line struct {
		Result *actionResult `json:"result"`
	}
	var got []actionResult
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	timeout := time.After(5 * time.Second)
	for len(got) < 2 {
		select {
		case l := <-lines:
			var x line
			if json.Unmarshal([]byte(l), &x) == nil && x.Result != nil {
				got = append(got, *x.Result)
			}
		case <-timeout:
			t.Fatalf("results = %+v", got)
		}
	}
	go func() {
		for range lines { // que el stream nunca quede trabado escribiendo
		}
	}()
	inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
	outW.Close()
	// Las dos acciones corren a la vez: llegan a appsRun en cualquier orden.
	want := map[runCall]bool{{"window.to_workspace", "app:comm:Web Content", "4"}: true, {"window.float", "0xdead", ""}: true}
	if len(*calls) != 2 || !want[(*calls)[0]] || !want[(*calls)[1]] || (*calls)[0] == (*calls)[1] {
		t.Fatalf("calls = %+v", *calls)
	}
	byID := map[string]actionResult{got[0].ID: got[0], got[1].ID: got[1]}
	if r := byID["window.to_workspace"]; !r.OK || r.Text != "Move to workspace: app:comm:Web Content" {
		t.Fatalf("ok result = %+v", r)
	}
	if r := byID["window.float"]; r.OK || r.Text != "the window no longer exists" {
		t.Fatalf("error result = %+v", r)
	}
}

// pipeStream arranca un stream con pipes y devuelve su stdin, sus líneas
// (leídas siempre, para que el stream nunca quede trabado escribiendo) y
// su código de salida.
func pipeStream(t *testing.T, a *app) (*io.PipeWriter, <-chan string, <-chan int) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	a.stdin, a.stdout = inR, outW
	done := make(chan int, 1)
	go func() {
		done <- a.run([]string{"stream", "--interval", "1h"})
		outW.Close()
	}()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	t.Cleanup(func() { inW.Close() })
	return inW, lines, done
}

// waitResults junta resultados de las líneas hasta tener n.
func waitResults(t *testing.T, lines <-chan string, n int) []actionResult {
	t.Helper()
	var got []actionResult
	timeout := time.After(5 * time.Second)
	for len(got) < n {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed; results = %+v", got)
			}
			var x struct {
				Result *actionResult `json:"result"`
			}
			if json.Unmarshal([]byte(l), &x) == nil && x.Result != nil {
				got = append(got, *x.Result)
			}
		case <-timeout:
			t.Fatalf("results = %+v", got)
		}
	}
	return got
}

// Pausado, ningún resultado se pierde: cada uno sale en su propia línea.
func TestStreamPausedKeepsResults(t *testing.T) {
	calls := stubApps(t)
	a, _, _, _ := newApp(t, "")
	inW, lines, done := pipeStream(t, a)
	io.WriteString(inW, "pause\n")
	io.WriteString(inW, `action window.to_workspace ["0xa1","2"]`+"\n")
	io.WriteString(inW, `action window.float ["0xdead"]`+"\n")
	io.WriteString(inW, `action window.to_workspace ["0xb2","3"]`+"\n")
	got := waitResults(t, lines, 3)
	inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
	texts := map[string]bool{}
	for _, r := range got {
		texts[r.Text] = true
	}
	if len(*calls) != 3 || !texts["Move to workspace: 0xa1"] || !texts["Move to workspace: 0xb2"] || !texts["the window no longer exists"] {
		t.Fatalf("results = %+v, calls = %+v", got, *calls)
	}
}

// Al cerrarse stdin, el stream espera la acción en curso (forzar el cierre
// tiene que llegar a mandar SIGKILL) antes de volver.
func TestStreamWaitsForActions(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	finished := false
	old := appsRun
	appsRun = func(_ source.Env, id, target, input string) (string, error) {
		<-release
		mu.Lock()
		finished = true
		mu.Unlock()
		return "killed", nil
	}
	t.Cleanup(func() { appsRun = old })
	a, _, _, _ := newApp(t, "")
	inW, lines, done := pipeStream(t, a)
	go func() {
		for range lines {
		}
	}()
	io.WriteString(inW, `action app.kill ["app:comm:foo"]`+"\n")
	inW.Close()
	select {
	case code := <-done:
		t.Fatalf("stream returned (%d) before the action finished", code)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not return after the action finished")
	}
	mu.Lock()
	defer mu.Unlock()
	if !finished {
		t.Fatal("the action did not finish")
	}
}

// Una acción que no termina no traba la salida más que actionWait.
func TestStreamActionWaitCap(t *testing.T) {
	release, returned := make(chan struct{}), make(chan struct{})
	old, oldWait := appsRun, actionWait
	appsRun = func(source.Env, string, string, string) (string, error) {
		<-release
		close(returned)
		return "", nil
	}
	actionWait = 50 * time.Millisecond
	// La acción trabada sigue viva cuando el stream sale: se la suelta y se
	// espera a que vuelva antes de restaurar appsRun (si no, -race avisa).
	t.Cleanup(func() {
		close(release)
		<-returned
		appsRun, actionWait = old, oldWait
	})
	a, _, _, _ := newApp(t, "")
	inW, lines, done := pipeStream(t, a)
	go func() {
		for range lines {
		}
	}()
	io.WriteString(inW, `action app.kill ["app:comm:foo"]`+"\n")
	inW.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a stuck action hung the stream's exit")
	}
}

func TestStreamActionBadJSON(t *testing.T) {
	calls := stubApps(t)
	a, _, _, errb := newApp(t, "action app.kill {bad\nquit\n")
	if code := a.run([]string{"stream", "--interval", "1h"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errb.String(), "usage: action ID [a JSON array with the row and the input]") || len(*calls) != 0 {
		t.Fatalf("stderr %q, calls %+v", errb.String(), *calls)
	}
}
