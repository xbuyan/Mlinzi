package report

import (
	"strings"
	"testing"
)

func TestSubmitCreatesAReport(t *testing.T) {
	s := NewStore()
	r, err := s.Submit("bribery", "ke-bribery-public-service", "Asked to pay KES 500 to collect a birth certificate.")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" {
		t.Fatal("expected a generated report ID")
	}
	if r.Status != Submitted {
		t.Fatalf("expected initial status Submitted, got %s", r.Status)
	}
	if r.VerificationCode == "" {
		t.Fatal("expected a non-empty verification code")
	}
}

func TestSubmitRejectsEmptyContent(t *testing.T) {
	s := NewStore()
	if _, err := s.Submit("bribery", "", ""); err == nil {
		t.Fatal("expected empty content to be rejected")
	}
}

func TestSubmitDoesNotRequireIdentity(t *testing.T) {
	// There is no field for a reporter's name, email, or phone anywhere in
	// submittedRecord or Report. This test exists to keep it that way: if a
	// future change adds an identity field, Get/Submit's signatures would
	// have to change and this test's intent would need revisiting.
	s := NewStore()
	r, err := s.Submit("gbv", "ke-gender-based-violence", "content")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.Content == "" {
		t.Fatal("expected a usable report")
	}
}

func TestGetUnknownReport(t *testing.T) {
	s := NewStore()
	if _, err := s.Get("rpt_does_not_exist"); err == nil {
		t.Fatal("expected ErrNotFound")
	}
}

func TestAdvanceFollowsValidOrder(t *testing.T) {
	s := NewStore()
	r, _ := s.Submit("bribery", "ke-bribery-public-service", "content")

	if _, err := s.Advance(r.ID, Acknowledged, "eacc", "Received, opening a file."); err != nil {
		t.Fatalf("expected submitted -> acknowledged to succeed, got %v", err)
	}
	if _, err := s.Advance(r.ID, Resolved, "eacc", "Officer referred for disciplinary action."); err != nil {
		t.Fatalf("expected acknowledged -> resolved to succeed, got %v", err)
	}

	got, err := s.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != Resolved {
		t.Fatalf("expected final status Resolved, got %s", got.Status)
	}
	if len(got.Events) != 2 {
		t.Fatalf("expected 2 status events, got %d", len(got.Events))
	}
}

func TestAdvanceRejectsSkippingAStep(t *testing.T) {
	s := NewStore()
	r, _ := s.Submit("bribery", "ke-bribery-public-service", "content")

	// This is the test that matters most in this file: an institution cannot
	// jump straight to "resolved" without ever acknowledging the report.
	_, err := s.Advance(r.ID, Resolved, "eacc", "closing this quietly")
	if err == nil {
		t.Fatal("expected submitted -> resolved to be rejected as an invalid transition")
	}
	if !strings.Contains(err.Error(), "cannot move from") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdvanceRejectsReopeningAResolvedReport(t *testing.T) {
	s := NewStore()
	r, _ := s.Submit("bribery", "ke-bribery-public-service", "content")
	s.Advance(r.ID, Acknowledged, "eacc", "")
	s.Advance(r.ID, Resolved, "eacc", "")

	if _, err := s.Advance(r.ID, Acknowledged, "eacc", "reopening"); err == nil {
		t.Fatal("expected a resolved report to reject further transitions")
	}
}

func TestAdvanceOnUnknownReport(t *testing.T) {
	s := NewStore()
	if _, err := s.Advance("rpt_ghost", Acknowledged, "eacc", ""); err == nil {
		t.Fatal("expected advancing an unknown report to fail")
	}
}

func TestVerifyReceiptAcceptsCorrectCode(t *testing.T) {
	s := NewStore()
	r, _ := s.Submit("bribery", "ke-bribery-public-service", "content")

	ok, err := s.VerifyReceipt(r.ID, r.VerificationCode)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected the reporter's own code to verify")
	}
}

func TestVerifyReceiptRejectsWrongCode(t *testing.T) {
	s := NewStore()
	r, _ := s.Submit("bribery", "ke-bribery-public-service", "content")

	ok, err := s.VerifyReceipt(r.ID, "0000000000")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected a fabricated code not to verify")
	}
}

func TestVerifyChainPassesForANormalHistory(t *testing.T) {
	s := NewStore()
	r1, _ := s.Submit("bribery", "ke-bribery-public-service", "first report")
	s.Advance(r1.ID, Acknowledged, "eacc", "")
	s.Submit("gbv", "ke-gender-based-violence", "second, unrelated report")
	s.Advance(r1.ID, Resolved, "eacc", "")

	if err := s.VerifyChain(); err != nil {
		t.Fatalf("expected a normal multi-report history to verify, got %v", err)
	}
}

func TestReportsDoNotInterleaveIncorrectly(t *testing.T) {
	// Two reports' events living in one shared ledger must not bleed into
	// each other's status trail.
	s := NewStore()
	r1, _ := s.Submit("bribery", "ke-bribery-public-service", "report one")
	r2, _ := s.Submit("policing", "ke-police-misconduct", "report two")

	s.Advance(r1.ID, Acknowledged, "eacc", "")
	s.Advance(r2.ID, Acknowledged, "ipoa", "")
	s.Advance(r1.ID, Resolved, "eacc", "")

	got1, _ := s.Get(r1.ID)
	got2, _ := s.Get(r2.ID)

	if got1.Status != Resolved {
		t.Fatalf("report one: expected Resolved, got %s", got1.Status)
	}
	if got2.Status != Acknowledged {
		t.Fatalf("report two: expected Acknowledged, got %s", got2.Status)
	}
	if len(got1.Events) != 2 {
		t.Fatalf("report one: expected 2 events, got %d", len(got1.Events))
	}
	if len(got2.Events) != 1 {
		t.Fatalf("report two: expected 1 event, got %d", len(got2.Events))
	}
}
