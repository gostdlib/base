// Package sizes contains constants for various data sizes that are commonly used.
// It is recommended to store data sizes in bytes or bits and convert as needed.
package sizes

// Sizes related to bytes. General uses is for storage sizes.
const (
	// Byte is a byte. 1 Byte = 8 bits.
	Byte = 8
	// Nibble is a nibble. 1 Nibble = 4 bits. Can be represented by a single hexadecimal digit.
	Nibble = 4
	// KiB is a kilobyte. 1 KiB = 1024 bytes.
	KiB = 1024
	// MiB is a megabyte. 1 MiB = 1024 KiB.
	MiB = 1024 * KiB
	// GiB is a gigabyte. 1 GiB = 1024 MiB.
	GiB = 1024 * MiB
	// TiB is a terabyte. 1 TiB = 1024 GiB.
	TiB = 1024 * GiB
	// PiB is a petabyte. 1 PiB = 1024 TiB.
	PiB = 1024 * TiB
	// EiB is an exabyte. 1 EiB = 1024 PiB.
	EiB = 1024 * PiB
	// ZiB is a zettabyte. 1 ZiB = 1024 EiB.
	ZiB = 1024 * EiB
	// YiB is a yottabyte. 1 YiB = 1024 ZiB.
	YiB = 1024 * ZiB
)

// Bandwidth related to bits. General uses is for network speeds.
// These are in bits per second. Bits are 1/8 the size of a byte.
const (
	// Bps is a bits per second.
	Bps = 1
	// Kbps is a kilobit per second.
	Kbps = 1024 * Bps
	// Mbps is a megabit per second.
	Mbps = 1024 * Kbps
	// Gbps is a gigabit per second.
	Gbps = 1024 * Mbps
	// Tbps is a terabit per second.
	Tbps = 1024 * Gbps
	// Pbps is a petabit per second.
	Pbps = 1024 * Tbps
	// Ebps is an exabit per second.
	Ebps = 1024 * Pbps
	// Zbps is a zettabit per second.
	Zbps = 1024 * Ebps
	// Ybps is a yottabit per second.
	Ybps = 1024 * Zbps
)

// Bandwidth related to bytes. General uses is for network speeds.
const (
	// BPS is a bytes per second.
	BPS = 1
	// KBps is a kilobyte per second.
	KBps = 1024 * BPS
	// MBps is a megabyte per second.
	MBps = 1024 * KBps
	// GBps is a gigabyte per second.
	GBps = 1024 * MBps
	// TBps is a terabyte per second.
	TBps = 1024 * GBps
	// PBps is a petabyte per second.
	PBps = 1024 * TBps
	// EBps is an exabyte per second.
	EBps = 1024 * PBps
	// ZBps is a zettabyte per second.
	ZBps = 1024 * EBps
	// YBps is a yottabyte per second.
	YBps = 1024 * ZBps
)
