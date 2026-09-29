package view

import (
	"math"
	"strings"
)

// PlainTypes son los tipos de bloque que existen. Cada uno tiene forma simple
// (Plain): así lazylazy dibuja la pestaña como cualquier app lazy, con texto.
var PlainTypes = []string{"kv", "meter", "spark", "table", "text", "group", "cores", "stat", "cards", "hero"}

// Plain es la forma de texto de un bloque, un renglón por línea, sin gráficos
// más allá de los caracteres de bloque. Un tipo desconocido devuelve nil.
func Plain(b Block) []string {
	switch b.Type {
	case "stat":
		return []string{statLine(b)}
	case "hero":
		var parts []string
		for _, s := range b.Blocks {
			parts = append(parts, statLine(s))
		}
		return []string{join(" · ", append(parts, b.Lines...)...)}
	case "cards", "group":
		var out []string
		if b.Title != "" {
			out = append(out, b.Title)
		}
		for _, c := range b.Blocks {
			out = append(out, Plain(c)...)
		}
		return out
	case "meter":
		v := 0.0
		if b.Value != nil {
			v = *b.Value
		}
		return []string{join("  ", b.Label, barText(v, 8)+" "+b.Text)}
	case "spark":
		return []string{join("  ", b.Label, sparkText(b.Values), b.Text)}
	case "cores":
		vs := make([]float64, len(b.Cells))
		for i, c := range b.Cells {
			vs[i] = c.Value
		}
		return []string{join("  ", b.Title, levelsText(vs))}
	case "kv":
		var out []string
		if b.Title != "" {
			out = append(out, b.Title)
		}
		rs, _ := b.Rows.([][2]string)
		for _, r := range rs {
			out = append(out, r[0]+": "+r[1])
		}
		return out
	case "table":
		var out []string
		if b.Title != "" {
			out = append(out, b.Title)
		}
		out = append(out, strings.Join(b.Columns, "  "))
		rs, _ := b.Rows.([]Row)
		for _, r := range rs {
			out = append(out, strings.Join(r.Cells, "  "))
		}
		return out
	case "text":
		return []string{b.Text}
	}
	return nil
}

func statLine(b Block) string {
	return join("  ", strings.ToUpper(b.Label), b.Text+b.unitSuffix(), strings.Join(b.Lines, " · "))
}

// unitSuffix pega "%" al número y separa con espacio las demás unidades.
func (b Block) unitSuffix() string {
	switch b.Unit {
	case "":
		return ""
	case "%":
		return "%"
	}
	return " " + b.Unit
}

// barText es una barra de n caracteres llena en la fracción v.
func barText(v float64, n int) string {
	full := int(math.Round(min(max(v, 0), 1) * float64(n)))
	return strings.Repeat("█", full) + strings.Repeat("░", n-full)
}

// sparkText escala los valores de 0 al máximo en ▁…█. Vacío si no hay valores.
func sparkText(vs []float64) string {
	const ticks = "▁▂▃▄▅▆▇█"
	hi := 0.0
	for _, v := range vs {
		hi = max(hi, v)
	}
	var sb strings.Builder
	for _, v := range vs {
		i := 0
		if hi > 0 {
			i = int(math.Round(max(v, 0) / hi * 7))
		}
		sb.WriteString(string([]rune(ticks)[i]))
	}
	return sb.String()
}

// levelsText dibuja valores de 0 a 1 en ▁…█ con escala fija (los núcleos).
func levelsText(vs []float64) string {
	const ticks = "▁▂▃▄▅▆▇█"
	var sb strings.Builder
	for _, v := range vs {
		sb.WriteString(string([]rune(ticks)[int(math.Round(min(max(v, 0), 1)*7))]))
	}
	return sb.String()
}
