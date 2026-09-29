package sensors

import "strings"

// kindOf clasifica un chip hwmon por su nombre. La tabla sigue la idea de
// Argus (nombres de driver → componente) ampliada con los chips vistos en
// máquinas reales.
func kindOf(name string, isNet func(string) bool) string {
	switch {
	case name == "k10temp" || name == "zenpower" || name == "coretemp" || name == "cpu_thermal" ||
		name == "cpu-thermal" || strings.HasPrefix(name, "soc_thermal"):
		return "cpu"
	case name == "amdgpu" || name == "nouveau" || name == "i915" || name == "xe" || name == "radeon":
		return "gpu"
	case name == "nvme":
		return "nvme"
	case name == "jc42" || name == "spd5118" || name == "ee1004":
		return "ram"
	case strings.HasPrefix(name, "asus") || strings.HasPrefix(name, "nct") || strings.HasPrefix(name, "it87") ||
		strings.HasPrefix(name, "it86") || name == "acpitz" || strings.HasPrefix(name, "gigabyte") ||
		strings.HasPrefix(name, "dell_smm") || name == "thinkpad":
		return "board"
	case strings.HasPrefix(name, "ath") || strings.HasPrefix(name, "iwlwifi") || strings.HasPrefix(name, "mt7") ||
		strings.HasPrefix(name, "rtw"):
		return "wifi"
	case strings.HasPrefix(name, "BAT"):
		return "battery"
	case isNet(name):
		return "nic"
	}
	return "other"
}

// baseLabel es el nombre legible del chip antes de numerar repetidos.
func baseLabel(kind, name, model string) string {
	switch kind {
	case "cpu":
		return "CPU"
	case "gpu":
		return "GPU"
	case "nvme":
		if model != "" {
			return "NVMe " + model
		}
		return "NVMe"
	case "ram":
		return "RAM"
	case "board":
		return "Motherboard"
	case "wifi":
		return "Wi-Fi"
	case "battery":
		return "Battery " + name
	case "nic":
		return "Ethernet " + name
	}
	return name
}
