package alphabet

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

const (
	netmapKey = "netmapScriptHash"
	proxyKey  = "proxyScriptHash"

	indexKey = "index"
	totalKey = "threshold"
	nameKey  = "name"

	notaryDisabledKey = "notary"
)

// OnNEP17Payment is a callback for NEP-17 compatible native GAS and NEO
// contracts.
func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, []byte(gas.Hash)) && !common.BytesEqual(caller, []byte(neo.Hash)) {
		common.AbortWithMessage("alphabet contract accepts GAS and NEO only")
	}
}

func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()

	if isUpdate {
		args := data.([]interface{})
		common.CheckVersion(args[len(args)-1].(int))
		//TODO(@acid-ant): #9 remove notaryDisabled from args in future version
		storage.Delete(ctx, notaryDisabledKey)
		return
	}

	args := data.(struct {
		//TODO(@acid-ant): #9 remove notaryDisabled in future version
		notaryDisabled bool
		addrNetmap     interop.Hash160
		addrProxy      interop.Hash160
		name           string
		index          int
		total          int
	})

	if args.notaryDisabled {
		panic(common.PanicMsgForNotaryDisabledEnv)
	}

	if len(args.addrNetmap) != interop.Hash160Len || len(args.addrProxy) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapKey, args.addrNetmap)
	storage.Put(ctx, proxyKey, args.addrProxy)
	storage.Put(ctx, nameKey, args.name)
	storage.Put(ctx, indexKey, args.index)
	storage.Put(ctx, totalKey, args.total)

	runtime.Log(args.name + " contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("alphabet contract updated")
}

// GAS returns the amount of the sidechain GAS stored in the contract account.
func Gas() int {
	return gas.BalanceOf(runtime.GetExecutingScriptHash())
}

// NEO returns the amount of sidechain NEO stored in the contract account.
func Neo() int {
	return neo.BalanceOf(runtime.GetExecutingScriptHash())
}

func currentEpoch(ctx storage.Context) int {
	netmapContractAddr := storage.Get(ctx, netmapKey).(interop.Hash160)
	return contract.Call(netmapContractAddr, "epoch", contract.ReadOnly).(int)
}

func name(ctx storage.Context) string {
	return storage.Get(ctx, nameKey).(string)
}

func index(ctx storage.Context) int {
	return storage.Get(ctx, indexKey).(int)
}

func checkPermission(ir []interop.PublicKey) bool {
	ctx := storage.GetReadOnlyContext()
	index := index(ctx) // read from contract memory

	if len(ir) <= index {
		return false
	}

	node := ir[index]
	return runtime.CheckWitness(node)
}

// Emit method produces sidechain GAS and distributes it among Inner Ring nodes
// and proxy contract. It can be invoked only by an Alphabet node of the Inner Ring.
//
// To produce GAS, an alphabet contract transfers all available NEO from the contract
// account to itself. 50% of the GAS in the contract account
// are transferred to proxy contract. 43.75% of the GAS are equally distributed
// among all Inner Ring nodes. Remaining 6.25% of the GAS stay in the contract.
func Emit() {
	ctx := storage.GetReadOnlyContext()

	alphabet := common.AlphabetNodes()
	if !checkPermission(alphabet) {
		panic("invalid invoker")
	}

	contractHash := runtime.GetExecutingScriptHash()

	if !neo.Transfer(contractHash, contractHash, neo.BalanceOf(contractHash), nil) {
		panic("failed to transfer funds, aborting")
	}

	gasBalance := gas.BalanceOf(contractHash)

	proxyAddr := storage.Get(ctx, proxyKey).(interop.Hash160)

	proxyGas := gasBalance / 2
	if proxyGas == 0 {
		panic("no gas to emit")
	}

	if !gas.Transfer(contractHash, proxyAddr, proxyGas, nil) {
		runtime.Log("could not transfer GAS to proxy contract")
	}

	gasBalance -= proxyGas

	runtime.Log("utility token has been emitted to proxy contract")

	innerRing := common.InnerRingNodes()

	gasPerNode := gasBalance * 7 / 8 / len(innerRing)

	if gasPerNode != 0 {
		for _, node := range innerRing {
			address := contract.CreateStandardAccount(node)
			if !gas.Transfer(contractHash, address, gasPerNode, nil) {
				runtime.Log("could not transfer GAS to one of IR node")
			}
		}

		runtime.Log("utility token has been emitted to inner ring nodes")
	}
}

// Vote method votes for the sidechain committee. It requires multisignature from
// Alphabet nodes of the Inner Ring.
//
// This method is used when governance changes the list of Alphabet nodes of the
// Inner Ring. Alphabet nodes share keys with sidechain validators, therefore
// it is required to change them as well. To do that, NEO holders (which are
// alphabet contracts) should vote for a new committee.
func Vote(epoch int, candidates []interop.PublicKey) {
	ctx := storage.GetContext()
	index := index(ctx)
	name := name(ctx)

	common.CheckAlphabetWitness()

	curEpoch := currentEpoch(ctx)
	if epoch != curEpoch {
		panic("invalid epoch")
	}

	candidate := candidates[index%len(candidates)]
	address := runtime.GetExecutingScriptHash()

	ok := neo.Vote(address, candidate)
	if ok {
		runtime.Log(name + ": successfully voted for validator")
	} else {
		runtime.Log(name + ": vote has been failed")
	}
}

// Name returns the Glagolitic name of the contract.
func Name() string {
	ctx := storage.GetReadOnlyContext()
	return name(ctx)
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}
