// Package collect tiene lo que comparten los colectores: el registro de datos
// no disponibles, PSI y la limpieza de textos de relleno.
package collect

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/i18n"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// Report junta los datos que no se pudieron leer en una muestra. Un mismo
// What se registra una sola vez.
type Report struct {
	Lang string
	List []model.Unavailable
}

func (r *Report) add(what, reason, msg string) {
	for _, u := range r.List {
		if u.What == what {
			return
		}
	}
	r.List = append(r.List, model.Unavailable{What: what, Reason: reason, Message: msg})
}

// Keep vuelve a registrar faltantes de una lectura anterior (fuentes que se
// leen cada varias muestras siguen explicando su hueco en las del medio).
func (r *Report) Keep(list []model.Unavailable) {
	for _, u := range list {
		r.add(u.What, u.Reason, u.Message)
	}
}

// Note registra un faltante con un motivo y un mensaje ya traducido.
func (r *Report) Note(what, reason, msg string) { r.add(what, reason, msg) }

// Missing: falta un programa externo.
func (r *Report) Missing(what, tool string) {
	r.add(what, "missing_tool", i18n.F(r.Lang, "requires %s", tool))
}

// Perm: el kernel no deja leerlo sin permisos. hint ya viene traducido.
func (r *Report) Perm(what, hint string) {
	if hint == "" {
		r.add(what, "permission", i18n.T(r.Lang, "permission denied"))
		return
	}
	r.add(what, "permission", i18n.F(r.Lang, "permission denied: %s", hint))
}

// Absent: el hardware o la función no existe en esta máquina.
func (r *Report) Absent(what string) {
	r.add(what, "not_present", i18n.T(r.Lang, "not present on this system"))
}

// Timeout: la fuente no respondió a tiempo.
func (r *Report) Timeout(what string) {
	r.add(what, "timeout", i18n.T(r.Lang, "timed out"))
}

// Err: cualquier otro error.
func (r *Report) Err(what string, err error) {
	r.add(what, "error", i18n.F(r.Lang, "could not read: %s", err))
}

// FromErr clasifica err y lo registra. tool es el programa involucrado (si hay).
func (r *Report) FromErr(what, tool string, err error) {
	switch {
	case errors.Is(err, exec.ErrNotFound):
		r.Missing(what, tool)
	case errors.Is(err, fs.ErrPermission):
		r.Perm(what, "")
	case errors.Is(err, context.DeadlineExceeded):
		r.Timeout(what)
	case errors.Is(err, fs.ErrNotExist):
		r.Absent(what)
	default:
		r.Err(what, err)
	}
}
