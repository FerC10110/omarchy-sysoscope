// Package i18n traduce los mensajes del colector. El texto fuente está en
// inglés y es la clave del catálogo en español (es.go). El idioma se pasa en
// cada llamada porque el colector puede atender a lazylazy en otro idioma que
// el del proceso.
package i18n

import (
	"fmt"
	"strings"
)

// Detect elige "es" o "en" con el primer valor no vacío entre LANGUAGE,
// LC_ALL, LC_MESSAGES y LANG. LANGUAGE es una lista separada por ":"; vale su
// primer elemento no vacío.
func Detect(env func(string) string) string {
	for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := env(k)
		if k == "LANGUAGE" {
			first := ""
			for _, p := range strings.Split(v, ":") {
				if p != "" {
					first = p
					break
				}
			}
			v = first
		}
		if v == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(v), "es") {
			return "es"
		}
		return "en"
	}
	return "en"
}

// Norm devuelve "es" o "en" a partir de un valor como "es_AR" o "english".
func Norm(lang string) string {
	if strings.HasPrefix(strings.ToLower(lang), "es") {
		return "es"
	}
	return "en"
}

// T traduce s al idioma lang; si no hay traducción devuelve s.
func T(lang, s string) string {
	if lang == "es" {
		if t, ok := es[s]; ok {
			return t
		}
	}
	return s
}

// F es fmt.Sprintf sobre el formato traducido.
func F(lang, format string, args ...any) string {
	return fmt.Sprintf(T(lang, format), args...)
}
