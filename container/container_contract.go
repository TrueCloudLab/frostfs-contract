package containercontract

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neofs-contract/common"
)

type (
	storageNode struct {
		info []byte
	}

	extendedACL struct {
		val []byte
		sig []byte
		pub interop.PublicKey
	}

	estimation struct {
		from interop.PublicKey
		size int
	}

	containerSizes struct {
		cid         []byte
		estimations []estimation
	}
)

const (
	version   = 1
	ownersKey = "ownersList"

	neofsIDContractKey = "identityScriptHash"
	balanceContractKey = "balanceScriptHash"
	netmapContractKey  = "netmapScriptHash"
	containerFeeKey    = "ContainerFee"

	containerIDSize = 32 // SHA256 size

	estimateKeyPrefix = "cnr"
	cleanupDelta      = 3
)

var (
	containerFeeTransferMsg = []byte("container creation fee")
	eACLPrefix              = []byte("eACL")

	ctx storage.Context
)

func init() {
	if runtime.GetTrigger() != runtime.Application {
		panic("contract has not been called in application node")
	}

	ctx = storage.GetContext()
}

func Init(addrNetmap, addrBalance, addrID []byte) {
	if storage.Get(ctx, netmapContractKey) != nil &&
		storage.Get(ctx, balanceContractKey) != nil &&
		storage.Get(ctx, neofsIDContractKey) != nil {
		panic("init: contract already deployed")
	}

	if len(addrNetmap) != 20 || len(addrBalance) != 20 || len(addrID) != 20 {
		panic("init: incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapContractKey, addrNetmap)
	storage.Put(ctx, balanceContractKey, addrBalance)
	storage.Put(ctx, neofsIDContractKey, addrID)

	runtime.Log("container contract initialized")
}

func Put(container, signature, publicKey []byte) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	innerRing := contract.Call(netmapContractAddr, "innerRingList").([]common.IRNode)
	threshold := len(innerRing)/3*2 + 1

	offset := int(container[1])
	offset = 2 + offset + 4                  // version prefix + version size + owner prefix
	ownerID := container[offset : offset+25] // offset + size of owner
	containerID := crypto.SHA256(container)
	neofsIDContractAddr := storage.Get(ctx, neofsIDContractKey).([]byte)

	// If invoked from storage node, ignore it.
	// Inner ring will find tx, validate it and send it again.
	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		// check provided key
		if !isSignedByOwnerKey(container, signature, ownerID, publicKey) {
			// check keys from NeoFSID
			keys := contract.Call(neofsIDContractAddr, "key", ownerID).([][]byte)
			if !verifySignature(container, signature, keys) {
				panic("put: invalid owner signature")
			}
		}

		runtime.Notify("containerPut", container, signature, publicKey)

		return true
	}

	from := walletToScripHash(ownerID)
	balanceContractAddr := storage.Get(ctx, balanceContractKey).([]byte)
	containerFee := contract.Call(netmapContractAddr, "config", containerFeeKey).(int)
	hashCandidate := common.InvokeID([]interface{}{container, signature, publicKey}, []byte("put"))

	n := common.Vote(ctx, hashCandidate, irKey)
	if n >= threshold {
		common.RemoveVotes(ctx, hashCandidate)
		// todo: check if new container with unique container id

		for i := 0; i < len(innerRing); i++ {
			node := innerRing[i]
			to := contract.CreateStandardAccount(node.PublicKey)

			tx := contract.Call(balanceContractAddr, "transferX",
				from,
				to,
				containerFee,
				containerFeeTransferMsg, // consider add container id to the message
			)
			if !tx.(bool) {
				// todo: consider using `return false` to remove votes
				panic("put: can't transfer assets for container creation")
			}
		}

		addContainer(ctx, containerID, ownerID, container)
		// try to remove underscore at v0.92.0
		_ = contract.Call(neofsIDContractAddr, "addKey", ownerID, [][]byte{publicKey})

		runtime.Log("put: added new container")
	} else {
		runtime.Log("put: processed invoke from inner ring")
	}

	return true
}

