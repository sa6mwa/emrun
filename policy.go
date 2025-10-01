package emrun

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

type Verdict int

const (
	ALLOW Verdict = iota
	DENY
)

type Digest any

var ErrDenied = errors.New("emrun: execution denied by policy")

type PolicyError struct {
	Verdict Verdict
	Digest  string
}

func (e *PolicyError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("emrun: %s digest %s", e.Verdict.String(), e.Digest)
}

func (e *PolicyError) Is(target error) bool {
	return target == ErrDenied
}

func (v Verdict) String() string {
	switch v {
	case ALLOW:
		return "allow"
	case DENY:
		return "deny"
	default:
		return fmt.Sprintf("verdict(%d)", v)
	}
}

type policyKey struct{}

type executionPolicy struct {
	defaultVerdict Verdict
	allow          map[[32]byte]struct{}
	deny           map[[32]byte]struct{}
}

func newExecutionPolicy() *executionPolicy {
	return &executionPolicy{
		defaultVerdict: ALLOW,
		allow:          make(map[[32]byte]struct{}),
		deny:           make(map[[32]byte]struct{}),
	}
}

func (p *executionPolicy) clone() *executionPolicy {
	if p == nil {
		return newExecutionPolicy()
	}
	clone := &executionPolicy{
		defaultVerdict: p.defaultVerdict,
	}
	if len(p.allow) > 0 {
		clone.allow = make(map[[32]byte]struct{}, len(p.allow))
		for k := range p.allow {
			clone.allow[k] = struct{}{}
		}
	} else {
		clone.allow = make(map[[32]byte]struct{})
	}
	if len(p.deny) > 0 {
		clone.deny = make(map[[32]byte]struct{}, len(p.deny))
		for k := range p.deny {
			clone.deny[k] = struct{}{}
		}
	} else {
		clone.deny = make(map[[32]byte]struct{})
	}
	return clone
}

func policyFromContext(ctx context.Context) *executionPolicy {
	if ctx == nil {
		return nil
	}
	if existing, ok := ctx.Value(policyKey{}).(*executionPolicy); ok {
		return existing
	}
	return nil
}

// WithPolicy returns a derived context that sets the default verdict consulted
// when no explicit allow/deny rule matches a payload digest.
//
//	ctx := emrun.WithPolicy(context.Background(), emrun.DENY)
//	ctx = emrun.WithRule(ctx, emrun.ALLOW, sha256FileBytes)
//	_ = emrun.CheckPolicy(ctx, digest, hexDigest)
func WithPolicy(ctx context.Context, verdict Verdict) context.Context {
	policy := policyFromContext(ctx)
	if policy == nil {
		policy = newExecutionPolicy()
	} else {
		policy = policy.clone()
	}
	policy.defaultVerdict = verdict
	return context.WithValue(ctx, policyKey{}, policy)
}

// WithRule returns a derived context containing explicit allow/deny entries for
// SHA-256 digests. Each argument may be a raw digest type (string, []byte,
// [32]byte) or sha256sum-formatted content; filenames are ignored. WithRule must
// succeed - invalid input causes a panic.
//
//	ctx := emrun.WithPolicy(ctx, emrun.DENY)
//	ctx = emrun.WithRule(ctx, emrun.ALLOW, []byte("<digest>  tool"))
//	ctx = emrun.WithRule(ctx, emrun.DENY, "deadbeef...deadbeef")
//	_ = emrun.CheckPolicy(ctx, digest, hexDigest)
func WithRule(ctx context.Context, rule Verdict, sha256Digests ...Digest) context.Context {
	ctx, err := WithRuleCatchError(ctx, rule, sha256Digests...)
	if err != nil {
		panic(err)
	}
	return ctx
}

// WithRuleCatchError mirrors WithRule but returns an error instead of panicking
// when digest parsing fails or an unsupported verdict is supplied.
func WithRuleCatchError(ctx context.Context, rule Verdict, sha256Digests ...Digest) (context.Context, error) {
	if len(sha256Digests) == 0 {
		return ctx, nil
	}
	policy := policyFromContext(ctx)
	if policy == nil {
		policy = newExecutionPolicy()
	} else {
		policy = policy.clone()
	}
	digests, err := collectDigests(sha256Digests...)
	if err != nil {
		return ctx, err
	}
	for _, digest := range digests {
		switch rule {
		case ALLOW:
			policy.allow[digest] = struct{}{}
			delete(policy.deny, digest)
		case DENY:
			policy.deny[digest] = struct{}{}
			delete(policy.allow, digest)
		default:
			return ctx, fmt.Errorf("unsupported verdict %d", rule)
		}
	}
	return context.WithValue(ctx, policyKey{}, policy), nil
}

