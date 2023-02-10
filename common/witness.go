package common

import "github.com/nspcc-dev/neo-go/pkg/interop/runtime"

var (
	// ErrAlphabetWitnessFailed appears when the method must be
	// called by the Alphabet but was not.
	ErrAlphabetWitnessFailed = "alphabet witness check failed"
	// ErrOwnerWitnessFailed appears when the method must be called
	// by an owner of some assets but was not.
	ErrOwnerWitnessFailed = "owner witness check failed"
	// ErrWitnessFailed appears when the method must be called
	// using certain public key but was not.
	ErrWitnessFailed = "witness check failed"
)

// CheckAlphabetWitness checks witness of the passed caller.
// It panics with ErrAlphabetWitnessFailed message on fail.
func CheckAlphabetWitness() {
	checkWitnessWithPanic(AlphabetAddress(), ErrAlphabetWitnessFailed)
}

// CheckOwnerWitness checks witness of the passed caller.
// It panics with ErrOwnerWitnessFailed message on fail.
func CheckOwnerWitness(caller []byte) {
	checkWitnessWithPanic(caller, ErrOwnerWitnessFailed)
}

// CheckWitness checks witness of the passed caller.
// It panics with ErrWitnessFailed message on fail.
func CheckWitness(caller []byte) {
	checkWitnessWithPanic(caller, ErrWitnessFailed)
}

func checkWitnessWithPanic(caller []byte, panicMsg string) {
	if !runtime.CheckWitness(caller) {
		panic(panicMsg)
	}
}
