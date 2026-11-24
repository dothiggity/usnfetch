package app

import (
	"runtime"
	"runtime/debug"
)

// ReleaseMemory hands the memory Go has freed back to the operating system now. Go does
// that by itself, but slowly: after loading a big share or finishing a job the process sat
// near 1 GB for minutes. Called after those one-off peaks; runs in the background.
func ReleaseMemory() { go debug.FreeOSMemory() }

// MemoryInfo is what the process uses, split into Go's own memory and the rest (mostly
// SQLite's, which Go does not see), for the System page and /api/debug/memory.
func MemoryInfo() map[string]any {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	goHeld := ms.Sys - ms.HeapReleased
	out := map[string]any{
		"go_heap_in_use": ms.HeapInuse,                  // live Go data (and garbage not yet collected)
		"go_heap_spare":  ms.HeapIdle - ms.HeapReleased, // freed, not yet given back
		"go_held":        goHeld,                        // all Go holds from the OS
		"gc_runs":        ms.NumGC,
		"goroutines":     runtime.NumGoroutine(),
	}
	if private, ws := processMemory(); private > 0 {
		out["private"], out["working_set"] = private, ws
		if private > goHeld {
			out["other"] = private - goHeld
		}
	}
	return out
}
