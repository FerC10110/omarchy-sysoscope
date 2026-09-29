// Package view arma el modelo de cada sección (bloques y acciones) a partir de
// un Snapshot. Lo consumen el panel QML y, en la etapa C, lazylazy (§7 de la
// spec): el contenido y sus traducciones viven en un solo lugar.
package view

// View es el modelo de una sección, igual para el panel y para lazylazy (§7).
type View struct {
	Schema  int      `json:"schema"`
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Blocks  []Block  `json:"blocks"`
	Actions []Action `json:"actions"`
}

// Block es uno de: kv, meter, spark, table, text, group, cores, stat, cards,
// hero. Solo se llenan los campos de su tipo.
type Block struct {
	Type       string    `json:"type"`
	Title      string    `json:"title,omitempty"`      // kv, table, group, cores
	Rows       any       `json:"rows,omitempty"`       // kv: [][2]string · table: []Row
	Label      string    `json:"label,omitempty"`      // meter, spark
	Value      *float64  `json:"value,omitempty"`      // meter: 0..1
	Text       string    `json:"text,omitempty"`       // meter, spark, text
	Level      string    `json:"level,omitempty"`      // ok|warn|crit (meter, text, celdas)
	Values     []float64 `json:"values,omitempty"`     // spark
	Columns    []string  `json:"columns,omitempty"`    // table
	Selectable bool      `json:"selectable,omitempty"` // table
	Blocks     []Block   `json:"blocks,omitempty"`     // group
	Cells      []Cell    `json:"cells,omitempty"`      // cores
	Link       string    `json:"link,omitempty"`       // text, stat: id de la sección a la que lleva
	Unit       string    `json:"unit,omitempty"`       // stat
	Lines      []string  `json:"lines,omitempty"`      // stat, hero
}

type Row struct {
	ID    string   `json:"id"`
	Cells []string `json:"cells"`
	Flags []string `json:"flags,omitempty"`
}

// Cell es un núcleo del mapa: la etiqueta, el uso 0..1 y su nivel.
type Cell struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Level string  `json:"level"`
}

type Action struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	ID      string   `json:"id"`
	Target  string   `json:"target"` // row|section
	Confirm string   `json:"confirm,omitempty"`
	Mode    string   `json:"mode"` // background|fullscreen
	Input   *Input   `json:"input,omitempty"`
	Hide    []string `json:"hide,omitempty"` // banderas de fila con las que no se ofrece (panel)
}

// Input es el dato que una acción pide antes de correr: un texto, un número
// o una de las opciones.
type Input struct {
	Type    string   `json:"type"` // text|number|choice
	Prompt  string   `json:"prompt"`
	Options []string `json:"options,omitempty"`
	Default string   `json:"default,omitempty"`
}

// Section es una entrada de la barra lateral.
type Section struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Group string `json:"group"` // "" (Resumen), components, system
	Badge int    `json:"badge,omitempty"`
}

// Options: idioma, unidad de temperatura ("c" o "f"), tarjetas del Resumen y
// los valores recientes de una métrica del historial (2 min; puede ser nil).
type Options struct {
	Lang   string
	Units  string
	Cards  []string
	Recent func(metric string) []float64
}

// recent devuelve los valores recientes de metric, o nil sin fuente.
func (o Options) recent(metric string) []float64 {
	if o.Recent == nil {
		return nil
	}
	return o.Recent(metric)
}

func KV(title string, rows ...[2]string) Block {
	if rows == nil {
		rows = [][2]string{}
	}
	return Block{Type: "kv", Title: title, Rows: rows}
}

// Meter: frac se recorta a 0..1.
func Meter(label string, frac float64, text, level string) Block {
	frac = min(max(frac, 0), 1)
	return Block{Type: "meter", Label: label, Value: &frac, Text: text, Level: level}
}

func Spark(label string, values []float64, text string) Block {
	return Block{Type: "spark", Label: label, Values: values, Text: text}
}

func Table(title string, columns []string, rows []Row, selectable bool) Block {
	if rows == nil {
		rows = []Row{}
	}
	return Block{Type: "table", Title: title, Columns: columns, Rows: rows, Selectable: selectable}
}

func Text(text, level string) Block { return Block{Type: "text", Text: text, Level: level} }

func Link(text, level, section string) Block {
	return Block{Type: "text", Text: text, Level: level, Link: section}
}

func Group(title string, blocks ...Block) Block {
	return Block{Type: "group", Title: title, Blocks: blocks}
}

func Cores(title string, cells []Cell) Block {
	return Block{Type: "cores", Title: title, Cells: cells}
}

// Stat es un número protagonista: text y unit llegan ya formateados y
// separados ("7" y "%"), lines son 0 a 3 líneas de detalle.
func Stat(label, text, unit, level string, lines ...string) Block {
	return Block{Type: "stat", Label: label, Text: text, Unit: unit, Level: level, Lines: lines}
}

// statOf es Stat con un valor formateado entero ("34 °C"), que Split separa.
func statOf(label, formatted, level string, lines ...string) Block {
	n, u := Split(formatted)
	return Stat(label, n, u, level, lines...)
}

// WithValues, WithValue y WithLink devuelven una copia con ese campo.
func (b Block) WithValues(v []float64) Block { b.Values = v; return b }

func (b Block) WithValue(frac float64) Block {
	f := min(max(frac, 0), 1)
	b.Value = &f
	return b
}

func (b Block) WithLink(id string) Block { b.Link = id; return b }

// Cards pone cada bloque en una tarjeta; el panel decide cuántas por fila.
func Cards(blocks ...Block) Block { return Block{Type: "cards", Blocks: blocks} }

// Hero es la cabecera de una sección: los stat en fila, las líneas de detalle
// y el historial reciente debajo (puede ser nil).
func Hero(stats []Block, lines []string, values []float64) Block {
	return Block{Type: "hero", Blocks: stats, Lines: lines, Values: values}
}
