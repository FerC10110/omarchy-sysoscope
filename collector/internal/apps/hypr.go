package apps

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/FerC10110/omarchy-sysoscope/collector/internal/model"
	"github.com/FerC10110/omarchy-sysoscope/collector/internal/source"
)

type monitor struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Scale  float64 `json:"scale"`
	X      int     `json:"x"`
	Y      int     `json:"y"`
	Active struct {
		ID int `json:"id"`
	} `json:"activeWorkspace"`
	Special struct {
		ID int `json:"id"`
	} `json:"specialWorkspace"`
}

// client es una ventana tal como la da j/clients.
type client struct {
	Address   string `json:"address"`
	Mapped    bool   `json:"mapped"`
	Hidden    bool   `json:"hidden"`
	At        [2]int `json:"at"`
	Size      [2]int `json:"size"`
	Workspace struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"workspace"`
	Floating       bool     `json:"floating"`
	Monitor        int      `json:"monitor"`
	Class          string   `json:"class"`
	Title          string   `json:"title"`
	PID            int      `json:"pid"`
	XWayland       bool     `json:"xwayland"`
	Pinned         bool     `json:"pinned"`
	Fullscreen     int      `json:"fullscreen"`
	Grouped        []string `json:"grouped"`
	FocusHistoryID int      `json:"focusHistoryID"`
	InhibitingIdle bool     `json:"inhibitingIdle"`
	StableID       string   `json:"stableId"`
}

// winRef es una ventana con su proceso y si está en pantalla (la captura la
// necesita visible).
type winRef struct {
	pid     int
	w       model.Window
	visible bool
}

// hyprState es lo que se leyó de Hyprland: ventanas por pid (en orden
// estable, §2.4) y por dirección, la clase de la de foco más reciente de
// cada pid, y los monitores.
type hyprState struct {
	windows  map[int][]model.Window
	byAddr   map[string]winRef
	class    map[int]string
	monitors []monitor
}

// readHypr pide j/clients y j/monitors por el socket, sin crear procesos.
func readHypr(h source.Hypr) (*hyprState, error) {
	if h == nil {
		return nil, source.ErrNoHyprland
	}
	cb, err := h.Request("j/clients")
	if err != nil {
		return nil, err
	}
	mb, err := h.Request("j/monitors")
	if err != nil {
		return nil, err
	}
	var cs []client
	if err := json.Unmarshal(cb, &cs); err != nil {
		return nil, err
	}
	var ms []monitor
	if err := json.Unmarshal(mb, &ms); err != nil {
		return nil, err
	}
	names := map[int]string{}
	mons := map[int]monitor{}
	for _, m := range ms {
		names[m.ID] = m.Name
		mons[m.ID] = m
	}
	st := &hyprState{windows: map[int][]model.Window{}, byAddr: map[string]winRef{}, class: map[int]string{}, monitors: ms}
	// La clase de una app es la de su ventana de foco más reciente.
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].FocusHistoryID < cs[j].FocusHistoryID })
	for _, c := range cs {
		if _, ok := st.class[c.PID]; !ok && c.Mapped && c.PID > 0 {
			st.class[c.PID] = c.Class
		}
	}
	// Las filas, en orden estable: el foco no las mueve bajo el cursor.
	sort.SliceStable(cs, func(i, j int) bool { return stableLess(cs[i], cs[j]) })
	for _, c := range cs {
		if !c.Mapped || c.PID <= 0 {
			continue
		}
		w := model.Window{Address: c.Address, StableID: c.StableID, Title: c.Title, Class: c.Class, Workspace: c.Workspace.Name,
			Monitor: names[c.Monitor], Floating: c.Floating, Pinned: c.Pinned, Fullscreen: c.Fullscreen,
			XWayland: c.XWayland, Grouped: len(c.Grouped) > 0, Focused: c.FocusHistoryID == 0,
			InhibitIdle: c.InhibitingIdle, Size: c.Size, At: c.At}
		st.windows[c.PID] = append(st.windows[c.PID], w)
		st.byAddr[c.Address] = winRef{pid: c.PID, w: w, visible: onScreen(c, mons)}
	}
	return st, nil
}

// onScreen: mapeada, sin hidden y en el workspace activo de su monitor o en
// su workspace especial abierto. "visible" de j/clients no sirve: es true
// también en un workspace especial escondido (0.56.2).
func onScreen(c client, mons map[int]monitor) bool {
	m, ok := mons[c.Monitor]
	if !ok || !c.Mapped || c.Hidden {
		return false
	}
	ws := c.Workspace.ID
	return ws == m.Active.ID || (m.Special.ID != 0 && ws == m.Special.ID)
}

// classes son los procesos con ventana y la clase de la de foco más reciente.
func (h *hyprState) classes() map[int]string { return h.class }

// stableLess ordena por stableId (Hyprland 0.56; no se reusa) o, sin él, por
// dirección. Los dos son hexadecimales: se comparan como números.
func stableLess(a, b client) bool {
	ka, kb := a.StableID, b.StableID
	if ka == "" || kb == "" {
		ka, kb = a.Address, b.Address
	}
	ka, kb = strings.TrimPrefix(ka, "0x"), strings.TrimPrefix(kb, "0x")
	va, ea := strconv.ParseUint(ka, 16, 64)
	vb, eb := strconv.ParseUint(kb, 16, 64)
	if ea == nil && eb == nil && va != vb {
		return va < vb
	}
	return ka < kb
}

func (h *hyprState) monitor(name string) (monitor, bool) {
	for _, m := range h.monitors {
		if m.Name == name {
			return m, true
		}
	}
	return monitor{}, false
}

func (h *hyprState) monitorNames() []string {
	out := make([]string, 0, len(h.monitors))
	for _, m := range h.monitors {
		out = append(out, m.Name)
	}
	return out
}
