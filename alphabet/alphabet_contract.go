package alphabet

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/neo"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
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
	if isUpdate {
		ctx := storage.GetContext()
		storage.Delete(ctx, "ballots")
		storage.Put(ctx, notaryDisabledKey, false)

		proxyContract := data.([]interface{})[0] // better to hardcode it
		storage.Put(ctx, proxyKey, proxyContract)

		return
	}

	args := data.(struct {
		notaryDisabled bool
		addrNetmap     interop.Hash160
		addrProxy      interop.Hash160
		name           string
		index          int
		total          int
	})

	ctx := storage.GetContext()

	if len(args.addrNetmap) != interop.Hash160Len || !args.notaryDisabled && len(args.addrProxy) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapKey, args.addrNetmap)
	storage.Put(ctx, proxyKey, args.addrProxy)
	storage.Put(ctx, nameKey, args.name)
	storage.Put(ctx, indexKey, args.index)
	storage.Put(ctx, totalKey, args.total)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, args.notaryDisabled)
	if args.notaryDisabled {
		common.InitVote(ctx)
		runtime.Log(args.name + " notary disabled")
	}

	runtime.Log(args.name + " contract initialized")
}

// Update method updates contract source code and manifest. Can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("alphabet contract updated")
}

// GAS returns amount of side chain GAS stored in contract account.
func Gas() int {
	return gas.BalanceOf(runtime.GetExecutingScriptHash())
}

// NEO returns amount of side chain NEO stored in contract account.
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

func checkPermission(ir []common.IRNode) bool {
	ctx := storage.GetReadOnlyContext()
	index := index(ctx) // read from contract memory

	if len(ir) <= index {
		return false
	}

	node := ir[index]
	return runtime.CheckWitness(node.PublicKey)
}

// Emit method produces side chain GAS and distributes it among Inner Ring nodes
// and proxy contract. Can be invoked only by Alphabet node of the Inner Ring.
//
// To produce GAS, alphabet contract transfers all available NEO from contract
// account to itself. If notary enabled, then 50% of the GAS in the contract account
// transferred to proxy contract. 43.75% of the GAS are equally distributed
// among all Inner Ring nodes. Remaining 6.25% of the GAS stays in the contract.
//
// If notary disabled, then 87.5% of the GAS are equally distributed among all
// Inner Ring nodes. Remaining 12.5% of the GAS stays in the contract.
func Emit() {
	ctx := storage.GetReadOnlyContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	alphabet := common.AlphabetNodes()
	if !checkPermission(alphabet) {
		panic("invalid invoker")
	}

	contractHash := runtime.GetExecutingScriptHash()

	if !neo.Transfer(contractHash, contractHash, neo.BalanceOf(contractHash), nil) {
		panic("failed to transfer funds, aborting")
	}

	gasBalance := gas.BalanceOf(contractHash)

	if !notaryDisabled {
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
	}

	var innerRing []common.IRNode

	if notaryDisabled {
		netmapContract := storage.Get(ctx, netmapKey).(interop.Hash160)
		innerRing = common.InnerRingNodesFromNetmap(netmapContract)
	} else {
		innerRing = common.InnerRingNodes()
	}

	gasPerNode := gasBalance * 7 / 8 / len(innerRing)

	if gasPerNode != 0 {
		for _, node := range innerRing {
			address := contract.CreateStandardAccount(node.PublicKey)
			if !gas.Transfer(contractHash, address, gasPerNode, nil) {
				runtime.Log("could not transfer GAS to one of IR node")
			}
		}

		runtime.Log("utility token has been emitted to inner ring nodes")
	}
}

// Vote method votes for side chain committee. Requires multisignature from
// Alphabet nodes of the Inner Ring.
//
// This method is used when governance changes list of Alphabet nodes of the
// Inner Ring. Alphabet nodes share keys with side chain validators, therefore
// it is required to change them as well. To do that NEO holders, which are
// alphabet contracts, should vote for new committee.
func Vote(epoch int, candidates []interop.PublicKey) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)
	index := index(ctx)
	name := name(ctx)

	var ( // for invocation collection without notary
		alphabet []common.IRNode
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("invalid invoker")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	curEpoch := currentEpoch(ctx)
	if epoch != curEpoch {
		panic("invalid epoch")
	}

	candidate := candidates[index%len(candidates)]
	address := runtime.GetExecutingScriptHash()

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := voteID(epoch, candidates)

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	ok := neo.Vote(address, candidate)
	if ok {
		runtime.Log(name + ": successfully voted for validator")
	} else {
		runtime.Log(name + ": vote has been failed")
	}

	return
}

func voteID(epoch interface{}, args []interop.PublicKey) []byte {
	var (
		result     []byte
		epochBytes = epoch.([]byte)
	)

	result = append(result, epochBytes...)

	for i := range args {
		result = append(result, args[i]...)
	}

	return crypto.Sha256(result)
}

// Name returns Glagolitic name of the contract.
func Name() string {
	ctx := storage.GetReadOnlyContext()
	return name(ctx)
}

// Version returns version of the contract.
func Version() int {
	return common.Version
}
