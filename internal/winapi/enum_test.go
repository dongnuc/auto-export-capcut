//go:build windows

package winapi

import (
	"os"
	"sync"
	"testing"
)

// Regression: Windows() used to create a new syscall callback per call, and the
// Go runtime throws a fatal (unrecoverable) error after 2000 of them.
func TestWindowsManyCalls(t *testing.T) {
	pids := []uint32{uint32(os.Getpid())}
	for i := 0; i < 5000; i++ {
		Windows(pids)
	}
}

func TestWindowsConcurrent(t *testing.T) {
	pids := []uint32{uint32(os.Getpid())}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				Windows(pids)
			}
		}()
	}
	wg.Wait()
}
