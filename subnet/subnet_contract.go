package subnet

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

const (
	// ErrInvalidSubnetID is thrown when subnet id is not a slice of 5 bytes.
	ErrInvalidSubnetID = "invalid subnet ID"
	// ErrInvalidGroupID is thrown when group id is not a slice of 5 bytes.
	ErrInvalidGroupID = "invalid group ID"
	// ErrInvalidOwner is thrown when owner has invalid format.
	ErrInvalidOwner = "invalid owner"
	// ErrInvalidAdmin is thrown when admin has invalid format.
	ErrInvalidAdmin = "invalid administrator"
	// ErrAlreadyExists is thrown when id already exists.
	ErrAlreadyExists = "subnet id already exists"
	// ErrNotExist is thrown when id doesn't exist.
	ErrNotExist = "subnet id doesn't exist"
	// ErrInvalidUser is thrown when user has invalid format.
	ErrInvalidUser = "invalid user"
	// ErrInvalidNode is thrown when node has invalid format.
	ErrInvalidNode = "invalid node key"
	// ErrNodeAdmNotExist is thrown when node admin is not found.
	ErrNodeAdmNotExist = "node admin not found"
	// ErrClientAdmNotExist is thrown when client admin is not found.
	ErrClientAdmNotExist = "client admin not found"
	// ErrNodeNotExist is thrown when node is not found.
	ErrNodeNotExist = "node not found"
	// ErrUserNotExist is thrown when user is not found.
	ErrUserNotExist = "user not found"
	// ErrAccessDenied is thrown when operation is denied for caller.
	ErrAccessDenied = "access denied"
)

const (
	nodeAdminPrefix   = 'a'
	infoPrefix        = 'i'
	clientAdminPrefix = 'm'
	nodePrefix        = 'n'
	ownerPrefix       = 'o'
	userPrefix        = 'u'
	notaryDisabledKey = 'z'
)

const (
	userIDSize   = 27
	subnetIDSize = 5
	groupIDSize  = 5
)

// _deploy function sets up initial list of inner ring public keys.
func _deploy(data interface{}, isUpdate bool) {
	if isUpdate {
		args := data.([]interface{})
		common.CheckVersion(args[len(args)-1].(int))
		return
	}

	args := data.(struct {
		notaryDisabled bool
	})

	ctx := storage.GetContext()
	storage.Put(ctx, []byte{notaryDisabledKey}, args.notaryDisabled)
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update", contract.All,
		script, manifest, common.AppendVersion(data))
	runtime.Log("subnet contract updated")
}

// Put creates a new subnet with the specified owner and info.
func Put(id []byte, ownerKey interop.PublicKey, info []byte) {
	// V2 format
	if len(id) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}
	if len(ownerKey) != interop.PublicKeyCompressedLen {
		panic(ErrInvalidOwner)
	}

	ctx := storage.GetContext()
	stKey := append([]byte{ownerPrefix}, id...)
	if storage.Get(ctx, stKey) != nil {
		panic(ErrAlreadyExists)
	}

	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)
	if notaryDisabled {
		alphabet := common.AlphabetNodes()
		nodeKey := common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			common.CheckWitness(ownerKey)
			runtime.Notify("Put", id, ownerKey, info)
			return
		}

		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{ownerKey, info}, []byte("put"))
		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	} else {
		common.CheckOwnerWitness(ownerKey)

		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	storage.Put(ctx, stKey, ownerKey)
	stKey[0] = infoPrefix
	storage.Put(ctx, stKey, info)
}

// Get returns info about the subnet with the specified id.
func Get(id []byte) []byte {
	// V2 format
	if len(id) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	ctx := storage.GetReadOnlyContext()
	key := append([]byte{infoPrefix}, id...)
	raw := storage.Get(ctx, key)
	if raw == nil {
		panic(ErrNotExist)
	}
	return raw.([]byte)
}

// Delete deletes the subnet with the specified id.
func Delete(id []byte) {
	// V2 format
	if len(id) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	ctx := storage.GetContext()
	key := append([]byte{ownerPrefix}, id...)
	raw := storage.Get(ctx, key)
	if raw == nil {
		return
	}

	owner := raw.([]byte)
	common.CheckOwnerWitness(owner)

	storage.Delete(ctx, key)

	key[0] = infoPrefix
	storage.Delete(ctx, key)

	key[0] = nodeAdminPrefix
	deleteByPrefix(ctx, key)

	key[0] = nodePrefix
	deleteByPrefix(ctx, key)

	key[0] = clientAdminPrefix
	deleteByPrefix(ctx, key)

	key[0] = userPrefix
	deleteByPrefix(ctx, key)

	runtime.Notify("Delete", id)
}

