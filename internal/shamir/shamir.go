// Package shamir implements Shamir's Secret Sharing over GF(256).
//
// A secret is split into n shares such that any threshold (k) of them
// reconstruct it exactly, while any k-1 shares reveal nothing about it. This
// is the primitive that lets Mlinzi's release key be split among guardians
// with no single party — including whoever operates Mlinzi — able to
// reconstruct it alone.
//
// This package is deliberately domain-agnostic: it knows nothing about
// guardians, cases, or release keys. That separation is what makes it
// possible to test the cryptography on its own terms, independent of the
// dead-man's-switch logic built on top of it in internal/guardian.
package shamir

import (
	"crypto/rand"
	"errors"
	"fmt"
)

var (
	ErrThresholdTooLow  = errors.New("shamir: threshold must be at least 2")
	ErrTooManyShares    = errors.New("shamir: cannot generate more than 255 shares")
	ErrThresholdExceeds = errors.New("shamir: threshold cannot exceed number of shares")
	ErrEmptySecret      = errors.New("shamir: secret must not be empty")
	ErrNotEnoughShares  = errors.New("shamir: need at least 2 shares to reconstruct")
	ErrShareLenMismatch = errors.New("shamir: shares are not all the same length")
	ErrDuplicateShareX  = errors.New("shamir: two shares have the same x-coordinate")
)

// Split divides secret into numShares shares, any threshold of which
// reconstruct it exactly. Each share is len(secret)+1 bytes: the y-values
// for each byte of the secret, followed by a single non-zero x-coordinate
// byte identifying the share. Shares are opaque; store and distribute them
// separately, never together.
func Split(secret []byte, numShares, threshold int) ([][]byte, error) {
	if len(secret) == 0 {
		return nil, ErrEmptySecret
	}
	if threshold < 2 {
		return nil, ErrThresholdTooLow
	}
	if numShares > 255 {
		return nil, ErrTooManyShares
	}
	if threshold > numShares {
		return nil, ErrThresholdExceeds
	}

	// One random polynomial of degree threshold-1 per byte of the secret,
	// with that byte as the constant term. Evaluating all polynomials at a
	// given x gives one share.
	polynomials := make([][]byte, len(secret))
	for i, b := range secret {
		poly := make([]byte, threshold)
		poly[0] = b
		if _, err := rand.Read(poly[1:]); err != nil {
			return nil, fmt.Errorf("shamir: generate polynomial: %w", err)
		}
		polynomials[i] = poly
	}

	shares := make([][]byte, numShares)
	for s := 0; s < numShares; s++ {
		x := byte(s + 1) // x-coordinates are 1..numShares; 0 would leak the secret directly
		share := make([]byte, len(secret)+1)
		for i, poly := range polynomials {
			share[i] = evalPolynomial(poly, x)
		}
		share[len(secret)] = x
		shares[s] = share
	}
	return shares, nil
}

// Combine reconstructs the secret from shares via Lagrange interpolation.
// Exactly len(shares) shares are used as the threshold — callers must supply
// precisely the number of shares that were the reconstruction threshold at
// split time. Supplying fewer than the true threshold succeeds without error
// and returns a wrong secret, because the scheme mathematically reveals
// nothing below threshold; there is no way to detect this from the shares
// alone, which is the entire point of the guarantee.
func Combine(shares [][]byte) ([]byte, error) {
	if len(shares) < 2 {
		return nil, ErrNotEnoughShares
	}
	shareLen := len(shares[0])
	if shareLen < 2 {
		return nil, ErrShareLenMismatch
	}
	xs := make([]byte, len(shares))
	for i, s := range shares {
		if len(s) != shareLen {
			return nil, ErrShareLenMismatch
		}
		xs[i] = s[shareLen-1]
		for j := 0; j < i; j++ {
			if xs[j] == xs[i] {
				return nil, ErrDuplicateShareX
			}
		}
	}

	secretLen := shareLen - 1
	secret := make([]byte, secretLen)
	for byteIdx := 0; byteIdx < secretLen; byteIdx++ {
		ys := make([]byte, len(shares))
		for i, s := range shares {
			ys[i] = s[byteIdx]
		}
		secret[byteIdx] = lagrangeInterpolateAtZero(xs, ys)
	}
	return secret, nil
}

// --- GF(256) arithmetic, using the AES/Rijndael reduction polynomial ---

// gfAdd is addition (and its own inverse, subtraction) in GF(256): XOR.
func gfAdd(a, b byte) byte { return a ^ b }

// gfMul multiplies two elements of GF(256) using peasant multiplication with
// the AES reduction polynomial (x^8 + x^4 + x^3 + x + 1, 0x11B).
func gfMul(a, b byte) byte {
	var p byte
	for i := 0; i < 8; i++ {
		if b&1 != 0 {
			p ^= a
		}
		hiBitSet := a&0x80 != 0
		a <<= 1
		if hiBitSet {
			a ^= 0x1B
		}
		b >>= 1
	}
	return p
}

// gfInverse returns the multiplicative inverse of a nonzero element of
// GF(256), found by brute-force search. GF(256) has only 255 nonzero
// elements, so this is fast and — unlike a lookup table — trivially auditable
// as correct by inspection.
func gfInverse(a byte) byte {
	if a == 0 {
		panic("shamir: gfInverse(0) is undefined")
	}
	for x := 1; x < 256; x++ {
		if gfMul(a, byte(x)) == 1 {
			return byte(x)
		}
	}
	panic("shamir: unreachable — every nonzero GF(256) element has an inverse")
}

// evalPolynomial evaluates poly (coefficients low-to-high degree) at x using
// Horner's method entirely in GF(256).
func evalPolynomial(poly []byte, x byte) byte {
	var result byte
	for i := len(poly) - 1; i >= 0; i-- {
		result = gfAdd(gfMul(result, x), poly[i])
	}
	return result
}

// lagrangeInterpolateAtZero evaluates the unique degree-(n-1) polynomial
// through the points (xs[i], ys[i]) at x=0 — which recovers the constant
// term, i.e. the original secret byte, without ever building the polynomial
// explicitly.
func lagrangeInterpolateAtZero(xs, ys []byte) byte {
	var result byte
	for i := range xs {
		term := ys[i]
		for j := range xs {
			if i == j {
				continue
			}
			// basis_i(0) = product over j!=i of (0 - x_j) / (x_i - x_j)
			// In GF(256), subtraction is XOR, so (0 - x_j) == x_j.
			num := xs[j]
			den := gfAdd(xs[i], xs[j])
			term = gfMul(term, gfMul(num, gfInverse(den)))
		}
		result = gfAdd(result, term)
	}
	return result
}
