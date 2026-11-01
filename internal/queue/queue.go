// Package queue is the work queue a level-triggered controller runs on.
//
// The loop is level-triggered: a round recomputes the truth from the disk, so the only thing a queue
// has to carry is "this unit may have changed". That makes coalescing the whole point — a thousand
// events for one unit are one reconcile — and it makes a lost event harmless, because the resync
// floor reconciles every unit anyway. The shape is controller-manager's: a dirty set, a processing
// set, per-key exponential backoff on failure, and forget on success.
package queue

import (
	"fmt"
	"time"
)

// The floors and caps, in one place. Every one of them is pinned by a test, so a later tuning pass
// changes a number here and nowhere else.
const (
	// DefaultBound caps the keys waiting at once. A full queue drops new keys instead of growing: a
	// dropped key costs one resync interval of latency, and an unbounded queue costs the process.
	DefaultBound = 128
	// DefaultBaseDelay is the first pause after a failed reconcile. It doubles per failure.
	DefaultBaseDelay = time.Second
	// DefaultMaxDelay caps the retry pause, so a stuck unit is still retried every interval.
	DefaultMaxDelay = 10 * time.Minute
	// DefaultResyncEvery is the floor under a queue that can lose events.
	DefaultResyncEvery = 10 * time.Minute
)

