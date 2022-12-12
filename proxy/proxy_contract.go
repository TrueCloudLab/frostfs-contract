package proxy

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, []byte(gas.Hash)) {
		common.AbortWithMessage("proxy contract accepts GAS only")
	}
}

func _deploy(data interface{}, isUpdate bool) {
	if isUpdate {
		args := data.([]interface{})
		common.CheckVersion(args[len(args)-1].(int))
		return
	}

	runtime.Log("proxy contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("proxy contract updated")
}

// Verify method returns true if transaction contains valid multisignature of
// Alphabet nodes of the Inner Ring.
func Verify() bool {
	alphabet := neo.GetCommittee()
	sig := common.Multiaddress(alphabet, false)

	if !runtime.CheckWitness(sig) {
		sig = common.Multiaddress(alphabet, true)
		return runtime.CheckWitness(sig)
	}

	return true
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}
