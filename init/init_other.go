//go:build !linux

package init

import (
	"os"
	"strconv"
)

// containerMemory returns the memory limit of the container in bytes. If not in a container
// it returns -1.
func containerMemory() (int64, error) {
	if setting, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		return strconv.ParseInt(setting, 10, 64)
	}
	return -1, nil
}
