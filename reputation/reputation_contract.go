package reputation

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

const (
	notaryDisabledKey     = "notary"
	reputationValuePrefix = 'r'
	reputationCountPrefix = 'c'
)

func _deploy(data interface{}, isUpdate bool) {
	//TODO(@acid-ant): #9 remove notaryDisabled from args in future version
	args := data.([]interface{})
	if args[0].(bool) {
		panic(common.PanicMsgForNotaryDisabledEnv)
	}
	storage.Delete(storage.GetContext(), notaryDisabledKey)

	if isUpdate {
		common.CheckVersion(args[len(args)-1].(int))
		return
	}

	runtime.Log("reputation contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("reputation contract updated")
}

// Put method saves DataAuditResult in contract storage. It can be invoked only by
// Inner Ring nodes. It does not require multisignature invocations.
//
// Epoch is the epoch number when DataAuditResult structure was generated.
// PeerID contains public keys of the Inner Ring node that has produced DataAuditResult.
// Value contains a stable marshaled structure of DataAuditResult.
func Put(epoch int, peerID []byte, value []byte) {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	if !runtime.CheckWitness(multiaddr) {
		runtime.Notify("reputationPut", epoch, peerID, value)
		return
	}

	id := storageID(epoch, peerID)
	key := getReputationKey(reputationCountPrefix, id)
	rawCnt := storage.Get(ctx, key)
	cnt := 0
	if rawCnt != nil {
		cnt = rawCnt.(int)
	}
	cnt++
	storage.Put(ctx, key, cnt)

	key[0] = reputationValuePrefix
	key = append(key, convert.ToBytes(cnt)...)
	storage.Put(ctx, key, value)
}

// Get method returns a list of all stable marshaled DataAuditResult structures
// produced by the specified Inner Ring node during the specified epoch.
func Get(epoch int, peerID []byte) [][]byte {
	id := storageID(epoch, peerID)
	return GetByID(id)
}

// GetByID method returns a list of all stable marshaled DataAuditResult with
// the specified id. Use ListByEpoch method to obtain the id.
func GetByID(id []byte) [][]byte {
	ctx := storage.GetReadOnlyContext()

	var data [][]byte

	it := storage.Find(ctx, getReputationKey(reputationValuePrefix, id), storage.ValuesOnly)
	for iterator.Next(it) {
		data = append(data, iterator.Value(it).([]byte))
	}
	return data
}

func getReputationKey(prefix byte, id []byte) []byte {
	return append([]byte{prefix}, id...)
}

// ListByEpoch returns a list of IDs that may be used to get reputation data
// with GetByID method.
func ListByEpoch(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()
	key := getReputationKey(reputationCountPrefix, convert.ToBytes(epoch))
	it := storage.Find(ctx, key, storage.KeysOnly)

	var result [][]byte

	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // iterator MUST BE `storage.KeysOnly`
		result = append(result, key[1:])
	}

	return result
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

func storageID(epoch int, peerID []byte) []byte {
	var buf interface{} = epoch

	return append(buf.([]byte), peerID...)
}
