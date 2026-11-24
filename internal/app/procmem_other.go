//go:build !windows

package app

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// processMemory: this process's anonymous resident memory and resident set, from
// /proc/self/status (Linux); 0 where there is no /proc.
func processMemory() (private, workingSet uint64) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fs := strings.Fields(v)
		if len(fs) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(fs[0], 10, 64)
		switch k {
		case "RssAnon":
			private = n * 1024
		case "VmRSS":
			workingSet = n * 1024
		}
	}
	return private, workingSet
}
