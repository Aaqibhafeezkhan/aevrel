package simulate

import "time"

// knot is one sampled value on a curve.
type knot struct {
	at    time.Duration
	value float64
}

type knots []knot

// at returns the piecewise-linear value at d, clamped to the ends. Linear rather than
// hold-last-value because a step time that doubles at 10:00 and stays doubled should not appear to
// start doubling at the previous sample.
func (k knots) at(d time.Duration) float64 {
	switch {
	case len(k) == 0:
		return 0
	case d <= k[0].at:
		return k[0].value
	}
	for i := 1; i < len(k); i++ {
		if d <= k[i].at {
			span := k[i].at - k[i-1].at
			if span <= 0 {
				return k[i].value
			}
			part := float64(d-k[i-1].at) / float64(span)
			return k[i-1].value + part*(k[i].value-k[i-1].value)
		}
	}
	return k[len(k)-1].value
}
