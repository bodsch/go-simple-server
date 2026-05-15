// Package flagx provides small concurrency primitives used by the probe
// service. The central type is DelayedFlag, a boolean state that becomes
// true only after a configured delay and can be reset safely from any
// goroutine.
package flagx

import (
	"sync"
	"sync/atomic"
	"time"
)

// DelayedFlag is a boolean state that becomes true after a configured
// delay. It is safe for concurrent use.
//
// The flag distinguishes between three logical states:
//   - false, expiring at time T  → Load() returns false; Remaining()>0
//   - false, never expires       → Load() returns false; Remaining()==0
//     (only reachable transiently between Reset and timer start)
//   - true                       → Load() returns true; Remaining()==0
//
// Reset() can be called any number of times. A generation counter
// guarded by the same mutex as the timer callback prevents stale timers
// from flipping the flag after a fresh Reset.
type DelayedFlag struct {
	delay time.Duration

	// val is read lock-free on the hot path (Load).
	val atomic.Bool
	// deadline carries the timer expiry in UnixNano; 0 means "no pending timer".
	// It is read lock-free by Remaining() to avoid contention with frequent
	// HTTP probes.
	deadline atomic.Int64

	// mu protects gen and timer, and serialises Reset with the timer callback
	// so that a stale callback cannot overwrite val.
	mu    sync.Mutex
	gen   uint64
	timer *time.Timer
}

// NewDelayedFlag creates a DelayedFlag, sets it to false, and immediately
// schedules it to flip to true after delay. A non-positive delay makes the
// flag true at construction time.
func NewDelayedFlag(delay time.Duration) *DelayedFlag {
	f := &DelayedFlag{delay: delay}
	f.Reset()
	return f
}

// Load returns the current boolean state without acquiring a lock.
func (f *DelayedFlag) Load() bool { return f.val.Load() }

// Reset sets the flag to false and schedules it to flip to true after
// the configured delay. Concurrent calls and a concurrent timer expiry
// cannot leave the flag in an inconsistent state: the latest Reset wins.
func (f *DelayedFlag) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.gen++
	g := f.gen
	f.val.Store(false)

	if f.timer != nil {
		f.timer.Stop()
		f.timer = nil
	}

	if f.delay <= 0 {
		f.deadline.Store(0)
		f.val.Store(true)
		return
	}

	f.deadline.Store(time.Now().Add(f.delay).UnixNano())
	f.timer = time.AfterFunc(f.delay, func() { f.expire(g) })
}

// expire is the timer callback. It only flips val to true if the
// generation it was scheduled under is still current; otherwise it is
// the leftover of a stopped/superseded timer and must do nothing.
func (f *DelayedFlag) expire(g uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gen != g {
		return
	}
	f.val.Store(true)
	f.deadline.Store(0)
}

// Remaining returns the time left until the flag flips to true.
// It returns 0 when the flag is already true or when no timer is pending.
func (f *DelayedFlag) Remaining() time.Duration {
	dl := f.deadline.Load()
	if dl <= 0 {
		return 0
	}
	rem := time.Until(time.Unix(0, dl))
	if rem < 0 {
		return 0
	}
	return rem
}
