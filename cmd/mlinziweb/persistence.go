package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/xbuyan/mlinzi/internal/evidence"
	"github.com/xbuyan/mlinzi/internal/guardian"
	"github.com/xbuyan/mlinzi/internal/ledger"
	"github.com/xbuyan/mlinzi/internal/persist"
	"github.com/xbuyan/mlinzi/internal/report"
)

// Persistence for the web app.
//
// What is saved and what is not, stated precisely:
//
//   - The report ledger and the guardian ledger go to disk as snapshots and
//     are verified (ledger.NewFromEntries) on restore — a tampered snapshot
//     refuses to load rather than serving altered history.
//   - Evidence files go to disk one file per content hash, named by that
//     hash, and are re-verified against it on restore — a swapped file is
//     refused. One file per hash rather than one JSON blob so that a
//     report at the evidence size limit (20MB) never has to be held twice
//     in memory inside a 256MB VM, and so the base64 expansion of embedding
//     bytes in JSON is never paid.
//   - Not saved, deliberately: in-flight guardian pendingShares. Shares are
//     key material; the platform's design rule is that key material never
//     persists anywhere — not in the Case, and not on its disk. A guardian
//     whose share was submitted before a restart submits it again; the
//     ledger records both submissions, which is the honest history.
//   - Not saved, deliberately: report verification codes are derived, not
//     stored — they come back exactly, because they are a function of the
//     restored ledger entries themselves.
//
// The environment variable is MLINZI_DATA_DIR. Unset (the default), the app
// behaves exactly as before: in-memory, gone on restart, which remains a
// legitimate configuration for throwaway demos.

const (
	envDataDir        = "MLINZI_DATA_DIR"
	reportLedgerFile  = "reports-ledger.json"
	guardianLedgerFle = "guardians-ledger.json"
	evidenceDirName   = "evidence"
)

// dataDir returns the configured persistence directory, or "" when
// persistence is off.
func dataDir() string {
	return os.Getenv(envDataDir)
}

// persistedLedger is the on-disk form of a ledger snapshot. Entry carries
// everything needed to verify the chain, so the snapshot needs nothing else.
type persistedLedger struct {
	Entries []ledger.Entry `json:"entries"`
}

// restoreStores rebuilds the report, evidence and guardian stores from disk,
// or returns fresh stores when there is nothing to restore or persistence
// is disabled.
//
// Failures to restore are loud (logged, and the app refuses to start) rather
// than silent: starting on empty stores after a corrupt snapshot would look
// exactly like "all reports vanished", which is the failure persistence
// exists to prevent. The one tolerated case is a missing file — a first
// boot has nothing to restore, and that is not corruption.
func restoreStores(dir string) (*report.Store, *evidence.Store, *guardian.Store, error) {
	if dir == "" {
		return report.NewStore(), evidence.NewStore(), guardian.NewStore(), nil
	}
	if err := persist.StoreDir(dir); err != nil {
		return nil, nil, nil, err
	}

	reports := report.NewStore()
	evid := evidence.NewStore()
	cases := guardian.NewStore()

	reportPath := filepath.Join(dir, reportLedgerFile)
	var reportSnap persistedLedger
	if err := persist.LoadJSON(reportPath, &reportSnap); err != nil {
		if !persist.Missing(err) {
			return nil, nil, nil, err
		}
	} else {
		l, err := ledger.NewFromEntries(reportSnap.Entries)
		if err != nil {
			return nil, nil, nil, err
		}
		reports, err = report.NewStoreFromLedger(l)
		if err != nil {
			return nil, nil, nil, err
		}
		log.Printf("persistence: restored %d report ledger entries", len(reportSnap.Entries))
	}

	guardianPath := filepath.Join(dir, guardianLedgerFle)
	var guardianSnap persistedLedger
	if err := persist.LoadJSON(guardianPath, &guardianSnap); err != nil {
		if !persist.Missing(err) {
			return nil, nil, nil, err
		}
	} else {
		l, err := ledger.NewFromEntries(guardianSnap.Entries)
		if err != nil {
			return nil, nil, nil, err
		}
		cases, err = guardian.NewStoreFromLedger(l)
		if err != nil {
			return nil, nil, nil, err
		}
		log.Printf("persistence: restored %d guardian ledger entries", len(guardianSnap.Entries))
	}

	evDir := filepath.Join(dir, evidenceDirName)
	if err := restoreEvidence(evid, evDir); err != nil {
		return nil, nil, nil, err
	}

	return reports, evid, cases, nil
}

// restoreEvidence loads each evidence file by its hash, verifying the bytes
// hash to the filename before accepting them. Any mismatch fails the boot:
// partial evidence would leave a report pointing at a file that silently
// isn't there.
func restoreEvidence(evid *evidence.Store, dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	snap := evidence.Snapshot{Files: make([]evidence.FileRecord, 0, len(entries))}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := persist.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		snap.Files = append(snap.Files, evidence.FileRecord{
			SHA256: e.Name(),
			Bytes:  raw,
		})
	}
	if len(snap.Files) == 0 {
		return nil
	}
	return evid.Restore(snap)
}

// saveEvidence writes one file per content hash. It is called before the
// store mutates, so the file on disk and the store in memory never
// disagree about what exists.
func saveEvidence(dir string, snap evidence.Snapshot) error {
	evDir := filepath.Join(dir, evidenceDirName)
	if err := persist.StoreDir(evDir); err != nil {
		return err
	}
	for _, f := range snap.Files {
		p := filepath.Join(evDir, f.SHA256)
		if _, err := os.Stat(p); err == nil {
			continue // content-addressed: already on disk, byte-identical by construction
		}
		if err := persist.SaveFile(p, f.Bytes); err != nil {
			return err
		}
	}
	return nil
}

// snapshotAll writes every durable state to disk. Called after every
// mutating request completes; atomic per file, so a crash mid-snapshot
// leaves the previous one intact and loadable.
func snapshotAll(dir string, reports *report.Store, evid *evidence.Store, cases *guardian.Store) error {
	if dir == "" {
		return nil
	}
	if err := persist.SaveJSON(filepath.Join(dir, reportLedgerFile), persistedLedger{Entries: reports.Ledger().Entries()}); err != nil {
		return err
	}
	if err := persist.SaveJSON(filepath.Join(dir, guardianLedgerFle), persistedLedger{Entries: cases.Ledger().Entries()}); err != nil {
		return err
	}
	return saveEvidence(dir, evid.Snapshot())
}
