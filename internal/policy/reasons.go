package policy

// The reason vocabulary. A decision carries either a condition name (something the world said) or
// one of the three repair reasons below (something the algorithm saw about its own checkpoints).
// That is the whole code space, and TestTheCodeSpaceIsClosed holds it.
const (
	// codeWriteInFlight: a write is already running.
	codeWriteInFlight = "write-in-flight"
	// codeRepairBrokenCopy: the newest copy does not match its manifest and must be rewritten.
	codeRepairBrokenCopy = "repair-broken-copy"
	// codeRepairStorageFailing: writes are failing in a row; a person has to look at the storage.
	codeRepairStorageFailing = "repair-storage-failing"
)

// ReasonSpec is the declarative half of a code: which numbers it carries and which fields it read.
// The tests hold the emitters to it, so a code cannot quietly grow a value or cite a field that
// does not exist.
type ReasonSpec struct {
	Code     string
	Values   []string
	Evidence []string
}

var reasonCatalog = []ReasonSpec{
	{
		Code:     codeWriteInFlight,
		Values:   []string{"progress"},
		Evidence: []string{"SaveInFlight", "SaveProgress"},
	},
	{
		Code:     codeRepairBrokenCopy,
		Values:   []string{"step"},
		Evidence: []string{"Copies"},
	},
	{
		Code:     codeRepairStorageFailing,
		Values:   []string{"failures"},
		Evidence: []string{"ConsecutiveWriteFailures"},
	},
}

// IsKnownCode reports whether a decision may carry this code: either a repair reason from the
// catalogue above, or a condition name. It is the question a UI asks before it renders a decision,
// and the question the replay asks of every decision it produces.
func IsKnownCode(code string) bool {
	for _, spec := range reasonCatalog {
		if spec.Code == code {
			return true
		}
	}
	_, ok := ConditionNamed(code)
	return ok
}

// ReasonCatalog returns the repair reasons in a stable order. Condition names are the other half of
// the vocabulary and are declared in condition.go.
func ReasonCatalog() []ReasonSpec {
	out := make([]ReasonSpec, len(reasonCatalog))
	copy(out, reasonCatalog)
	for i := range out {
		out[i].Values = append([]string(nil), out[i].Values...)
		out[i].Evidence = append([]string(nil), out[i].Evidence...)
	}
	return out
}

// reason builds one entry for the catalogue. It is the only constructor the policies use, so the
// values a code carries are the ones its spec names.
func reason(code string, values map[string]float64, evidence ...string) ReasonEntry {
	return ReasonEntry{Code: code, Values: values, Evidence: evidence}
}
