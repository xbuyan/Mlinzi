// Package guardian implements the dead-man's-switch layer: a reporter checks
// in on a cadence, and if they go silent, evidence escalates to pre-named
// guardians whose combined shares — not any single party's — are required
// to release it.
//
// The load-bearing design rule: a Case never stores enough information to
// reconstruct the release key by itself. Split happens once, at creation,
// and the resulting shares leave the process immediately for out-of-band
// distribution to guardians. Everything the Case tracks after that is
// metadata — check-in timestamps and escalation state — never key material.
package guardian

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/xbuyan/mlinzi/internal/ledger"
	"github.com/xbuyan/mlinzi/internal/shamir"
)

// Status is where a case stands.
type Status string

const (
	Active    Status = "active"    // reporter is checking in normally
	Escalated Status = "escalated" // a check-in was missed; guardians may now submit shares
	Released  Status = "released"  // threshold shares combined; key reconstructed
)

var (
	ErrNotOverdue        = errors.New("guardian: case has not missed a check-in yet")
	ErrAlreadyEscalated  = errors.New("guardian: case is already escalated")
	ErrAlreadyReleased   = errors.New("guardian: case has already released")
	ErrNotEscalated      = errors.New("guardian: shares can only be submitted after escalation")
	ErrUnknownGuardian   = errors.New("guardian: submitter is not a named guardian on this case")
	ErrDuplicateSubmit   = errors.New("guardian: this guardian has already submitted a share")
	ErrInsufficientCount = errors.New("guardian: not enough shares yet to attempt reconstruction")
)

// ReleaseKey generates a fresh 32-byte (AES-256-length) key. What this key
// actually encrypts is Layer 2 report content in a future iteration; here it
// is treated opaquely, since the escalation mechanism is what this layer
// proves, independent of what the key is eventually used for.
func ReleaseKey() ([]byte, error) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, fmt.Errorf("guardian: generate release key: %w", err)
	}
	return k, nil
}

// caseCreatedRecord, checkInRecord, escalatedRecord, and releasedRecord are
// what actually enter the shared ledger. Recording check-ins here — not just
// escalations — is what lets a reporter later prove they *did* check in on
// time, not only what lets guardians prove a release was triggered
// correctly.
type caseCreatedRecord struct {
	CaseID     string   `json:"case_id"`
	ReportID   string   `json:"report_id,omitempty"`
	GuardianID []string `json:"guardian_ids"`
	Threshold  int      `json:"threshold"`
	Interval   string   `json:"interval"`
}

type checkInRecord struct {
	CaseID string `json:"case_id"`
}

type escalatedRecord struct {
	CaseID      string `json:"case_id"`
	MissedSince string `json:"missed_since"`
	LastCheckIn string `json:"last_check_in"`
}

type shareSubmittedRecord struct {
	CaseID      string `json:"case_id"`
	GuardianID  string `json:"guardian_id"`
	SubmittedAt string `json:"submitted_at"`
}

type releasedRecord struct {
	CaseID         string   `json:"case_id"`
	GuardiansUsed  []string `json:"guardians_used"`
	KeyFingerprint string   `json:"key_fingerprint"` // hash of the key, never the key itself
}

// Case is a dead-man's-switch instance. Every field here is metadata; the
// only place key material transiently exists is the return value of
// NewCase (the shares, handed to the caller for distribution) and the return
// value of a successful SubmitShare that crosses threshold (the
// reconstructed key, handed to the caller for immediate use — the Case
// itself never retains it).
type Case struct {
	ID          string
	ReportID    string
	GuardianIDs []string
	Threshold   int
	Interval    time.Duration
	CreatedAt   time.Time
	LastCheckIn time.Time
	Status      Status

	pendingShares map[string][]byte // guardianID -> share, only populated after escalation
	shareLen      int               // for a basic sanity check on submitted shares
}

// Store holds cases and the ledger recording every check-in and escalation
// event, so reporters and guardians can independently verify the history.
type Store struct {
	ledger *ledger.Ledger
	cases  map[string]*Case
	now    func() time.Time
}

// NewStore returns an empty guardian store backed by a fresh ledger.
func NewStore() *Store {
	return &Store{
		ledger: ledger.New(),
		cases:  make(map[string]*Case),
		now:    time.Now,
	}
}

// NewCase creates a dead-man's-switch case for a release key, splitting it
// into shares immediately and returning them for out-of-band distribution to
// guardians. The Case retains none of the shares — only which guardian IDs
// exist and how many of them (threshold) must act together to release.
func (s *Store) NewCase(reportID string, key []byte, guardianIDs []string, threshold int, interval time.Duration) (*Case, [][]byte, error) {
	if len(guardianIDs) < 2 {
		return nil, nil, errors.New("guardian: at least 2 guardians are required")
	}
	shares, err := shamir.Split(key, len(guardianIDs), threshold)
	if err != nil {
		return nil, nil, fmt.Errorf("guardian: split release key: %w", err)
	}

	id, err := newID()
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	c := &Case{
		ID:            id,
		ReportID:      reportID,
		GuardianIDs:   append([]string{}, guardianIDs...),
		Threshold:     threshold,
		Interval:      interval,
		CreatedAt:     now,
		LastCheckIn:   now,
		Status:        Active,
		pendingShares: make(map[string][]byte),
		shareLen:      len(shares[0]),
	}

	if _, err := s.ledger.Append("guardian.case_created", caseCreatedRecord{
		CaseID:     id,
		ReportID:   reportID,
		GuardianID: guardianIDs,
		Threshold:  threshold,
		Interval:   interval.String(),
	}); err != nil {
		return nil, nil, err
	}

	s.cases[id] = c
	return c, shares, nil
}

