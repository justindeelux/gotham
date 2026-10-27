//go:build linux

package stats

// Linux filesystem magic numbers that never represent durable local storage.
// They come from include/uapi/linux/magic.h.
const (
	magicProc       = 0x9fa0
	magicSysfs      = 0x62656572
	magicTmpfs      = 0x01021994
	magicDevpts     = 0x1cd1
	magicCgroup     = 0x27e0eb
	magicCgroup2    = 0x63677270
	magicDebugfs    = 0x64626720
	magicSecurityfs = 0x73636673
	magicTracefs    = 0x74726163
	magicBinfmtfs   = 0x42494e4d
	magicNsfs       = 0x6e736673
	magicConfigfs   = 0x62656570
	magicBpfFs      = 0xcafe4a11
	magicPstore     = 0x6165676c
	magicEfivarfs   = 0xde5e81e4
)

func pseudoFilesystem(kind int64) bool {
	switch kind {
	case magicProc, magicSysfs, magicTmpfs, magicDevpts, magicCgroup,
		magicCgroup2, magicDebugfs, magicSecurityfs, magicTracefs,
		magicBinfmtfs, magicNsfs, magicConfigfs, magicBpfFs, magicPstore,
		magicEfivarfs:
		return true
	default:
		return false
	}
}
