package combat

import "math"

const (
	TickSeconds    = 0.125
	TicksPerSecond = int64(8)
)

func TickToSeconds(tick int64) float64 {
	return float64(tick) * TickSeconds
}

func DelayUntilTick(nowTick int64, deadlineSec float64) int64 {
	delta := deadlineSec - TickToSeconds(nowTick)
	if delta <= 0 {
		return 0
	}
	return int64(math.Ceil(delta / TickSeconds))
}

func minPositiveDeadline(a, b float64) float64 {
	if b <= 0 {
		return a
	}
	if a <= 0 || b < a {
		return b
	}
	return a
}
