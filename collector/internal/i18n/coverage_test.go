package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const lit = `"((?:[^"\\]|\\.)*)"`

// patterns encuentran el texto traducible en el código: el segundo argumento
// de i18n.T e i18n.F (el primero es el idioma) y el formato de a.fail de la
// CLI (que lo traduce con i18n.F).
var patterns = []*regexp.Regexp{
	regexp.MustCompile(`i18n\.[TF]\(\s*[^,()]+,\s*` + lit),
	regexp.MustCompile(`a\.fail\(\s*` + lit),
}

// sourceKeys devuelve las claves usadas en el código del colector.
func sourceKeys(t *testing.T) map[string]string {
	t.Helper()
	keys := map[string]string{}
	for _, dir := range []string{"../../internal", "../../cmd"} {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") || strings.Contains(path, "/i18n/") {
				return nil
			}
			data, _ := os.ReadFile(path)
			for _, re := range patterns {
				for _, m := range re.FindAllStringSubmatch(string(data), -1) {
					s, err := strconv.Unquote(`"` + m[1] + `"`)
					if err != nil {
						t.Fatalf("%s: %q: %v", path, m[1], err)
					}
					keys[s] = path
				}
			}
			return nil
		})
	}
	return keys
}

// neutral son claves que se escriben igual en los dos idiomas.
func neutral(s string) bool {
	return !regexp.MustCompile(`[A-Za-z]{2,}`).MatchString(s)
}

func TestSpanishCatalogCoversSource(t *testing.T) {
	keys := sourceKeys(t)
	for k, path := range keys {
		if _, ok := es[k]; !ok && !neutral(k) {
			t.Errorf("%s: falta traducción de %q", path, k)
		}
	}
	for k := range es {
		if _, ok := keys[k]; !ok {
			t.Errorf("traducción sin uso: %q", k)
		}
	}
}