func collectDigests(values ...Digest) ([][32]byte, error) {
	var result [][32]byte
	for _, v := range values {
		if v == nil {
			continue
		}
		switch chk := v.(type) {
		case [32]byte:
			result = append(result, chk)
		case *[32]byte:
			if chk != nil {
				result = append(result, *chk)
			}
		case []byte:
			digests, err := digestsFromBytes(chk)
			if err != nil {
				return nil, err
			}
			result = append(result, digests...)
		case [][]byte:
			for _, entry := range chk {
				digests, err := digestsFromBytes(entry)
				if err != nil {
					return nil, err
				}
				result = append(result, digests...)
			}
		case string:
			digests, err := digestsFromString(chk)
			if err != nil {
				return nil, err
			}
			result = append(result, digests...)
		case fmt.Stringer:
			digests, err := digestsFromString(chk.String())
			if err != nil {
				return nil, err
			}
			result = append(result, digests...)
		case io.Reader:
			data, err := io.ReadAll(chk)
			if err != nil {
				return nil, err
			}
			digests, err := digestsFromBytes(data)
			if err != nil {
				return nil, err
			}
			result = append(result, digests...)
		default:
			return nil, fmt.Errorf("unsupported checksum type %T", v)
		}
	}
	return result, nil
}

func digestsFromBytes(data []byte) ([][32]byte, error) {
	switch {
	case len(data) == 0:
		return nil, nil
	case len(data) == 32:
		var digest [32]byte
		copy(digest[:], data)
		return [][32]byte{digest}, nil
	case len(data) == 64 && isHexString(string(data)):
		return decodeSingleDigest(string(data))
	default:
		return digestsFromString(string(data))
	}
}

func digestsFromString(value string) ([][32]byte, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if !strings.ContainsAny(trimmed, " \t\n\r") && len(trimmed) == 64 && isHexString(trimmed) {
		return decodeSingleDigest(trimmed)
	}
	scanner := bufio.NewScanner(strings.NewReader(value))
	var digests [][32]byte
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) < 64 {
			return nil, fmt.Errorf("line shorter than sha256 digest: %q", line)
		}
		candidate := line[:64]
		if !isHexString(candidate) {
			return nil, fmt.Errorf("invalid sha256 digest: %q", candidate)
		}
		digestBytes, err := hex.DecodeString(candidate)
		if err != nil {
			return nil, fmt.Errorf("decode sha256 digest: %w", err)
		}
		var digest [32]byte
		copy(digest[:], digestBytes)
		digests = append(digests, digest)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return digests, nil
}

func decodeSingleDigest(hexDigest string) ([][32]byte, error) {
	bytes, err := hex.DecodeString(hexDigest)
	if err != nil {
		return nil, fmt.Errorf("decode sha256 digest: %w", err)
	}
	if len(bytes) != 32 {
		return nil, fmt.Errorf("unexpected digest length %d", len(bytes))
	}
	var digest [32]byte
	copy(digest[:], bytes)
	return [][32]byte{digest}, nil
}

func isHexString(value string) bool {
	if len(value) == 0 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

// CheckPolicy inspects the context policy and returns ErrDenied if the digest
// violates the configured rules.
//
//	ctx := emrun.WithRule(context.Background(), emrun.DENY, "deadbeef...")
//	digest := sha256.Sum256(payload)
//	if err := emrun.CheckPolicy(ctx, digest, hex.EncodeToString(digest[:])); err != nil {
//		return err
//	}
func CheckPolicy(ctx context.Context, digest [32]byte, hexDigest string) error {
	return enforcePolicy(ctx, digest, hexDigest)
}

func enforcePolicy(ctx context.Context, digest [32]byte, hexDigest string) error {
	policy := policyFromContext(ctx)
	if policy == nil {
		return nil
	}
	switch policy.evaluate(digest) {
	case ALLOW:
		return nil
	case DENY:
		return &PolicyError{Verdict: DENY, Digest: hexDigest}
	default:
		return nil
	}
}

func (p *executionPolicy) evaluate(digest [32]byte) Verdict {
	if p == nil {
		return ALLOW
	}
	if _, denied := p.deny[digest]; denied {
		return DENY
	}
	if _, allowed := p.allow[digest]; allowed {
		return ALLOW
	}
	return p.defaultVerdict
}
