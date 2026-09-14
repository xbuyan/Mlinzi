package guardian

import (
	"bytes"
	"testing"
	"time"
)

func newTestStore(now time.Time) *Store {
	s := NewStore()
	s.now = func() time.Time { return now }
	return s
}

func TestNewCaseSplitsKeyAndReturnsSharesNotStored(t *testing.T) {
	now := time.Now()
	s := newTestStore(now)
	key, err := ReleaseKey()
	if err != nil {
		t.Fatal(err)
	}

	c, shares, err := s.NewCase("rpt_1", key, []string{"lawyer", "journalist", "family"}, 2, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 3 {
		t.Fatalf("expected 3 shares, got %d", len(shares))
	}
	if c.Status != Active {
		t.Fatalf("expected new case to be Active, got %s", c.Status)
	}

	// The load-bearing guarantee: nothing reachable from the stored Case
	// exposes the shares. pendingShares is unexported and starts empty.
	if len(c.pendingShares) != 0 {
		t.Fatal("expected a freshly created case to hold no shares itself")
	}
}

func TestNewCaseRequiresAtLeastTwoGuardians(t *testing.T) {
	s := newTestStore(time.Now())
	key, _ := ReleaseKey()
	if _, _, err := s.NewCase("rpt_1", key, []string{"only-one"}, 1, time.Hour); err == nil {
		t.Fatal("expected at least 2 guardians to be required")
	}
}

func TestCheckInResetsOverdueClock(t *testing.T) {
	start := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, _, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)

	// Move forward, but within the interval, then check in.
	s.now = func() time.Time { return start.Add(50 * time.Minute) }
	if err := s.CheckIn(c.ID); err != nil {
		t.Fatal(err)
	}

	// Now move forward another 50 minutes — total 100 minutes since case
	// creation, which would be overdue against the original clock, but the
	// check-in at t+50m should have reset it.
	updated, _ := s.Get(c.ID)
	now := start.Add(100 * time.Minute)
	if updated.IsOverdue(now) {
		t.Fatal("expected the check-in to have reset the overdue clock")
	}
}

func TestIsOverdueAfterMissingCheckIn(t *testing.T) {
	start := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, _, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)

	got, _ := s.Get(c.ID)
	if got.IsOverdue(start.Add(30 * time.Minute)) {
		t.Fatal("expected case not to be overdue within the interval")
	}
	if !got.IsOverdue(start.Add(2 * time.Hour)) {
		t.Fatal("expected case to be overdue well past the interval")
	}
}

func TestEscalateRejectedWhenNotOverdue(t *testing.T) {
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, _, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)

	if err := s.Escalate(c.ID, start.Add(10*time.Minute)); err != ErrNotOverdue {
		t.Fatalf("expected ErrNotOverdue, got %v", err)
	}
}

func TestEscalateSucceedsWhenOverdue(t *testing.T) {
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, _, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)

	if err := s.Escalate(c.ID, start.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(c.ID)
	if got.Status != Escalated {
		t.Fatalf("expected Escalated, got %s", got.Status)
	}
}

func TestEscalateCannotBeCalledTwice(t *testing.T) {
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, _, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)

	s.Escalate(c.ID, start.Add(2*time.Hour))
	if err := s.Escalate(c.ID, start.Add(3*time.Hour)); err != ErrAlreadyEscalated {
		t.Fatalf("expected ErrAlreadyEscalated, got %v", err)
	}
}

func TestSubmitShareRejectedBeforeEscalation(t *testing.T) {
	s := newTestStore(time.Now())
	key, _ := ReleaseKey()
	c, shares, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)

	// The case has not been escalated — a guardian trying to submit early
	// (whether by mistake or by attempting an unauthorised early release)
	// must be refused.
	if _, err := s.SubmitShare(c.ID, "a", shares[0]); err != ErrNotEscalated {
		t.Fatalf("expected ErrNotEscalated, got %v", err)
	}
}

func TestSubmitShareRejectsUnknownGuardian(t *testing.T) {
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, shares, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)
	s.Escalate(c.ID, start.Add(2*time.Hour))

	if _, err := s.SubmitShare(c.ID, "impersonator", shares[0]); err != ErrUnknownGuardian {
		t.Fatalf("expected ErrUnknownGuardian, got %v", err)
	}
}

