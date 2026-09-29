package maint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// JournalErrArgs pide a journalctl los errores (prioridad err o más grave)
// de este arranque: los 1000 más nuevos, en JSON, solo con los campos que
// se leen. -a: sin él, un campo de más de 4096 bytes llega null.
var JournalErrArgs = []string{"-b", "-p", "err", "-q", "--no-pager", "-a", "-o", "json", "-n", "1000",
	"--output-fields=MESSAGE,SYSLOG_IDENTIFIER,_COMM,PRIORITY,__REALTIME_TIMESTAMP"}

const (
	healthTimeout = 5 * time.Second // tope de cada comando de salud
	errorsMax     = 1000            // el -n de JournalErrArgs: con tantos, "1000+"
	errorsRecent  = 5               // errores recientes que se muestran
	messageMax    = 160             // runas de un mensaje
	nameMax       = 40              // runas de una fuente del journal o de un programa matado
)

// journalGroups pueden leer el journal de todo el sistema.
var journalGroups = []string{"wheel", "adm", "systemd-journal"}

// journalLine es una línea de journalctl -o json. Cada campo puede llegar
// como texto, como arreglo de bytes (no es UTF-8 o tiene caracteres de
// control) o como arreglo de textos (el campo repetido): se lee con field.
type journalLine struct {
	Message    json.RawMessage `json:"MESSAGE"`
	Identifier json.RawMessage `json:"SYSLOG_IDENTIFIER"`
	Comm       json.RawMessage `json:"_COMM"`
	Priority   json.RawMessage `json:"PRIORITY"`
	Realtime   json.RawMessage `json:"__REALTIME_TIMESTAMP"`
}

// journalLines decodifica la salida de journalctl -o json (una entrada por
// línea, de la más vieja a la más nueva). Una línea que no es JSON se
// saltea.
func journalLines(out []byte) []journalLine {
	var ls []journalLine
	for _, l := range bytes.Split(out, []byte("\n")) {
		l = bytes.TrimSpace(l)
		if len(l) == 0 {
			continue
		}
		var jl journalLine
		if json.Unmarshal(l, &jl) == nil {
			ls = append(ls, jl)
		}
	}
	return ls
}

// field lee un campo del journal: un texto; un arreglo de bytes (UTF-8, o
// binary en true si no lo es); un arreglo de textos (el primero). "" si
// falta o es null.
func field(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, false
	}
	var nums []int
	if json.Unmarshal(raw, &nums) == nil {
		b := make([]byte, 0, len(nums))
		for _, n := range nums {
			if n < 0 || n > 255 {
				return "", true
			}
			b = append(b, byte(n))
		}
		if !utf8.Valid(b) {
			return "", true
		}
		return string(b), false
	}
	var ss []string
	if json.Unmarshal(raw, &ss) == nil && len(ss) > 0 {
		return ss[0], false
	}
	return "", false
}

// micros lee __REALTIME_TIMESTAMP (microsegundos). Cero si no es un número.
func micros(raw json.RawMessage) time.Time {
	s, _ := field(raw)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.UnixMicro(n).UTC()
}

// oneLine deja un texto en una línea: sin colores ANSI, lo de control como
// espacio, sin caracteres de formato (Cf: los que invierten el sentido del
// texto, como U+202E, o no se ven) y los espacios seguidos como uno.
func oneLine(s string) string {
	s = ansi.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// cut recorta s a n runas, la última "…".
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func message(raw json.RawMessage, lang string) string {
	s, binary := field(raw)
	if binary {
		return i18n.T(lang, "(binary)")
	}
	return cut(oneLine(s), messageMax)
}

func priority(raw json.RawMessage) int {
	s, _ := field(raw)
	if p, err := strconv.Atoi(s); err == nil {
		return p
	}
	return 3 // -p err
}

// parseErrors cuenta los errores, por fuente (SYSLOG_IDENTIFIER, si no
// _COMM), y guarda los 5 más recientes, el más nuevo primero.
func parseErrors(out []byte, lang string) *model.JournalErrors {
	je := &model.JournalErrors{Sources: []model.ErrorSource{}, Recent: []model.JournalEntry{}}
	counts := map[string]int{}
	var last []model.JournalEntry
	for _, jl := range journalLines(out) {
		id, _ := field(jl.Identifier)
		src := oneLine(id)
		if src == "" {
			comm, _ := field(jl.Comm)
			src = oneLine(comm)
		}
		if src == "" {
			src = i18n.T(lang, "Unknown")
		}
		src = cut(src, nameMax)
		je.Count++
		counts[src]++
		last = append(last, model.JournalEntry{Time: micros(jl.Realtime), Source: src, Priority: priority(jl.Priority),
			Message: message(jl.Message, lang)})
		if len(last) > errorsRecent {
			last = last[1:]
		}
	}
	je.Capped = je.Count >= errorsMax
	for i := len(last) - 1; i >= 0; i-- {
		je.Recent = append(je.Recent, last[i])
	}
	for name, n := range counts {
		je.Sources = append(je.Sources, model.ErrorSource{Name: name, Count: n})
	}
	sort.Slice(je.Sources, func(i, j int) bool {
		a, b := je.Sources[i], je.Sources[j]
		return a.Count > b.Count || (a.Count == b.Count && a.Name < b.Name)
	})
	return je
}

// ReadErrors lee los errores del journal de este arranque (unos 10 ms).
// Si falla: nil y el unavailable maintenance.health.errors.
func ReadErrors(env source.Env, r *collect.Report) *model.JournalErrors {
	out, err := runHealth(env, "journalctl", JournalErrArgs)
	if err != nil && !NoEntries(out, err) {
		healthErr(r, "maintenance.health.errors", "journalctl", err)
		return nil
	}
	je := parseErrors(out, env.Lang)
	je.Partial = JournalPartial(env)
	return je
}

// JournalPartial dice si el usuario ve solo sus propios mensajes: no es
// root y /etc/group no lo pone en wheel, adm ni systemd-journal. Sin
// /etc/group no se afirma nada.
func JournalPartial(env source.Env) bool {
	if env.User.UID == 0 || !env.FS.Exists("/etc/group") {
		return false
	}
	for _, g := range GroupNames(env, env.User) {
		if slices.Contains(journalGroups, g) {
			return false
		}
	}
	return true
}

// runHealth corre un comando de salud con su tope.
func runHealth(env source.Env, name string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	return env.Run.Run(ctx, name, args...)
}

// NoEntries: el comando salió con error sin escribir nada y sin otro motivo
// que "no hay" (coredumpctl sin volcados, journalctl sin entradas). Es una
// lista vacía, no una falla. Exportada: fixture.Capture la usa con el mismo
// criterio para decidir si guarda una salida vacía de journalctl/coredumpctl.
func NoEntries(out []byte, err error) bool {
	if err == nil || errors.Is(err, exec.ErrNotFound) || errors.Is(err, context.DeadlineExceeded) ||
		len(bytes.TrimSpace(out)) > 0 {
		return false
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return source.ExitStatus(err) > 0 // el .exit de un fixture
	}
	msg := strings.ToLower(strings.TrimSpace(string(ee.Stderr)))
	return msg == "" || strings.Contains(msg, "no coredumps found") || strings.Contains(msg, "no entries")
}

// healthErr registra por qué no se pudo leer: sin el programa, sin tiempo,
// o la última línea de stderr.
func healthErr(r *collect.Report, what, tool string, err error) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		r.Err(what, errors.New(reason(err)))
		return
	}
	r.FromErr(what, tool, err)
}
