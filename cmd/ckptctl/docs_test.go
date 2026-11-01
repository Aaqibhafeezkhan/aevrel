package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// markdownLink finds [text](target). It is deliberately narrow: it must not fire on code blocks that
// show a shell command, and it does not try to parse Markdown in general.
var markdownLink = regexp.MustCompile(`\[[^\]]+\]\(([^)\s]+)\)`)

// F10's "zero dead links": a page that tells a user where to look must not send them nowhere. This
// walks every page in docs/ and checks that each relative link resolves to a file that exists.
func TestDocsHaveNoDeadLinks(t *testing.T) {
	docs, err := filepath.Glob("../../docs/*.md")
	if err != nil || len(docs) == 0 {
		t.Fatalf("no docs found: %v", err)
	}

	links := 0
	for _, page := range docs {
		raw, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("cannot read %s: %v", page, err)
		}
		for _, m := range markdownLink.FindAllStringSubmatch(string(raw), -1) {
			target := m[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			// Strip a fragment: the file is what we can check, and an anchor is checked by the eye.
			path := strings.SplitN(target, "#", 2)[0]
			if path == "" {
				continue
			}
			links++
			resolved := filepath.Join(filepath.Dir(page), path)
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s links to %s, which does not exist", filepath.Base(page), target)
			}
		}
	}
	t.Logf("docs: %d pages, %d local links, 0 dead", len(docs), links)
}
