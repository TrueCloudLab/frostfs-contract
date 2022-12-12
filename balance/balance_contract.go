package balance

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

type (
	// Token holds all token info.
	Token struct {
		// Ticker symbol
		Symbol string
		// Amount of decimals
		Decimals int
		// Storage key for circulation value
		CirculationKey string
	}

	// Account structure stores metadata of each NeoFS balance account.
	Account struct {
		// Active balance
		Balance int
		// Until valid for lock accounts
		Until int
		// Parent field used in lock accounts, used to return assets back if
		// account wasn't burnt.
		Parent []byte
	}
)

const (
	symbol      = "NEOFS"
	decimals    = 12
	circulation = "MainnetGAS"

	netmapContractKey    = "netmapScriptHash"
	containerContractKey = "containerScriptHash"
	notaryDisabledKey    = "notary"
)

var token Token

func createToken() Token {
	return Token{
		Symbol:         symbol,
		Decimals:       decimals,
		CirculationKey: circulation,
	}
}

func init() {
	token = createToken()
}

func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()
	if isUpdate {
		args := data.([]interface{})
		common.CheckVersion(args[len(args)-1].(int))
		return
	}

	args := data.(struct {
		notaryDisabled bool
		addrNetmap     interop.Hash160
		addrContainer  interop.Hash160
	})

	if len(args.addrNetmap) != interop.Hash160Len || len(args.addrContainer) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapContractKey, args.addrNetmap)
	storage.Put(ctx, containerContractKey, args.addrContainer)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, args.notaryDisabled)
	if args.notaryDisabled {
		common.InitVote(ctx)
		runtime.Log("balance contract notary disabled")
	}

	runtime.Log("balance contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("balance contract updated")
}

// Symbol is a NEP-17 standard method that returns NEOFS token symbol.
func Symbol() string {
	return token.Symbol
}

// Decimals is a NEP-17 standard method that returns precision of NeoFS
// balances.
func Decimals() int {
	return token.Decimals
}

// TotalSupply is a NEP-17 standard method that returns total amount of main
// chain GAS in NeoFS network.
func TotalSupply() int {
	ctx := storage.GetReadOnlyContext()
	return token.getSupply(ctx)
}

// BalanceOf is a NEP-17 standard method that returns NeoFS balance of the specified
// account.
func BalanceOf(account interop.Hash160) int {
	ctx := storage.GetReadOnlyContext()
	return token.balanceOf(ctx, account)
}

// Transfer is a NEP-17 standard method that transfers NeoFS balance from one
// account to another. It can be invoked only by the account owner.
//
// It produces Transfer and TransferX notifications. TransferX notification
// will have empty details field.
func Transfer(from, to interop.Hash160, amount int, data interface{}) bool {
	ctx := storage.GetContext()
	return token.transfer(ctx, from, to, amount, false, nil)
}

