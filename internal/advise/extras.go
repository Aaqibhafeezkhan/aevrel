package advise

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// Extras returns the fields of the published file that this build does not know.
//
// The decision gains structured fields over time (reasons, sensitivity, whatever comes next). The
// view must show them without a release on this side, so it renders what is in the file and ignores
// nothing: "known" is derived from the struct's own tags, which means a field added here stops being
// an extra in the same commit that adds it.
func Extras(dir string) (map[string]json.RawMessage, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, BaseDir, stateName))
	if err != nil {
		return nil, false
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, false
	}
	for key := range knownKeys() {
		delete(all, key)
	}
	return all, true
}

func knownKeys() map[string]bool {
	t := reflect.TypeOf(stateFile{})
	out := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" {
			continue
		}
		out[strings.Split(tag, ",")[0]] = true
	}
	return out
}

// RenderExtras prints the unknown fields, one line each, in name order. Values are shown as they
// appear in the file: a view that reformats a structure it does not understand is a view that loses
// the information it was trying to show.
func RenderExtras(w io.Writer, extras map[string]json.RawMessage) {
	if len(extras) == 0 {
		return
	}
	names := make([]string, 0, len(extras))
	for name := range extras {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "extra           %s: %s\n", name, compact(extras[name]))
	}
}

func compact(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