func Delete(containerID, signature []byte) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	innerRing := contract.Call(netmapContractAddr, "innerRingList").([]common.IRNode)
	threshold := len(innerRing)/3*2 + 1

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("delete: container does not exist")
	}

	// If invoked from storage node, ignore it.
	// Inner ring will find tx, validate it and send it again.
	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		// check provided key
		neofsIDContractAddr := storage.Get(ctx, neofsIDContractKey).([]byte)
		keys := contract.Call(neofsIDContractAddr, "key", ownerID).([][]byte)

		if !verifySignature(containerID, signature, keys) {
			panic("delete: invalid owner signature")
		}

		runtime.Notify("containerDelete", containerID, signature)
		return true
	}

	hashCandidate := common.InvokeID([]interface{}{containerID, signature}, []byte("delete"))

	n := common.Vote(ctx, hashCandidate, irKey)
	if n >= threshold {
		common.RemoveVotes(ctx, hashCandidate)
		removeContainer(ctx, containerID, ownerID)
		runtime.Log("delete: remove container")
	} else {
		runtime.Log("delete: processed invoke from inner ring")
	}

	return true
}

func Get(containerID []byte) []byte {
	return storage.Get(ctx, containerID).([]byte)
}

func Owner(containerID []byte) []byte {
	return getOwnerByID(ctx, containerID)
}

func List(owner []byte) [][]byte {
	if len(owner) == 0 {
		return getAllContainers(ctx)
	}

	var list [][]byte

	owners := getList(ctx, ownersKey)
	for i := 0; i < len(owners); i++ {
		ownerID := owners[i]
		if len(owner) != 0 && !common.BytesEqual(owner, ownerID) {
			continue
		}

		containers := getList(ctx, ownerID)

		for j := 0; j < len(containers); j++ {
			container := containers[j]
			list = append(list, container)
		}
	}

	return list
}

func SetEACL(eACL, signature []byte) bool {
	// get container ID
	offset := int(eACL[1])
	offset = 2 + offset + 4
	containerID := eACL[offset : offset+32]

	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("setEACL: container does not exists")
	}

	neofsIDContractAddr := storage.Get(ctx, neofsIDContractKey).([]byte)
	keys := contract.Call(neofsIDContractAddr, "key", ownerID).([][]byte)

	if !verifySignature(eACL, signature, keys) {
		panic("setEACL: invalid eACL signature")
	}

	rule := extendedACL{
		val: eACL,
		sig: signature,
	}

	key := append(eACLPrefix, containerID...)
	common.SetSerialized(ctx, key, rule)

	runtime.Log("setEACL: success")

	return true
}

func EACL(containerID []byte) extendedACL {
	ownerID := getOwnerByID(ctx, containerID)
	if len(ownerID) == 0 {
		panic("getEACL: container does not exists")
	}

	eacl := getEACL(ctx, containerID)

	if len(eacl.sig) == 0 {
		return eacl
	}

	// attach corresponding public key if it was not revoked from neofs id

	neofsIDContractAddr := storage.Get(ctx, neofsIDContractKey).([]byte)
	keys := contract.Call(neofsIDContractAddr, "key", ownerID).([][]byte)

	for i := range keys {
		key := keys[i]
		if crypto.ECDsaSecp256r1Verify(eacl.val, key, eacl.sig) {
			eacl.pub = key

			break
		}
	}

	return eacl
}

func PutContainerSize(epoch int, cid []byte, usedSize int, pubKey interop.PublicKey) bool {
	if !runtime.CheckWitness(pubKey) {
		panic("container: invalid witness for size estimation")
	}

	if !isStorageNode(pubKey) {
		panic("container: only storage nodes can save size estimations")
	}

	key := estimationKey(epoch, cid)
	s := getContainerSizeEstimation(key, cid)

	// do not add estimation twice
	for i := range s.estimations {
		est := s.estimations[i]
		if common.BytesEqual(est.from, pubKey) {
			return false
		}
	}

	s.estimations = append(s.estimations, estimation{
		from: pubKey,
		size: usedSize,
	})

	storage.Put(ctx, key, binary.Serialize(s))

	runtime.Log("container: saved container size estimation")

	return true
}

