// Package ledger implements a generic, tamper-evident, append-only log.
//
// This is the primitive the rest of Mlinzi is built on. It knows nothing
// about reports, guardians, or check-ins — only that each entry commits to
// its own content and to the hash of the entry before it. Change, reorder,
// or backdate any entry and every hash after it stops matching. That is what
// lets a report's status history be checked independently by a reporter, a
// journalist, or a court, without trusting whoever operates the ledger.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// genesisHash is the PrevHash of the first entry in any ledger.
const genesisHash = "genesis"

// Entry is one record in the chain.
type Entry struct {
	Seq       int             `json:"seq"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	Timestamp time.Time       `json:"timestamp"`
	PrevHash  string          `json:"prev_hash"`
	Hash      string          `json:"hash"`
}

// computeHash derives this entry's hash from its own fields. It is a pure
// function of the entry's content, so it can be recomputed and checked at
// any time without needing to trust a stored value.
func (e Entry) computeHash() string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%s|%s|%s|%s", e.Seq, e.Type, e.Data, e.Timestamp.UTC().Format(time.RFC3339Nano), e.PrevHash)
	return hex.EncodeToString(h.Sum(nil))
}

// Ledger is an append-only, hash-chained sequence of entries, safe for
// concurrent use.
type Ledger struct {
	mu      sync.Mutex
	entries []Entry
	now     func() time.Time // overridable for tests
}

// New returns an empty ledger.
func New() *Ledger {
	return &Ledger{now: time.Now}
}

// NewFromEntries returns a ledger pre-loaded with entries, after verifying
// the chain they form. This is the restore half of Snapshot: without it,
// "persistence" would mean trusting whatever bytes were on disk — which is
// exactly the trust this primitive exists to remove. A snapshot that was
// edited, truncated, or reordered fails here rather than loading, so what
// comes back from disk is proven unaltered, not assumed to be.
func NewFromEntries(entries []Entry) (*Ledger, error) {
	l := &Ledger{now: time.Now, entries: append([]Entry{}, entries...)}
	if err := l.Verify(); err != nil {
		return nil, err
	}
	return l, nil
}

// Append adds a new entry carrying data, marshalled to JSON, and returns the
// committed entry including its computed hash.
func (l *Ledger) Append(entryType string, data any) (Entry, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Entry{}, fmt.Errorf("marshal entry data: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	prev := genesisHash
	if n := len(l.entries); n > 0 {
		prev = l.entries[n-1].Hash
	}
	e := Entry{
		Seq:       len(l.entries),
		Type:      entryType,
		Data:      raw,
		Timestamp: l.now().UTC(),
		PrevHash:  prev,
	}
	e.Hash = e.computeHash()
	l.entries = append(l.entries, e)
	return e, nil
}

// Entries returns a copy of every entry, in order. A copy, not a reference,
// so callers cannot mutate ledger state through the slice they're handed.
func (l *Ledger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Len reports how many entries the ledger holds.
func (l *Ledger) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// ErrTampered is returned by Verify when the chain does not check out.
type ErrTampered struct {
	AtSeq  int
	Reason string
}

func (e ErrTampered) Error() string {
	return fmt.Sprintf("ledger tampered at entry %d: %s", e.AtSeq, e.Reason)
}

// Verify walks the whole chain, confirming every entry's hash is correctly
// derived from its own content and from the previous entry's hash. This is
// the check that runs independently of whoever operates the ledger.
func (l *Ledger) Verify() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	prev := genesisHash
	for _, e := range l.entries {
		if e.PrevHash != prev {
			return ErrTampered{AtSeq: e.Seq, Reason: "prev_hash does not match the preceding entry"}
		}
		if want := e.computeHash(); e.Hash != want {
			return ErrTampered{AtSeq: e.Seq, Reason: "hash does not match entry content"}
		}
		prev = e.Hash
	}
	return nil
}
