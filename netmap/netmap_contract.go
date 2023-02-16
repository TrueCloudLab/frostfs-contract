package netmap

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

// NodeState is an enumeration for node states.
type NodeState int

// Various Node states
const (
	_ NodeState = iota

	// NodeStateOnline stands for nodes that are in full network and
	// operational availability.
	NodeStateOnline

	// NodeStateOffline stands for nodes that are in network unavailability.
	NodeStateOffline

	// NodeStateMaintenance stands for nodes under maintenance with partial
	// network availability.
	NodeStateMaintenance
)

// Node groups data related to FrostFS storage nodes registered in the FrostFS
// network. The information is stored in the current contract.
type Node struct {
	// Information about the node encoded according to the FrostFS binary
	// protocol.
	BLOB []byte

	// Current node state.
	State NodeState
}

const (
	notaryDisabledKey = "notary"
	innerRingKey      = "innerring"

	// DefaultSnapshotCount contains the number of previous snapshots stored by this contract.
	// Must be less than 255.
	DefaultSnapshotCount = 10
	snapshotCountKey     = "snapshotCount"
	snapshotKeyPrefix    = "snapshot_"
	snapshotCurrentIDKey = "snapshotCurrent"
	snapshotEpoch        = "snapshotEpoch"
	snapshotBlockKey     = "snapshotBlock"

	containerContractKey = "containerScriptHash"
	balanceContractKey   = "balanceScriptHash"

	cleanupEpochMethod = "newEpoch"
)

var (
	configPrefix    = []byte("config")
	candidatePrefix = []byte("candidate")
)

// _deploy function sets up initial list of inner ring public keys.
func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()

	//TODO(@acid-ant): #9 remove notaryDisabled from args in future version
	if data.([]interface{})[0].(bool) {
		panic(common.PanicMsgForNotaryDisabledEnv)
	}
	storage.Delete(ctx, notaryDisabledKey)

	var args = data.(struct {
		//TODO(@acid-ant): #9 remove notaryDisabled in future version
		notaryDisabled bool
		addrBalance    interop.Hash160
		addrContainer  interop.Hash160
		keys           []interop.PublicKey
		config         [][]byte
		version        int
	})

	ln := len(args.config)
	if ln%2 != 0 {
		panic("bad configuration")
	}

	for i := 0; i < ln/2; i++ {
		key := args.config[i*2]
		val := args.config[i*2+1]

		setConfig(ctx, key, val)
	}

	if isUpdate {
		common.CheckVersion(args.version)
		return
	}

	if len(args.addrBalance) != interop.Hash160Len || len(args.addrContainer) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	// epoch number is a little endian int, it doesn't need to be serialized
	storage.Put(ctx, snapshotCountKey, DefaultSnapshotCount)
	storage.Put(ctx, snapshotEpoch, 0)
	storage.Put(ctx, snapshotBlockKey, 0)

	prefix := []byte(snapshotKeyPrefix)
	for i := 0; i < DefaultSnapshotCount; i++ {
		common.SetSerialized(ctx, append(prefix, byte(i)), []Node{})
	}
	storage.Put(ctx, snapshotCurrentIDKey, 0)

	storage.Put(ctx, balanceContractKey, args.addrBalance)
	storage.Put(ctx, containerContractKey, args.addrContainer)

	runtime.Log("netmap contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("netmap contract updated")
}

// AddPeerIR accepts Alphabet calls in the notary-enabled contract setting and
// behaves similar to AddPeer in the notary-disabled one.
//
// AddPeerIR MUST NOT be called in notary-disabled contract setting.
// AddPeerIR MUST be called by the Alphabet member only.
func AddPeerIR(nodeInfo []byte) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness(common.AlphabetAddress())

	publicKey := nodeInfo[2:35] // V2 format: offset:2, len:33

	addToNetmap(ctx, publicKey, Node{
		BLOB:  nodeInfo,
		State: NodeStateOnline,
	})
}

// AddPeer accepts information about the network map candidate in the FrostFS
// binary protocol format and does nothing. Keep method because storage node
// creates a notary transaction with this method, which produces a notary
// notification (implicit here).
func AddPeer(nodeInfo []byte) {
	// V2 format - offset:2, len:33
	common.CheckWitness(nodeInfo[2:35])
	return
}

