package text

import "testing"

func TestClean(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"USB Receiver\x1b[31m", "USB Receiver"}, // secuencia CSI completa (color), no solo el ESC
		{"AURA\x1b[0m\x1b[1;32m", "AURA"},        // varias secuencias seguidas
		{"caf\x00\x01e\x07", "cafe"},             // caracteres de control (Cc) sueltos
		{"movie]0;x.mkv", "movie]0;x.mkv"},       // OSC sin ESC ni CSI: queda igual
		{"foo‮bar", "foobar"},                    // Cf (override RTL) se saca
		{"", ""},
		{"plano", "plano"},
	} {
		if got := Clean(c.in); got != c.want {
			t.Errorf("Clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
