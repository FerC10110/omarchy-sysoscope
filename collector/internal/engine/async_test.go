package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// blockingRunner frena fwupdmgr hasta que se cierra release.
type blockingRunner struct {
	*source.FakeRunner
	release chan struct{}
	fw      atomic.Int32
}

func (b *blockingRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "fwupdmgr" {
		b.fw.Add(1)
		<-b.release
		return []byte("No updatable devices"), nil
	}
	return b.FakeRunner.Run(ctx, name, args...)
}

func TestCheckFirmwareDoesNotBlockTick(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	br := &blockingRunner{FakeRunner: runs(env), release: make(chan struct{})}
	env.Run = br
	e := newEngine(t, env, "")
	done := make(chan bool)
	go func() {
		e.CheckFirmware()
		s := e.Tick()
		done <- s.Firmware == nil
	}()
	select {
	case nilFw := <-done:
		if !nilFw {
			t.Fatal("firmware present before fwupdmgr answered")
		}
	case <-time.After(time.Second):
		close(br.release)
		t.Fatal("CheckFirmware + Tick blocked on fwupdmgr")
	}
	e.CheckFirmware() // mientras corre: no lanza otro
	close(br.release)
	deadline := time.Now().Add(2 * time.Second)
	for e.Tick().Firmware == nil {
		if time.Now().After(deadline) {
			t.Fatal("firmware never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := br.fw.Load(); n != 1 {
		t.Fatalf("fwupdmgr ran %d times, want 1", n)
	}
}

func TestSecurityFocusReadsPrivacy(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	e := newEngine(t, env, "security")
	if s := e.Tick(); s.Privacy == nil || s.Privacy.Camera == nil {
		t.Fatalf("privacy = %+v", s.Privacy)
	}
	e.SetFocus("")
	if s := e.Tick(); s.Privacy != nil {
		t.Fatalf("privacy without focus = %+v", s.Privacy)
	}
}

func TestSetLangRetranslates(t *testing.T) {
	env := source.Fixture(t, "minimal")
	e := newEngine(t, env, "")
	e.Tick()
	e.SetLang("es")
	s := e.Tick()
	if s.Lang != "es" {
		t.Fatalf("lang = %q", s.Lang)
	}
	for _, u := range s.Unavailable {
		if english(u.Message) {
			t.Errorf("%s still in English: %q", u.What, u.Message)
		}
	}
}

func english(msg string) bool {
	for _, w := range []string{"not present", "requires ", "permission denied", "could not read", "timed out"} {
		if strings.Contains(msg, w) {
			return true
		}
	}
	return false
}

// Un caché escrito en otro idioma no se usa: sus mensajes vienen traducidos.
func TestStaticCacheKeepsLanguage(t *testing.T) {
	env := source.Fixture(t, "threadripper") // tiene boot_id: usa el caché
	e := newEngine(t, env, "")
	c := e.loadCache(e.bootID())
	if c == nil {
		t.Fatal("no cache written")
	}
	c.Unavailable = []model.Unavailable{{What: "memory.modules", Reason: "missing_tool", Message: "requires inxi"}}
	e.saveCache(c)
	env.Lang = "es"
	s := newEngine(t, env, "").Tick()
	for _, u := range s.Unavailable {
		if english(u.Message) {
			t.Errorf("%s still in English: %q", u.What, u.Message)
		}
	}
}
