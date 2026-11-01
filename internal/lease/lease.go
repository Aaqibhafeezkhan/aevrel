// Package lease keeps one writer per checkpoint directory.
//
// It is a cooperative lease, not consensus: the file says who is expected to be the writer and
// until when. Two controllers that start at the same instant can both believe they won, which is
// the same guarantee every lock file has ever given. What it does prevent — the case that actually
// happens — is a second controller started by hand, by a cron job, or by a supervisor after a
// restart, writing advice on top of the first one's.
package lease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const name = "lease"

// State is the file. ExpiresAt is a unix second: a lease is not a precision instrument.
type State struct {
	Holder    string `json:"holder"`
	ExpiresAt int64  `json:"expiresAt"`
}

func path(dir string) string { return filepath.Join(dir, ".ckpt-governor", name) }

// Read returns the current lease, reporting false when there is none.
func Read(dir string) (State, bool) {
	raw, err := os.ReadFile(path(dir))
	if err != nil {
		return State{}, false
	}
	var s State
	if json.Unmarshal(raw, &s) != nil {
		return State{}, false
	}
	return s, true
}

// Acquire takes the lease when it is free or expired, or renews it when we already hold it.
//
// A holder that is not us and has not expired is the answer "no", which is the whole point.
func Acquire(dir, holder string, ttl time.Duration, now time.Time) (bool, error) {
	cur, ok := Read(dir)
	if ok && cur.Holder != holder && now.Unix() < cur.ExpiresAt {
		return false, nil
	}
	s := State{Holder: holder, ExpiresAt: now.Add(ttl).Unix()}
	raw, err := json.Marshal(s)
	if err != nil {
		return false, err
	}
	return true, write(dir, raw)
}

// Release gives the lease up, but only if we are the holder: a controller that lost the lease must
// not delete the new holder's file.
func Release(dir, holder string) error {
	cur, ok := Read(dir)
	if !ok || cur.Holder != holder {
		return nil
	}
	return os.Remove(path(dir))
}

// write replaces the lease atomically, so a reader never sees half a holder name.
func write(dir string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path(dir)), 0o755); err != nil {
		return err
	}
	tmp := path(dir) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path(dir))
}
