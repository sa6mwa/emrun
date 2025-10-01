package emrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestPolicyAllowsKnownDigestAndDeniesUnknown(t *testing.T) {
	payload := []byte("#!/bin/sh\necho policy\n")
	sum := sha256.Sum256(payload)
	hexDigest := hex.EncodeToString(sum[:])

	ctx := WithPolicy(context.Background(), DENY)
	ctx = WithRule(ctx, ALLOW, hexDigest)

	if err := CheckPolicy(ctx, sum, hexDigest); err != nil {
		t.Fatalf("expected digest to be allowed, got %v", err)
	}

	other := sha256.Sum256([]byte("different payload"))
	otherHex := hex.EncodeToString(other[:])
	err := CheckPolicy(ctx, other, otherHex)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("expected ErrDenied for unknown digest, got %v", err)
	}
	var policyErr *PolicyError
	if !errors.As(err, &policyErr) {
		t.Fatalf("expected PolicyError, got %T", err)
	}
	if policyErr.Digest != otherHex {
		t.Fatalf("unexpected digest on PolicyError: got %q want %q", policyErr.Digest, otherHex)
	}
}

func TestWithRuleParsesChecksumFile(t *testing.T) {
	payload := []byte("file contents")
	sum := sha256.Sum256(payload)
	hexDigest := hex.EncodeToString(sum[:])
	file := hexDigest + "  ./bin/tool\n"

	ctx := WithPolicy(context.Background(), DENY)
	ctx = WithRule(ctx, ALLOW, []byte(file))
	if err := CheckPolicy(ctx, sum, hexDigest); err != nil {
		t.Fatalf("expected checksum from file to be allowed, got %v", err)
	}
}

func TestWithRuleRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	if _, err := WithRuleCatchError(ctx, ALLOW, "invalid"); err == nil {
		t.Fatalf("expected error for invalid checksum input")
	}
}

func TestWithRuleFromReader(t *testing.T) {
	payload := []byte("reader input")
	sum := sha256.Sum256(payload)
	hexDigest := hex.EncodeToString(sum[:])

	reader := strings.NewReader(hexDigest + " *file\n")

	ctx := WithPolicy(context.Background(), DENY)
	ctx = WithRule(ctx, ALLOW, reader)
	if err := CheckPolicy(ctx, sum, hexDigest); err != nil {
		t.Fatalf("expected reader checksum to be allowed, got %v", err)
	}
}
func TestWithRulePanicsOnInvalidInput(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	_ = WithRule(context.Background(), ALLOW, "invalid")
}
