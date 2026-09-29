package system

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/collect"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

// Firmware consulta actualizaciones con fwupd (solo a pedido: sale a
// internet). Sin actualizaciones devuelve una lista vacía.
func Firmware(env source.Env, r *collect.Report) *model.Firmware {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := env.Run.Run(ctx, "fwupdmgr", "get-updates", "--json")
	f := &model.Firmware{Checked: env.Now().UTC(), Devices: []model.FirmwareDevice{}}
	text := strings.TrimSpace(string(out))
	if err != nil && !(source.ExitStatus(err) == 2 && !strings.HasPrefix(text, "{")) {
		r.FromErr("firmware", "fwupdmgr", err)
		return nil
	}
	if !strings.HasPrefix(text, "{") {
		return f // "No updatable devices" / "No updates available"
	}
	var res struct {
		Devices []struct {
			Name, Vendor, Version string
			Releases              []struct{ Version string }
		}
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		r.Err("firmware", err)
		return nil
	}
	for _, d := range res.Devices {
		fd := model.FirmwareDevice{Name: d.Name, Current: d.Version, Vendor: d.Vendor}
		if len(d.Releases) > 0 {
			fd.Available = d.Releases[0].Version
		}
		f.Devices = append(f.Devices, fd)
	}
	return f
}
