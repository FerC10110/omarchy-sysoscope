package audio

import (
	"context"
	"encoding/json"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// availScript es el script de Omarchy con el que la barra decide qué
// salidas ofrecer: marca con 0 un HDMI sin cable o el parlante que está
// detrás de una afinación.
const availScript = "omarchy-audio-sink-availability"

// retryAfter: tras una lectura fallida (sin pactl, sin servidor o colgado)
// no se vuelve a probar antes; así un pactl que no responde no traba cada
// muestra.
const retryAfter = 10 * time.Second

// Reader lee el audio. Recuerda la respuesta del script de disponibilidad
// (se vuelve a correr solo si cambian las salidas o sus puertos) y, tras una
// falla, cuándo volver a probar. El cero sirve.
type Reader struct {
	availKey string          // salidas y puertos con que se corrió el script
	availOK  bool            // ya se corrió con availKey
	avail    map[string]bool // nombre → disponible; nil = todas
	retryAt  time.Time
	failed   []model.Unavailable
}

// Reset olvida lo recordado: la próxima lectura es completa (después de
// una acción audio.*).
func (r *Reader) Reset() { *r = Reader{} }

// Read corre `pactl --format=json list sinks`, `… list sources` e `… info`
// y arma las salidas (sin las que Omarchy marca no disponibles) y las
// entradas (sin monitores). Sin pactl, sin servidor o con una respuesta
// rara: nil y "system.sound" en rep (la vista no lo muestra).
func (r *Reader) Read(ctx context.Context, env source.Env, rep *collect.Report) *model.Sound {
	now := env.Now()
	if now.Before(r.retryAt) {
		rep.Keep(r.failed)
		return nil
	}
	s, err := r.read(ctx, env, true)
	if err != nil {
		sub := &collect.Report{Lang: rep.Lang}
		sub.FromErr("system.sound", "pactl", err)
		r.failed, r.retryAt = sub.List, now.Add(retryAfter)
		rep.Keep(sub.List)
		return nil
	}
	r.failed, r.retryAt = nil, time.Time{}
	return s
}

// read lee las tres pactl. checkAvail dice si hay que correr (o reusar) el
// script de disponibilidad de Omarchy y esconder con él las salidas: una
// acción sobre una fila puntual (mute, volumen) no lo necesita, porque
// busca esa fila por nombre exacto sea o no visible; audio.default sí,
// porque no se puede hacer predeterminada una salida que Omarchy esconde.
func (r *Reader) read(ctx context.Context, env source.Env, checkAvail bool) (*model.Sound, error) {
	var sinks, sources []pactlNode
	var def defaults
	for _, q := range []struct {
		args []string
		dst  any
	}{
		{[]string{"--format=json", "list", "sinks"}, &sinks},
		{[]string{"--format=json", "list", "sources"}, &sources},
		{[]string{"--format=json", "info"}, &def},
	} {
		out, err := env.Run.Run(ctx, "pactl", q.args...)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(out, q.dst); err != nil {
			return nil, err
		}
	}
	if checkAvail {
		if k := availKey(sinks); !r.availOK || k != r.availKey {
			r.avail, r.availKey, r.availOK = availability(ctx, env), k, true
		}
	}
	s := &model.Sound{Outputs: []model.AudioNode{}, Inputs: []model.AudioNode{}}
	for _, n := range sinks {
		if checkAvail {
			if ok, known := r.avail[n.Name]; known && !ok {
				continue
			}
		}
		s.Outputs = append(s.Outputs, n.node(def.Sink))
	}
	for _, n := range sources {
		if !n.monitor() {
			s.Inputs = append(s.Inputs, n.node(def.Source))
		}
	}
	return s, nil
}

// availability corre el script de Omarchy. Sin él, o si falla, nil: se
// muestran todas las salidas.
func availability(ctx context.Context, env source.Env) map[string]bool {
	out, err := env.Run.Run(ctx, availScript)
	if err != nil {
		return nil
	}
	return parseAvailability(out)
}
