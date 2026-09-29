package cpu

import "github.com/FerC10110/omarchy-sysoscope/collector/internal/model"

type capDef struct{ group, name, flag string }

// x86Caps son las capacidades que se muestran en x86, por grupo y en orden.
var x86Caps = []capDef{
	{"simd", "MMX", "mmx"}, {"simd", "SSE", "sse"}, {"simd", "SSE2", "sse2"}, {"simd", "SSE3", "pni"},
	{"simd", "SSSE3", "ssse3"}, {"simd", "SSE4.1", "sse4_1"}, {"simd", "SSE4.2", "sse4_2"},
	{"simd", "AVX", "avx"}, {"simd", "AVX2", "avx2"}, {"simd", "FMA3", "fma"},
	{"simd", "AVX-512F", "avx512f"}, {"simd", "AVX-512BW", "avx512bw"}, {"simd", "AVX-512DQ", "avx512dq"},
	{"simd", "AVX-512VL", "avx512vl"}, {"simd", "AVX-512 VNNI", "avx512_vnni"},
	{"crypto", "AES-NI", "aes"}, {"crypto", "VAES", "vaes"}, {"crypto", "SHA-NI", "sha_ni"},
	{"crypto", "PCLMULQDQ", "pclmulqdq"}, {"crypto", "RDRAND", "rdrand"}, {"crypto", "RDSEED", "rdseed"},
	{"security", "SMEP", "smep"}, {"security", "SMAP", "smap"}, {"security", "CET shadow stack", "user_shstk"},
}

// armCaps son las capacidades en ARM (campo Features de /proc/cpuinfo).
var armCaps = []capDef{
	{"simd", "NEON", "asimd"}, {"simd", "SVE", "sve"}, {"simd", "SVE2", "sve2"},
	{"crypto", "AES", "aes"}, {"crypto", "SHA-1", "sha1"}, {"crypto", "SHA-2", "sha2"}, {"crypto", "SHA-3", "sha3"},
	{"security", "BTI", "bti"}, {"security", "PAC", "paca"},
}

// capabilities arma la lista a partir de los flags. En x86 la virtualización
// se muestra según el fabricante: VT-x/EPT/VPID en Intel, AMD-V/NPT en AMD (y
// la del otro fabricante como no soportada, para que se vea la ausencia).
func capabilities(arch, vendor string, flags map[string]bool) []model.Capability {
	defs := x86Caps
	if arch == "aarch64" || arch == "arm" || vendor == "ARM" {
		defs = armCaps
	} else {
		virt := []capDef{{"virtualization", "VT-x", "vmx"}, {"virtualization", "EPT", "ept"}, {"virtualization", "VPID", "vpid"},
			{"virtualization", "AMD-V", "svm"}, {"virtualization", "NPT", "npt"}}
		defs = append(append([]capDef{}, defs...), virt...)
	}
	out := make([]model.Capability, 0, len(defs))
	for _, d := range defs {
		out = append(out, model.Capability{Group: d.group, Name: d.name, Supported: flags[d.flag]})
	}
	return out
}
