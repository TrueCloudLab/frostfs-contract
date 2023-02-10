package frostfsid

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

type (
	UserInfo struct {
		Keys [][]byte
	}
)

const (
	ownerSize = 1 + interop.Hash160Len + 4
)

const (
	netmapContractKey    = "netmapScriptHash"
	containerContractKey = "containerScriptHash"
	notaryDisabledKey    = "notary"
	ownerKeysPrefix      = 'o'
)

func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()

	//TODO(@acid-ant): #9 remove notaryDisabled from args in future version
	if data.([]interface{})[0].(bool) {
		panic(common.PanicMsgForNotaryDisabledEnv)
	}
	storage.Delete(ctx, notaryDisabledKey)

	if isUpdate {
		args := data.([]interface{})
		common.CheckVersion(args[len(args)-1].(int))
		return
	}

	args := data.(struct {
		//TODO(@acid-ant): #9 remove notaryDisabled in future version
		notaryDisabled bool
		addrNetmap     interop.Hash160
		addrContainer  interop.Hash160
	})

	if len(args.addrNetmap) != interop.Hash160Len || len(args.addrContainer) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapContractKey, args.addrNetmap)
	storage.Put(ctx, containerContractKey, args.addrContainer)

	runtime.Log("frostfsid contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("frostfsid contract updated")
}

// AddKey binds a list of the provided public keys to the OwnerID. It can be invoked only by
// Alphabet nodes.
//
// This method panics if the OwnerID is not an ownerSize byte or the public key is not 33 byte long.
// If the key is already bound, the method ignores it.
func AddKey(owner []byte, keys []interop.PublicKey) {
	// V2 format
	if len(owner) != ownerSize {
		panic("incorrect owner")
	}

	for i := range keys {
		if len(keys[i]) != interop.PublicKeyCompressedLen {
			panic("incorrect public key")
		}
	}

	ctx := storage.GetContext()

	common.CheckAlphabetWitness()

	ownerKey := append([]byte{ownerKeysPrefix}, owner...)
	for i := range keys {
		stKey := append(ownerKey, keys[i]...)
		storage.Put(ctx, stKey, []byte{1})
	}

	runtime.Log("key bound to the owner")
}

// RemoveKey unbinds the provided public keys from the OwnerID. It can be invoked only by
// Alphabet nodes.
//
// This method panics if the OwnerID is not an ownerSize byte or the public key is not 33 byte long.
// If the key is already unbound, the method ignores it.
func RemoveKey(owner []byte, keys []interop.PublicKey) {
	// V2 format
	if len(owner) != ownerSize {
		panic("incorrect owner")
	}

	for i := range keys {
		if len(keys[i]) != interop.PublicKeyCompressedLen {
			panic("incorrect public key")
		}
	}

	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	if !runtime.CheckWitness(multiaddr) {
		panic("invocation from non inner ring node")
	}

	ownerKey := append([]byte{ownerKeysPrefix}, owner...)
	for i := range keys {
		stKey := append(ownerKey, keys[i]...)
		storage.Delete(ctx, stKey)
	}
}

// Key method returns a list of 33-byte public keys bound with the OwnerID.
//
// This method panics if the owner is not ownerSize byte long.
func Key(owner []byte) [][]byte {
	// V2 format
	if len(owner) != ownerSize {
		panic("incorrect owner")
	}

	ctx := storage.GetReadOnlyContext()

	ownerKey := append([]byte{ownerKeysPrefix}, owner...)
	info := getUserInfo(ctx, ownerKey)

	return info.Keys
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

func getUserInfo(ctx storage.Context, key interface{}) UserInfo {
	it := storage.Find(ctx, key, storage.KeysOnly|storage.RemovePrefix)
	pubs := [][]byte{}
	for iterator.Next(it) {
		pub := iterator.Value(it).([]byte)
		pubs = append(pubs, pub)
	}

	return UserInfo{Keys: pubs}
}
