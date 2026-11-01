package advise

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// NumbersFile is where the two answers live when they come from `init` rather than from flags.
//
// The reason this is a file and not a flag is the same reason conditions.json is a file: a number that
// lives only on the machine that started the controller cannot be audited later, and these two numbers
// are the whole contract. The flags still win when they are given — a person overriding one run should
// not have to edit a file — but the file is what travels with the checkpoint directory.
const NumbersFile = "numbers.json"

// Numbers is the pair as written by `init`. Empty means "not answered", which the run reports as
// unset rather than guessing.
type Numbers struct {
	Schema int    `json:"schema"`
	NoUs   string `json:"no-us,omitempty"`
	WithUs string `json:"with-us,omitempty"`
}

// LoadNumbers reads the file. A missing file is not an error: it means nobody answered, which is a
// valid state and the one every first run is in.
func LoadNumbers(dir string) (Numbers, error) {
	var n Numbers
	raw, err := os.ReadFile(filepath.Join(dir, BaseDir, NumbersFile))
	if err != nil {
		if os.IsNotExist(err) {
			return n, nil
		}
		return n, fmt.Errorf("%s: %w", NumbersFile, err)
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		return n, fmt.Errorf("%s: %w", NumbersFile, err)
	}
	return n, nil
}

// Write puts the answers on disk in one step: a reader must never see half a configuration, and a
// half-written pair is worse than none.
func (n Numbers) Write(dir string) error {
	path := filepath.Join(dir, BaseDir, NumbersFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	n.Schema = 1
	raw, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".numbers-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Empty reports whether the user answered nothing at all.
func (n Numbers) Empty() bool { return n.NoUs == "" && n.WithUs == "" }
