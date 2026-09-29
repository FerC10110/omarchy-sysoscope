// Package storage lee discos (lsblk), filesystems montados (con LUKS, LVM y
// subvolúmenes btrfs agrupados), la E/S de /proc/diskstats y SMART vía
// udisks2, sin root.
package storage

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// LsblkArgs son los argumentos de lsblk (también los usa la captura de fixtures).
var LsblkArgs = []string{"-J", "-b", "-o", "NAME,KNAME,PATH,TYPE,SIZE,MODEL,SERIAL,TRAN,ROTA,FSTYPE,MOUNTPOINTS,PKNAME,LABEL"}

type node struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Type        string    `json:"type"`
	Size        uint64    `json:"size"`
	Model       *string   `json:"model"`
	Serial      *string   `json:"serial"`
	Tran        *string   `json:"tran"`
	Rota        bool      `json:"rota"`
	FSType      *string   `json:"fstype"`
	Mountpoints []*string `json:"mountpoints"`
	Children    []node    `json:"children"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func (n node) mounts() []string {
	var out []string
	for _, m := range n.Mountpoints {
		if s := str(m); s != "" && s != "[SWAP]" {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Static lee el árbol de bloques. Se refresca cada 60 s (y al montar).
func Static(env source.Env, r *collect.Report) *model.Storage {
	s := &model.Storage{Disks: []model.Disk{}, Filesystems: []model.Filesystem{}}
	out, err := env.Run.Run(context.Background(), "lsblk", LsblkArgs...)
	if err != nil {
		r.FromErr("storage", "lsblk", err)
		return s
	}
	var tree struct {
		Blockdevices []node `json:"blockdevices"`
	}
	if err := json.Unmarshal(out, &tree); err != nil {
		r.Err("storage", err)
		return s
	}
	fs := map[string]*model.Filesystem{}
	var order []string
	for _, n := range tree.Blockdevices {
		if n.Type != "disk" || strings.HasPrefix(n.Name, "zram") || strings.HasPrefix(n.Name, "loop") {
			continue
		}
		d := model.Disk{Name: n.Name, Model: str(n.Model), Serial: str(n.Serial), Transport: str(n.Tran), Rotational: n.Rota, SizeB: n.Size}
		for _, c := range n.Children {
			p := model.Partition{Name: c.Name, SizeB: c.Size, FSType: str(c.FSType), Mounts: c.mounts()}
			if len(c.Children) > 0 {
				p.Holder = c.Children[0].Name
			}
			d.Partitions = append(d.Partitions, p)
		}
		s.Disks = append(s.Disks, d)
		walk(n, n.Name, false, false, fs, &order)
	}
	for _, dev := range order {
		f := fs[dev]
		sort.Strings(f.Disks)
		s.Filesystems = append(s.Filesystems, *f)
	}
	if p, err := env.FS.Read("/proc/pressure/io"); err == nil {
		s.PSI = collect.ParsePSI(p)
	}
	return s
}

// walk recorre el árbol desde un disco y crea un Filesystem por dispositivo
// montado. Un mismo dispositivo (RAID, LVM sobre varios discos) junta sus
// discos físicos.
func walk(n node, disk string, crypt, lvm bool, fs map[string]*model.Filesystem, order *[]string) {
	crypt = crypt || n.Type == "crypt"
	lvm = lvm || n.Type == "lvm"
	if mounts := n.mounts(); len(mounts) > 0 && str(n.FSType) != "" && str(n.FSType) != "swap" {
		f, ok := fs[n.Path]
		if !ok {
			f = &model.Filesystem{Device: n.Path, FSType: str(n.FSType), Mounts: mounts, Crypt: crypt, LVM: lvm}
			fs[n.Path] = f
			*order = append(*order, n.Path)
		}
		if !contains(f.Disks, disk) {
			f.Disks = append(f.Disks, disk)
		}
	}
	for _, c := range n.Children {
		walk(c, disk, crypt, lvm, fs, order)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Usage completa tamaño y uso de cada filesystem con statfs sobre su primer
// punto de montaje (size, free, avail en bytes). Usado% es como df:
// usado / (usado + disponible).
func Usage(fss []model.Filesystem, statfs func(path string) (size, free, avail uint64, err error)) {
	for i := range fss {
		f := &fss[i]
		if len(f.Mounts) == 0 {
			continue
		}
		size, free, avail, err := statfs(f.Mounts[0])
		if err != nil || size == 0 {
			continue
		}
		f.SizeB, f.FreeB = size, avail
		if size > free {
			f.UsedB = size - free
		}
		if f.UsedB+avail > 0 {
			f.UsedPct = float64(f.UsedB) / float64(f.UsedB+avail) * 100
		}
	}
}
