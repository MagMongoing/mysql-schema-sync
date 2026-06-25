package internal

import (
	"fmt"
	"time"
)

type myTimer struct {
	start time.Time
	end   time.Time
}

func newMyTimer() *myTimer {
	return &myTimer{
		start: time.Now(),
	}
}

func (mt *myTimer) stop() {
	mt.end = time.Now()
}

func (mt *myTimer) usedSecond() string {
	if mt.end.IsZero() {
		return "N/A"
	}
	return fmt.Sprintf("%f s", mt.end.Sub(mt.start).Seconds())
}
