package network

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func iface(t *testing.T, n *model.Network, name string) *model.Iface {
	t.Helper()
	for i := range n.Interfaces {
		if n.Interfaces[i].Name == name {
			return &n.Interfaces[i]
		}
	}
	t.Fatalf("iface %s not found", name)
	return nil
}

func noAddrs(string) ([]string, []string) { return nil, nil }

func TestStaticThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	n := &model.Network{Interfaces: Static(env, &collect.Report{Lang: "en"})}
	if len(n.Interfaces) != 8 {
		t.Fatalf("interfaces = %d", len(n.Interfaces))
	}
	for name, want := range map[string][2]any{
		"lo": {"loopback", true}, "docker0": {"bridge", true}, "br-300c5ee3c149": {"bridge", true},
		"enp5s0": {"ethernet", false}, "enp10s0": {"ethernet", false}, "wlp3s0": {"wifi", false},
	} {
		i := iface(t, n, name)
		if i.Kind != want[0] || i.Virtual != want[1] {
			t.Errorf("%s = %s virtual=%v, want %v", name, i.Kind, i.Virtual, want)
		}
	}
	if iface(t, n, "enp5s0").MAC != "02:00:00:00:00:05" || iface(t, n, "lo").MAC != "" {
		t.Errorf("mac = %q / lo %q", iface(t, n, "enp5s0").MAC, iface(t, n, "lo").MAC)
	}
}

func TestRates(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	r := &collect.Report{Lang: "en"}
	n := &model.Network{Interfaces: Static(env, r)}
	s := Sampler{Addrs: noAddrs}
	s.Sample(env, n, false, r)
	e := iface(t, n, "enp5s0")
	if !e.Up || e.SpeedMbps == nil || *e.SpeedMbps != 1000 || e.RxBps != nil || e.RxTotalB == 0 {
		t.Fatalf("enp5s0 first = %+v", e)
	}
	if d := iface(t, n, "docker0"); d.Up || d.SpeedMbps != nil {
		t.Errorf("docker0 = %+v", d)
	}
	dst := t.TempDir()
	os.CopyFS(dst, os.DirFS(env.FS.Root))
	data, _ := os.ReadFile(filepath.Join(source.FixturesDir(t), "threadripper/netdev.2"))
	os.WriteFile(filepath.Join(dst, "proc/net/dev"), data, 0o644)
	env.FS.Root = dst
	now = now.Add(2 * time.Second)
	s.Sample(env, n, false, r)
	e = iface(t, n, "enp5s0")
	if e.RxBps == nil || *e.RxBps != 1e6 || *e.TxBps != 0 {
		t.Fatalf("enp5s0 rate = %v %v", e.RxBps, e.TxBps)
	}
	if l := iface(t, n, "lo"); l.RxBps == nil || *l.RxBps != 5e6 {
		t.Errorf("lo rate = %v", l.RxBps)
	}
	if n.RxBps != 1e6 || n.TxBps != 0 {
		t.Fatalf("totals = %v/%v (lo must not count)", n.RxBps, n.TxBps)
	}
}

func TestGatewayDNS(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	n := &model.Network{Interfaces: Static(env, r)}
	s := Sampler{Addrs: func(name string) ([]string, []string) {
		if name == "enp5s0" {
			return []string{"192.168.1.20/24"}, []string{"fe80::1/64"}
		}
		return nil, nil
	}}
	s.Sample(env, n, true, r)
	if n.Gateway != "192.168.1.1" || n.GatewayIface != "enp5s0" {
		t.Errorf("gateway = %q %q", n.Gateway, n.GatewayIface)
	}
	if !reflect.DeepEqual(n.DNS, []string{"8.8.8.8", "8.8.4.4", "2001:4860:4860::8888"}) {
		t.Errorf("dns = %v", n.DNS)
	}
	if e := iface(t, n, "enp5s0"); !reflect.DeepEqual(e.IPv4, []string{"192.168.1.20/24"}) || len(e.IPv6) != 1 {
		t.Errorf("addrs = %v %v", e.IPv4, e.IPv6)
	}
	if runs := env.Run.(*source.FakeRunner); runs.Count("iw") != 0 {
		t.Errorf("iw called for Wi-Fi interfaces that are down: %v", runs.Calls)
	}
}

