package exec

import (
	"fmt"
	"time"
)

// Window is the only clock the executor owns: the user's `with-us`.
//
// It answers one question — "has a checkpoint been written recently enough?" — and it is a deadman,
// not a schedule: it is restarted by any write that seals, so the user's span is a bound on staleness
// and not a periodic request. Two consequences follow, and both are the point:
//
//   - an exception write in the middle of the span pays for the window (the window that would have
//     been due does not happen), so the span costs what it says and not more;
//   - the period is measured from the seal, not from the request, because the promise is about the
//     newest complete checkpoint. A request made at the deadline would seal one write-time late.
//
// Zero span means off: nothing time-based will ever ask for a write, and the only writes are the
// catalogue's exceptions and the end of training.
type Window struct {
	span       time.Duration
	due        time.Time
	refreshed  time.Time
	lead       time.Duration
	infeasible bool
}

// NewWindow builds the window from the user's span. Zero means off.
func NewWindow(span time.Duration) Window { return Window{span: span} }

// On reports whether a window exists at all.
func (w Window) On() bool { return w.span > 0 }

// Span is the user's number.
func (w Window) Span() time.Duration { return w.span }

// Infeasible reports that the span is shorter than one write. The bound cannot hold, and the window
// is due immediately rather than silently degrading into back-to-back writes.
func (w Window) Infeasible() bool { return w.infeasible }

// DueAt is when the window closes; zero when it is off or has never been armed.
func (w Window) DueAt() time.Time { return w.due }

// Armed reports whether the window has ever been started. A restart that finds a checkpoint on disk
// re-arms from the checkpoint's own timestamp, so "not armed" is only true before the first seal.
func (w Window) Armed() bool { return !w.due.IsZero() || !w.refreshed.IsZero() }

// RefreshedAt is when the window was last restarted: the seal it was armed from.
func (w Window) RefreshedAt() time.Time { return w.refreshed }

// Lead is the write duration the due date was computed with.
func (w Window) Lead() time.Duration { return w.lead }

// Arm restarts the window from base: the seal it just observed, or the process start when there is
// no seal yet. cost is one write's duration (W); zero means unknown, and then the request goes out
// at the deadline with no lead, which is the best a shop without a measurement can do.
//
// Arming is the only way the window moves. It is never moved by a request: a request that is refused,
// held or abandoned must not extend the window, or "we tried" would become "we are covered".
func (w *Window) Arm(base time.Time, cost time.Duration) {
	if w.span <= 0 || base.IsZero() {
		return
	}
	if cost < 0 {
		cost = 0
	}
	w.refreshed = base
	w.lead = cost
	w.infeasible = cost >= w.span
	if w.infeasible {
		// The span cannot hold: one write takes longer than the whole window. Due immediately, and
		// the status says why, instead of pretending the bound is kept.
		w.due = base
		return
	}
	w.due = base.Add(w.span - cost)
}

// Due reports whether the window has closed.
func (w Window) Due(now time.Time) bool {
	return w.span > 0 && !w.due.IsZero() && !now.Before(w.due)
}

// Remaining is how long is left; negative once the window has closed.
func (w Window) Remaining(now time.Time) time.Duration {
	if w.span <= 0 || w.due.IsZero() {
		return 0
	}
	return w.due.Sub(now)
}

// String is the line a status view prints.
func (w Window) String() string {
	switch {
	case !w.On():
		return "window off (exceptions only)"
	case w.infeasible:
		return fmt.Sprintf("window %s is shorter than one write (%s) — this bound cannot hold",
			shortDuration(w.span), shortDuration(w.lead))
	case w.due.IsZero():
		return fmt.Sprintf("window %s (not armed yet)", shortDuration(w.span))
	default:
		return fmt.Sprintf("window %s · due %s (seal %s plus %s)",
			shortDuration(w.span), w.due.Format("15:04:05"), w.refreshed.Format("15:04:05"),
			shortDuration(w.span-w.lead))
	}
}

// shortDuration is how a status line says a length of time: minutes and seconds, never "1h0m0s".
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%.0fs", d.Round(time.Second).Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Round(time.Second).Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
