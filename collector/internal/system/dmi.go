package system

import (
	"strconv"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

const dmi = "/sys/class/dmi/id/"

// dmiField lee un campo DMI sin textos de relleno. Los campos que solo lee
// root (serie) se omiten sin registrarlos.
func dmiField(env source.Env, name string) string {
	v, err := env.FS.Read(dmi + name)
	if err != nil {
		return ""
	}
	return collect.Clean(v)
}

func readDMI(env source.Env, s *model.System) {
	s.Product = model.DMIProduct{Vendor: dmiField(env, "sys_vendor"), Name: dmiField(env, "product_name"),
		Family: dmiField(env, "product_family"), Version: dmiField(env, "product_version")}
	s.Board = model.DMIBoard{Vendor: dmiField(env, "board_vendor"), Name: dmiField(env, "board_name"), Version: dmiField(env, "board_version")}
	s.BIOS = model.DMIBIOS{Vendor: dmiField(env, "bios_vendor"), Version: dmiField(env, "bios_version"),
		Date: dmiField(env, "bios_date"), Release: dmiField(env, "bios_release")}
	if n, err := strconv.Atoi(dmiField(env, "chassis_type")); err == nil {
		s.Chassis = chassis(n)
	}
}

// chassis traduce el tipo SMBIOS de gabinete.
func chassis(n int) string {
	switch n {
	case 3:
		return "Desktop"
	case 4:
		return "Low Profile Desktop"
	case 6:
		return "Mini Tower"
	case 7:
		return "Tower"
	case 8:
		return "Portable"
	case 9:
		return "Laptop"
	case 10:
		return "Notebook"
	case 13:
		return "All in One"
	case 14:
		return "Sub Notebook"
	case 17:
		return "Main Server Chassis"
	case 23:
		return "Rack Mount Chassis"
	case 30:
		return "Tablet"
	case 31:
		return "Convertible"
	case 32:
		return "Detachable"
	case 35:
		return "Mini PC"
	case 36:
		return "Stick PC"
	}
	return "Other"
}