func TestSubmitShareRejectsDuplicateFromSameGuardian(t *testing.T) {
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, shares, _ := s.NewCase("rpt_1", key, []string{"a", "b", "c"}, 3, time.Hour)
	s.Escalate(c.ID, start.Add(2*time.Hour))

	if _, err := s.SubmitShare(c.ID, "a", shares[0]); err != ErrInsufficientCount {
		t.Fatalf("expected ErrInsufficientCount on first of three needed, got %v", err)
	}
	if _, err := s.SubmitShare(c.ID, "a", shares[0]); err != ErrDuplicateSubmit {
		t.Fatalf("expected ErrDuplicateSubmit, got %v", err)
	}
}

func TestReleaseRequiresThreshold(t *testing.T) {
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, shares, _ := s.NewCase("rpt_1", key, []string{"lawyer", "journalist", "family"}, 2, time.Hour)
	s.Escalate(c.ID, start.Add(2*time.Hour))

	// One share: not enough yet.
	if _, err := s.SubmitShare(c.ID, "lawyer", shares[0]); err != ErrInsufficientCount {
		t.Fatalf("expected ErrInsufficientCount, got %v", err)
	}
	got, _ := s.Get(c.ID)
	if got.Status != Escalated {
		t.Fatalf("expected case to remain Escalated below threshold, got %s", got.Status)
	}

	// Second share crosses the 2-of-3 threshold and reconstructs the key.
	recovered, err := s.SubmitShare(c.ID, "journalist", shares[1])
	if err != nil {
		t.Fatalf("expected reconstruction to succeed at threshold, got %v", err)
	}
	if !bytes.Equal(recovered, key) {
		t.Fatal("reconstructed key does not match the original release key")
	}

	got, _ = s.Get(c.ID)
	if got.Status != Released {
		t.Fatalf("expected Released, got %s", got.Status)
	}
}

func TestNoSinglePartyCanReleaseAlone(t *testing.T) {
	// This is the test that matters most in this file: with a 2-of-3
	// threshold, one guardian's share — even the platform operator's own
	// test harness holding it — must never be sufficient.
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, shares, _ := s.NewCase("rpt_1", key, []string{"a", "b", "c"}, 2, time.Hour)
	s.Escalate(c.ID, start.Add(2*time.Hour))

	_, err := s.SubmitShare(c.ID, "a", shares[0])
	if err != ErrInsufficientCount {
		t.Fatalf("expected a single share to be insufficient, got %v", err)
	}
	got, _ := s.Get(c.ID)
	if got.Status == Released {
		t.Fatal("a single guardian must never be able to trigger release alone")
	}
}

func TestSubmitShareRejectedAfterAlreadyReleased(t *testing.T) {
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, shares, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)
	s.Escalate(c.ID, start.Add(2*time.Hour))
	s.SubmitShare(c.ID, "a", shares[0])
	s.SubmitShare(c.ID, "b", shares[1])

	if _, err := s.SubmitShare(c.ID, "a", shares[0]); err != ErrAlreadyReleased {
		t.Fatalf("expected ErrAlreadyReleased, got %v", err)
	}
}

func TestCheckInRejectedAfterEscalation(t *testing.T) {
	// Once escalated, a late check-in must not silently cancel the
	// escalation — that would let an attacker who has taken control of the
	// reporter's phone quietly suppress a legitimate escalation.
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, _, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)
	s.Escalate(c.ID, start.Add(2*time.Hour))

	if err := s.CheckIn(c.ID); err == nil {
		t.Fatal("expected check-in to be rejected on an already-escalated case")
	}
}

func TestVerifyChainCoversFullLifecycle(t *testing.T) {
	start := time.Now()
	s := newTestStore(start)
	key, _ := ReleaseKey()
	c, shares, _ := s.NewCase("rpt_1", key, []string{"a", "b"}, 2, time.Hour)
	s.CheckIn(c.ID)
	s.Escalate(c.ID, start.Add(2*time.Hour))
	s.SubmitShare(c.ID, "a", shares[0])
	s.SubmitShare(c.ID, "b", shares[1])

	if err := s.VerifyChain(); err != nil {
		t.Fatalf("expected the full case lifecycle to verify, got %v", err)
	}
}
