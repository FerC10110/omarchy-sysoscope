package ondisk

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// sentinels es lo que no puede llegar al disco: cada uno está en un campo
// que identifica a la máquina, a su dueño o lo que hace.
var sentinels = []string{
	"TITLE-SENTINEL", "WHO-SENTINEL", "WHY-SENTINEL", "CLASS-SENTINEL", "IDLE-TITLE-SENTINEL",
	"DISK-SERIAL-SENTINEL", "MON-SERIAL-SENTINEL", "HOST-SENTINEL",
	"aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:03",
	"SSID-SENTINEL", "NEAR-SSID-SENTINEL", "CONNECTING-SENTINEL", "NMCLI-ERR-SENTINEL", "0badc0de-0000-4000-8000-000000000001",
	"BT-NAME-SENTINEL", "AUDIO-NAME-SENTINEL",
	"JOURNAL-SENTINEL", "CRASH-USER-SENTINEL",
	"sentineluser", "otheruser",
	"203.0.113.7", "2001:db8::7", "fe80::a8bb:ccff:fedd:ee01", "198.51.100.9", "2001:db8::53",
	"0123abcd-0123-4abc-8def-0123456789ab", `0123abcd\\x2d0123`, // en JSON, la \ va doble
	"wlx0123456789ab",
	"aa:bb:cc:dd:ee:04", "AA:BB:CC:DD:EE:05", "203.0.113.8",
}

