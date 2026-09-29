package source

import "path/filepath"

// InPath dice si name está en alguno de los directorios absolutos de path
// (el valor de PATH), buscado a través del FS: así un fixture decide si un
// comando está.
func InPath(fs FS, path, name string) bool {
	for _, d := range filepath.SplitList(path) {
		if filepath.IsAbs(d) && fs.Exists(filepath.Join(d, name)) {
			return true
		}
	}
	return false
}
