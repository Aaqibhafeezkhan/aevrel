package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// The executor's constants, in one place. Every one of them is a floor or a cap that a test pins,
// so a later tuning pass changes a number here and nowhere else.
const (
	// MinCooldown is the shortest pause between two attempts. A write that costs less than twenty
	// seconds still deserves a minute of quiet: the next one would otherwise be a retry storm.
	MinCooldown = 60 * time.Second
	// CooldownFactor is how many write-times of quiet are required. A slow write gets proportionally
	// longer quiet, because that is how long the storage is busy.
	CooldownFactor = 3
	// MaxBackoff caps the backoff while a condition keeps firing. Ten minutes is the cadence a job
	// with no controller would use, so the controller never becomes the slower one.
	MaxBackoff = 10 * time.Minute
	// UnknownWindow is the deadline of a write when its cost has never been measured. Ten minutes is
	// a guess in the safe direction: it declares nothing dead too early.
	UnknownWindow = 10 * time.Minute
)

// Cooldown is the pause required after one attempt, given the write cost that was measured.
func Cooldown(writeSeconds float64) time.Duration {
	d := time.Duration(writeSeconds * float64(time.Second) * CooldownFactor)
	if d < MinCooldown {
		return MinCooldown
	}
	return d
}

// Backoff is the exponential pause for the n-th repeated attempt on the same condition. n=0 is the
// cooldown; each further n doubles it up to MaxBackoff.
func Backoff(writeSeconds float64, n int) time.Duration {
	d := Cooldown(writeSeconds)
	for i := 0; i < n; i++ {
		d *= 2
		if d > MaxBackoff {
			return MaxBackoff
		}
	}
	return d
}

// Deadline is when an attempt that has produced nothing is declared dead. It is the cooldown, so
// "we waited long enough to have expected one write" is one rule everywhere.
func Deadline(writeSeconds float64, at time.Time) time.Time {
	w := time.Duration(writeSeconds * float64(time.Second))
	if w <= 0 {
		return at.Add(UnknownWindow)
	}
	return at.Add(Cooldown(writeSeconds))
}

// NewAttempt builds the request one decision asked for. Its ID is a hash of the identity, so the
// same request arriving after a restart is the same attempt and never a second one.
func NewAttempt(unitID string, generation, step int64, trigger Trigger, reason string, at time.Time, deadline time.Time) Attempt {
	return Attempt{
		ID:         identity(unitID, generation, step, trigger, reason),
		UnitID:     unitID,
		Generation: generation,
		Trigger:    trigger,
		Reason:     reason,
		Step:       step,
		StartedAt:  at,
		Deadline:   deadline,
	}
}

// identity is the attempt's name: stable across restarts, and distinct per generation, step, trigger
// and reason. Two writes of the same step for different reasons are two attempts on purpose.
func identity(unitID string, generation, step int64, trigger Trigger, reason string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d|%s|%s", unitID, generation, step, trigger, reason)))
	return hex.EncodeToString(sum[:8])
}