func wifiRoot(t *testing.T, withIw bool) source.Env {
	t.Helper()
	root, cmd := t.TempDir(), t.TempDir()
	w := func(rel, content string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	w("sys/class/net/wlan0/type", "1\n")
	w("sys/class/net/wlan0/operstate", "up\n")
	w("sys/class/net/wlan0/address", "02:00:00:00:00:09\n")
	w("sys/class/net/wlan0/device/.keep", "")
	w("sys/class/net/wlan0/wireless/.keep", "")
	w("proc/net/dev", "Inter-|\n face |\n wlan0: 100 1 0 0 0 0 0 0 200 2 0 0 0 0 0 0\n")
	w("proc/net/wireless", "Inter-| sta-|   Quality        |\n face | tus | link level noise |\n wlan0: 0000   58.  -52.  -256        0      0      0      0     10        0\n")
	if withIw {
		os.WriteFile(filepath.Join(cmd, source.Key("iw", "dev", "wlan0", "link")+".txt"),
			[]byte("Connected to aa:bb:cc:dd:ee:ff (on wlan0)\n\tSSID: casa\n\tfreq: 5180\n\tsignal: -52 dBm\n\ttx bitrate: 866.7 MBit/s VHT-MCS 9 80MHz short GI VHT-NSS 2\n"), 0o644)
	}
	return source.Env{FS: source.FS{Root: root}, Run: &source.FakeRunner{Dir: cmd}, Bus: &source.FakeBus{},
		Now: func() time.Time { return source.FixtureNow }, Lang: "en"}
}

func TestWifiIw(t *testing.T) {
	env := wifiRoot(t, true)
	r := &collect.Report{Lang: "en"}
	n := &model.Network{Interfaces: Static(env, r)}
	s := Sampler{Addrs: noAddrs}
	s.Sample(env, n, true, r)
	w := n.Interfaces[0].Wifi
	if w == nil || w.SSID != "casa" || w.SignalDBm == nil || *w.SignalDBm != -52 || w.BitrateMbps == nil || *w.BitrateMbps != 866.7 {
		t.Fatalf("wifi = %+v", w)
	}
	if len(r.List) != 0 {
		t.Errorf("unavailable = %+v", r.List)
	}
}

func TestNoIw(t *testing.T) {
	env := wifiRoot(t, false)
	r := &collect.Report{Lang: "en"}
	n := &model.Network{Interfaces: Static(env, r)}
	s := Sampler{Addrs: noAddrs}
	s.Sample(env, n, true, r)
	w := n.Interfaces[0].Wifi
	if w == nil || w.SSID != "" || w.SignalDBm == nil || *w.SignalDBm != -52 {
		t.Fatalf("wifi without iw = %+v (signal comes from /proc/net/wireless)", w)
	}
	if len(r.List) != 1 || r.List[0].What != "network.wifi.ssid" || r.List[0].Reason != "missing_tool" {
		t.Fatalf("unavailable = %+v", r.List)
	}
	if !n.Interfaces[0].Up || n.Interfaces[0].RxTotalB != 100 {
		t.Errorf("rest of network broken: %+v", n.Interfaces[0])
	}
}

func TestNetlinkAddrsLoopback(t *testing.T) {
	addrs := netlinkAddrs(source.Env{FS: source.FS{}}, []model.Iface{{Name: "lo"}})
	v4, _ := addrs("lo")
	if !reflect.DeepEqual(v4, []string{"127.0.0.1/8"}) {
		t.Fatalf("lo v4 = %v", v4)
	}
	if v4, v6 := addrs("nope0"); v4 != nil || v6 != nil {
		t.Fatal("unknown interface must have no addresses")
	}
}
