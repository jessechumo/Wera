package sources

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestThrottle(t *testing.T) {
	th := NewThrottle(2, 20*time.Millisecond)
	var inFlight, peak atomic.Int32
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			th.Do(context.Background(), func() error {
				n := inFlight.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				inFlight.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()
	if peak.Load() > 2 {
		t.Errorf("peak concurrency %d > 2", peak.Load())
	}
	if el := time.Since(start); el < 100*time.Millisecond {
		t.Errorf("6 starts 20ms apart finished in %v", el)
	}
}
