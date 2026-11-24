package service

import (
	"math"
	"time"
)

func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

func dayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
