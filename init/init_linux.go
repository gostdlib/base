//go:build linux

package init

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// containerMemory returns the memory limit of the container in bytes. If not in a container
// it returns -1. If GOMEMLIMIT is set, it returns that value.
func containerMemory() (int64, error) {
	if setting, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		return parseByteSize(setting)
	}

	// Try cgroup v1 first.
	limit, err := readCgroupMemory("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	if err == nil && limit > 0 {
		return int64(float64(limit) * .80), nil
	}

	// Try cgroup v2.
	limit, err = readCgroupMemory("/sys/fs/cgroup/memory.max")
	if err == nil && limit > 0 {
		return int64(float64(limit) * .80), nil
	}

	return -1, nil
}

// readCgroupMemory reads a cgroup memory limit file and returns the value in bytes.
// Returns -1 if the value is "max" (unlimited) or cannot be parsed.
func readCgroupMemory(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1, err
	}

	str := strings.TrimSpace(string(data))
	if str == "max" {
		return -1, nil
	}

	limit, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return -1, err
	}
	if limit <= 0 {
		return -1, nil
	}
	return limit, nil
}

// parseByteSize parses a byte size string that may have suffixes like B, KiB, MiB, GiB, TiB.
// Plain integers are treated as bytes.
func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)

	// Try plain integer first.
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v, nil
	}

	type suffixEntry struct {
		suffix string
		mult   int64
	}
	// Ordered longest-suffix-first so "KiB" matches before "B", etc.
	suffixes := []suffixEntry{
		{"TiB", 1024 * 1024 * 1024 * 1024},
		{"GiB", 1024 * 1024 * 1024},
		{"MiB", 1024 * 1024},
		{"KiB", 1024},
		{"B", 1},
	}

	for _, entry := range suffixes {
		if strings.HasSuffix(s, entry.suffix) {
			numStr := strings.TrimSpace(strings.TrimSuffix(s, entry.suffix))
			v, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid byte size %q: %w", s, err)
			}
			return v * entry.mult, nil
		}
	}

	return 0, fmt.Errorf("invalid byte size %q: unrecognized suffix", s)
}
