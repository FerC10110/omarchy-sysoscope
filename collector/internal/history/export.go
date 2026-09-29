package history

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
)

// WriteCSV escribe "time,<métrica>" con tiempos RFC 3339 en UTC; vacío = sin dato.
func WriteCSV(w io.Writer, metric string, pts []Point) error {
	if _, err := fmt.Fprintf(w, "time,%s\n", metric); err != nil {
		return err
	}
	for _, p := range pts {
		v := ""
		if p.V != nil {
			v = strconv.FormatFloat(*p.V, 'f', -1, 64)
		}
		if _, err := fmt.Fprintf(w, "%s,%s\n", p.T.UTC().Format(time.RFC3339), v); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSON escribe {"metric": …, "points": [{"t": …, "v": …|null}]}.
func WriteJSON(w io.Writer, metric string, pts []Point) error {
	type pt struct {
		T string   `json:"t"`
		V *float64 `json:"v"`
	}
	out := struct {
		Schema int    `json:"schema"`
		Metric string `json:"metric"`
		Points []pt   `json:"points"`
	}{Schema: 1, Metric: metric, Points: make([]pt, len(pts))}
	for i, p := range pts {
		out.Points[i] = pt{p.T.UTC().Format(time.RFC3339), p.V}
	}
	return json.NewEncoder(w).Encode(out)
}
