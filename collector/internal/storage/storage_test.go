package storage

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func disk(t *testing.T, s *model.Storage, name string) *model.Disk {
	t.Helper()
	for i := range s.Disks {
		if s.Disks[i].Name == name {
			return &s.Disks[i]
		}
	}
	t.Fatalf("disk %s not found", name)
	return nil
}

func fsByDevice(s *model.Storage, dev string) *model.Filesystem {
	for i := range s.Filesystems {
		if s.Filesystems[i].Device == dev {
			return &s.Filesystems[i]
		}
	}
	return nil
}

func TestStaticThreadripper(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	s := Static(env, r)
	var names []string
	for _, d := range s.Disks {
		names = append(names, d.Name)
	}
	if !reflect.DeepEqual(names, []string{"sda", "sdb", "sdc", "sdd", "nvme0n1", "nvme1n1"}) {
		t.Fatalf("disks = %v (zram must be excluded)", names)
	}
	n := disk(t, s, "nvme1n1")
	if n.Model != "KINGSTON SNV2S2000G" || n.Transport != "nvme" || n.Rotational || n.Serial != "SERIAL6" || n.SizeB == 0 {
		t.Errorf("nvme1n1 = %+v", n)
	}
	if len(n.Partitions) != 2 || n.Partitions[1].Name != "nvme1n1p2" || n.Partitions[1].Holder != "root" || n.Partitions[1].FSType != "crypto_LUKS" {
		t.Errorf("partitions = %+v", n.Partitions)
	}
	if a := disk(t, s, "sda"); !a.Rotational || a.Transport != "sata" || a.Model != "WDC WD120EFGX-68CPHN0" {
		t.Errorf("sda = %+v", a)
	}
	root := fsByDevice(s, "/dev/mapper/root")
	if root == nil {
		t.Fatalf("no root fs in %+v", s.Filesystems)
	}
	if root.FSType != "btrfs" || !root.Crypt || root.LVM || !reflect.DeepEqual(root.Disks, []string{"nvme1n1"}) ||
		!reflect.DeepEqual(root.Mounts, []string{"/", "/home", "/var/cache/pacman/pkg", "/var/log"}) {
		t.Errorf("root fs = %+v", root)
	}
	boot := fsByDevice(s, "/dev/nvme1n1p1")
	if boot == nil || boot.FSType != "vfat" || !reflect.DeepEqual(boot.Mounts, []string{"/boot"}) || boot.Crypt {
		t.Errorf("boot fs = %+v", boot)
	}
	if len(s.Filesystems) != 2 {
		t.Errorf("filesystems = %+v (unmounted and swap are not filesystems)", s.Filesystems)
	}
}

func TestUsage(t *testing.T) {
	fss := []model.Filesystem{{Device: "/dev/mapper/root", Mounts: []string{"/", "/home"}}, {Device: "/dev/x", Mounts: []string{"/gone"}}}
	var asked []string
	Usage(fss, func(p string) (uint64, uint64, uint64, error) {
		asked = append(asked, p)
		if p == "/gone" {
			return 0, 0, 0, errors.New("no")
		}
		return 1000, 400, 300, nil
	})
	f := fss[0]
	if f.SizeB != 1000 || f.UsedB != 600 || f.FreeB != 300 || math.Abs(f.UsedPct-200.0/3) > 1e-9 {
		t.Errorf("usage = %+v", f)
	}
	if fss[1].SizeB != 0 || !reflect.DeepEqual(asked, []string{"/", "/gone"}) {
		t.Errorf("failing statfs = %+v asked %v", fss[1], asked)
	}
}

func TestIODeltas(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	s := Static(env, r)
	now := source.FixtureNow
	env.Now = func() time.Time { return now }
	var io IO
	io.Sample(env, s.Disks)
	if disk(t, s, "nvme1n1").ReadBps != nil {
		t.Fatal("first sample must not have rates")
	}
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(env.FS.Root)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(source.FixturesDir(t), "threadripper/diskstats.2"))
	os.WriteFile(filepath.Join(dst, "proc/diskstats"), data, 0o644)
	env.FS.Root = dst
	now = now.Add(2 * time.Second)
	io.Sample(env, s.Disks)
	n := disk(t, s, "nvme1n1")
	if n.ReadBps == nil || *n.ReadBps != 2048*512/2 || *n.WriteBps != 0 || n.BusyPct == nil || *n.BusyPct != 25 {
		t.Fatalf("nvme1n1 io = %v %v %v", n.ReadBps, n.WriteBps, n.BusyPct)
	}
	if a := disk(t, s, "sda"); a.ReadBps == nil || *a.ReadBps != 0 {
		t.Errorf("sda rate = %v", a.ReadBps)
	}
}

func TestSmart(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	r := &collect.Report{Lang: "en"}
	s := Static(env, r)
	Smart(env, s.Disks, r)
	sam := disk(t, s, "nvme0n1").Smart
	if sam == nil || sam.Kind != "nvme" || *sam.PowerOnHours != 4408 || *sam.TempC != 18.85 || len(sam.Warnings) != 0 || sam.Failing || *sam.PercentUsed != 4 || *sam.MediaErrors != 0 {
		t.Errorf("samsung smart = %+v", sam)
	}
	wd := disk(t, s, "sda").Smart
	if wd == nil || wd.Kind != "ata" || wd.Failing || *wd.PowerOnHours != 3666 || *wd.TempC != 36.85 || *wd.BadSectors != 0 {
		t.Errorf("wd smart = %+v", wd)
	}
	if sd := disk(t, s, "sdd").Smart; sd == nil || *sd.BadSectors != 34 {
		t.Errorf("sandisk smart = %+v", sd)
	}
	if len(r.List) != 0 {
		t.Errorf("unavailable = %+v", r.List)
	}
}

func TestNoUdisks(t *testing.T) {
	env := source.Fixture(t, "threadripper")
	env.Bus = &source.FakeBus{}
	r := &collect.Report{Lang: "en"}
	s := Static(env, r)
	Smart(env, s.Disks, r)
	if len(s.Disks) != 6 || s.Disks[0].Smart != nil {
		t.Fatalf("disks = %+v", s.Disks)
	}
	if len(r.List) != 1 || r.List[0].What != "storage.smart" || r.List[0].Reason != "missing_tool" {
		t.Fatalf("unavailable = %+v", r.List)
	}
}

func TestNoLsblk(t *testing.T) {
	env := source.Fixture(t, "minimal")
	r := &collect.Report{Lang: "en"}
	s := Static(env, r)
	if s == nil || len(s.Disks) != 0 || len(r.List) != 1 || r.List[0].What != "storage" || r.List[0].Reason != "missing_tool" {
		t.Fatalf("storage = %+v, unavailable %+v", s, r.List)
	}
}
