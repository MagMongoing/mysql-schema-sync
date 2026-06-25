package internal

import (
	"sync"
	"time"
)

// myTimer is a simple start/stop timer. L7: guarded by sync.RWMutex so
// concurrent stop()/usedSecond() calls don't race.
type myTimer struct {
	mu    sync.RWMutex
	start time.Time
	end   time.Time
}

func newMyTimer() *myTimer {
	return &myTimer{
		start: time.Now(),
	}
}

func (mt *myTimer) stop() {
	mt.mu.Lock()
	mt.end = time.Now()
	mt.mu.Unlock()
}

func (mt *myTimer) usedSecond() string {
	mt.mu.RLock()
	end := mt.end
	start := mt.start
	mt.mu.RUnlock()
	if end.IsZero() {
		return "N/A"
	}
	// L8: use Duration.String() for human-friendly formatting.
	d := end.Sub(start)
	return d.String()
}
