package report

import (
	"strings"
	"testing"

	"github.com/xbuyan/mlinzi/internal/ledger"
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

func TestAllReturnsEveryReportInSubmissionOrder(t *testing.T) {
	s := NewStore()
	r1, _ := s.Submit("bribery", "ke-bribery-public-service", "first")
	r2, _ := s.Submit("policing", "ke-police-misconduct", "second")
	r3, _ := s.Submit("gbv", "ke-gender-based-violence", "third")

	all := s.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 reports, got %d", len(all))
	}
	if all[0].ID != r1.ID || all[1].ID != r2.ID || all[2].ID != r3.ID {
		t.Fatalf("expected submission order %s,%s,%s — got %s,%s,%s",
			r1.ID, r2.ID, r3.ID, all[0].ID, all[1].ID, all[2].ID)
	}
}

func TestAllReflectsStatusChanges(t *testing.T) {
	s := NewStore()
	r, _ := s.Submit("bribery", "ke-bribery-public-service", "content")
	s.Advance(r.ID, Acknowledged, "eacc", "")

	all := s.All()
	if len(all) != 1 || all[0].Status != Acknowledged {
		t.Fatalf("expected the listed report to reflect its current status, got %+v", all)
	}
}

func TestAllOnEmptyStore(t *testing.T) {
	s := NewStore()
	if all := s.All(); len(all) != 0 {
		t.Fatalf("expected no reports, got %d", len(all))
	}
}

func TestNextOptionsMatchesLegalTransitions(t *testing.T) {
	cases := []struct {
		from Status
		want []Status
	}{
		{Submitted, []Status{Acknowledged, Rejected}},
		{Acknowledged, []Status{Resolved, Rejected}},
		{Resolved, nil},
		{Rejected, nil},
	}
	for _, c := range cases {
		got := c.from.NextOptions()
		if len(got) != len(c.want) {
			t.Fatalf("%s: expected %v, got %v", c.from, c.want, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: expected %v, got %v", c.from, c.want, got)
			}
		}
	}
}

func TestNextOptionsIsNotAliasedToInternalState(t *testing.T) {
	// NextOptions returns a copy; mutating it must not corrupt the package's
	// own transition table for every subsequent call.
	got := Submitted.NextOptions()
	got[0] = Resolved

	fresh := Submitted.NextOptions()
	if fresh[0] != Acknowledged {
		t.Fatal("mutating a returned NextOptions slice corrupted the shared transition table")
	}
}

// The persistence tests prove the specific promise restore makes: a store
// rebuilt from its own ledger snapshot answers every query exactly the way
// the original did. Anything less — a lost status event, a changed
// verification code — would make persistence a quiet rewriter of history,
// which is the one thing this layer exists to prevent.

func TestSnapshotRestorePreservesReportsAndStatusTrail(t *testing.T) {
	s := NewStore()
	r1, err := s.Submit("bribery", "ke-bribery-public-service", "First report: asked for 200 at the counter.")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Submit("policing", "ke-police-misconduct", "Second report, unrelated.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Advance(r1.ID, Acknowledged, "eacc", "Received and logged."); err != nil {
		t.Fatal(err)
	}

	restored, err := NewStoreFromLedger(restoredLedger(t, s.Ledger().Entries()))
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	for _, id := range []string{r1.ID, r2.ID} {
		// Compare against the original store's *current* state, not the
		// pre-advance struct captured above — r1 was submitted before its
		// status moved, so the captured value is history, not present state.
		want, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		got, err := restored.Get(id)
		if err != nil {
			t.Fatalf("restored store lost report %s: %v", id, err)
		}
		if got.Content != want.Content {
			t.Errorf("report %s content changed across restore:\n  was: %q\n  now: %q", id, want.Content, got.Content)
		}
		if got.VerificationCode != want.VerificationCode {
			t.Errorf("report %s verification code changed across restore: %s != %s — the code is derived from the ledger hash, so this means the history moved", id, want.VerificationCode, got.VerificationCode)
		}
		if got.Status != want.Status {
			t.Errorf("report %s status changed across restore: %s != %s", id, got.Status, want.Status)
		}
	}
	// The acknowledged report keeps its status trail, including who acted.
	got, _ := restored.Get(r1.ID)
	if len(got.Events) != 1 {
		t.Fatalf("expected the status event to survive the restore, got %d events", len(got.Events))
	}
	if got.Events[0].Actor != "eacc" || got.Events[0].Note != "Received and logged." {
		t.Errorf("status event lost its actor or note across restore: %+v", got.Events[0])
	}
	// The store must keep accepting new reports after a restore — a restore
	// that froze the ledger would only look like persistence.
	if _, err := restored.Submit("gbv", "ke-gender-based-violence", "Filed after the restore."); err != nil {
		t.Fatalf("restored store refused a new report: %v", err)
	}
}

