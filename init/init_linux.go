//go:build linux

package init

import (
	"os"
	"strconv"
	"strings"
)

// containerMemory returns the memory limit of the container in bytes. If not in a container
// it returns -1. If GOMEMLIMIT is set, it returns that value.
func containerMemory() (int64, error) {
	if setting, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		return strconv.ParseInt(setting, 10, 64)
	}

	data, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	if err != nil {
		return -1, nil
	}

	str := strings.TrimSpace(string(data))
	limit, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return -1, err
	}
	if limit <= 0 {
		return -1, nil
	}

	// We want to use 80% of the memory limit.
	limit = int64(float64(limit) * .80)
	return limit, nil
}