func sample() *model.Snapshot {
	yes := 3
	return &model.Snapshot{
		Schema: 1,
		Memory: &model.Memory{Swaps: []model.Swap{{Name: "/dev/mapper/luks-0123abcd-0123-4abc-8def-0123456789ab", Kind: "partition"}, {Name: "/dev/zram0", Kind: "zram"}}},
		Storage: &model.Storage{
			Disks: []model.Disk{{Name: "nvme0n1", Model: "Samsung SSD 980", Serial: "DISK-SERIAL-SENTINEL",
				Partitions: []model.Partition{{Name: "nvme0n1p2", Holder: "luks-0123abcd-0123-4abc-8def-0123456789ab", Mounts: []string{"/run/media/sentineluser/USB"}}}}},
			Filesystems: []model.Filesystem{{Device: "/dev/mapper/luks-0123abcd-0123-4abc-8def-0123456789ab", Disks: []string{"nvme0n1"}, Mounts: []string{"/", "/home/sentineluser/data"}}},
		},
		Network: &model.Network{
			Interfaces: []model.Iface{
				{Name: "enp5s0", Kind: "ethernet", MAC: "aa:bb:cc:dd:ee:01", IPv4: []string{"192.168.1.10/24", "203.0.113.7/24"}, IPv6: []string{"2001:db8::7/64", "fe80::a8bb:ccff:fedd:ee01/64", "::1/128"}},
				{Name: "wlx0123456789ab", Kind: "wifi", MAC: "aa:bb:cc:dd:ee:02", Wifi: &model.Wifi{SSID: "SSID-SENTINEL", Manager: "nm", State: "connected",
					Connecting: "CONNECTING-SENTINEL", Error: "NMCLI-ERR-SENTINEL",
					Networks: []model.WifiNet{{SSID: "NEAR-SSID-SENTINEL", Signal: 50, UUID: "0badc0de-0000-4000-8000-000000000001"}, {Hidden: true, BSSID: "aa:bb:cc:dd:ee:03"}}}},
			},
			Gateway: "198.51.100.9", GatewayIface: "wlx0123456789ab", DNS: []string{"192.168.1.1", "2001:db8::53"},
		},
		Power: &model.Power{
			Inhibit: &model.Inhibit{
				Logind:  []model.Inhibitor{{What: []string{"sleep"}, Who: "WHO-SENTINEL", Why: "WHY-SENTINEL", Mode: "block", UID: 1000, PID: 42}},
				Windows: []model.IdleWindow{{Class: "CLASS-SENTINEL", Title: "IDLE-TITLE-SENTINEL"}},
			},
			Batteries: []model.Battery{{Name: "BAT0"}, {Name: "hid-aa:bb:cc:dd:ee:04-battery", Peripheral: true},
				{Name: "ps-controller-battery-AA:BB:CC:DD:EE:05", Peripheral: true}},
			Wakeup: []model.WakeDevice{{Name: "wlx0123456789ab", Kind: "net", Path: "0000:05:00.0"}},
		},
		System: &model.System{
			Hostname: "HOST-SENTINEL", Kernel: "6.17.1-arch1-1",
			Monitors:    []model.Monitor{{Connector: "DP-1", Manufacturer: "DEL", Model: "DELL S2721DGF", Serial: "MON-SERIAL-SENTINEL", Connected: true}},
			Bluetooth:   []model.BTDevice{{Name: "BT-NAME-SENTINEL", Address: "aa:bb:cc:dd:ee:03", Connected: true, Battery: &yes}},
			Sound:       &model.Sound{Outputs: []model.AudioNode{{ID: "51", Name: "AUDIO-NAME-SENTINEL", Label: "Speakers", Volume: 40}}, Inputs: []model.AudioNode{}},
			FailedUnits: []model.Unit{{Name: `systemd-cryptsetup@luks\x2d0123abcd\x2d0123\x2d4abc\x2d8def\x2d0123456789ab.service`, Scope: "system"}},
		},
		Apps: &model.Apps{List: []model.App{{ID: "app-firefox.scope", Name: "Firefox", Windows: []model.Window{{Address: "0xa1", Title: "TITLE-SENTINEL", Class: "firefox"}}}}},
		Maintenance: &model.Maintenance{
			Caches:    []model.CacheDir{{ID: "yay", Path: "/home/sentineluser/.cache/yay", SizeB: 1}},
			Snapshots: &model.Snapshots{User: "sentineluser", Configs: []model.SnapConfig{{Name: "root", AllowUsers: "sentineluser otheruser"}}},
			Health: &model.Health{Errors: &model.JournalErrors{Count: 1, Recent: []model.JournalEntry{{Source: "kernel", Message: "JOURNAL-SENTINEL"}}},
				Crashes: &model.Crashes{Total: 1, List: []model.Crash{{Program: "x", User: "CRASH-USER-SENTINEL"}}}},
		},
		Security: &model.Security{
			Listening: []model.Listener{{Proto: "tcp", Addr: "2001:db8::7", Port: 22, Scope: "addr", Iface: "wlx0123456789ab",
				Owners: []model.ListenerOwner{{UID: 1000, User: "sentineluser", Program: "sshd", Own: true}, {UID: 0, User: "root"}}},
				{Proto: "tcp", Addr: "0.0.0.0", Port: 80, Scope: "all", Firewall: "limited", From: "192.168.1.0/24, 203.0.113.8, any"}},
			Firewall: &model.Firewall{Tool: "ufw", State: "on", Rules: []model.FwRule{{Action: "allow", Proto: "tcp", Port: "22", To: "0.0.0.0/0", From: "203.0.113.7", Iface: "wlx0123456789ab"}}},
			Volumes:  []model.CryptVolume{{Device: "/dev/mapper/luks-0123abcd-0123-4abc-8def-0123456789ab", Mounts: []string{"/home/sentineluser/x"}, Encrypted: true}},
			Swaps:    []model.SwapCrypt{{Name: "/home/sentineluser/swapfile", Kind: "file", State: "plain"}},
		},
		Unavailable: []model.Unavailable{{What: "maintenance.caches", Reason: "permission", Message: "open /home/sentineluser/.cache/paru: permission denied"}},
	}
}