func GetContainerSize(id []byte) containerSizes {
	return getContainerSizeEstimation(id, nil)
}

func ListContainerSizes(epoch int) [][]byte {
	var buf interface{} = epoch

	key := []byte(estimateKeyPrefix)
	key = append(key, buf.([]byte)...)

	it := storage.Find(ctx, key)

	var result [][]byte

	for iterator.Next(it) {
		key := iterator.Key(it).([]byte)
		result = append(result, key)
	}

	return result
}

func ProcessEpoch(epochNum int) {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	innerRing := contract.Call(netmapContractAddr, "innerRingList").([]common.IRNode)
	threshold := len(innerRing)/3*2 + 1

	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		panic("processEpoch: this method must be invoked from inner ring")
	}

	candidates := keysToDelete(epochNum)
	epochID := common.InvokeID([]interface{}{epochNum}, []byte("epoch"))

	n := common.Vote(ctx, epochID, irKey)
	if n >= threshold {
		common.RemoveVotes(ctx, epochID)

		for i := range candidates {
			candidate := candidates[i]
			storage.Delete(ctx, candidate)
		}
	}
}

func StartContainerEstimation(epoch int) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	innerRing := contract.Call(netmapContractAddr, "innerRingList").([]common.IRNode)
	threshold := len(innerRing)/3*2 + 1

	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		panic("startEstimation: only inner ring nodes can invoke this")
	}

	hashCandidate := common.InvokeID([]interface{}{epoch}, []byte("startEstimation"))

	n := common.Vote(ctx, hashCandidate, irKey)
	if n >= threshold {
		common.RemoveVotes(ctx, hashCandidate)
		runtime.Notify("StartEstimation", epoch)
		runtime.Log("startEstimation: notification has been produced")
	} else {
		runtime.Log("startEstimation: processed invoke from inner ring")
	}

	return true
}

func StopContainerEstimation(epoch int) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	innerRing := contract.Call(netmapContractAddr, "innerRingList").([]common.IRNode)
	threshold := len(innerRing)/3*2 + 1

	irKey := common.InnerRingInvoker(innerRing)
	if len(irKey) == 0 {
		panic("stopEstimation: only inner ring nodes can invoke this")
	}

	hashCandidate := common.InvokeID([]interface{}{epoch}, []byte("stopEstimation"))

	n := common.Vote(ctx, hashCandidate, irKey)
	if n >= threshold {
		common.RemoveVotes(ctx, hashCandidate)
		runtime.Notify("StopEstimation", epoch)
		runtime.Log("stopEstimation: notification has been produced")
	} else {
		runtime.Log("stopEstimation: processed invoke from inner ring")
	}

	return true
}

func Version() int {
	return version
}

func addContainer(ctx storage.Context, id []byte, owner []byte, container []byte) {
	addOrAppend(ctx, ownersKey, owner)
	addOrAppend(ctx, owner, id)
	storage.Put(ctx, id, container)
}

func removeContainer(ctx storage.Context, id []byte, owner []byte) {
	n := remove(ctx, owner, id)

	// if it was last container, remove owner from the list of owners
	if n == 0 {
		_ = remove(ctx, ownersKey, owner)
	}

	storage.Delete(ctx, id)
}

func addOrAppend(ctx storage.Context, key interface{}, value []byte) {
	list := getList(ctx, key)
	for i := 0; i < len(list); i++ {
		if common.BytesEqual(list[i], value) {
			return
		}
	}

	if len(list) == 0 {
		list = [][]byte{value}
	} else {
		list = append(list, value)
	}

	common.SetSerialized(ctx, key, list)
}