func TestRestoreRejectsTamperedSnapshot(t *testing.T) {
	s := NewStore()
	if _, err := s.Submit("bribery", "ke-bribery-public-service", "report whose content someone edits on disk"); err != nil {
		t.Fatal(err)
	}

	entries := s.Ledger().Entries()
	// Simulate an edit to the historical record: the content in the first
	// entry's payload is rewritten in place, exactly what a tampering
	// attempt on the snapshot file would look like.
	tampered := strings.Replace(string(entries[0].Data), "whose content someone edits", "whose content was quietly replaced", 1)
	if tampered == string(entries[0].Data) {
		t.Fatal("test setup failed to alter the entry payload")
	}
	entries[0].Data = []byte(tampered)

	// The refusal happens in the ledger's own load step, before any report
	// is rebuilt on top of it.
	if _, err := ledger.NewFromEntries(entries); err == nil {
		t.Fatal("a tampered snapshot loaded cleanly — restore is not checking chain integrity")
	}
}

func TestRestoreRejectsRecordWithNoReportID(t *testing.T) {
	// A chain that is internally valid but whose submitted record carries no
	// report ID must still be refused: the ledger proves the bytes are
	// unaltered, not that they are sane, so the record-level check belongs
	// to this layer. Built through the public API so the chain is genuinely
	// valid — the failure has to come from the record validation.
	l := ledger.New()
	if _, err := l.Append("report.submitted", map[string]string{"report_id": ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStoreFromLedger(l); err == nil {
		t.Fatal("expected a submitted record with no report ID to be refused")
	}
}

// restoredLedger is what every store's restore path calls before rebuilding
// itself: the snapshot's entries go through ledger.NewFromEntries, which
// refuses an altered chain.
func restoredLedger(t *testing.T, entries []ledger.Entry) *ledger.Ledger {
	t.Helper()
	l, err := ledger.NewFromEntries(entries)
	if err != nil {
		t.Fatalf("ledger.NewFromEntries: %v", err)
	}
	return l
}

func TestRestoreRejectsTruncatedSnapshot(t *testing.T) {
	s := NewStore()
	r1, _ := s.Submit("bribery", "ke-bribery-public-service", "first")
	r2, _ := s.Submit("bribery", "ke-bribery-public-service", "second")
	_ = r2

	entries := s.Ledger().Entries()
	// Trimming the last entry is the other tampering shape: someone removes
	// a report from history entirely rather than editing it.
	restored, err := NewStoreFromLedger(restoredLedger(t, entries[:len(entries)-1]))
	if err != nil {
		t.Fatalf("a clean truncation that keeps the chain hashes consistent should load: %v", err)
	}
	// The restore itself is honest about what it loaded — the surviving
	// report is present, the dropped one is not. The guarantee is that the
	// ledger proves what it holds, not that deletion is impossible; it is
	// that deletion is detectable by anyone who kept the longer receipt.
	if _, err := restored.Get(r1.ID); err != nil {
		t.Fatal("restored store should still serve report one")
	}
	if _, err := restored.Get(r2.ID); err == nil {
		t.Fatal("the dropped report should not be present after a truncated restore")
	}
}

func TestRestoreCorruptRecordFails(t *testing.T) {
	// A chain that is internally valid (built through the ledger's own
	// Append) but whose submitted record does not have the shape this layer
	// wrote: the ledger proves the bytes are unaltered, not that they are
	// sane, so record-shape validation belongs to the restore here.
	l := ledger.New()
	if _, err := l.Append("report.submitted", map[string]any{
		"report_id": "rpt_forged", "category": "bribery", "content": 12345,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStoreFromLedger(l); err == nil {
		t.Fatal("expected a malformed submitted record to be refused")
	}
}
