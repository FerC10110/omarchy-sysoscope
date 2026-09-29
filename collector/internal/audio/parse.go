// Package audio lee las salidas y entradas de audio con pactl (la capa de
// PulseAudio de PipeWire) y corre las acciones audio.*: predeterminada,
// silencio y volumen.
package audio

import (
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// volumeNorm es el 100 % de PulseAudio (PA_VOLUME_NORM).
const volumeNorm = 65536

// pactlNode es lo que se usa de un elemento de `pactl --format=json list
// sinks|sources`.
type pactlNode struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mute        bool   `json:"mute"`
	Volume      map[string]struct {
		Value int64 `json:"value"`
	} `json:"volume"`
	Properties map[string]any `json:"properties"`
	Ports      []struct {
		Name         string `json:"name"`
		Availability string `json:"availability"`
	} `json:"ports"`
}

// defaults es lo que se usa de `pactl --format=json info`.
type defaults struct {
	Sink   string `json:"default_sink_name"`
	Source string `json:"default_source_name"`
}

func (n pactlNode) prop(k string) string {
	s, _ := n.Properties[k].(string)
	return s
}

// monitor dice si una entrada es el monitor de una salida (lo que suena en
// ella), que la barra no ofrece como micrófono.
func (n pactlNode) monitor() bool {
	return n.prop("device.class") == "monitor" || strings.HasSuffix(n.Name, ".monitor")
}

// node es el nodo para el modelo: el id de PipeWire (object.id, el que piden
// los scripts de Omarchy), la etiqueta como la barra (node.nick, si no la
// descripción) y el volumen promedio de los canales. def es el nombre del
// predeterminado.
func (n pactlNode) node(def string) model.AudioNode {
	label := strings.TrimSpace(n.prop("node.nick"))
	if label == "" {
		label = strings.TrimSpace(n.Description)
	}
	if label == "" {
		label = n.Name
	}
	return model.AudioNode{ID: n.prop("object.id"), Name: n.Name, Label: label, Volume: percent(n), Muted: n.Mute,
		Default: n.Name != "" && n.Name == def}
}

// percent es el volumen en %, redondeado, del promedio de los canales.
func percent(n pactlNode) int {
	if len(n.Volume) == 0 {
		return 0
	}
	var sum int64
	for _, c := range n.Volume {
		sum += c.Value
	}
	den := int64(len(n.Volume)) * volumeNorm
	return int((sum*100 + den/2) / den)
}

// parseAvailability lee la salida de omarchy-audio-sink-availability
// ("nombre\t0|1" por línea) como la barra: lo que no es "0" está disponible.
func parseAvailability(out []byte) map[string]bool {
	m := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		name, v, ok := strings.Cut(strings.TrimSpace(l), "\t")
		if ok && name != "" {
			m[name] = strings.TrimSpace(v) != "0"
		}
	}
	return m
}

// availKey resume las salidas y la disponibilidad de sus puertos: de eso
// depende la respuesta del script (una afinación de parlantes aparece como
// una salida más).
func availKey(sinks []pactlNode) string {
	var b strings.Builder
	for _, n := range sinks {
		b.WriteString(n.Name)
		for _, p := range n.Ports {
			b.WriteString("|" + p.Name + "=" + p.Availability)
		}
		b.WriteString("\n")
	}
	return b.String()
}
