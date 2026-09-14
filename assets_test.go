package mlinzi

import (
	"testing"

	"github.com/xbuyan/mlinzi/internal/guide"
)

// This is the regression test for a real bug: the first version of the CLI
// located data relative to `go env GOMOD`, which only resolves inside a Go
// toolchain and broke the moment the binary ran from an arbitrary directory
// on a machine without Go installed — exactly the "basic device" case this
// project is meant to serve. Embedding removes the working-directory
// dependency entirely; this test exists so nobody reintroduces it.
func TestEmbeddedDataLoadsIndependentOfWorkingDirectory(t *testing.T) {
	s, err := guide.Load(DataFS, "data")
	if err != nil {
		t.Fatalf("embedded dataset failed to load: %v", err)
	}
	if s.Len() == 0 {
		t.Fatal("expected the embedded dataset to contain guides")
	}
}