// TransferX is a method for NeoFS balance to be transferred from one account to
// another. It can be invoked by the account owner or by Alphabet nodes.
//
// It produces Transfer and TransferX notifications.
//
// TransferX method expands Transfer method by having extra details argument.
// TransferX method also allows to transfer assets by Alphabet nodes of the
// Inner Ring with multisignature.
func TransferX(from, to interop.Hash160, amount int, details []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet     []interop.PublicKey
		nodeKey      []byte
		indirectCall bool
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked from inner ring")
		}

		indirectCall = common.FromKnownContract(
			ctx,
			runtime.GetCallingScriptHash(),
			containerContractKey,
		)
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	if notaryDisabled && !indirectCall {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{from, to, amount}, []byte("transfer"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	result := token.transfer(ctx, from, to, amount, true, details)
	if !result {
		panic("can't transfer assets")
	}

	runtime.Log("successfully transferred assets")
}

// Lock is a method that transfers assets from a user account to the lock account
// related to the user. It can be invoked only by Alphabet nodes of the Inner Ring.
//
// It produces Lock, Transfer and TransferX notifications.
//
// Lock method is invoked by Alphabet nodes of the Inner Ring when they process
// Withdraw notification from NeoFS contract. This should transfer assets
// to a new lock account that won't be used for anything beside Unlock and Burn.
func Lock(txDetails []byte, from, to interop.Hash160, amount, until int) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked from inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	details := common.LockTransferDetails(txDetails)

	lockAccount := Account{
		Balance: 0,
		Until:   until,
		Parent:  from,
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{txDetails}, []byte("lock"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	common.SetSerialized(ctx, to, lockAccount)

	result := token.transfer(ctx, from, to, amount, true, details)
	if !result {
		// consider using `return false` to remove votes
		panic("can't lock funds")
	}

	runtime.Log("created lock account")
	runtime.Notify("Lock", txDetails, from, to, amount, until)
}

// NewEpoch is a method that checks timeout on lock accounts and returns assets
// if lock is not available anymore. It can be invoked only by NewEpoch method
// of Netmap contract.
//
// It produces Transfer and TransferX notifications.
func NewEpoch(epochNum int) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	if notaryDisabled {
		indirectCall := common.FromKnownContract(
			ctx,
			runtime.GetCallingScriptHash(),
			netmapContractKey,
		)
		if !indirectCall {
			panic("this method must be invoked from inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	it := storage.Find(ctx, []byte{}, storage.KeysOnly)
	for iterator.Next(it) {
		addr := iterator.Value(it).(interop.Hash160) // it MUST BE `storage.KeysOnly`
		if len(addr) != interop.Hash160Len {
			continue
		}

		acc := getAccount(ctx, addr)
		if acc.Until == 0 {
			continue
		}

		if epochNum >= acc.Until {
			details := common.UnlockTransferDetails(epochNum)
			// return assets back to the parent
			token.transfer(ctx, addr, acc.Parent, acc.Balance, true, details)
		}
	}
}

// Mint is a method that transfers assets to a user account from an empty account.
// It can be invoked only by Alphabet nodes of the Inner Ring.
//
// It produces Mint, Transfer and TransferX notifications.
//
// Mint method is invoked by Alphabet nodes of the Inner Ring when they process
// Deposit notification from NeoFS contract. Before that, Alphabet nodes should
// synchronize precision of mainchain GAS contract and Balance contract.
// Mint increases total supply of NEP-17 compatible NeoFS token.
func Mint(to interop.Hash160, amount int, txDetails []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked from inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	details := common.MintTransferDetails(txDetails)

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{txDetails}, []byte("mint"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	ok := token.transfer(ctx, nil, to, amount, true, details)
	if !ok {
		panic("can't transfer assets")
	}

	supply := token.getSupply(ctx)
	supply = supply + amount
	storage.Put(ctx, token.CirculationKey, supply)
	runtime.Log("assets were minted")
	runtime.Notify("Mint", to, amount)
}

// Burn is a method that transfers assets from a user account to an empty account.
// It can be invoked only by Alphabet nodes of the Inner Ring.
//
// It produces Burn, Transfer and TransferX notifications.
//
// Burn method is invoked by Alphabet nodes of the Inner Ring when they process
// Cheque notification from NeoFS contract. It means that locked assets have been
// transferred to the user in the mainchain, therefore the lock account should be destroyed.
// Before that, Alphabet nodes should synchronize precision of mainchain GAS
// contract and Balance contract. Burn decreases total supply of NEP-17
// compatible NeoFS token.
func Burn(from interop.Hash160, amount int, txDetails []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var ( // for invocation collection without notary
		alphabet []interop.PublicKey
		nodeKey  []byte
	)

	if notaryDisabled {
		alphabet = common.AlphabetNodes()
		nodeKey = common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			panic("this method must be invoked from inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	details := common.BurnTransferDetails(txDetails)

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{txDetails}, []byte("burn"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	ok := token.transfer(ctx, from, nil, amount, true, details)
	if !ok {
		panic("can't transfer assets")
	}

	supply := token.getSupply(ctx)
	if supply < amount {
		panic("negative supply after burn")
	}

	supply = supply - amount
	storage.Put(ctx, token.CirculationKey, supply)
	runtime.Log("assets were burned")
	runtime.Notify("Burn", from, amount)
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

// getSupply gets the token totalSupply value from VM storage.
func (t Token) getSupply(ctx storage.Context) int {
	supply := storage.Get(ctx, t.CirculationKey)
	if supply != nil {
		return supply.(int)
	}

	return 0
}

// BalanceOf gets the token balance of a specific address.
func (t Token) balanceOf(ctx storage.Context, holder interop.Hash160) int {
	acc := getAccount(ctx, holder)

	return acc.Balance
}

func (t Token) transfer(ctx storage.Context, from, to interop.Hash160, amount int, innerRing bool, details []byte) bool {
	amountFrom, ok := t.canTransfer(ctx, from, to, amount, innerRing)
	if !ok {
		return false
	}

	if len(from) == 20 {
		if amountFrom.Balance == amount {
			storage.Delete(ctx, from)
		} else {
			amountFrom.Balance = amountFrom.Balance - amount // neo-go#953
			common.SetSerialized(ctx, from, amountFrom)
		}
	}

	if len(to) == 20 {
		amountTo := getAccount(ctx, to)
		amountTo.Balance = amountTo.Balance + amount // neo-go#953
		common.SetSerialized(ctx, to, amountTo)
	}

	runtime.Notify("Transfer", from, to, amount)
	runtime.Notify("TransferX", from, to, amount, details)

	return true
}

// canTransfer returns the amount it can transfer.
func (t Token) canTransfer(ctx storage.Context, from, to interop.Hash160, amount int, innerRing bool) (Account, bool) {
	var (
		emptyAcc = Account{}
	)

	if !innerRing {
		if len(to) != interop.Hash160Len || !isUsableAddress(from) {
			runtime.Log("bad script hashes")
			return emptyAcc, false
		}
	} else if len(from) == 0 {
		return emptyAcc, true
	}

	amountFrom := getAccount(ctx, from)
	if amountFrom.Balance < amount {
		runtime.Log("not enough assets")
		return emptyAcc, false
	}

	// return amountFrom value back to transfer, reduces extra Get
	return amountFrom, true
}

// isUsableAddress checks if the sender is either a correct NEO address or SC address.
func isUsableAddress(addr interop.Hash160) bool {
	if len(addr) == 20 {
		if runtime.CheckWitness(addr) {
			return true
		}

		// Check if a smart contract is calling script hash
		callingScriptHash := runtime.GetCallingScriptHash()
		if common.BytesEqual(callingScriptHash, addr) {
			return true
		}
	}

	return false
}

func getAccount(ctx storage.Context, key interface{}) Account {
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).(Account)
	}

	return Account{}
}
