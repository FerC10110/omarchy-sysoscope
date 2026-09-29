package gpu

import (
	"bufio"
	"context"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// loopRetry es la espera mínima antes de relanzar un nvidia-smi en bucle que murió.
const loopRetry = time.Minute

// smiLoop es un nvidia-smi en modo bucle (-lms): un proceso de larga vida
// cuesta mucha menos CPU que uno nuevo por lectura. Solo se usa con GPUs que
// nunca se suspenden (power/control = on): el bucle no deja dormir a la GPU.
type smiLoop struct {
	every   time.Duration
	started time.Time
	cancel  context.CancelFunc

	mu   sync.Mutex
	rows map[string]smiRow
	at   time.Time
	dead bool
}

// alwaysOn dice si todas las GPU NVIDIA despiertas tienen el runtime PM apagado.
func alwaysOn(env source.Env, gpus []model.GPU) bool {
	found := false
	for _, g := range gpus {
		if g.Vendor != "nvidia" || g.Suspended {
			continue
		}
		c, err := env.FS.Read(devDir(g.Card) + "/power/control")
		if err != nil || strings.TrimSpace(c) != "on" {
			return false
		}
		found = true
	}
	return found
}

// loopRows arranca, vigila o relanza el bucle y devuelve sus filas frescas
// (nil si no hay: todavía no llegó nada, se colgó o murió).
func (s *Sampler) loopRows(env source.Env, gpus []model.GPU, now time.Time) map[string]smiRow {
	st, ok := env.Run.(source.Streamer)
	if s.Loop <= 0 || !ok || !alwaysOn(env, gpus) {
		s.Close()
		return nil
	}
	if l := s.loop; l != nil && l.every != s.Loop {
		s.Close()
	}
	if l := s.loop; l != nil {
		if l.stalled(now) {
			l.cancel()
		}
		if !l.ended() || now.Sub(l.started) < loopRetry {
			return l.latest(now) // las últimas filas valen mientras sean frescas
		}
		s.Close()
	}
	args := append(append([]string{}, SMIArgs...), "-lms", strconv.FormatInt(s.Loop.Milliseconds(), 10))
	ctx, cancel := context.WithCancel(context.Background())
	rc, err := st.Stream(ctx, "nvidia-smi", args...)
	if err != nil {
		cancel()
		return nil
	}
	l := &smiLoop{every: s.Loop, started: now, cancel: cancel}
	s.loop = l
	go l.read(rc, env.Now)
	return nil
}

// stalled dice si el bucle lleva 3 períodos sin escribir (nvidia-smi colgado).
func (l *smiLoop) stalled(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	last := l.at
	if last.IsZero() {
		last = l.started
	}
	if !l.dead && now.Sub(last) > 3*l.every {
		l.dead = true
		return true
	}
	return false
}

func (l *smiLoop) ended() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dead
}

// read guarda cada tanda de filas hasta que el proceso termina.
func (l *smiLoop) read(rc io.ReadCloser, clock func() time.Time) {
	defer rc.Close()
	sc := bufio.NewScanner(rc)
	for sc.Scan() {
		rows := parseSMI(sc.Text())
		if len(rows) == 0 {
			continue
		}
		now := time.Now()
		if clock != nil {
			now = clock()
		}
		l.mu.Lock()
		if l.rows == nil {
			l.rows = map[string]smiRow{}
		}
		for _, r := range rows {
			l.rows[busKey(r.bus)] = r
		}
		l.at = now
		l.mu.Unlock()
	}
	l.mu.Lock()
	l.dead = true
	l.mu.Unlock()
}

// latest devuelve las últimas filas si no son más viejas que 3 períodos.
func (l *smiLoop) latest(now time.Time) map[string]smiRow {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rows == nil || now.Sub(l.at) > 3*l.every {
		return nil
	}
	out := make(map[string]smiRow, len(l.rows))
	for k, v := range l.rows {
		out[k] = v
	}
	return out
}

// Close termina el nvidia-smi en bucle, si hay uno.
func (s *Sampler) Close() {
	if s.loop != nil {
		s.loop.cancel()
		s.loop = nil
	}
}
