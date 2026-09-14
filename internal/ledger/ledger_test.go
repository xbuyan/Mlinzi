package ledger

import (
	"strings"
	"testing"
	"time"
)

type sample struct {
	Msg string `json:"msg"`
}

func TestAppendChainsHashes(t *testing.T) {
	l := New()
	e0, err := l.Append("test", sample{Msg: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if e0.PrevHash != genesisHash {
		t.Fatalf("expected genesis prev hash on first entry, got %q", e0.PrevHash)
	}

	e1, err := l.Append("test", sample{Msg: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if e1.PrevHash != e0.Hash {
		t.Fatalf("expected second entry to chain to first, got prev=%q want=%q", e1.PrevHash, e0.Hash)
	}
	if e1.Seq != 1 {
		t.Fatalf("expected seq 1, got %d", e1.Seq)
	}
}

func TestVerifyPassesOnUntamperedChain(t *testing.T) {
	l := New()
	for i := 0; i < 5; i++ {
		if _, err := l.Append("test", sample{Msg: "entry"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Verify(); err != nil {
		t.Fatalf("expected untampered chain to verify, got %v", err)
	}
}

func TestVerifyDetectsMutatedData(t *testing.T) {
	l := New()
	l.Append("test", sample{Msg: "original"})
	l.Append("test", sample{Msg: "second"})

	// White-box: reach into the unexported slice to simulate an operator
	// editing a stored entry after the fact. Nothing in the public API
	// allows this — that's the point of the package — so the only way to
	// prove Verify() catches it is to force the mutation here.
	l.entries[0].Data = []byte(`{"msg":"edited after the fact"}`)

	err := l.Verify()
	if err == nil {
		t.Fatal("expected tampering to be detected")
	}
	var te ErrTampered
	if !asErrTampered(err, &te) {
		t.Fatalf("expected ErrTampered, got %T: %v", err, err)
	}
	if te.AtSeq != 0 {
		t.Fatalf("expected tampering flagged at seq 0, got %d", te.AtSeq)
	}
}

func TestVerifyDetectsBrokenPrevHashLink(t *testing.T) {
	l := New()
	l.Append("test", sample{Msg: "first"})
	l.Append("test", sample{Msg: "second"})

	// Simulate deleting or reordering an entry: the second entry's PrevHash
	// no longer points at what actually precedes it.
	l.entries[1].PrevHash = "not-the-real-previous-hash"

	err := l.Verify()
	if err == nil || !strings.Contains(err.Error(), "prev_hash") {
		t.Fatalf("expected a prev_hash mismatch to be detected, got %v", err)
	}
}

func TestVerifyDetectsBackdating(t *testing.T) {
	l := New()
	l.Append("test", sample{Msg: "first"})

	// Simulate an operator backdating an entry to make it look earlier than
	// it really was. Timestamp is part of the hashed content, so this must
	// be caught the same way any other content edit is.
	l.entries[0].Timestamp = l.entries[0].Timestamp.Add(-30 * 24 * time.Hour)

	if err := l.Verify(); err == nil {
		t.Fatal("expected backdating to be detected")
	}
}

func TestEntriesReturnsACopy(t *testing.T) {
	l := New()
	l.Append("test", sample{Msg: "first"})
	got := l.Entries()
	got[0].Data = []byte(`{"msg":"mutated via the returned slice"}`)

	if err := l.Verify(); err != nil {
		t.Fatalf("mutating the returned slice must not affect the ledger, got %v", err)
	}
}

func asErrTampered(err error, target *ErrTampered) bool {
	te, ok := err.(ErrTampered)
	if ok {
		*target = te
	}
	return ok
}
