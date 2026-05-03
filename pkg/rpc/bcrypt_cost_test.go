package rpc

import "golang.org/x/crypto/bcrypt"

// Drop bcrypt cost to MinCost for the entire test binary. Production hashing
// uses cost=14 (~800ms/hash); MinCost=4 (~3ms) keeps `make test` fast.
// Compares (CompareHashAndPassword) read the cost from the hash itself, so
// hardcoded production-cost hashes (e.g. seeded admins in init.sql / fixtures)
// keep verifying correctly regardless of this knob.
//
//nolint:gochecknoinits // test-only override of the package cost knob
func init() { bcryptCost = bcrypt.MinCost }