// CheckIn records that the reporter is still in control, resetting the
// overdue clock. Logged to the ledger so a reporter can later prove exactly
// when they checked in.
func (s *Store) CheckIn(caseID string) error {
	c, err := s.get(caseID)
	if err != nil {
		return err
	}
	if c.Status != Active {
		return fmt.Errorf("guardian: cannot check in on a case with status %s", c.Status)
	}
	c.LastCheckIn = s.now()
	_, err = s.ledger.Append("guardian.check_in", checkInRecord{CaseID: caseID})
	return err
}

// IsOverdue reports whether a case has missed its check-in window as of now.
func (c *Case) IsOverdue(now time.Time) bool {
	return now.Sub(c.LastCheckIn) > c.Interval
}

// Escalate marks a case as escalated because a check-in was missed. This
// must be checked against real elapsed time — evaluated at call time, not
// trusted as a flag someone could set directly — which is why it is a
// method that recomputes IsOverdue rather than a status anyone can assign.
func (s *Store) Escalate(caseID string, now time.Time) error {
	c, err := s.get(caseID)
	if err != nil {
		return err
	}
	switch c.Status {
	case Escalated:
		return ErrAlreadyEscalated
	case Released:
		return ErrAlreadyReleased
	}
	if !c.IsOverdue(now) {
		return ErrNotOverdue
	}
	c.Status = Escalated
	_, err = s.ledger.Append("guardian.escalated", escalatedRecord{
		CaseID:      caseID,
		MissedSince: c.LastCheckIn.Add(c.Interval).UTC().Format(time.RFC3339),
		LastCheckIn: c.LastCheckIn.UTC().Format(time.RFC3339),
	})
	return err
}

// SubmitShare accepts one guardian's share once a case has escalated. Once
// enough shares have been submitted to meet the threshold, it attempts
// reconstruction and returns the recovered key. Below threshold, it returns
// ErrInsufficientCount so callers can distinguish "still waiting on
// guardians" from a real failure.
func (s *Store) SubmitShare(caseID, guardianID string, share []byte) ([]byte, error) {
	c, err := s.get(caseID)
	if err != nil {
		return nil, err
	}
	if c.Status == Active {
		return nil, ErrNotEscalated
	}
	if c.Status == Released {
		return nil, ErrAlreadyReleased
	}
	if !contains(c.GuardianIDs, guardianID) {
		return nil, ErrUnknownGuardian
	}
	if _, dup := c.pendingShares[guardianID]; dup {
		return nil, ErrDuplicateSubmit
	}
	if len(share) != c.shareLen {
		return nil, fmt.Errorf("guardian: share from %s has unexpected length %d, want %d", guardianID, len(share), c.shareLen)
	}

	c.pendingShares[guardianID] = share
	if _, err := s.ledger.Append("guardian.share_submitted", shareSubmittedRecord{
		CaseID:      caseID,
		GuardianID:  guardianID,
		SubmittedAt: s.now().UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, err
	}

	if len(c.pendingShares) < c.Threshold {
		return nil, ErrInsufficientCount
	}

	shares := make([][]byte, 0, len(c.pendingShares))
	used := make([]string, 0, len(c.pendingShares))
	for gid, sh := range c.pendingShares {
		shares = append(shares, sh)
		used = append(used, gid)
	}
	key, err := shamir.Combine(shares)
	if err != nil {
		return nil, fmt.Errorf("guardian: reconstruction failed: %w", err)
	}

	c.Status = Released
	if _, err := s.ledger.Append("guardian.released", releasedRecord{
		CaseID:         caseID,
		GuardiansUsed:  used,
		KeyFingerprint: fingerprint(key),
	}); err != nil {
		return nil, err
	}
	return key, nil
}

// Get returns the current metadata for a case.
func (s *Store) Get(caseID string) (Case, error) {
	c, err := s.get(caseID)
	if err != nil {
		return Case{}, err
	}
	// Return a value copy without the pending-shares map, which is
	// intentionally not part of the public view of a case: it is transient
	// coordination state during escalation, not part of the case's identity.
	return Case{
		ID:          c.ID,
		ReportID:    c.ReportID,
		GuardianIDs: append([]string{}, c.GuardianIDs...),
		Threshold:   c.Threshold,
		Interval:    c.Interval,
		CreatedAt:   c.CreatedAt,
		LastCheckIn: c.LastCheckIn,
		Status:      c.Status,
	}, nil
}

// VerifyChain proves the ledger backing every case in this store — every
// check-in, escalation, share submission, and release — is unaltered.
func (s *Store) VerifyChain() error {
	return s.ledger.Verify()
}

func (s *Store) get(caseID string) (*Case, error) {
	c, ok := s.cases[caseID]
	if !ok {
		return nil, fmt.Errorf("guardian: case %q not found", caseID)
	}
	return c, nil
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "case_" + hex.EncodeToString(b), nil
}

// fingerprint returns a short identifier for a key, for logging and
// confirmation purposes, without ever writing the key itself to the ledger.
func fingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	h := hex.EncodeToString(sum[:])
	return h[:12]
}
