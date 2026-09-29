package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

// CanonicalPlanJSON encodes a plan with sorted object keys, preserved array
// order, and no digest member. encoding/json sorts string map keys by contract.
func CanonicalPlanJSON(plan Plan) ([]byte, error) {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("encoding plan: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, fmt.Errorf("reading encoded plan: %w", err)
	}
	delete(object, "digest")
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing plan: %w", err)
	}
	return canonical, nil
}

// CanonicalPlanDigest returns the lowercase SHA-256 digest of the canonical
// plan representation, excluding Plan.Digest itself.
func CanonicalPlanDigest(plan Plan) (string, error) {
	canonical, err := CanonicalPlanJSON(plan)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// CanonicalJSON canonicalizes one JSON value. It rejects duplicate keys and
// trailing values before sorting object keys; array order remains significant.
func CanonicalJSON(data []byte) ([]byte, error) {
	if err := rejectDuplicateKeys(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("decoding JSON: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("canonicalizing JSON: trailing value")
		}
		return nil, fmt.Errorf("decoding trailing JSON: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding canonical JSON: %w", err)
	}
	return canonical, nil
}
