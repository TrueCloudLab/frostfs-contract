package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
)

const (
	PanicMsgForNotaryDisabledEnv = "contract not applicable for notary-disabled environment"
)

// BytesEqual compares two slices of bytes by wrapping them into strings,
// which is necessary with new util.Equals interop behaviour, see neo-go#1176.
func BytesEqual(a []byte, b []byte) bool {
	return util.Equals(string(a), string(b))
}