// updates state of the network map candidate by its public key in the contract
// storage, and throws UpdateStateSuccess notification after this.
//
// State MUST be from the NodeState enum.
func updateCandidateState(ctx storage.Context, publicKey interop.PublicKey, state NodeState) {
	switch state {
	case NodeStateOffline:
		removeFromNetmap(ctx, publicKey)
		runtime.Log("remove storage node from the network map")
	case NodeStateOnline, NodeStateMaintenance:
		updateNetmapState(ctx, publicKey, state)
		runtime.Log("update state of the network map candidate")
	default:
		panic("unsupported state")
	}

	runtime.Notify("UpdateStateSuccess", publicKey, state)
}

// UpdateState accepts new state to be assigned to network map candidate
// identified by the given public key, identifies the signer.
// Applicable only for notary-enabled environment.
//
// Signers:
//
//	(a) candidate himself only, if provided public key corresponds to the signer
//	(b) Alphabet member only
//	(ab) both candidate and Alphabet member
//	(c) others
//
// UpdateState case-by-case behavior:
//
//	(a) panics
//	(b) panics
//	(ab) updates candidate's state in the contract storage (*), and throws
//	UpdateStateSuccess with the provided key and new state
//	(c) panics
//
// (*) Candidate is removed from the candidate set if state is NodeStateOffline.
// Any other state is written into candidate's descriptor in the contract storage.
// If requested candidate is missing, panic occurs. Throws UpdateStateSuccess
// notification on success.
//
// State MUST be from the NodeState enum. Public key MUST be
// interop.PublicKeyCompressedLen bytes.
func UpdateState(state NodeState, publicKey interop.PublicKey) {
	if len(publicKey) != interop.PublicKeyCompressedLen {
		panic("incorrect public key")
	}

	ctx := storage.GetContext()

	common.CheckWitness(publicKey)
	common.CheckAlphabetWitness(common.AlphabetAddress())

	updateCandidateState(ctx, publicKey, state)
}

// UpdateStateIR accepts Alphabet calls in the notary-enabled contract setting
// and behaves similar to UpdateState, but does not require candidate's
// signature presence.
//
// UpdateStateIR MUST NOT be called in notary-disabled contract setting.
// UpdateStateIR MUST be called by the Alphabet member only.
func UpdateStateIR(state NodeState, publicKey interop.PublicKey) {
	ctx := storage.GetContext()

	common.CheckAlphabetWitness(common.AlphabetAddress())

	updateCandidateState(ctx, publicKey, state)
}

// NewEpoch method changes the epoch number up to the provided epochNum argument. It can
// be invoked only by Alphabet nodes. If provided epoch number is less than the
// current epoch number or equals it, the method throws panic.
//
// When epoch number is updated, the contract sets storage node candidates as the current
// network map. The contract also invokes NewEpoch method on Balance and Container
// contracts.
//
// It produces NewEpoch notification.
func NewEpoch(epochNum int) {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)
	if epochNum <= currentEpoch {
		panic("invalid epoch") // ignore invocations with invalid epoch
	}

	dataOnlineState := filterNetmap(ctx)

	runtime.Log("process new epoch")

	// todo: check if provided epoch number is bigger than current
	storage.Put(ctx, snapshotEpoch, epochNum)
	storage.Put(ctx, snapshotBlockKey, ledger.CurrentIndex())

	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	id = (id + 1) % getSnapshotCount(ctx)
	storage.Put(ctx, snapshotCurrentIDKey, id)

	// put netmap into actual snapshot
	common.SetSerialized(ctx, snapshotKeyPrefix+string([]byte{byte(id)}), dataOnlineState)

	// make clean up routines in other contracts
	cleanup(ctx, epochNum)

	runtime.Notify("NewEpoch", epochNum)
}

// Epoch method returns the current epoch number.
func Epoch() int {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, snapshotEpoch).(int)
}

// LastEpochBlock method returns the block number when the current epoch was applied.
func LastEpochBlock() int {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, snapshotBlockKey).(int)
}

// Netmap returns set of information about the storage nodes representing a network
// map in the current epoch.
//
// Current state of each node is represented in the State field. It MAY differ
// with the state encoded into BLOB field, in this case binary encoded state
// MUST NOT be processed.
func Netmap() []Node {
	ctx := storage.GetReadOnlyContext()
	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	return getSnapshot(ctx, snapshotKeyPrefix+string([]byte{byte(id)}))
}

