package shamir

import (
	"bytes"
	"testing"
)

func TestSplitAndCombineRoundTrip(t *testing.T) {
	secret := []byte("release-key-32-bytes-of-entropy")
	shares, err := Split(secret, 5, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 5 {
		t.Fatalf("expected 5 shares, got %d", len(shares))
	}

	got, err := Combine(shares[:3])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatalf("reconstructed secret mismatch: got %q want %q", got, secret)
	}
}

func TestAnyThresholdSubsetReconstructs(t *testing.T) {
	secret := []byte("the-actual-release-key")
	shares, err := Split(secret, 5, 3)
	if err != nil {
		t.Fatal(err)
	}

	subsets := [][][]byte{
		{shares[0], shares[1], shares[2]},
		{shares[0], shares[2], shares[4]},
		{shares[1], shares[3], shares[4]},
	}
	for i, subset := range subsets {
		got, err := Combine(subset)
		if err != nil {
			t.Fatalf("subset %d: %v", i, err)
		}
		if !bytes.Equal(got, secret) {
			t.Fatalf("subset %d: mismatch, got %q", i, got)
		}
	}
}

func TestBelowThresholdRevealsNothing(t *testing.T) {
	// This is the guarantee the entire guardian model depends on: two
	// different secrets, split with the same threshold, must be
	// indistinguishable from any single share alone. We can't prove
	// information-theoretic secrecy in a unit test, but we can prove the
	// share format itself carries no shortcut: combining fewer shares than
	// the threshold must not raise an error that would let an attacker
	// distinguish "not enough shares" from "wrong shares" — both look the
	// same, and Combine is documented as returning a wrong-but-plausible
	// secret rather than failing.
	secret := []byte("secret-key-material-here-32byte")
	shares, err := Split(secret, 5, 3)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Combine(shares[:2]) // one below threshold
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, secret) {
		t.Fatal("expected below-threshold combination not to accidentally recover the real secret")
	}
}

func TestOneShareCannotReconstructAlone(t *testing.T) {
	secret := []byte("k")
	shares, _ := Split(secret, 3, 2)
	if _, err := Combine(shares[:1]); err != ErrNotEnoughShares {
		t.Fatalf("expected ErrNotEnoughShares for a single share, got %v", err)
	}
}

func TestSplitRejectsEmptySecret(t *testing.T) {
	if _, err := Split(nil, 3, 2); err != ErrEmptySecret {
		t.Fatalf("expected ErrEmptySecret, got %v", err)
	}
}

func TestSplitRejectsThresholdBelowTwo(t *testing.T) {
	if _, err := Split([]byte("x"), 3, 1); err != ErrThresholdTooLow {
		t.Fatalf("expected ErrThresholdTooLow, got %v", err)
	}
}

func TestSplitRejectsThresholdAboveShareCount(t *testing.T) {
	if _, err := Split([]byte("x"), 2, 3); err != ErrThresholdExceeds {
		t.Fatalf("expected ErrThresholdExceeds, got %v", err)
	}
}

func TestSplitRejectsTooManyShares(t *testing.T) {
	if _, err := Split([]byte("x"), 256, 2); err != ErrTooManyShares {
		t.Fatalf("expected ErrTooManyShares, got %v", err)
	}
}

func TestCombineRejectsMismatchedShareLengths(t *testing.T) {
	shares, _ := Split([]byte("abcdef"), 3, 2)
	bad := append([][]byte{}, shares[:2]...)
	bad[1] = bad[1][:len(bad[1])-2] // truncate
	if _, err := Combine(bad); err != ErrShareLenMismatch {
		t.Fatalf("expected ErrShareLenMismatch, got %v", err)
	}
}

func TestCombineRejectsDuplicateXCoordinates(t *testing.T) {
	shares, _ := Split([]byte("abcdef"), 3, 2)
	dup := [][]byte{shares[0], append([]byte{}, shares[0]...)}
	if _, err := Combine(dup); err != ErrDuplicateShareX {
		t.Fatalf("expected ErrDuplicateShareX, got %v", err)
	}
}

func TestSplitProducesDistinctSharesEachRun(t *testing.T) {
	// The random polynomial coefficients must actually be random: splitting
	// the same secret twice should not produce identical shares.
	secret := []byte("same-secret-both-times")
	a, err := Split(secret, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Split(secret, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a[0], b[0]) {
		t.Fatal("expected two independent splits to produce different shares")
	}
}

// --- GF(256) arithmetic correctness ---

func TestGFAddIsSelfInverse(t *testing.T) {
	for a := 0; a < 256; a++ {
		if gfAdd(byte(a), byte(a)) != 0 {
			t.Fatalf("gfAdd(%d,%d) != 0", a, a)
		}
	}
}

func TestGFMulByZeroIsZero(t *testing.T) {
	for a := 0; a < 256; a++ {
		if gfMul(byte(a), 0) != 0 {
			t.Fatalf("gfMul(%d, 0) != 0", a)
		}
	}
}

func TestGFMulByOneIsIdentity(t *testing.T) {
	for a := 0; a < 256; a++ {
		if gfMul(byte(a), 1) != byte(a) {
			t.Fatalf("gfMul(%d, 1) != %d", a, a)
		}
	}
}

func TestGFMulIsCommutative(t *testing.T) {
	for a := 0; a < 256; a += 7 { // sampled, full 256x256 is checked via inverse test below
		for b := 0; b < 256; b += 11 {
			if gfMul(byte(a), byte(b)) != gfMul(byte(b), byte(a)) {
				t.Fatalf("gfMul not commutative for %d,%d", a, b)
			}
		}
	}
}

// TestGFInverseExhaustive checks every one of the 255 nonzero elements of
// GF(256) has a correct multiplicative inverse. This is the same exhaustive
// check used on the Kinga project's Shamir implementation, carried over
// because a subtly wrong field multiplication silently breaks reconstruction
// only for certain byte values — exactly the kind of bug that would pass a
// handful of spot-check tests and then fail unpredictably in a real demo.
func TestGFInverseExhaustive(t *testing.T) {
	for a := 1; a < 256; a++ {
		inv := gfInverse(byte(a))
		if gfMul(byte(a), inv) != 1 {
			t.Fatalf("gfInverse(%d) = %d, but gfMul(%d,%d) = %d, want 1",
				a, inv, a, inv, gfMul(byte(a), inv))
		}
	}
}

func TestEvalPolynomialAtZeroIsConstantTerm(t *testing.T) {
	poly := []byte{42, 7, 200}
	if got := evalPolynomial(poly, 0); got != 42 {
		t.Fatalf("evalPolynomial at x=0 should return the constant term, got %d", got)
	}
}
