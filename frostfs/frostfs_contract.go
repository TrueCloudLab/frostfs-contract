package frostfs

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/gas"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/roles"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

type (
	record struct {
		key []byte
		val []byte
	}
)

const (
	// CandidateFeeConfigKey contains fee for a candidate registration.
	CandidateFeeConfigKey = "InnerRingCandidateFee"
	withdrawFeeConfigKey  = "WithdrawFee"

	alphabetKey       = "alphabet"
	candidatesKey     = "candidates"
	notaryDisabledKey = "notary"

	processingContractKey = "processingScriptHash"

	maxBalanceAmount    = 9000 // Max integer of Fixed12 in JSON bound (2**53-1)
	maxBalanceAmountGAS = int64(maxBalanceAmount) * 1_0000_0000

	// hardcoded value to ignore deposit notification in onReceive
	ignoreDepositNotification = "\x57\x0b"
)

var (
	configPrefix = []byte("config")
)

// _deploy sets up initial alphabet node keys.
func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()

	if isUpdate {
		args := data.([]interface{})
		common.CheckVersion(args[len(args)-1].(int))
		return
	}

	args := data.(struct {
		notaryDisabled bool
		addrProc       interop.Hash160
		keys           []interop.PublicKey
		config         [][]byte
	})

	if len(args.keys) == 0 {
		panic("at least one alphabet key must be provided")
	}

	if len(args.addrProc) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	for i := 0; i < len(args.keys); i++ {
		pub := args.keys[i]
		if len(pub) != interop.PublicKeyCompressedLen {
			panic("incorrect public key length")
		}
	}

	// initialize all storage slices
	common.SetSerialized(ctx, alphabetKey, args.keys)

	storage.Put(ctx, processingContractKey, args.addrProc)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, args.notaryDisabled)
	if args.notaryDisabled {
		common.InitVote(ctx)
		runtime.Log("frostfs contract notary disabled")
	}

	ln := len(args.config)
	if ln%2 != 0 {
		panic("bad configuration")
	}

	for i := 0; i < ln/2; i++ {
		key := args.config[i*2]
		val := args.config[i*2+1]

		setConfig(ctx, key, val)
	}

	runtime.Log("frostfs: contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by sidechain committee.
func Update(script []byte, manifest []byte, data interface{}) {
	blockHeight := ledger.CurrentIndex()
	alphabetKeys := roles.GetDesignatedByRole(roles.NeoFSAlphabet, uint32(blockHeight+1))
	alphabetCommittee := common.Multiaddress(alphabetKeys, true)

	common.CheckAlphabetWitness(alphabetCommittee)

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("frostfs contract updated")
}

// AlphabetList returns an array of alphabet node keys. It is used in sidechain notary
// disabled environment.
func AlphabetList() []common.IRNode {
	ctx := storage.GetReadOnlyContext()
	pubs := getAlphabetNodes(ctx)
	nodes := []common.IRNode{}
	for i := range pubs {
		nodes = append(nodes, common.IRNode{PublicKey: pubs[i]})
	}
	return nodes
}

// AlphabetAddress returns 2\3n+1 multisignature address of alphabet nodes.
// It is used in sidechain notary disabled environment.
func AlphabetAddress() interop.Hash160 {
	ctx := storage.GetReadOnlyContext()
	return multiaddress(getAlphabetNodes(ctx))
}

// InnerRingCandidates returns an array of structures that contain an Inner Ring
// candidate node key.
func InnerRingCandidates() []common.IRNode {
	ctx := storage.GetReadOnlyContext()
	nodes := []common.IRNode{}

	it := storage.Find(ctx, candidatesKey, storage.KeysOnly|storage.RemovePrefix)
	for iterator.Next(it) {
		pub := iterator.Value(it).([]byte)
		nodes = append(nodes, common.IRNode{PublicKey: pub})
	}
	return nodes
}

// InnerRingCandidateRemove removes a key from a list of Inner Ring candidates.
// It can be invoked by Alphabet nodes or the candidate itself.
//
// This method does not return fee back to the candidate.
func InnerRingCandidateRemove(key interop.PublicKey) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	keyOwner := runtime.CheckWitness(key)

	if !keyOwner {
		if notaryDisabled {
			alphabet = getAlphabetNodes(ctx)
			nodeKey = common.InnerRingInvoker(alphabet)
			if len(nodeKey) == 0 {
				panic("this method must be invoked by candidate or alphabet")
			}
		} else {
			multiaddr := AlphabetAddress()
			if !runtime.CheckWitness(multiaddr) {
				panic("this method must be invoked by candidate or alphabet")
			}
		}
	}

	if notaryDisabled && !keyOwner {
		threshold := len(alphabet)*2/3 + 1
		id := append(key, []byte("delete")...)
		hashID := crypto.Sha256(id)

		n := common.Vote(ctx, hashID, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, hashID)
	}

	prefix := []byte(candidatesKey)
	stKey := append(prefix, key...)
	if storage.Get(ctx, stKey) != nil {
		storage.Delete(ctx, stKey)
		runtime.Log("candidate has been removed")
	}
}

