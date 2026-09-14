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
	"sync"
	"time"

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

// submittedRecord is what enters the ledger when a report is first filed.
// GuideID links back to the Layer 1 civic guide the reporter came from (e.g.
// "ke-police-misconduct"), connecting the two layers of Mlinzi.
type submittedRecord struct {
	ReportID string `json:"report_id"`
	Category string `json:"category"`
	GuideID  string `json:"guide_id,omitempty"`
	Content  string `json:"content"`
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
	Status           Status
	SubmittedAt      time.Time
	Events           []StatusEvent
	VerificationCode string
}

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

// Submit files a new anonymous report. No identity is collected or required.
//
// It returns a verification code the reporter should keep. Presenting that
// code later proves exactly what was submitted and when — to themselves, to
// a journalist, to an NGO — without the reporter having given a name at
// submission time.
func (s *Store) Submit(category, guideID, content string) (Report, error) {
	if content == "" {
		return Report{}, errors.New("report content is empty")
	}
	id, err := newID()
	if err != nil {
		return Report{}, fmt.Errorf("generate report id: %w", err)
	}

	e, err := s.ledger.Append("report.submitted", submittedRecord{
		ReportID: id,
		Category: category,
		GuideID:  guideID,
		Content:  content,
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