func TestSnapshotLeavesNothingIdentifying(t *testing.T) {
	s := sample()
	before, _ := json.Marshal(s)
	d := Snapshot(s, "/home/sentineluser", "sentineluser")
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("the live snapshot was changed")
	}
	for _, x := range sentinels {
		if !strings.Contains(string(before), x) {
			t.Fatalf("the sample lacks %q", x)
		}
	}
	data, _ := json.Marshal(d)
	out := string(data)
	for _, x := range sentinels {
		if strings.Contains(out, x) {
			t.Errorf("on disk: %q", x)
		}
	}
	// Lo que se guarda a conciencia.
	for _, keep := range []string{`"Samsung SSD 980"`, `"DELL S2721DGF"`, `"192.168.1.10/24"`, `"::1/128"`, `"192.168.1.1"`,
		`"Firefox"`, `"firefox"`, `"Speakers"`, `"sshd"`, `"root"`, `"~/.cache/yay"`, `"/run/media/~/USB"`, `"~/data"`,
		`"open ~/.cache/paru: permission denied"`, `"BAT0"`, `"hid-…-battery"`, `"ps-controller-battery-…"`, `"192.168.1.0/24, any"`, `"/dev/zram0"`, `"6.17.1-arch1-1"`, `"0xa1"`, `"luks-…"`, `"/dev/mapper/luks-…"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %s", keep)
		}
	}
	if d.Maintenance.Health != nil {
		t.Error("the health (journal, crashes, OOM) must not reach the disk")
	}
	if n := d.Network.Interfaces[1]; n.Name != "wlx1" || d.Network.GatewayIface != "wlx1" || d.Security.Listening[0].Iface != "wlx1" {
		t.Errorf("a MAC-named interface keeps one name everywhere: %q %q", n.Name, d.Network.GatewayIface)
	}
	if w := d.Network.Interfaces[1].Wifi; w == nil || w.Manager != "nm" || w.State != "connected" || w.Networks != nil {
		t.Errorf("wifi = %+v", w)
	}
	if b := d.System.Bluetooth[0]; !b.Connected || b.Battery == nil || *b.Battery != 3 {
		t.Errorf("bluetooth state lost: %+v", b)
	}
}

func TestSnapshotNil(t *testing.T) {
	if Snapshot(nil, "", "") != nil {
		t.Fatal("nil in, nil out")
	}
	s := &model.Snapshot{Schema: 1}
	if d := Snapshot(s, "/home/u", "u"); d == nil || d.Schema != 1 {
		t.Fatal("empty snapshot")
	}
}

// System es lo que va al caché del inventario (static.json): sin hostname
// ni series, sin tocar el original.
func TestSystem(t *testing.T) {
	s := sample().System
	d := System(s)
	data, _ := json.Marshal(d)
	for _, x := range []string{"HOST-SENTINEL", "MON-SERIAL-SENTINEL", "BT-NAME-SENTINEL", "AUDIO-NAME-SENTINEL"} {
		if strings.Contains(string(data), x) {
			t.Errorf("in the cache: %q", x)
		}
	}
	if s.Hostname != "HOST-SENTINEL" || s.Monitors[0].Serial != "MON-SERIAL-SENTINEL" || d.Monitors[0].Model != "DELL S2721DGF" {
		t.Fatalf("original changed or model lost: %+v %+v", s.Monitors, d.Monitors)
	}
	if System(nil) != nil {
		t.Fatal("nil")
	}
}

// Sin nombre de usuario (no se pudo leer), se usa el de la carpeta.
func TestSnapshotUserFromHome(t *testing.T) {
	s := &model.Snapshot{Storage: &model.Storage{Filesystems: []model.Filesystem{{Mounts: []string{"/run/media/sentineluser2/USB", "/media/sentineluser2/X"}}}}}
	d := Snapshot(s, "/home/sentineluser2", "")
	if m := d.Storage.Filesystems[0].Mounts; m[0] != "/run/media/~/USB" || m[1] != "/media/~/X" {
		t.Fatalf("mounts = %q", m)
	}
}
