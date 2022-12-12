package processing

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/roles"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

const (
	neofsContractKey = "neofsScriptHash"

	multiaddrMethod = "alphabetAddress"
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, []byte(gas.Hash)) {
		common.AbortWithMessage("processing contract accepts GAS only")
	}
}

func _deploy(data interface{}, isUpdate bool) {
	if isUpdate {
		args := data.([]interface{})
		common.CheckVersion(args[len(args)-1].(int))
		return
	}

	args := data.(struct {
		addrNeoFS interop.Hash160
	})

	ctx := storage.GetContext()

	if len(args.addrNeoFS) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, neofsContractKey, args.addrNeoFS)

	runtime.Log("processing contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by the sidechain committee.
func Update(script []byte, manifest []byte, data interface{}) {
	blockHeight := ledger.CurrentIndex()
	alphabetKeys := roles.GetDesignatedByRole(roles.NeoFSAlphabet, uint32(blockHeight+1))
	alphabetCommittee := common.Multiaddress(alphabetKeys, true)

	if !runtime.CheckWitness(alphabetCommittee) {
		panic("only side chain committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("processing contract updated")
}

// Verify method returns true if transaction contains valid multisignature of
// Alphabet nodes of the Inner Ring.
func Verify() bool {
	ctx := storage.GetContext()
	neofsContractAddr := storage.Get(ctx, neofsContractKey).(interop.Hash160)
	multiaddr := contract.Call(neofsContractAddr, multiaddrMethod, contract.ReadOnly).(interop.Hash160)

	return runtime.CheckWitness(multiaddr)
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}