// InnerRingCandidateAdd adds a key to a list of Inner Ring candidates.
// It can be invoked only by the candidate itself.
//
// This method transfers fee from a candidate to the contract account.
// Fee value is specified in FrostFS network config with the key InnerRingCandidateFee.
func InnerRingCandidateAdd(key interop.PublicKey) {
	ctx := storage.GetContext()

	common.CheckWitness(key)

	stKey := append([]byte(candidatesKey), key...)
	if storage.Get(ctx, stKey) != nil {
		panic("candidate already in the list")
	}

	from := contract.CreateStandardAccount(key)
	to := runtime.GetExecutingScriptHash()
	fee := getConfig(ctx, CandidateFeeConfigKey).(int)

	transferred := gas.Transfer(from, to, fee, []byte(ignoreDepositNotification))
	if !transferred {
		panic("failed to transfer funds, aborting")
	}

	storage.Put(ctx, stKey, []byte{1})
	runtime.Log("candidate has been added")
}

// OnNEP17Payment is a callback for NEP-17 compatible native GAS contract.
// It takes no more than 9000.0 GAS. Native GAS has precision 8, and
// FrostFS balance contract has precision 12. Values bigger than 9000.0 can
// break JSON limits for integers when precision is converted.
func OnNEP17Payment(from interop.Hash160, amount int, data interface{}) {
	rcv := data.(interop.Hash160)
	if common.BytesEqual(rcv, []byte(ignoreDepositNotification)) {
		return
	}

	if amount <= 0 {
		common.AbortWithMessage("amount must be positive")
	} else if maxBalanceAmountGAS < int64(amount) {
		common.AbortWithMessage("out of max amount limit")
	}

	caller := runtime.GetCallingScriptHash()
	if !common.BytesEqual(caller, interop.Hash160(gas.Hash)) {
		common.AbortWithMessage("only GAS can be accepted for deposit")
	}

	switch len(rcv) {
	case 20:
	case 0:
		rcv = from
	default:
		common.AbortWithMessage("invalid data argument, expected Hash160")
	}

	runtime.Log("funds have been transferred")

	tx := runtime.GetScriptContainer()
	runtime.Notify("Deposit", from, amount, rcv, tx.Hash)
}

// Withdraw initializes gas asset withdraw from FrostFS. It can be invoked only
// by the specified user.
//
// This method produces Withdraw notification to lock assets in the sidechain and
// transfers withdraw fee from a user account to each Alphabet node. If notary
// is enabled in the mainchain, fee is transferred to Processing contract.
// Fee value is specified in FrostFS network config with the key WithdrawFee.
func Withdraw(user interop.Hash160, amount int) {
	if !runtime.CheckWitness(user) {
		panic("you should be the owner of the wallet")
	}

	if amount < 0 {
		panic("non positive amount number")
	}

	if amount > maxBalanceAmount {
		panic("out of max amount limit")
	}

	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	// transfer fee to proxy contract to pay cheque invocation
	fee := getConfig(ctx, withdrawFeeConfigKey).(int)

	if notaryDisabled {
		alphabet := getAlphabetNodes(ctx)
		for _, node := range alphabet {
			processingAddr := contract.CreateStandardAccount(node)

			transferred := gas.Transfer(user, processingAddr, fee, []byte{})
			if !transferred {
				panic("failed to transfer withdraw fee, aborting")
			}
		}
	} else {
		processingAddr := storage.Get(ctx, processingContractKey).(interop.Hash160)

		transferred := gas.Transfer(user, processingAddr, fee, []byte{})
		if !transferred {
			panic("failed to transfer withdraw fee, aborting")
		}
	}

	// notify alphabet nodes
	amount = amount * 100000000
	tx := runtime.GetScriptContainer()

	runtime.Notify("Withdraw", user, amount, tx.Hash)
}

// Cheque transfers GAS back to the user from the contract account, if assets were
// successfully locked in FrostFS balance contract. It can be invoked only by
// Alphabet nodes.
//
// This method produces Cheque notification to burn assets in sidechain.
func Cheque(id []byte, user interop.Hash160, amount int, lockAcc []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = getAlphabetNodes(ctx)
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked by alphabet")
		}
	} else {
		multiaddr := AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	from := runtime.GetExecutingScriptHash()

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	transferred := gas.Transfer(from, user, amount, nil)
	if !transferred {
		panic("failed to transfer funds, aborting")
	}

	runtime.Log("funds have been transferred")
	runtime.Notify("Cheque", id, user, amount, lockAcc)
}

