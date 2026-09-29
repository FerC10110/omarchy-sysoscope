package i18n

import "testing"

func TestDetect(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"LANGUAGE": "es_AR:en"}, "es"},
		{map[string]string{"LANG": "es_ES.UTF-8"}, "es"},
		{map[string]string{"LANG": "C.UTF-8"}, "en"},
		{map[string]string{"LC_ALL": "en_US", "LANG": "es"}, "en"},
		{map[string]string{"LC_MESSAGES": "es_MX.UTF-8", "LANG": "en_US"}, "es"},
		{map[string]string{"LANGUAGE": ":es", "LANG": "en_US"}, "es"},
		{map[string]string{}, "en"},
	}
	for _, c := range cases {
		if got := Detect(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("Detect(%v) = %q, want %q", c.env, got, c.want)
		}
	}
}

func TestTF(t *testing.T) {
	es["test key %d"] = "clave de prueba %d"
	defer delete(es, "test key %d")
	if got := T("es", "test key %d"); got != "clave de prueba %d" {
		t.Fatalf("T es = %q", got)
	}
	if got := T("en", "test key %d"); got != "test key %d" {
		t.Fatalf("T en = %q", got)
	}
	if got := T("es", "untranslated"); got != "untranslated" {
		t.Fatalf("T fallback = %q", got)
	}
	if got := F("es", "test key %d", 7); got != "clave de prueba 7" {
		t.Fatalf("F = %q", got)
	}
}
