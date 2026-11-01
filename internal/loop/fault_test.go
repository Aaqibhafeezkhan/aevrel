package loop

import "errors"

// faulty is a Fault hook that fails at one named point and lets the others through. The crash tests
// share it so that "which window" is one word in each test.
func faulty(point string, err error) func(string) error {
	return func(p string) error {
		if p == point {
			return err
		}
		return nil
	}
}

var _ = errors.New
