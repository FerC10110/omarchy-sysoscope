package system

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestListUnits(t *testing.T) {
	bus := &source.FakeBus{System: map[string]json.RawMessage{unitsKey: json.RawMessage(`[[` +
		`["systemd-timesyncd.service","Network Time Synchronization","loaded","active","running","","/a",0,"","/"],` +
		`["ufw.service","CLI Netfilter Manager","loaded","active","exited","","/b",0,"","/"],` +
		`["firewalld.service","firewalld.service","not-found","inactive","dead","","/c",0,"","/"],` +
		`["nftables.service","Netfilter Tables","loaded","inactive","dead","","/d",0,"","/"],` +
		`["iptables.service","IPv4 Packet Filtering Framework","loaded","active","exited","","/e",0,"","/"],` +
		`["ip6tables.service","ip6tables.service","not-found","inactive","dead","","/f",0,"","/"],` +
		`"not a unit"]]`)}}
	got := ListUnits(source.Env{Bus: bus})
	want := Units{"systemd-timesyncd.service": "active", "ufw.service": "active", "nftables.service": "inactive", "iptables.service": "active"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("units = %v, want %v", got, want)
	}
	if n := bus.Called("ListUnitsByNames"); n != 1 {
		t.Errorf("ListUnitsByNames calls = %d, want 1", n)
	}
	if u := ListUnits(source.Env{Bus: &source.FakeBus{}}); u != nil {
		t.Errorf("without systemd1 = %v, want nil", u)
	}
}

// El reloj y el firewall comparten la llamada: Slow la hace una vez y la
// devuelve.
func TestSlowSharesTheUnitsCall(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	s := &model.System{}
	units := Slow(env, s, &collect.Report{Lang: "en"})
	want := Units{"systemd-timesyncd.service": "active", "ufw.service": "active"}
	if !reflect.DeepEqual(units, want) {
		t.Fatalf("units = %v, want %v", units, want)
	}
	if n := env.Bus.(*source.FakeBus).Called("ListUnitsByNames"); n != 1 {
		t.Errorf("ListUnitsByNames calls = %d, want 1", n)
	}
	if s.Time == nil || !s.Time.NTP || !s.Time.CanNTP {
		t.Errorf("clock = %+v", s.Time)
	}
}
