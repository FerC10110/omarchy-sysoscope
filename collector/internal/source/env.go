package source

import (
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Env es todo lo que necesita un colector.
type Env struct {
	FS       FS
	Run      Runner
	Bus      Bus
	Hypr     Hypr // socket de Hyprland (nil o sin sesión: ErrNoHyprland)
	Now      func() time.Time
	Timex    func() (int32, error) // palabra de estado del reloj del kernel (adjtimex); nil = no se lee
	Lang     string                // "en" | "es"
	Home     string                // directorio del usuario (ruta del sistema real, sin Root)
	State    string                // directorio de estado (historial, latest.json)
	Cache    string                // directorio de caché (inventario)
	User     User                  // quién corre el colector: uid, nombre y grupos
	HyprLock string                // hyprland.lock de la sesión (ruta del sistema real); "" sin Hyprland
}

// Real arma el Env del sistema: FS{} + ExecRunner{2s} + DBus + time.Now +
// adjtimex + dirs XDG.
func Real(lang string) Env {
	home, _ := os.UserHomeDir()
	return Env{
		FS:       FS{},
		Run:      ExecRunner{Timeout: 2 * time.Second},
		Bus:      &DBus{},
		Hypr:     HyprSocket{Path: HyprPath(os.Getenv), Timeout: time.Second},
		Now:      time.Now,
		Timex:    KernelTimex,
		Lang:     lang,
		Home:     home,
		State:    filepath.Join(xdg("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "sysoscope"),
		Cache:    filepath.Join(xdg("XDG_CACHE_HOME", filepath.Join(home, ".cache")), "sysoscope"),
		User:     CurrentUser(),
		HyprLock: HyprLockPath(os.Getenv),
	}
}

// User es quien corre el colector: el uid, el nombre y los grupos (el
// primario primero y después los suplementarios).
type User struct {
	UID  uint32
	Name string
	GIDs []uint32
}

// CurrentUser lee el usuario del proceso sin crear procesos (os/user lee
// /etc/passwd en un binario sin cgo).
func CurrentUser() User {
	u := User{UID: uint32(os.Getuid()), GIDs: []uint32{uint32(os.Getgid())}}
	if cu, err := user.Current(); err == nil {
		u.Name = cu.Username
	}
	if gs, err := os.Getgroups(); err == nil {
		for _, g := range gs {
			if !slices.Contains(u.GIDs, uint32(g)) {
				u.GIDs = append(u.GIDs, uint32(g))
			}
		}
	}
	return u
}

// KernelTimex lee el estado del reloj del kernel con adjtimex(2) y Modes=0:
// solo lectura, sin permisos y sin despertar a ningún servicio.
func KernelTimex() (int32, error) {
	var tx unix.Timex
	if _, err := unix.Adjtimex(&tx); err != nil {
		return 0, err
	}
	return tx.Status, nil
}

// FileTimex es el Timex de un fixture: el archivo tiene la palabra de estado
// en decimal. Sin archivo, error (como un adjtimex que falla).
func FileTimex(path string) func() (int32, error) {
	return func() (int32, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 32)
		return int32(n), err
	}
}

func xdg(name, def string) string {
	if v := os.Getenv(name); v != "" && filepath.IsAbs(v) {
		return v
	}
	return def
}

// TB es lo que Fixture usa de testing.TB (sin importar testing en el binario).
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	TempDir() string
}

// FixtureNow es el reloj fijo de los fixtures.
var FixtureNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

// FixtureHyprLock es dónde un fixture guarda el hyprland.lock (la captura
// lo escribe ahí, sea cual sea la firma de la sesión real).
const FixtureHyprLock = "/run/user/1000/hypr/fixture/hyprland.lock"

// FixtureUser es el usuario de los fixtures: fijo, para que un test que
// corre como root dé lo mismo que uno que corre como usuario.
var FixtureUser = User{UID: 1000, Name: "user", GIDs: []uint32{1000}}

// Fixture arma un Env sobre <collector>/fixtures/<name>: FS en root/, un
// FakeRunner sobre cmd/, un FakeBus sobre dbus.json, el estado de adjtimex
// en el archivo timex, FixtureUser y FixtureHyprLock. State y Cache son
// directorios temporales del test.
func Fixture(t TB, name string) Env {
	t.Helper()
	dir := filepath.Join(FixturesDir(t), name)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return fixtureAt(t, name, dir)
}

// FixtureCopy es Fixture sobre una copia del fixture en un directorio
// temporal: para los tests que cambian archivos (quitar un vmlinuz,
// escribir un config) sin tocar collector/fixtures.
func FixtureCopy(t TB, name string) Env {
	t.Helper()
	src := filepath.Join(FixturesDir(t), name)
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("fixture %s: copy: %v", name, err)
	}
	return fixtureAt(t, name, dst)
}

func fixtureAt(t TB, name, dir string) Env {
	t.Helper()
	bus, err := LoadFakeBus(filepath.Join(dir, "dbus.json"))
	if err != nil {
		t.Fatalf("fixture %s: dbus.json: %v", name, err)
	}
	return Env{
		FS:       FS{Root: filepath.Join(dir, "root")},
		Run:      &FakeRunner{Dir: filepath.Join(dir, "cmd")},
		Bus:      bus,
		Hypr:     &FakeHypr{Dir: filepath.Join(dir, "hypr")},
		Now:      func() time.Time { return FixtureNow },
		Timex:    FileTimex(filepath.Join(dir, "timex")),
		Lang:     "en",
		Home:     "/home/user",
		State:    t.TempDir(),
		Cache:    t.TempDir(),
		User:     FixtureUser,
		HyprLock: FixtureHyprLock,
	}
}

// FixturesDir busca hacia arriba el go.mod del colector y devuelve su
// fixtures/ (no testdata/: el escaneo del Market solo saltea fixtures/ y tests/).
func FixturesDir(t TB) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for d := wd; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return filepath.Join(d, "fixtures")
		}
		if filepath.Dir(d) == d {
			t.Fatalf("go.mod not found above %s", wd)
			return ""
		}
	}
}