// remove returns amount of left elements in the list
func remove(ctx storage.Context, key interface{}, value []byte) int {
	var (
		list    = getList(ctx, key)
		newList = [][]byte{}
	)

	for i := 0; i < len(list); i++ {
		if !common.BytesEqual(list[i], value) {
			newList = append(newList, list[i])
		}
	}

	ln := len(newList)
	if ln == 0 {
		storage.Delete(ctx, key)
	} else {
		common.SetSerialized(ctx, key, newList)
	}

	return ln
}

func getList(ctx storage.Context, key interface{}) [][]byte {
	data := storage.Get(ctx, key)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([][]byte)
	}

	return [][]byte{}
}

func getAllContainers(ctx storage.Context) [][]byte {
	var list [][]byte

	it := storage.Find(ctx, []byte{})
	for iterator.Next(it) {
		key := iterator.Key(it).([]byte)
		if len(key) == containerIDSize {
			list = append(list, key)
		}
	}

	return list
}

func getEACL(ctx storage.Context, cid []byte) extendedACL {
	key := append(eACLPrefix, cid...)
	data := storage.Get(ctx, key)
	if data != nil {
		return binary.Deserialize(data.([]byte)).(extendedACL)
	}

	return extendedACL{val: []byte{}, sig: []byte{}, pub: []byte{}}
}

func walletToScripHash(wallet []byte) []byte {
	return wallet[1 : len(wallet)-4]
}

func verifySignature(msg, sig []byte, keys [][]byte) bool {
	for i := range keys {
		key := keys[i]
		if crypto.ECDsaSecp256r1Verify(msg, key, sig) {
			return true
		}
	}

	return false
}

func getOwnerByID(ctx storage.Context, id []byte) []byte {
	owners := getList(ctx, ownersKey)
	for i := 0; i < len(owners); i++ {
		ownerID := owners[i]
		containers := getList(ctx, ownerID)

		for j := 0; j < len(containers); j++ {
			container := containers[j]
			if common.BytesEqual(container, id) {
				return ownerID
			}
		}
	}

	return nil
}

func isSignedByOwnerKey(msg, sig, owner, key []byte) bool {
	if !isOwnerFromKey(owner, key) {
		return false
	}

	return crypto.ECDsaSecp256r1Verify(msg, key, sig)
}

func isOwnerFromKey(owner []byte, key []byte) bool {
	ownerSH := walletToScripHash(owner)
	keySH := contract.CreateStandardAccount(key)

	return common.BytesEqual(ownerSH, keySH)
}

func estimationKey(epoch int, cid []byte) []byte {
	var buf interface{} = epoch

	result := []byte(estimateKeyPrefix)
	result = append(result, buf.([]byte)...)

	return append(result, cid...)
}

func getContainerSizeEstimation(key, cid []byte) containerSizes {
	data := storage.Get(ctx, key)
	if data != nil {
		return binary.Deserialize(data.([]byte)).(containerSizes)
	}

	return containerSizes{
		cid:         cid,
		estimations: []estimation{},
	}
}

// isStorageNode looks into _previous_ epoch network map, because storage node
// announce container size estimation of previous epoch.
func isStorageNode(key interop.PublicKey) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).([]byte)
	snapshot := contract.Call(netmapContractAddr, "snapshot", 1).([]storageNode)

	for i := range snapshot {
		nodeInfo := snapshot[i].info
		nodeKey := nodeInfo[2:35] // offset:2, len:33

		if common.BytesEqual(key, nodeKey) {
			return true
		}
	}

	return false
}

func keysToDelete(epoch int) [][]byte {
	results := [][]byte{}

	it := storage.Find(ctx, []byte(estimateKeyPrefix))
	for iterator.Next(it) {
		k := iterator.Key(it).([]byte)
		nbytes := k[len(estimateKeyPrefix) : len(k)-32]

		var n interface{} = nbytes

		if epoch-n.(int) > cleanupDelta {
			results = append(results, k)
		}
	}

	return results
}