// Options is what an operator may tune. The zero value is the default.
type Options struct {
	Bound     int
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

func (o Options) bound() int {
	if o.Bound <= 0 {
		return DefaultBound
	}
	return o.Bound
}

func (o Options) baseDelay() time.Duration {
	if o.BaseDelay <= 0 {
		return DefaultBaseDelay
	}
	return o.BaseDelay
}

func (o Options) maxDelay() time.Duration {
	if o.MaxDelay <= 0 {
		return DefaultMaxDelay
	}
	return o.MaxDelay
}

// task is one key's state. A key can be waiting, being processed, or both: an add that arrives while
// the key is being processed must survive the reconcile that is already running.
type task struct {
	queued     bool
	processing bool
	failures   int
	notBefore  time.Time
}

// Stats is the line the acceptance test prints, plus what a view needs.
type Stats struct {
	// Queued is how many keys this queue has ever accepted as a new entry, Processed how many it has
	// handed out, Dropped how many a full queue refused.
	Queued, Processed, Dropped int
	// Waiting and Processing are the current sizes.
	Waiting, Processing int
}

func (s Stats) String() string {
	return fmt.Sprintf("queued=%d processed=%d waiting=%d processing=%d dropped=%d",
		s.Queued, s.Processed, s.Waiting, s.Processing, s.Dropped)
}

// Queue is the dirty set and its rate limiter. It holds no clock: Get and Fail are given the time,
// because time belongs to whoever owns the loop (0016 R1).
type Queue struct {
	opt       Options
	tasks     map[string]*task
	order     []string // FIFO of keys that are waiting; entries go stale, never removed in place
	queued    int
	processed int
	dropped   int
}

func New(opt Options) *Queue {
	return &Queue{opt: opt, tasks: map[string]*task{}}
}

// Add marks a key dirty. It returns false when a full queue refused the key; every other answer
// means it is queued.
//
// Coalescing is here: a key that is already waiting is not added twice, and a key that is being
// processed is marked dirty so the round after this one reconciles the change too. That is how a
// level-triggered controller never loses a change that happened during a reconcile.
func (q *Queue) Add(key string) bool {
	if key == "" {
		return false
	}
	if t, ok := q.tasks[key]; ok {
		if t.processing && !t.queued {
			// Already admitted once: marking it dirty again is a second touch of the same key, not
			// a new entry.
			t.queued = true
			q.order = append(q.order, key)
			return true
		}
		if t.queued {
			return true // already dirty: this is the coalesce
		}
		t.queued = true
		q.order = append(q.order, key)
		q.queued++
		return true
	}
	if q.waiting() >= q.opt.bound() {
		// A dropped key is not lost: the resync floor adds it back within one interval.
		q.dropped++
		return false
	}
	q.tasks[key] = &task{queued: true}
	q.order = append(q.order, key)
	q.queued++
	return true
}

// Get hands out the next key whose retry delay has passed, or "" when there is none.
func (q *Queue) Get(now time.Time) string {
	q.compact()
	for _, key := range q.order {
		t, ok := q.tasks[key]
		if !ok || !t.queued {
			continue
		}
		if now.Before(t.notBefore) {
			continue // rate limited: still waiting, not ready
		}
		t.queued, t.processing = false, true
		q.processed++
		return key
	}
	return ""
}

// Done forgets a success: the failure count of that key resets. A key that was marked dirty while it
// was being processed goes back to the queue, so the change is not lost.
func (q *Queue) Done(key string) {
	t, ok := q.tasks[key]
	if !ok {
		return
	}
	t.processing, t.failures, t.notBefore = false, 0, time.Time{}
	if t.queued {
		q.order = append(q.order, key)
		return
	}
	delete(q.tasks, key)
}

// Fail puts a key back with a growing pause. The first failure waits baseDelay, each further one
// doubles it, and the pause is capped: a unit that keeps failing is still retried every interval
// instead of being abandoned.
func (q *Queue) Fail(key string, now time.Time) {
	t, ok := q.tasks[key]
	if !ok {
		q.Add(key)
		if t, ok = q.tasks[key]; !ok {
			// A full queue refused the key: the resync floor will bring it back.
			return
		}
	}
	t.processing = false
	t.failures++
	t.notBefore = now.Add(Backoff(q.opt, t.failures))
	t.queued = true
	q.order = append(q.order, key)
}

// Backoff is the pause after the n-th consecutive failure of one key.
func Backoff(opt Options, n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := opt.baseDelay()
	for i := 1; i < n; i++ {
		d *= 2
		if d > opt.maxDelay() {
			return opt.maxDelay()
		}
	}
	if d > opt.maxDelay() {
		return opt.maxDelay()
	}
	return d
}

// NotBefore is when a key may be handed out again, zero when it is not limited.
func (q *Queue) NotBefore(key string) time.Time {
	if t, ok := q.tasks[key]; ok {
		return t.notBefore
	}
	return time.Time{}
}

// Len is how many keys are waiting.
func (q *Queue) Len() int { return q.waiting() }

// Stats is the queue's current and cumulative numbers.
func (q *Queue) Stats() Stats {
	s := Stats{Queued: q.queued, Processed: q.processed, Dropped: q.dropped}
	for _, t := range q.tasks {
		if t.queued {
			s.Waiting++
		}
		if t.processing {
			s.Processing++
		}
	}
	return s
}

func (q *Queue) waiting() int {
	n := 0
	for _, t := range q.tasks {
		if t.queued {
			n++
		}
	}
	return n
}

// compact removes the stale entries of the FIFO. It runs when they dominate, which keeps the cost
// amortised and the slice small under a long run.
func (q *Queue) compact() {
	stale := 0
	for _, key := range q.order {
		if t, ok := q.tasks[key]; !ok || !t.queued {
			stale++
		}
	}
	if stale <= len(q.tasks) {
		return
	}
	kept := q.order[:0]
	for _, key := range q.order {
		if t, ok := q.tasks[key]; ok && t.queued {
			kept = append(kept, key)
		}
	}
	q.order = kept
}

// Resync is the floor under the queue: every Every, the caller adds every unit it knows, so a unit
// whose event was never delivered is reconciled one interval late instead of never. It holds no
// clock of its own: Due and Sweep are given the time.
type Resync struct {
	every time.Duration
	last  time.Time
}

func NewResync(every time.Duration) *Resync {
	if every <= 0 {
		every = DefaultResyncEvery
	}
	return &Resync{every: every}
}

// Every is the interval a view prints.
func (r *Resync) Every() time.Duration { return r.every }

// Last is when the last sweep happened, zero when there has been none.
func (r *Resync) Last() time.Time { return r.last }

// Due says whether the floor has passed. The first call is always due: a controller that just
// started has reconciled nothing yet.
func (r *Resync) Due(now time.Time) bool {
	if r.last.IsZero() {
		return true
	}
	return now.Sub(r.last) >= r.every
}

// Sweep reconciles the floor: when it is due, every key is added to the queue, and the returned
// count is how many new entries that created. A key the queue already had is not recounted: the
// point is "what did the floor find on its own", not "what did it touch".
func (r *Resync) Sweep(q *Queue, keys []string, now time.Time) int {
	if !r.Due(now) {
		return 0
	}
	before := q.queued
	for _, key := range keys {
		q.Add(key)
	}
	r.last = now
	return q.queued - before
}

// Pass records a sweep that the caller did itself, so the floor does not immediately repeat it.
func (r *Resync) Pass(now time.Time) { r.last = now }