// Bind method produces notification to bind the specified public keys in FrostFSID
// contract in the sidechain. It can be invoked only by specified user.
//
// This method produces Bind notification. This method panics if keys are not
// 33 byte long. User argument must be a valid 20 byte script hash.
func Bind(user []byte, keys []interop.PublicKey) {
	if !runtime.CheckWitness(user) {
		panic("you should be the owner of the wallet")
	}

	for i := 0; i < len(keys); i++ {
		pubKey := keys[i]
		if len(pubKey) != interop.PublicKeyCompressedLen {
			panic("incorrect public key size")
		}
	}

	runtime.Notify("Bind", user, keys)
}

// Unbind method produces notification to unbind the specified public keys in FrostFSID
// contract in the sidechain. It can be invoked only by the specified user.
//
// This method produces Unbind notification. This method panics if keys are not
// 33 byte long. User argument must be a valid 20 byte script hash.
func Unbind(user []byte, keys []interop.PublicKey) {
	if !runtime.CheckWitness(user) {
		panic("you should be the owner of the wallet")
	}

	for i := 0; i < len(keys); i++ {
		pubKey := keys[i]
		if len(pubKey) != interop.PublicKeyCompressedLen {
			panic("incorrect public key size")
		}
	}

	runtime.Notify("Unbind", user, keys)
}

// AlphabetUpdate updates a list of alphabet nodes with the provided list of
// public keys. It can be invoked only by alphabet nodes.
//
// This method is used in notary disabled sidechain environment. In this case,
// the actual alphabet list should be stored in the FrostFS contract.
func AlphabetUpdate(id []byte, args []interop.PublicKey) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	if len(args) == 0 {
		panic("bad arguments")
	}

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = getAlphabetNodes(ctx)
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked by alphabet")
		}
	} else {
		multiaddr := AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	newAlphabet := []interop.PublicKey{}

	for i := 0; i < len(args); i++ {
		pubKey := args[i]
		if len(pubKey) != interop.PublicKeyCompressedLen {
			panic("invalid public key in alphabet list")
		}

		newAlphabet = append(newAlphabet, pubKey)
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	common.SetSerialized(ctx, alphabetKey, newAlphabet)

	runtime.Notify("AlphabetUpdate", id, newAlphabet)
	runtime.Log("alphabet list has been updated")
}

// Config returns configuration value of FrostFS configuration. If the key does
// not exists, returns nil.
func Config(key []byte) interface{} {
	ctx := storage.GetReadOnlyContext()
	return getConfig(ctx, key)
}

// SetConfig key-value pair as a FrostFS runtime configuration value. It can be invoked
// only by Alphabet nodes.
func SetConfig(id, key, val []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = getAlphabetNodes(ctx)
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(key) == 0 {
			panic("this method must be invoked by alphabet")
		}
	} else {
		multiaddr := AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	setConfig(ctx, key, val)

	runtime.Notify("SetConfig", id, key, val)
	runtime.Log("configuration has been updated")
}

// ListConfig returns an array of structures that contain a key and a value of all
// FrostFS configuration records. Key and value are both byte arrays.
func ListConfig() []record {
	ctx := storage.GetReadOnlyContext()

	var config []record

	it := storage.Find(ctx, configPrefix, storage.None)
	for iterator.Next(it) {
		pair := iterator.Value(it).(struct {
			key []byte
			val []byte
		})
		r := record{key: pair.key[len(configPrefix):], val: pair.val}

		config = append(config, r)
	}

	return config
}

// Version returns version of the contract.
func Version() int {
	return common.Version
}

// getAlphabetNodes returns a deserialized slice of nodes from storage.
func getAlphabetNodes(ctx storage.Context) []interop.PublicKey {
	data := storage.Get(ctx, alphabetKey)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]interop.PublicKey)
	}

	return []interop.PublicKey{}
}

// getConfig returns the installed frostfs configuration value or nil if it is not set.
func getConfig(ctx storage.Context, key interface{}) interface{} {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	return storage.Get(ctx, storageKey)
}

// setConfig sets a frostfs configuration value in the contract storage.
func setConfig(ctx storage.Context, key, val interface{}) {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	storage.Put(ctx, storageKey, val)
}

// multiaddress returns a multisignature address from the list of IRNode structures
// with m = 2/3n+1.
func multiaddress(keys []interop.PublicKey) []byte {
	threshold := len(keys)*2/3 + 1

	return contract.CreateMultisigAccount(threshold, keys)
}