// AddNodeAdmin adds a new node administrator to the specified subnetwork.
func AddNodeAdmin(subnetID []byte, adminKey interop.PublicKey) {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	if len(adminKey) != interop.PublicKeyCompressedLen {
		panic(ErrInvalidAdmin)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic(ErrNotExist)
	}

	owner := rawOwner.([]byte)
	common.CheckOwnerWitness(owner)

	stKey[0] = nodeAdminPrefix

	if keyInList(ctx, adminKey, stKey) {
		return
	}

	putKeyInList(ctx, adminKey, stKey)
}

// RemoveNodeAdmin removes node administrator from the specified subnetwork.
// Must be called by the subnet owner only.
func RemoveNodeAdmin(subnetID []byte, adminKey interop.PublicKey) {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	if len(adminKey) != interop.PublicKeyCompressedLen {
		panic(ErrInvalidAdmin)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic(ErrNotExist)
	}

	owner := rawOwner.([]byte)
	common.CheckOwnerWitness(owner)

	stKey[0] = nodeAdminPrefix

	if !keyInList(ctx, adminKey, stKey) {
		return
	}

	deleteKeyFromList(ctx, adminKey, stKey)
}

// AddNode adds a node to the specified subnetwork.
// Must be called by the subnet's owner or the node administrator
// only.
func AddNode(subnetID []byte, node interop.PublicKey) {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	if len(node) != interop.PublicKeyCompressedLen {
		panic(ErrInvalidNode)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic(ErrNotExist)
	}

	stKey[0] = nodeAdminPrefix

	owner := rawOwner.([]byte)

	if !calledByOwnerOrAdmin(ctx, owner, stKey) {
		panic(ErrAccessDenied)
	}

	stKey[0] = nodePrefix

	if keyInList(ctx, node, stKey) {
		return
	}

	putKeyInList(ctx, node, stKey)
}

// RemoveNode removes a node from the specified subnetwork.
// Must be called by the subnet's owner or the node administrator
// only.
func RemoveNode(subnetID []byte, node interop.PublicKey) {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	if len(node) != interop.PublicKeyCompressedLen {
		panic(ErrInvalidNode)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic(ErrNotExist)
	}

	stKey[0] = nodeAdminPrefix

	owner := rawOwner.([]byte)

	if !calledByOwnerOrAdmin(ctx, owner, stKey) {
		panic(ErrAccessDenied)
	}

	stKey[0] = nodePrefix

	if !keyInList(ctx, node, stKey) {
		return
	}

	storage.Delete(ctx, append(stKey, node...))

	runtime.Notify("RemoveNode", subnetID, node)
}

// NodeAllowed checks if a node is included in the
// specified subnet.
func NodeAllowed(subnetID []byte, node interop.PublicKey) bool {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	if len(node) != interop.PublicKeyCompressedLen {
		panic(ErrInvalidNode)
	}

	ctx := storage.GetReadOnlyContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic(ErrNotExist)
	}

	stKey[0] = nodePrefix

	return storage.Get(ctx, append(stKey, node...)) != nil
}

// AddClientAdmin adds a new client administrator of the specified group in the specified subnetwork.
// Must be called by the owner only.
func AddClientAdmin(subnetID []byte, groupID []byte, adminPublicKey interop.PublicKey) {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	// V2 format
	if len(groupID) != groupIDSize {
		panic(ErrInvalidGroupID)
	}

	if len(adminPublicKey) != interop.PublicKeyCompressedLen {
		panic(ErrInvalidAdmin)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic(ErrNotExist)
	}

	owner := rawOwner.([]byte)
	common.CheckOwnerWitness(owner)

	stKey[0] = clientAdminPrefix
	stKey = append(stKey, groupID...)

	if keyInList(ctx, adminPublicKey, stKey) {
		return
	}

	putKeyInList(ctx, adminPublicKey, stKey)
}

