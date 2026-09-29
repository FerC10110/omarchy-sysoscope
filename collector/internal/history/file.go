package history

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const magic = "SYSOHST1"

// ErrCorrupt: el archivo no es un historial v1 válido; se apartó a .bad.
var ErrCorrupt = errors.New("history file is corrupt or from another version")

// Load lee el historial. Si no existe devuelve uno vacío. Si está corrupto,
// es de otra versión o tiene otras métricas, lo renombra a .bad y devuelve
// uno vacío con ErrCorrupt (el formato v1 no migra).
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return New(), err
	}
	s, err := read(bytes.NewReader(data))
	if err != nil {
		os.Rename(path, path+".bad")
		return New(), fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return s, nil
}

func read(r io.Reader) (*Store, error) {
	br := bufio.NewReader(r)
	m := make([]byte, len(magic))
	if _, err := io.ReadFull(br, m); err != nil || string(m) != magic {
		return nil, errors.New("bad magic")
	}
	var n uint16
	if err := binary.Read(br, binary.LittleEndian, &n); err != nil || int(n) != len(Metrics) {
		return nil, errors.New("metric count differs")
	}
	for _, want := range Metrics {
		l, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		name := make([]byte, l)
		if _, err := io.ReadFull(br, name); err != nil || string(name) != want {
			return nil, errors.New("metric names differ")
		}
	}
	s := New()
	for _, rg := range s.rings {
		var hdr [2]uint32
		if err := binary.Read(br, binary.LittleEndian, &hdr); err != nil {
			return nil, err
		}
		if int(hdr[0]) != rg.res.Slots || int64(hdr[1]) != int64(rg.res.Step.Seconds()) {
			return nil, errors.New("resolutions differ")
		}
		for i := 0; i < rg.res.Slots; i++ {
			if err := binary.Read(br, binary.LittleEndian, &rg.bucket[i]); err != nil {
				return nil, err
			}
			if err := binary.Read(br, binary.LittleEndian, rg.vals[i]); err != nil {
				return nil, err
			}
		}
	}
	if _, err := br.ReadByte(); err != io.EOF {
		return nil, errors.New("trailing data")
	}
	return s, nil
}

func (s *Store) write(w io.Writer) error {
	bw := bufio.NewWriter(w)
	bw.WriteString(magic)
	binary.Write(bw, binary.LittleEndian, uint16(len(Metrics)))
	for _, name := range Metrics {
		bw.WriteByte(byte(len(name)))
		bw.WriteString(name)
	}
	for _, rg := range s.rings {
		binary.Write(bw, binary.LittleEndian, [2]uint32{uint32(rg.res.Slots), uint32(rg.res.Step.Seconds())})
		for i := range rg.bucket {
			binary.Write(bw, binary.LittleEndian, rg.bucket[i])
			binary.Write(bw, binary.LittleEndian, rg.vals[i])
		}
	}
	return bw.Flush()
}

// Save escribe el historial de forma atómica (temporal + rename).
func (s *Store) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".history-*")
	if err != nil {
		return err
	}
	if err := s.write(tmp); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
