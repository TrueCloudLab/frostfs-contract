package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/roles"
)

type IRNode struct {
	PublicKey interop.PublicKey
}

// InnerRingNodes return a list of inner ring nodes from state validator role
// in the sidechain.
func InnerRingNodes() []interop.PublicKey {
	blockHeight := ledger.CurrentIndex()
	return roles.GetDesignatedByRole(roles.NeoFSAlphabet, uint32(blockHeight+1))
}

// AlphabetNodes returns a list of alphabet nodes from committee in the sidechain.
func AlphabetNodes() []interop.PublicKey {
	return neo.GetCommittee()
}

// AlphabetAddress returns multi address of alphabet public keys.
func AlphabetAddress() []byte {
	alphabet := neo.GetCommittee()
	return Multiaddress(alphabet, false)
}

// CommitteeAddress returns multi address of committee.
func CommitteeAddress() []byte {
	committee := neo.GetCommittee()
	return Multiaddress(committee, true)
}

// Multiaddress returns default multisignature account address for N keys.
// If committee set to true, it is `M = N/2+1` committee account.
func Multiaddress(n []interop.PublicKey, committee bool) []byte {
	threshold := len(n)*2/3 + 1
	if committee {
		threshold = len(n)/2 + 1
	}

	return contract.CreateMultisigAccount(threshold, n)
}
