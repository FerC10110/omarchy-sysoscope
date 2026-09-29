package security

import (
	"reflect"
	"testing"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

func TestCryptRootAndSwaps(t *testing.T) {
	env := source.Env{FS: fsWith(t, map[string]string{
		"/sys/block/dm-0/dm/uuid":                "CRYPT-LUKS2-0123456789abcdef0123456789abcdef-root\n",
		"/sys/block/dm-0/dm/name":                "root\n",
		"/sys/block/dm-0/slaves/nvme1n1p2/.keep": "",
	})}
	fss := []model.Filesystem{
		{Device: "/dev/nvme1n1p1", FSType: "vfat", Mounts: []string{"/boot"}},
		{Device: "/dev/mapper/root", FSType: "btrfs", Mounts: []string{"/", "/home", "/var/log"}, Crypt: true},
	}
	swaps := []model.Swap{{Name: "/swap/swapfile", Kind: "file"}, {Name: "/dev/zram0", Kind: "zram"}, {Name: "/dev/nvme0n1p1", Kind: "partition"}}
	vols, sw := Crypt(env, fss, swaps)
	wantV := []model.CryptVolume{
		{Device: "/dev/nvme1n1p1", Mounts: []string{"/boot"}},
		{Device: "/dev/mapper/root", Mounts: []string{"/", "/home", "/var/log"}, Encrypted: true, Type: "LUKS2", On: "nvme1n1p2"},
	}
	wantS := []model.SwapCrypt{
		{Name: "/swap/swapfile", Kind: "file", State: "encrypted", Type: "LUKS2", On: "nvme1n1p2"},
		{Name: "/dev/zram0", Kind: "zram", State: "memory"},
		{Name: "/dev/nvme0n1p1", Kind: "partition", State: "plain"},
	}
	if !reflect.DeepEqual(vols, wantV) || !reflect.DeepEqual(sw, wantS) {
		t.Fatalf("volumes %+v\nswaps %+v", vols, sw)
	}
}

// dm sobre dm, en las dos direcciones, cryptswap PLAIN y un ciclo.
func TestCryptStacked(t *testing.T) {
	env := source.Env{FS: fsWith(t, map[string]string{
		"/sys/block/dm-0/dm/uuid": "CRYPT-LUKS1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-cryptlvm", "/sys/block/dm-0/dm/name": "cryptlvm",
		"/sys/block/dm-0/slaves/nvme0n1p2/.keep": "",
		"/sys/block/dm-1/dm/uuid":                "LVM-abc", "/sys/block/dm-1/dm/name": "vg-root", "/sys/block/dm-1/slaves/dm-0/.keep": "",
		"/sys/block/dm-2/dm/uuid": "LVM-def", "/sys/block/dm-2/dm/name": "vg-swap", "/sys/block/dm-2/slaves/dm-0/.keep": "",
		// LUKS sobre un LV: el cifrado arriba, la partición dos niveles abajo.
		"/sys/block/dm-3/dm/uuid": "LVM-ghi", "/sys/block/dm-3/dm/name": "vg2-data", "/sys/block/dm-3/slaves/sda1/.keep": "",
		"/sys/block/dm-4/dm/uuid": "CRYPT-LUKS2-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-data", "/sys/block/dm-4/dm/name": "data",
		"/sys/block/dm-4/slaves/dm-3/.keep": "",
		// cryptswap con clave al azar.
		"/sys/block/dm-5/dm/uuid": "CRYPT-PLAIN-swap", "/sys/block/dm-5/dm/name": "swap", "/sys/block/dm-5/slaves/nvme0n1p3/.keep": "",
		// Un ciclo (no debería existir) no cuelga.
		"/sys/block/dm-6/dm/uuid": "LVM-x", "/sys/block/dm-6/slaves/dm-7/.keep": "",
		"/sys/block/dm-7/dm/uuid": "LVM-y", "/sys/block/dm-7/slaves/dm-6/.keep": "",
	})}
	fss := []model.Filesystem{{Device: "/dev/mapper/vg-root", Mounts: []string{"/"}}, {Device: "/dev/mapper/data", Mounts: []string{"/data"}},
		{Device: "/dev/dm-6", Mounts: []string{"/loop"}}}
	swaps := []model.Swap{{Name: "/dev/dm-2", Kind: "partition"}, {Name: "/dev/dm-5", Kind: "partition"}}
	vols, sw := Crypt(env, fss, swaps)
	if v := vols[0]; !v.Encrypted || v.Type != "LUKS1" || v.On != "nvme0n1p2" {
		t.Errorf("LVM on LUKS = %+v", v)
	}
	if v := vols[1]; !v.Encrypted || v.Type != "LUKS2" || v.On != "sda1" {
		t.Errorf("LUKS on LVM = %+v", v)
	}
	if v := vols[2]; v.Encrypted {
		t.Errorf("a cycle = %+v", v)
	}
	if s := sw[0]; s.State != "encrypted" || s.Type != "LUKS1" || s.On != "nvme0n1p2" {
		t.Errorf("swap LV = %+v", s)
	}
	if s := sw[1]; s.State != "encrypted" || s.Type != "PLAIN" || s.On != "nvme0n1p3" {
		t.Errorf("cryptswap = %+v", s)
	}
}

func TestSwapFileMount(t *testing.T) {
	env := source.Env{FS: fsWith(t, nil)} // sin sysfs: vale lo que dijo lsblk
	fss := []model.Filesystem{{Device: "/dev/mapper/root", Mounts: []string{"/"}, Crypt: true}, {Device: "/dev/sdb1", Mounts: []string{"/home"}}}
	swaps := []model.Swap{{Name: "/home/swapfile", Kind: "file"}, {Name: "/homework/swap", Kind: "file"}, {Name: "/swap/swapfile", Kind: "file"}}
	vols, sw := Crypt(env, fss, swaps)
	if !vols[0].Encrypted || vols[0].Type != "" || vols[1].Encrypted {
		t.Fatalf("volumes = %+v", vols)
	}
	if got := []string{sw[0].State, sw[1].State, sw[2].State}; !reflect.DeepEqual(got, []string{"plain", "encrypted", "encrypted"}) {
		t.Fatalf("swaps = %+v", sw)
	}
	if _, sw := Crypt(env, nil, swaps[:1]); sw[0].State != "unknown" {
		t.Errorf("no filesystem = %+v", sw)
	}
}

// dm-verity no es cifrado: aunque su uuid empiece con CRYPT-, un volumen
// verity sobre una partición sin cifrar no debe informarse "Encrypted".
func TestCryptVerityNotEncryption(t *testing.T) {
	env := source.Env{FS: fsWith(t, map[string]string{
		"/sys/block/dm-8/dm/uuid":           "CRYPT-VERITY-cccccccccccccccccccccccccccccccc-rootverity",
		"/sys/block/dm-8/dm/name":           "rootverity",
		"/sys/block/dm-8/slaves/sdc1/.keep": "",
	})}
	fss := []model.Filesystem{{Device: "/dev/mapper/rootverity", Mounts: []string{"/verity"}}}
	vols, _ := Crypt(env, fss, nil)
	if v := vols[0]; v.Encrypted || v.Type != "" {
		t.Errorf("dm-verity sobre partición plana = %+v", v)
	}
}

// dm-integrity debajo de LUKS2 (el cifrado arriba, la integridad abajo): el
// volumen sigue siendo LUKS2 hasta la partición física, atravesando la capa
// de integridad como cualquier otra capa de device-mapper.
func TestCryptIntegrityUnderLuks(t *testing.T) {
	env := source.Env{FS: fsWith(t, map[string]string{
		"/sys/block/dm-9/dm/uuid":            "CRYPT-LUKS2-dddddddddddddddddddddddddddddddd-securedata",
		"/sys/block/dm-9/dm/name":            "securedata",
		"/sys/block/dm-9/slaves/dm-10/.keep": "",
		"/sys/block/dm-10/dm/uuid":           "CRYPT-INTEGRITY-eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee-integritylayer",
		"/sys/block/dm-10/dm/name":           "integritylayer",
		"/sys/block/dm-10/slaves/sdc2/.keep": "",
	})}
	fss := []model.Filesystem{{Device: "/dev/mapper/securedata", Mounts: []string{"/secure"}}}
	vols, _ := Crypt(env, fss, nil)
	if v := vols[0]; !v.Encrypted || v.Type != "LUKS2" || v.On != "sdc2" {
		t.Errorf("LUKS2 sobre dm-integrity = %+v", v)
	}
}

// Un volumen con varias ramas (LVM sobre varios discos) es cifrado solo si
// TODAS lo son; con las dos cifradas, On junta las particiones con ", ".
func TestCryptMultiSlaveRequiresAll(t *testing.T) {
	env := source.Env{FS: fsWith(t, map[string]string{
		// Mezclado: un PV en LUKS2 y otro sin cifrar → no cifrado.
		"/sys/block/dm-11/dm/uuid":                "LVM-mix",
		"/sys/block/dm-11/dm/name":                "vg-mixed",
		"/sys/block/dm-11/slaves/dm-12/.keep":     "",
		"/sys/block/dm-11/slaves/sdd1/.keep":      "",
		"/sys/block/dm-12/dm/uuid":                "CRYPT-LUKS2-ffffffffffffffffffffffffffffffff-mixpv",
		"/sys/block/dm-12/dm/name":                "mixpv",
		"/sys/block/dm-12/slaves/nvme2n1p1/.keep": "",
		// Los dos PV cifrados → cifrado, On junta las dos particiones.
		"/sys/block/dm-13/dm/uuid":                "LVM-all",
		"/sys/block/dm-13/dm/name":                "vg-allcrypt",
		"/sys/block/dm-13/slaves/dm-14/.keep":     "",
		"/sys/block/dm-13/slaves/dm-15/.keep":     "",
		"/sys/block/dm-14/dm/uuid":                "CRYPT-LUKS2-1111111111111111111111111111111a-pva",
		"/sys/block/dm-14/dm/name":                "pva",
		"/sys/block/dm-14/slaves/nvme3n1p1/.keep": "",
		"/sys/block/dm-15/dm/uuid":                "CRYPT-LUKS1-2222222222222222222222222222222b-pvb",
		"/sys/block/dm-15/dm/name":                "pvb",
		"/sys/block/dm-15/slaves/nvme3n2p1/.keep": "",
	})}
	fss := []model.Filesystem{
		{Device: "/dev/mapper/vg-mixed", Mounts: []string{"/mixed"}},
		{Device: "/dev/mapper/vg-allcrypt", Mounts: []string{"/allcrypt"}},
	}
	vols, _ := Crypt(env, fss, nil)
	if v := vols[0]; v.Encrypted {
		t.Errorf("LVM mixta (un PV plano) = %+v", v)
	}
	if v := vols[1]; !v.Encrypted || v.On != "nvme3n1p1, nvme3n2p1" {
		t.Errorf("LVM con todos los PV cifrados = %+v", v)
	}
}

// lsblk marca TYPE crypt también a dm-verity y dm-integrity, y a una LVM
// mixta por la rama cifrada: si sysfs resuelve el dm, manda sysfs y el
// Crypt de lsblk no cuenta. Solo sin sysfs (dm no encontrado) vale lsblk.
func TestCryptLsblkOnlyWhenSysfsUnresolved(t *testing.T) {
	env := source.Env{FS: fsWith(t, map[string]string{
		"/sys/block/dm-8/dm/uuid":                 "CRYPT-VERITY-cccccccccccccccccccccccccccccccc-rootverity",
		"/sys/block/dm-8/dm/name":                 "rootverity",
		"/sys/block/dm-8/slaves/sdc1/.keep":       "",
		"/sys/block/dm-11/dm/uuid":                "LVM-mix",
		"/sys/block/dm-11/dm/name":                "vg-mixed",
		"/sys/block/dm-11/slaves/dm-12/.keep":     "",
		"/sys/block/dm-11/slaves/sdd1/.keep":      "",
		"/sys/block/dm-12/dm/uuid":                "CRYPT-LUKS2-ffffffffffffffffffffffffffffffff-mixpv",
		"/sys/block/dm-12/dm/name":                "mixpv",
		"/sys/block/dm-12/slaves/nvme2n1p1/.keep": "",
	})}
	fss := []model.Filesystem{
		{Device: "/dev/mapper/rootverity", Mounts: []string{"/verity"}, Crypt: true},
		{Device: "/dev/mapper/vg-mixed", Mounts: []string{"/mixed"}, Crypt: true},
		{Device: "/dev/mapper/unknown", Mounts: []string{"/other"}, Crypt: true},
	}
	vols, _ := Crypt(env, fss, nil)
	if v := vols[0]; v.Encrypted || v.Type != "" {
		t.Errorf("dm-verity con lsblk crypt = %+v", v)
	}
	if v := vols[1]; v.Encrypted {
		t.Errorf("LVM mixta con lsblk crypt = %+v", v)
	}
	if v := vols[2]; !v.Encrypted || v.Type != "" || v.On != "" {
		t.Errorf("dm que sysfs no resuelve = %+v", v)
	}
}
