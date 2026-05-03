package vt

import "golang.org/x/crypto/bcrypt"

// See pkg/rpc/bcrypt_cost_test.go — same trick: drop the bcrypt work factor
// to MinCost for the test binary so password-hashing fixtures don't dominate
// the suite runtime.
//
//nolint:gochecknoinits // test-only override of the package cost knob
func init() { bcryptCost = bcrypt.MinCost }