// NetmapCandidates returns set of information about the storage nodes
// representing candidates for the network map in the coming epoch.
//
// Current state of each node is represented in the State field. It MAY differ
// with the state encoded into BLOB field, in this case binary encoded state
// MUST NOT be processed.
func NetmapCandidates() []Node {
	ctx := storage.GetReadOnlyContext()
	return getNetmapNodes(ctx)
}

// Snapshot returns set of information about the storage nodes representing a network
// map in (current-diff)-th epoch.
//
// Diff MUST NOT be negative. Diff MUST be less than maximum number of network
// map snapshots stored in the contract. The limit is a contract setting,
// DefaultSnapshotCount by default. See UpdateSnapshotCount for details.
//
// Current state of each node is represented in the State field. It MAY differ
// with the state encoded into BLOB field, in this case binary encoded state
// MUST NOT be processed.
func Snapshot(diff int) []Node {
	ctx := storage.GetReadOnlyContext()
	count := getSnapshotCount(ctx)
	if diff < 0 || count <= diff {
		panic("incorrect diff")
	}

	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	needID := (id - diff + count) % count
	key := snapshotKeyPrefix + string([]byte{byte(needID)})
	return getSnapshot(ctx, key)
}

func getSnapshotCount(ctx storage.Context) int {
	return storage.Get(ctx, snapshotCountKey).(int)
}

// UpdateSnapshotCount updates the number of the stored snapshots.
// If a new number is less than the old one, old snapshots are removed.
// Otherwise, history is extended with empty snapshots, so
// `Snapshot` method can return invalid results for `diff = new-old` epochs
// until `diff` epochs have passed.
//
// Count MUST NOT be negative.
func UpdateSnapshotCount(count int) {
	common.CheckAlphabetWitness(common.AlphabetAddress())
	if count < 0 {
		panic("count must be positive")
	}
	ctx := storage.GetContext()
	curr := getSnapshotCount(ctx)
	if curr == count {
		panic("count has not changed")
	}
	storage.Put(ctx, snapshotCountKey, count)

	id := storage.Get(ctx, snapshotCurrentIDKey).(int)
	var delStart, delFinish int
	if curr < count {
		// Increase history size.
		//
		// Old state (N = count, K = curr, E = current index, C = current epoch)
		// KEY INDEX: 0   | 1     | ... | E | E+1   | ... | K-1   | ... | N-1
		// EPOCH    : C-E | C-E+1 | ... | C | C-K+1 | ... | C-E-1 |
		//
		// New state:
		// KEY INDEX: 0   | 1     | ... | E | E+1 | ... | K-1 | ... | N-1
		// EPOCH    : C-E | C-E+1 | ... | C | nil | ... | .   | ... | C-E-1
		//
		// So we need to move tail snapshots N-K keys forward,
		// i.e. from E+1 .. K to N-K+E+1 .. N
		diff := count - curr
		lower := diff + id + 1
		for k := count - 1; k >= lower; k-- {
			moveSnapshot(ctx, k-diff, k)
		}
		delStart, delFinish = id+1, id+1+diff
		if curr < delFinish {
			delFinish = curr
		}
	} else {
		// Decrease history size.
		//
		// Old state (N = curr, K = count)
		// KEY INDEX: 0   | 1     | ... K1 ... | E | E+1   | ... K2-1 ... | N-1
		// EPOCH    : C-E | C-E+1 | ... .. ... | C | C-N+1 | ... ...  ... | C-E-1
		var step, start int
		if id < count {
			// K2 case, move snapshots from E+1+N-K .. N-1 range to E+1 .. K-1
			// New state:
			// KEY INDEX: 0   | 1     | ... | E | E+1   | ... | K-1
			// EPOCH    : C-E | C-E+1 | ... | C | C-K+1 | ... | C-E-1
			step = curr - count
			start = id + 1
		} else {
			// New state:
			// KEY INDEX: 0     | 1     | ... | K-1
			// EPOCH    : C-K+1 | C-K+2 | ... | C
			// K1 case, move snapshots from E-K+1 .. E range to 0 .. K-1
			// AND replace current id with K-1
			step = id - count + 1
			storage.Put(ctx, snapshotCurrentIDKey, count-1)
		}
		for k := start; k < count; k++ {
			moveSnapshot(ctx, k+step, k)
		}
		delStart, delFinish = count, curr
	}
	for k := delStart; k < delFinish; k++ {
		key := snapshotKeyPrefix + string([]byte{byte(k)})
		storage.Delete(ctx, key)
	}
}

