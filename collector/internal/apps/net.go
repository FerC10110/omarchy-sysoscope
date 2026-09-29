package apps

import (
	"strconv"
	"strings"
	"time"
)

// SSArgs son los argumentos de ss: TCP, con datos internos, procesos, sin
// resolver nombres ni encabezado.
var SSArgs = []string{"-tipnH"}

// sock es un socket TCP con el proceso que lo usa y sus contadores.
type sock struct {
	key    string // origen y destino
	pid    int
	rx, tx uint64
}

// parseSS lee `ss -tipnH`: una línea por socket y, debajo y con sangría, sus
// datos internos (bytes_received y bytes_acked, lo enviado que llegó).
func parseSS(out string) []sock {
	var socks []sock
	cur := -1
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if l[0] != ' ' && l[0] != '\t' {
			cur = -1
			f := strings.Fields(l)
			if len(f) < 5 {
				continue
			}
			if pid := pidOf(l); pid > 0 {
				socks = append(socks, sock{key: f[3] + " " + f[4], pid: pid})
				cur = len(socks) - 1
			}
			continue
		}
		if cur < 0 {
			continue
		}
		for _, w := range strings.Fields(l) {
			if v, ok := strings.CutPrefix(w, "bytes_received:"); ok {
				socks[cur].rx, _ = strconv.ParseUint(v, 10, 64)
			} else if v, ok := strings.CutPrefix(w, "bytes_acked:"); ok {
				socks[cur].tx, _ = strconv.ParseUint(v, 10, 64)
			}
		}
	}
	return socks
}

// pidOf es el primer pid de users:(("chromium",pid=2843,fd=48)).
func pidOf(l string) int {
	i := strings.Index(l, "pid=")
	if i < 0 {
		return 0
	}
	j := i + 4
	for j < len(l) && l[j] >= '0' && l[j] <= '9' {
		j++
	}
	n, _ := strconv.Atoi(l[i+4 : j])
	return n
}

// netMeter calcula tasas por diferencia entre dos lecturas de ss.
type netMeter struct {
	last map[string][2]uint64
	at   time.Time
}

// rates devuelve bytes/s por pid desde la lectura anterior. Un socket que no
// estaba es nuevo: todo lo que movió cuenta. La primera lectura no da tasas.
func (m *netMeter) rates(socks []sock, now time.Time) map[int][2]float64 {
	out := map[int][2]float64{}
	next := make(map[string][2]uint64, len(socks))
	dt := now.Sub(m.at).Seconds()
	for _, s := range socks {
		next[s.key] = [2]uint64{s.rx, s.tx}
		if m.at.IsZero() || dt <= 0 {
			continue
		}
		prev := m.last[s.key]
		r := out[s.pid]
		if s.rx >= prev[0] {
			r[0] += float64(s.rx-prev[0]) / dt
		}
		if s.tx >= prev[1] {
			r[1] += float64(s.tx-prev[1]) / dt
		}
		out[s.pid] = r
	}
	m.last, m.at = next, now
	return out
}
