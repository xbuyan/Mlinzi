// Package report implements anonymous civic reporting on top of the
// tamper-evident ledger in internal/ledger.
//
// A Report is never itself the source of truth: it is a projection assembled
// by replaying ledger entries. There is no mutable "status" field an
// institution could quietly edit — only new entries to append. Content is
// stored in plaintext in this proof of concept; encryption at rest is a
// deliberate scope cut, deferred to the guardian-release layer (Layer 3),
// where it belongs rather than being faked here without the key-release
// mechanism behind it.
package report

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xbuyan/mlinzi/internal/evidence"
	"github.com/xbuyan/mlinzi/internal/ledger"
)

// Status is where a report stands in its lifecycle.
type Status string

const (
	Submitted    Status = "submitted"
	Acknowledged Status = "acknowledged"
	Resolved     Status = "resolved"
	Rejected     Status = "rejected"
)

// validNext enforces the ledger's real value: status only moves forward. A
// report cannot be marked resolved without first being acknowledged, and a
// resolved report cannot be quietly reopened, because there is no field to
// flip — only a new, chained entry that would itself need to be a valid
// forward transition.
var validNext = map[Status][]Status{
	Submitted:    {Acknowledged, Rejected},
	Acknowledged: {Resolved, Rejected},
}

func (s Status) canTransitionTo(next Status) bool {
	for _, allowed := range validNext[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// NextOptions returns the statuses this status can legally move to next, in
// the order they're defined. An institution-facing UI uses this to render
// only the actions that are actually valid — never to offer a button whose
// click would just be rejected by Advance.
func (s Status) NextOptions() []Status {
	return append([]Status{}, validNext[s]...)
}

// submittedRecord is what enters the ledger when a report is first filed.
// GuideID links back to the Layer 1 civic guide the reporter came from (e.g.
// "ke-police-misconduct"), connecting the two layers of Mlinzi.
type submittedRecord struct {
	ReportID       string         `json:"report_id"`
	Category       string         `json:"category"`
	GuideID        string         `json:"guide_id,omitempty"`
	Content        string         `json:"content"`
	Evidence       []evidence.Ref `json:"evidence,omitempty"`
	OnBehalfOf     bool           `json:"on_behalf_of,omitempty"`
	OnBehalfOfNote string         `json:"on_behalf_of_note,omitempty"`
}

// statusRecord is what enters the ledger on every status change.
type statusRecord struct {
	ReportID string `json:"report_id"`
	Status   Status `json:"status"`
	Actor    string `json:"actor,omitempty"` // institution ID, if known
	Note     string `json:"note,omitempty"`
}

// StatusEvent is one step in a report's status trail, carrying the ledger
// coordinates that let it be checked independently.
type StatusEvent struct {
	Status    Status
	Actor     string
	Note      string
	Timestamp time.Time
	EntrySeq  int
	EntryHash string
}

// Report is a point-in-time view of a report's full history.
type Report struct {
	ID               string
	Category         string
	GuideID          string
	Content          string
	Evidence         []evidence.Ref
	OnBehalfOf       bool
	OnBehalfOfNote   string
	Status           Status
	SubmittedAt      time.Time
	Events           []StatusEvent
	VerificationCode string
}

// HasEvidence reports whether any files are attached. Templates use this
// rather than checking len(Evidence) directly so the meaning stays named.
func (r Report) HasEvidence() bool { return len(r.Evidence) > 0 }

// ErrNotFound is returned when a report ID is not in the store.
type ErrNotFound struct{ ID string }

func (e ErrNotFound) Error() string { return fmt.Sprintf("report %q not found", e.ID) }

// Store is the report layer, built on a single underlying ledger shared by
// every report it holds. Safe for concurrent use.
type Store struct {
	ledger *ledger.Ledger
	mu     sync.RWMutex
	byID   map[string]int // report ID -> ledger seq of its submittedRecord
}

// NewStore returns an empty report store with a fresh ledger.
func NewStore() *Store {
	return &Store{
		ledger: ledger.New(),
		byID:   make(map[string]int),
	}
}

// Submit files a new anonymous report with no evidence and no proxy
// submitter. It is a thin call to SubmitWithEvidence kept for every existing
// caller (the CLI demos, most tests) that has no reason to touch either of
// those newer fields.
//
// It returns a verification code the reporter should keep. Presenting that
// code later proves exactly what was submitted and when — to themselves, to
// a journalist, to an NGO — without the reporter having given a name at
// submission time.
func (s *Store) Submit(category, guideID, content string) (Report, error) {
	return s.SubmitWithEvidence(category, guideID, content, nil, false, "")
}

// SubmitWithEvidence is Submit, plus two things a reporter may need that a
// bare text report doesn't cover.
//
// evidenceRefs are files already validated and stored by an evidence.Store
// (see internal/evidence) — this package deliberately never touches raw
// file bytes itself, only the Refs describing what was already processed,
// the same way it never touches raw ledger bytes beyond what Append hands
// back. Their hashes become part of the same ledger entry as the report
// text, so a file swapped out after submission is exactly as detectable as
// edited report text already is.
//
// onBehalfOf and onBehalfOfNote exist for a person who cannot file their own
// report — no device, no literacy, no ability to reach a network, most
// concretely someone in pre-trial detention with no phone access at all —
// where a trusted third party (a family member, a paralegal, a fellow
// inmate's visitor) files on their behalf. This is recorded openly rather
// than the proxy silently posing as the person affected: an institution
// reading the report afterward can see it arrived this way, which matters
// for how much weight to give a first-person claim ("I was beaten") relayed
// by someone else.
func (s *Store) SubmitWithEvidence(category, guideID, content string, evidenceRefs []evidence.Ref, onBehalfOf bool, onBehalfOfNote string) (Report, error) {
	if content == "" {
		return Report{}, errors.New("report content is empty")
	}
	id, err := newID()
	if err != nil {
		return Report{}, fmt.Errorf("generate report id: %w", err)
	}

	e, err := s.ledger.Append("report.submitted", submittedRecord{
		ReportID:       id,
		Category:       category,
		GuideID:        guideID,
		Content:        content,
		Evidence:       evidenceRefs,
		OnBehalfOf:     onBehalfOf,
		OnBehalfOfNote: onBehalfOfNote,
	})
	if err != nil {
		return Report{}, err
	}

	s.mu.Lock()
	s.byID[id] = e.Seq
	s.mu.Unlock()

	return Report{
		ID:               id,
		Category:         category,
		GuideID:          guideID,
		Content:          content,
		Evidence:         evidenceRefs,
		OnBehalfOf:       onBehalfOf,
		OnBehalfOfNote:   onBehalfOfNote,
		Status:           Submitted,
		SubmittedAt:      e.Timestamp,
		VerificationCode: verificationCode(e),
	}, nil
}

// Advance moves a report to a new status, recording who did it (an
// institution ID, or "" if not yet known) and why. Only the forward
// transitions in validNext are allowed; anything else is rejected outright
// rather than silently applied — an out-of-order status is exactly the kind
// of quiet rewrite this whole layer exists to prevent.
func (s *Store) Advance(reportID string, next Status, actor, note string) (StatusEvent, error) {
	r, err := s.Get(reportID)
	if err != nil {
		return StatusEvent{}, err
	}
	if !r.Status.canTransitionTo(next) {
		return StatusEvent{}, fmt.Errorf("report %s: cannot move from %s to %s", reportID, r.Status, next)
	}

	e, err := s.ledger.Append("report.status", statusRecord{
		ReportID: reportID,
		Status:   next,
		Actor:    actor,
		Note:     note,
	})
	if err != nil {
		return StatusEvent{}, err
	}

	return StatusEvent{
		Status:    next,
		Actor:     actor,
		Note:      note,
		Timestamp: e.Timestamp,
		EntrySeq:  e.Seq,
		EntryHash: e.Hash,
	}, nil
}

// Get assembles the current state of a report by replaying every ledger
// entry that concerns it. This is a projection, not a mutable record, so
// there is nowhere for tampered state to hide.
func (s *Store) Get(reportID string) (Report, error) {
	s.mu.RLock()
	seq, ok := s.byID[reportID]
	s.mu.RUnlock()
	if !ok {
		return Report{}, ErrNotFound{ID: reportID}
	}

	entries := s.ledger.Entries()
	if seq >= len(entries) {
		return Report{}, ErrNotFound{ID: reportID}
	}
	base := entries[seq]
	var sub submittedRecord
	if err := json.Unmarshal(base.Data, &sub); err != nil {
		return Report{}, fmt.Errorf("corrupt submitted record for %s: %w", reportID, err)
	}

	r := Report{
		ID:               reportID,
		Category:         sub.Category,
		GuideID:          sub.GuideID,
		Content:          sub.Content,
		Evidence:         sub.Evidence,
		OnBehalfOf:       sub.OnBehalfOf,
		OnBehalfOfNote:   sub.OnBehalfOfNote,
		Status:           Submitted,
		SubmittedAt:      base.Timestamp,
		VerificationCode: verificationCode(base),
	}

	for _, e := range entries[seq+1:] {
		if e.Type != "report.status" {
			continue
		}
		var st statusRecord
		if err := json.Unmarshal(e.Data, &st); err != nil {
			continue
		}
		if st.ReportID != reportID {
			continue
		}
		r.Status = st.Status
		r.Events = append(r.Events, StatusEvent{
			Status:    st.Status,
			Actor:     st.Actor,
			Note:      st.Note,
			Timestamp: e.Timestamp,
			EntrySeq:  e.Seq,
			EntryHash: e.Hash,
		})
	}
	return r, nil
}

// VerifyChain proves the entire ledger backing every report in this store is
// unaltered. This is the check a reporter, journalist, or auditor can run
// independently of whoever operates Mlinzi.
func (s *Store) VerifyChain() error {
	return s.ledger.Verify()
}

// VerifyReceipt confirms a verification code matches a report that genuinely
// exists in the ledger, without requiring the presenter to prove identity.
func (s *Store) VerifyReceipt(reportID, code string) (bool, error) {
	r, err := s.Get(reportID)
	if err != nil {
		return false, err
	}
	return r.VerificationCode == code, nil
}

// All returns every report in the store, ordered by submission sequence
// (oldest first). This is what an institution-facing view lists — the
// report layer otherwise only supports lookup by a specific ID, which is
// the right shape for a reporter checking on their own report but not for
// an institution reviewing everything routed to it.
func (s *Store) All() []Report {
	s.mu.RLock()
	type idSeq struct {
		id  string
		seq int
	}
	ordered := make([]idSeq, 0, len(s.byID))
	for id, seq := range s.byID {
		ordered = append(ordered, idSeq{id, seq})
	}
	s.mu.RUnlock()

	sort.Slice(ordered, func(i, j int) bool { return ordered[i].seq < ordered[j].seq })

	out := make([]Report, 0, len(ordered))
	for _, e := range ordered {
		if r, err := s.Get(e.id); err == nil {
			out = append(out, r)
		}
	}
	return out
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "rpt_" + hex.EncodeToString(b), nil
}

// verificationCode derives a short, deterministic proof-of-submission code
// from the ledger entry's own hash. Because it is derived rather than
// separately issued, a code that matches a report ID is itself evidence the
// report was genuinely computed at submission time, not assigned after the
// fact.
func verificationCode(e ledger.Entry) string {
	if len(e.Hash) < 10 {
		return e.Hash
	}
	return e.Hash[:10]
}
