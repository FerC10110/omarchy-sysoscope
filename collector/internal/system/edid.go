package system

import (
	"fmt"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
)

// parseEDID completa fabricante, modelo, serie, tamaño, modo preferido y la
// frecuencia vertical máxima del rango del monitor, de un bloque EDID base
// (128 bytes).
func parseEDID(e []byte, m *model.Monitor) {
	if len(e) < 128 || e[0] != 0 || e[1] != 0xff || e[7] != 0 {
		return
	}
	id := uint16(e[8])<<8 | uint16(e[9])
	m.Manufacturer = string([]byte{byte(id>>10&0x1f) + 64, byte(id>>5&0x1f) + 64, byte(id&0x1f) + 64})
	m.WidthMM, m.HeightMM = int(e[21])*10, int(e[22])*10
	pref, maxV := 0.0, 0
	for off := 54; off+18 <= 126; off += 18 {
		d := e[off : off+18]
		if d[0] != 0 || d[1] != 0 {
			if off == 54 {
				m.Preferred, pref = timing(d), refresh(d)
			}
			continue
		}
		if d[3] == 0xfd {
			maxV = rangeMaxV(d)
			continue
		}
		text := strings.TrimSpace(strings.SplitN(string(d[5:18]), "\n", 2)[0])
		switch d[3] {
		case 0xfc:
			m.Model = text
		case 0xff:
			m.Serial = text
		}
	}
	// Un rango que no llega al modo preferido no describe al monitor (un
	// descriptor de relleno): se ignora.
	if maxV > 0 && (pref == 0 || float64(maxV)+1 >= pref) {
		m.MaxVHz = maxV
	}
}

// rangeMaxV es la frecuencia vertical máxima del descriptor de rango
// (0xFD): bytes 5 (mínimo) y 6 (máximo), en Hz. En EDID 1.4 el bit 0 del
// byte 4 suma 255 al mínimo y el bit 1 al máximo. 0 si no tiene sentido.
func rangeMaxV(d []byte) int {
	lo, hi := int(d[5]), int(d[6])
	if d[4]&0x01 != 0 {
		lo += 255
	}
	if d[4]&0x02 != 0 {
		hi += 255
	}
	if hi == 0 || lo > hi {
		return 0
	}
	return hi
}

// dtd lee las medidas de un detailed timing descriptor: activo y blanking,
// horizontal y vertical.
func dtd(d []byte) (ha, hb, va, vb int) {
	ha = int(d[2]) | int(d[4]>>4)<<8
	hb = int(d[3]) | int(d[4]&0x0f)<<8
	va = int(d[5]) | int(d[7]>>4)<<8
	vb = int(d[6]) | int(d[7]&0x0f)<<8
	return
}

// refresh es la frecuencia de un detailed timing descriptor en Hz (0 si no
// se puede calcular).
func refresh(d []byte) float64 {
	clk := float64(uint16(d[1])<<8|uint16(d[0])) * 10000
	ha, hb, va, vb := dtd(d)
	if ha == 0 || va == 0 {
		return 0
	}
	return clk / float64((ha+hb)*(va+vb))
}

// timing interpreta un detailed timing descriptor → "2560x1440@143.91".
func timing(d []byte) string {
	ha, _, va, _ := dtd(d)
	hz := refresh(d)
	if hz == 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d@%.2f", ha, va, hz)
}
