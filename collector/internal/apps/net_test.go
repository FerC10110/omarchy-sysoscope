package apps

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestParseSS(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(source.FixturesDir(t), "apps", "cmd", "ss_-tipnH.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ss := parseSS(string(b))
	if len(ss) != 3 {
		t.Fatalf("%d sockets: %+v", len(ss), ss)
	}
	if s := ss[0]; s.pid != 201 || s.rx != 93376 || s.tx != 106913 || s.key != "192.168.1.10:39172 203.0.113.54:443" {
		t.Fatalf("first socket = %+v", s)
	}
	if s := ss[2]; s.pid != 500 || s.rx != 70000 || s.tx != 5000 {
		t.Fatalf("syncthing socket = %+v", s)
	}
}

func TestParseSSSkipsSocketsWithoutProcess(t *testing.T) {
	out := "ESTAB 0 0 10.0.0.1:1 10.0.0.2:2\n\t bytes_received:5\n" +
		"ESTAB 0 0 10.0.0.1:3 10.0.0.2:4 users:((\"x\",pid=9,fd=3))\n\t bytes_received:7 bytes_acked:1\n"
	ss := parseSS(out)
	if len(ss) != 1 || ss[0].pid != 9 || ss[0].rx != 7 {
		t.Fatalf("sockets = %+v", ss)
	}
}

func TestNetRates(t *testing.T) {
	t0 := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	var m netMeter
	if r := m.rates([]sock{{key: "a", pid: 1, rx: 1000, tx: 100}}, t0); len(r) != 0 {
		t.Fatalf("first read gave rates: %v", r)
	}
	r := m.rates([]sock{
		{key: "a", pid: 1, rx: 2000, tx: 150}, // +1000 / +50 en 10 s
		{key: "b", pid: 1, rx: 500},           // nuevo: cuenta entero
		{key: "c", pid: 2, rx: 10, tx: 10},    // nuevo en otro proceso
	}, t0.Add(10*time.Second))
	if r[1] != [2]float64{150, 5} || r[2] != [2]float64{1, 1} {
		t.Fatalf("rates = %v", r)
	}
	// Un contador que bajó (socket reusado) no da una tasa negativa.
	r = m.rates([]sock{{key: "a", pid: 1, rx: 10, tx: 10}}, t0.Add(20*time.Second))
	if r[1] != [2]float64{0, 0} {
		t.Fatalf("after reset = %v", r)
	}
}