// RemoveClientAdmin removes client administrator from the
// specified group in the specified subnetwork.
// Must be called by the owner only.
func RemoveClientAdmin(subnetID []byte, groupID []byte, adminPublicKey interop.PublicKey) {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	// V2 format
	if len(groupID) != groupIDSize {
		panic(ErrInvalidGroupID)
	}

	if len(adminPublicKey) != interop.PublicKeyCompressedLen {
		panic(ErrInvalidAdmin)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic(ErrNotExist)
	}

	owner := rawOwner.([]byte)
	common.CheckOwnerWitness(owner)

	stKey[0] = clientAdminPrefix
	stKey = append(stKey, groupID...)

	if !keyInList(ctx, adminPublicKey, stKey) {
		return
	}

	deleteKeyFromList(ctx, adminPublicKey, stKey)
}

// AddUser adds user to the specified subnetwork and group.
// Must be called by the owner or the group's admin only.
func AddUser(subnetID []byte, groupID []byte, userID []byte) {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	// V2 format
	if len(userID) != userIDSize {
		panic(ErrInvalidUser)
	}

	// V2 format
	if len(groupID) != groupIDSize {
		panic(ErrInvalidGroupID)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic(ErrNotExist)
	}

	stKey[0] = clientAdminPrefix
	stKey = append(stKey, groupID...)

	owner := rawOwner.([]byte)

	if !calledByOwnerOrAdmin(ctx, owner, stKey) {
		panic(ErrAccessDenied)
	}

	stKey[0] = userPrefix

	if keyInList(ctx, userID, stKey) {
		return
	}

	putKeyInList(ctx, userID, stKey)
}

// RemoveUser removes a user from the specified subnetwork and group.
// Must be called by the owner or the group's admin only.
func RemoveUser(subnetID []byte, groupID []byte, userID []byte) {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	// V2 format
	if len(groupID) != groupIDSize {
		panic(ErrInvalidGroupID)
	}

	// V2 format
	if len(userID) != userIDSize {
		panic(ErrInvalidUser)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)

	rawOwner := storage.Get(ctx, stKey)
	if rawOwner == nil {
		panic(ErrNotExist)
	}

	stKey[0] = clientAdminPrefix
	stKey = append(stKey, groupID...)

	owner := rawOwner.([]byte)

	if !calledByOwnerOrAdmin(ctx, owner, stKey) {
		panic(ErrAccessDenied)
	}

	stKey[0] = userPrefix

	if !keyInList(ctx, userID, stKey) {
		return
	}

	deleteKeyFromList(ctx, userID, stKey)
}

// UserAllowed returns bool that indicates if a node is included in the
// specified subnet.
func UserAllowed(subnetID []byte, user []byte) bool {
	// V2 format
	if len(subnetID) != subnetIDSize {
		panic(ErrInvalidSubnetID)
	}

	ctx := storage.GetContext()

	stKey := append([]byte{ownerPrefix}, subnetID...)
	if storage.Get(ctx, stKey) == nil {
		panic(ErrNotExist)
	}

	stKey[0] = userPrefix
	prefixLen := len(stKey) + groupIDSize

	iter := storage.Find(ctx, stKey, storage.KeysOnly)
	for iterator.Next(iter) {
		key := iterator.Value(iter).([]byte)
		if common.BytesEqual(user, key[prefixLen:]) {
			return true
		}
	}

	return false
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

func keyInList(ctx storage.Context, searchedKey interop.PublicKey, prefix []byte) bool {
	return storage.Get(ctx, append(prefix, searchedKey...)) != nil
}

func putKeyInList(ctx storage.Context, keyToPut interop.PublicKey, prefix []byte) {
	storage.Put(ctx, append(prefix, keyToPut...), []byte{1})
}

func deleteKeyFromList(ctx storage.Context, keyToDelete interop.PublicKey, prefix []byte) {
	storage.Delete(ctx, append(prefix, keyToDelete...))
}

func deleteByPrefix(ctx storage.Context, prefix []byte) {
	iter := storage.Find(ctx, prefix, storage.KeysOnly)
	for iterator.Next(iter) {
		k := iterator.Value(iter).([]byte)
		storage.Delete(ctx, k)
	}
}

func calledByOwnerOrAdmin(ctx storage.Context, owner []byte, adminPrefix []byte) bool {
	if runtime.CheckWitness(owner) {
		return true
	}

	iter := storage.Find(ctx, adminPrefix, storage.KeysOnly|storage.RemovePrefix)
	for iterator.Next(iter) {
		key := iterator.Value(iter).([]byte)
		if runtime.CheckWitness(key) {
			return true
		}
	}

	return false
}
