package view

import (
	"strconv"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

const (
	staleDays    = 30  // días sin actualización completa para avisar (Omarchy es rolling)
	updatesShown = 100 // filas de la tabla de actualizaciones
)

func init() {
	builders["maintenance"] = maintenanceView
	actions["maintenance"] = func(_ *model.Snapshot, o Options) []Action {
		return []Action{{Key: "u", Label: i18n.T(o.Lang, "Check for updates"), ID: "updates.check", Target: "section", Mode: "background"}}
	}
}

// Ago dice hace cuánto: "just now", "5 min ago", "12 h ago" (hasta 48 h),
// "3 days ago". Una fecha en el futuro (un reloj corrido) es "just now".
func Ago(d time.Duration, lang string) string {
	switch {
	case d < time.Minute:
		return i18n.T(lang, "just now")
	case d < time.Hour:
		return i18n.F(lang, "%d min ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return i18n.F(lang, "%d h ago", int(d/time.Hour))
	}
	return i18n.F(lang, "%d days ago", int(d/(24*time.Hour)))
}

// daysSince son los días enteros de t a now; 0 si t es cero o futura.
func daysSince(now, t time.Time) int {
	if t.IsZero() || !now.After(t) {
		return 0
	}
	return int(now.Sub(t) / (24 * time.Hour))
}

// when es la fecha local y hace cuánto: "2026-09-22 20:41 (12 h ago)".
func when(t, now time.Time, l string) string {
	return t.Local().Format("2006-01-02 15:04") + " (" + Ago(now.Sub(t), l) + ")"
}

// rebootWarnings son los avisos de reinicio: los usan el Resumen, el badge
// y la vista (una sola fuente de verdad).
func rebootWarnings(s *model.Snapshot, l string) []string {
	if s.Maintenance == nil || s.Maintenance.Reboot == nil {
		return nil
	}
	rb := s.Maintenance.Reboot
	var out []string
	if rb.KernelGone {
		out = append(out, i18n.F(l, "Restart needed: the running kernel (%s) is no longer installed", rb.Kernel))
	}
	if rb.Marker {
		out = append(out, i18n.T(l, "Restart needed after the last update"))
	}
	if rb.Hyprland != nil && *rb.Hyprland {
		out = append(out, i18n.T(l, "Hyprland was updated: log out and back in to use it"))
	}
	if rb.NVIDIALoaded != "" && rb.NVIDIAInstalled != "" && rb.NVIDIALoaded != rb.NVIDIAInstalled {
		out = append(out, i18n.F(l, "NVIDIA driver updated (loaded %s, installed %s): restart to use it", rb.NVIDIALoaded, rb.NVIDIAInstalled))
	}
	return out
}

// freshUpdates es la última comprobación si sirve para avisar: hecha, sin
// error y sin un cambio de paquetes después.
func freshUpdates(m *model.Maintenance) *model.Updates {
	if m == nil || m.Updates == nil {
		return nil
	}
	if u := m.Updates; !u.Checked.IsZero() && u.Error == "" && !u.Stale {
		return u
	}
	return nil
}

// updateWarnings: más de 30 días sin actualización completa (salvo que una
// comprobación fresca diga que no hay nada) y las pendientes de una
// comprobación fresca.
func updateWarnings(s *model.Snapshot, l string) []string {
	m := s.Maintenance
	if m == nil {
		return nil
	}
	var out []string
	fresh := freshUpdates(m)
	if m.Upgrade != nil {
		if d := daysSince(s.Time, m.Upgrade.LastFull); d > staleDays && (fresh == nil || len(fresh.Repo) > 0) {
			out = append(out, i18n.F(l, "No full system update in %d days", d))
		}
	}
	if fresh != nil {
		switch n := len(fresh.Repo); {
		case n == 1:
			out = append(out, i18n.T(l, "1 update available"))
		case n > 1:
			out = append(out, i18n.F(l, "%d updates available", n))
		}
	}
	return out
}

// maintenanceWarnings son los avisos de Mantenimiento para el Resumen y su
// badge (entrada de attention).
func maintenanceWarnings(s *model.Snapshot, l string) []Block {
	var out []Block
	for _, w := range append(rebootWarnings(s, l), updateWarnings(s, l)...) {
		out = append(out, Link(w, "warn", "maintenance"))
	}
	return out
}

func maintenanceHero(s *model.Snapshot, o Options) *Block {
	m, l := s.Maintenance, o.Lang
	if m == nil {
		return nil
	}
	restart, level := i18n.T(l, "No"), ""
	if len(rebootWarnings(s, l)) > 0 {
		restart, level = i18n.T(l, "Yes"), "warn"
	}
	last := "—"
	if m.Upgrade != nil && !m.Upgrade.LastFull.IsZero() {
		last = Ago(s.Time.Sub(m.Upgrade.LastFull), l)
	}
	stats := []Block{Stat(i18n.T(l, "Restart needed"), restart, "", level), Stat(i18n.T(l, "Last update"), last, "", "")}
	if len(m.Caches) > 0 {
		var sum uint64
		for _, c := range m.Caches {
			sum += c.SizeB
		}
		stats = append(stats, statOf(i18n.T(l, "Caches"), Bytes(sum), ""))
	}
	stats = append(stats, Stat(i18n.T(l, "Errors this boot"), errorsCount(m.Health), "", ""))
	return heroOf(stats, nil, nil)
}

func maintenanceView(s *model.Snapshot, o Options) []Block {
	if s.Maintenance == nil {
		return appendUnavailable(nil, s, "maintenance")
	}
	l := o.Lang
	var bs []Block
	bs = append(bs, restartBlocks(s, l)...)
	bs = append(bs, updatesBlocks(s, l)...)
	bs = append(bs, cachesBlocks(s, l)...)
	bs = append(bs, snapshotsBlocks(s, l)...)
	return append(bs, healthBlocks(s, l)...)
}

func restartBlocks(s *model.Snapshot, l string) []Block {
	rb := s.Maintenance.Reboot
	if rb == nil {
		return nil
	}
	var bs []Block
	for _, w := range rebootWarnings(s, l) {
		bs = append(bs, Text(w, "warn"))
	}
	hypr := ""
	if rb.Hyprland != nil {
		hypr = i18n.T(l, "Up to date")
		if *rb.Hyprland {
			hypr = i18n.T(l, "Updated: log out and back in to use it")
		}
	}
	return append(bs, kvIf(i18n.T(l, "Restart needed"),
		[2]string{i18n.T(l, "Running kernel"), rb.Kernel},
		[2]string{i18n.T(l, "Installed kernels"), strings.Join(rb.Installed, ", ")},
		[2]string{i18n.T(l, "NVIDIA driver loaded"), rb.NVIDIALoaded},
		[2]string{i18n.T(l, "NVIDIA driver installed"), rb.NVIDIAInstalled},
		[2]string{"Hyprland", hypr},
		[2]string{i18n.T(l, "Waiting for a restart"), strings.Join(rb.Services, ", ")})...)
}

func updatesBlocks(s *model.Snapshot, l string) []Block {
	m := s.Maintenance
	var bs []Block
	for _, w := range updateWarnings(s, l) {
		bs = append(bs, Text(w, "warn"))
	}
	last, pkgs, change := "—", "", ""
	if up := m.Upgrade; up != nil {
		last = i18n.T(l, "No full update recorded yet")
		if !up.LastFull.IsZero() {
			last, pkgs = when(up.LastFull, s.Time, l), strconv.Itoa(up.FullPackages)
		}
		if !up.LastChange.IsZero() {
			change = when(up.LastChange, s.Time, l)
		}
	}
	bs = append(bs, KV(i18n.T(l, "Updates"), rows(
		[2]string{i18n.T(l, "Last full update"), last},
		[2]string{i18n.T(l, "Packages updated"), pkgs},
		[2]string{i18n.T(l, "Last package change"), change})...))
	bs = appendUnavailable(bs, s, "maintenance.upgrade")
	u := m.Updates
	if u == nil {
		return append(bs,
			Text(i18n.T(l, "Updates are checked on request (press u). This downloads the package lists from your pacman mirrors; nothing about your system is sent."), ""),
			Text(i18n.T(l, "AUR packages are not checked (that would send their names to aur.archlinux.org)"), ""))
	}
	if u.Running {
		bs = append(bs, Text(i18n.T(l, "Checking for updates…"), ""))
	}
	if !u.Checked.IsZero() {
		switch {
		case u.Error != "":
			bs = append(bs, Text(i18n.F(l, "Could not check for updates: %s", u.Error), "warn"))
		case u.Stale:
			if !u.Running {
				bs = append(bs, Text(i18n.T(l, "Checked before the last package change; press u to check again"), ""))
			}
		case len(u.Repo) == 0:
			bs = append(bs, Text(i18n.F(l, "No updates available (checked %s)", Ago(s.Time.Sub(u.Checked), l)), "ok"))
		default:
			shown := u.Repo[:min(len(u.Repo), updatesShown)]
			rs := make([]Row, 0, len(shown))
			for _, p := range shown {
				rs = append(rs, Row{ID: p.Name, Cells: []string{p.Name, p.Old, p.New, dash(p.Repo)}})
			}
			bs = append(bs, Table(i18n.T(l, "Available updates"),
				[]string{i18n.T(l, "Package"), i18n.T(l, "Installed version"), i18n.T(l, "New"), i18n.T(l, "Repository")}, rs, false))
			if n := len(u.Repo) - len(shown); n > 0 {
				bs = append(bs, Text(i18n.F(l, "and %d more", n), ""))
			}
		}
	}
	return append(bs, Text(i18n.T(l, "AUR packages are not checked (that would send their names to aur.archlinux.org)"), ""))
}

func cachesBlocks(s *model.Snapshot, l string) []Block {
	m := s.Maintenance
	var bs []Block
	switch {
	case len(m.Caches) > 0:
		rs := make([]Row, 0, len(m.Caches))
		for _, c := range m.Caches {
			size := Bytes(c.SizeB)
			if c.Partial {
				size = i18n.F(l, "%s (partial)", size)
			}
			name, free := cacheText(c.ID, l)
			rs = append(rs, Row{ID: c.ID, Cells: []string{name, size, free}})
		}
		bs = append(bs, Table(i18n.T(l, "Disk space used by caches"),
			[]string{i18n.T(l, "What"), i18n.T(l, "Size"), i18n.T(l, "To free it")}, rs, false),
			Text(i18n.F(l, "Measured %s", Ago(s.Time.Sub(m.CachesAt), l)), ""))
	case m.CachesAt.IsZero():
		bs = append(bs, Text(i18n.T(l, "Measuring the caches…"), ""))
	}
	return appendUnavailable(bs, s, "maintenance.caches")
}

// cacheText es el nombre de una caché y cómo liberarla. El comando se
// muestra, nunca se corre (paccache -rk2 es lo que hace omarchy update).
func cacheText(id, l string) (string, string) {
	switch id {
	case "pacman":
		return i18n.T(l, "Package cache (pacman)"), "sudo paccache -rk2 · sudo paccache -ruk0"
	case "yay":
		return i18n.T(l, "yay cache"), "yay -Sc"
	case "paru":
		return i18n.T(l, "paru cache"), "paru -Sc"
	case "journal":
		return i18n.T(l, "System journal"), "sudo journalctl --vacuum-size=200M"
	case "coredump":
		return i18n.T(l, "Crash dumps"), i18n.T(l, "Removed automatically by systemd")
	}
	return id, "—"
}

func snapshotsBlocks(s *model.Snapshot, l string) []Block {
	sn := s.Maintenance.Snapshots
	if sn == nil {
		return appendUnavailable(nil, s, "maintenance.snapshots")
	}
	var in []Block
	for _, c := range sn.Configs {
		switch {
		case !c.Allowed:
			in = append(in, Text(i18n.F(l, "Snapshots are not visible to your user. To see them here, run once: %s", enableCommand(c, sn.User)), ""))
		case c.List == nil: // ListSnapshots falló: lo explica el unavailable
		case c.Total == 0:
			in = append(in, Text(i18n.F(l, "No snapshots of %s yet", c.Name), ""))
		default:
			rs := make([]Row, 0, len(c.List))
			for _, x := range c.List {
				n, date := strconv.FormatUint(uint64(x.Number), 10), "—"
				if !x.Date.IsZero() {
					date = x.Date.Local().Format("2006-01-02 15:04")
				}
				rs = append(rs, Row{ID: n, Cells: []string{n, date, snapType(x.Type, l), dash(x.Description), dash(x.Cleanup)}})
			}
			in = append(in, Table(i18n.F(l, "Snapshots of %s (%d)", c.Name, c.Total),
				[]string{"#", i18n.T(l, "Date"), i18n.T(l, "Type"), i18n.T(l, "Description"), i18n.T(l, "Cleanup")}, rs, false))
		}
	}
	in = appendUnavailable(in, s, "maintenance.snapshots")
	if len(in) == 0 {
		return nil
	}
	return []Block{Group(i18n.T(l, "Snapshots"), in...)}
}

func snapType(t, l string) string {
	switch t {
	case "single":
		return i18n.T(l, "Single")
	case "pre":
		return i18n.T(l, "Before the change")
	case "post":
		return i18n.T(l, "After the change")
	}
	return dash(t)
}

// enableCommand es el comando que habilita los snapshots para el usuario
// sin quitar a los que ya estaban (se muestra; lo corre el usuario).
func enableCommand(c model.SnapConfig, user string) string {
	users := strings.Join(append(strings.Fields(c.AllowUsers), user), " ")
	if strings.Contains(users, " ") {
		users = `"` + users + `"`
	}
	return "sudo snapper -c " + c.Name + " set-config ALLOW_USERS=" + users
}