func moveSnapshot(ctx storage.Context, from, to int) {
	keyFrom := snapshotKeyPrefix + string([]byte{byte(from)})
	keyTo := snapshotKeyPrefix + string([]byte{byte(to)})
	data := storage.Get(ctx, keyFrom)
	storage.Put(ctx, keyTo, data)
}

// SnapshotByEpoch returns set of information about the storage nodes representing
// a network map in the given epoch.
//
// Behaves like Snapshot: it is called after difference with the current epoch is
// calculated.
func SnapshotByEpoch(epoch int) []Node {
	ctx := storage.GetReadOnlyContext()
	currentEpoch := storage.Get(ctx, snapshotEpoch).(int)

	return Snapshot(currentEpoch - epoch)
}

// Config returns configuration value of FrostFS configuration. If key does
// not exists, returns nil.
func Config(key []byte) interface{} {
	ctx := storage.GetReadOnlyContext()
	return getConfig(ctx, key)
}

// SetConfig key-value pair as a FrostFS runtime configuration value. It can be invoked
// only by Alphabet nodes.
func SetConfig(id, key, val []byte) {
	ctx := storage.GetContext()

	multiaddr := common.AlphabetAddress()
	common.CheckAlphabetWitness(multiaddr)

	setConfig(ctx, key, val)

	runtime.Log("configuration has been updated")
}

type record struct {
	key []byte
	val []byte
}

// ListConfig returns an array of structures that contain key and value of all
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

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

// serializes and stores the given Node by its public key in the contract storage,
// and throws AddPeerSuccess notification after this.
//
// Public key MUST match the one encoded in BLOB field.
func addToNetmap(ctx storage.Context, publicKey []byte, node Node) {
	storageKey := append(candidatePrefix, publicKey...)
	storage.Put(ctx, storageKey, std.Serialize(node))

	runtime.Notify("AddPeerSuccess", interop.PublicKey(publicKey))
}

func removeFromNetmap(ctx storage.Context, key interop.PublicKey) {
	storageKey := append(candidatePrefix, key...)
	storage.Delete(ctx, storageKey)
}

func updateNetmapState(ctx storage.Context, key interop.PublicKey, state NodeState) {
	storageKey := append(candidatePrefix, key...)
	raw := storage.Get(ctx, storageKey).([]byte)
	if raw == nil {
		panic("peer is missing")
	}
	node := std.Deserialize(raw).(Node)
	node.State = state
	storage.Put(ctx, storageKey, std.Serialize(node))
}

func filterNetmap(ctx storage.Context) []Node {
	var (
		netmap = getNetmapNodes(ctx)
		result = []Node{}
	)

	for i := 0; i < len(netmap); i++ {
		item := netmap[i]
		if item.State != NodeStateOffline {
			result = append(result, item)
		}
	}

	return result
}

func getNetmapNodes(ctx storage.Context) []Node {
	result := []Node{}

	it := storage.Find(ctx, candidatePrefix, storage.ValuesOnly|storage.DeserializeValues)
	for iterator.Next(it) {
		node := iterator.Value(it).(Node)
		result = append(result, node)
	}

	return result
}

func getSnapshot(ctx storage.Context, key string) []Node {
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]Node)
	}

	return []Node{}
}

func getConfig(ctx storage.Context, key interface{}) interface{} {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	return storage.Get(ctx, storageKey)
}

func setConfig(ctx storage.Context, key, val interface{}) {
	postfix := key.([]byte)
	storageKey := append(configPrefix, postfix...)

	storage.Put(ctx, storageKey, val)
}

func cleanup(ctx storage.Context, epoch int) {
	balanceContractAddr := storage.Get(ctx, balanceContractKey).(interop.Hash160)
	contract.Call(balanceContractAddr, cleanupEpochMethod, contract.All, epoch)

	containerContractAddr := storage.Get(ctx, containerContractKey).(interop.Hash160)
	contract.Call(containerContractAddr, cleanupEpochMethod, contract.All, epoch)
}
