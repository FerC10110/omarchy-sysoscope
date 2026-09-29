package view

import "testing"

func TestFormats(t *testing.T) {
	cases := []struct{ got, want string }{
		{Bytes(0), "0 B"},
		{Bytes(1023), "1023 B"},
		{Bytes(1536), "1.5 KiB"},
		{Bytes(33554432), "32.0 MiB"},
		{Bytes(34359738368), "32.0 GiB"},
		{Rate(0), "0 B/s"},
		{Rate(999), "999 B/s"},
		{Rate(2100), "2.1 kB/s"},
		{Rate(2100000), "2.1 MB/s"},
		{Pct(23.456), "23%"},
		{Pct(0.4), "0.4%"},
		{Temp(41.3, "c"), "41 °C"},
		{Temp(41.3, "f"), "106 °F"},
		{Temp(-5, "f"), "23 °F"},
		{MHz(3400), "3.40 GHz"},
		{MHz(800), "800 MHz"},
		{Duration(3725, "en"), "1 h 2 min"},
		{Duration(273600, "en"), "3 d 4 h"},
		{Duration(42, "en"), "42 s"},
		{Duration(0, "en"), "0 s"},
		{Duration(273600, "es"), "3 d 4 h"},
		{Watts(20.891), "20.9 W"},
		{Level(79.9, 80, 90), "ok"},
		{Level(80, 80, 90), "warn"},
		{Level(95, 80, 90), "crit"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}
func TestSplit(t *testing.T) {
	for _, c := range []struct{ in, num, unit string }{
		{"7%", "7", "%"},
		{"0.4%", "0.4", "%"},
		{"34 °C", "34", "°C"},
		{"95 °F", "95", "°F"},
		{"1.1 TiB free", "1.1", "TiB free"},
		{"1.1 TiB libres", "1.1", "TiB libres"},
		{"17.1 kB/s", "17.1", "kB/s"},
		{"512 B", "512", "B"},
		{"threadripper", "threadripper", ""},
		{"", "", ""},
	} {
		n, u := Split(c.in)
		if n != c.num || u != c.unit {
			t.Errorf("Split(%q) = %q, %q; want %q, %q", c.in, n, u, c.num, c.unit)
		}
	}
}
