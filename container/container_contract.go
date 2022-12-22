package container

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/convert"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

type (
	storageNode struct {
		info []byte
	}

	Container struct {
		value []byte
		sig   interop.Signature
		pub   interop.PublicKey
		token []byte
	}

	ExtendedACL struct {
		value []byte
		sig   interop.Signature
		pub   interop.PublicKey
		token []byte
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
	frostfsIDContractKey = "identityScriptHash"
	balanceContractKey   = "balanceScriptHash"
	netmapContractKey    = "netmapScriptHash"
	nnsContractKey       = "nnsScriptHash"
	nnsRootKey           = "nnsRoot"
	nnsHasAliasKey       = "nnsHasAlias"
	notaryDisabledKey    = "notary"

	// RegistrationFeeKey is a key in netmap config which contains fee for container registration.
	RegistrationFeeKey = "ContainerFee"
	// AliasFeeKey is a key in netmap config which contains fee for nice-name registration.
	AliasFeeKey = "ContainerAliasFee"

	// V2 format
	containerIDSize = 32 // SHA256 size

	singleEstimatePrefix = "est"
	estimateKeyPrefix    = "cnr"
	containerKeyPrefix   = 'x'
	ownerKeyPrefix       = 'o'
	estimatePostfixSize  = 10
	// CleanupDelta contains the number of the last epochs for which container estimations are present.
	CleanupDelta = 3
	// TotalCleanupDelta contains the number of the epochs after which estimation
	// will be removed by epoch tick cleanup if any of the nodes hasn't updated
	// container size and/or container has been removed. It must be greater than CleanupDelta.
	TotalCleanupDelta = CleanupDelta + 1

	// NotFoundError is returned if container is missing.
	NotFoundError = "container does not exist"

	// default SOA record field values
	defaultRefresh = 3600                 // 1 hour
	defaultRetry   = 600                  // 10 min
	defaultExpire  = 3600 * 24 * 365 * 10 // 10 years
	defaultTTL     = 3600                 // 1 hour
)

var (
	eACLPrefix = []byte("eACL")
)

// OnNEP11Payment is needed for registration with contract as the owner to work.
func OnNEP11Payment(a interop.Hash160, b int, c []byte, d interface{}) {
}

func _deploy(data interface{}, isUpdate bool) {
	ctx := storage.GetContext()
	if isUpdate {
		args := data.([]interface{})
		common.CheckVersion(args[len(args)-1].(int))

		it := storage.Find(ctx, []byte{}, storage.None)
		for iterator.Next(it) {
			item := iterator.Value(it).(struct {
				key   []byte
				value []byte
			})

			// Migrate container.
			if len(item.key) == containerIDSize {
				storage.Delete(ctx, item.key)
				storage.Put(ctx, append([]byte{containerKeyPrefix}, item.key...), item.value)
			}

			// Migrate owner-cid map.
			if len(item.key) == 25 /* owner id size */ +containerIDSize {
				storage.Delete(ctx, item.key)
				storage.Put(ctx, append([]byte{ownerKeyPrefix}, item.key...), item.value)
			}
		}
		return
	}

	args := data.(struct {
		notaryDisabled bool
		addrNetmap     interop.Hash160
		addrBalance    interop.Hash160
		addrID         interop.Hash160
		addrNNS        interop.Hash160
		nnsRoot        string
	})

	if len(args.addrNetmap) != interop.Hash160Len ||
		len(args.addrBalance) != interop.Hash160Len ||
		len(args.addrID) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapContractKey, args.addrNetmap)
	storage.Put(ctx, balanceContractKey, args.addrBalance)
	storage.Put(ctx, frostfsIDContractKey, args.addrID)
	storage.Put(ctx, nnsContractKey, args.addrNNS)
	storage.Put(ctx, nnsRootKey, args.nnsRoot)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, args.notaryDisabled)
	if args.notaryDisabled {
		common.InitVote(ctx)
		runtime.Log("container contract notary disabled")
	}

	// add NNS root for container alias domains
	registerNiceNameTLD(args.addrNNS, args.nnsRoot)

	runtime.Log("container contract initialized")
}

func registerNiceNameTLD(addrNNS interop.Hash160, nnsRoot string) {
	isAvail := contract.Call(addrNNS, "isAvailable", contract.AllowCall|contract.ReadStates,
		"container").(bool)
	if !isAvail {
		return
	}

	res := contract.Call(addrNNS, "register", contract.All,
		nnsRoot, runtime.GetExecutingScriptHash(), "ops@nspcc.ru",
		defaultRefresh, defaultRetry, defaultExpire, defaultTTL).(bool)
	if !res {
		panic("can't register NNS TLD")
	}
}

// Update method updates contract source code and manifest. It can be invoked
// by committee only.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("container contract updated")
}

// Put method creates a new container if it has been invoked by Alphabet nodes
// of the Inner Ring. Otherwise, it produces containerPut notification.
//
// Container should be a stable marshaled Container structure from API.
// Signature is a RFC6979 signature of the Container.
// PublicKey contains the public key of the signer.
// Token is optional and should be a stable marshaled SessionToken structure from
// API.
func Put(container []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte) {
	PutNamed(container, signature, publicKey, token, "", "")
}

// PutNamed is similar to put but also sets a TXT record in nns contract.
// Note that zone must exist.
func PutNamed(container []byte, signature interop.Signature,
	publicKey interop.PublicKey, token []byte,
	name, zone string) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	ownerID := ownerFromBinaryContainer(container)
	containerID := crypto.Sha256(container)
	frostfsIDContractAddr := storage.Get(ctx, frostfsIDContractKey).(interop.Hash160)
	cnr := Container{
		value: container,
		sig:   signature,
		pub:   publicKey,
		token: token,
	}

	var (
		needRegister    bool
		nnsContractAddr interop.Hash160
		domain          string
	)
	if name != "" {
		if zone == "" {
			zone = storage.Get(ctx, nnsRootKey).(string)
		}
		nnsContractAddr = storage.Get(ctx, nnsContractKey).(interop.Hash160)
		domain = name + "." + zone
		needRegister = checkNiceNameAvailable(nnsContractAddr, domain)
	}

	alphabet := common.AlphabetNodes()
	from := common.WalletToScriptHash(ownerID)
	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	balanceContractAddr := storage.Get(ctx, balanceContractKey).(interop.Hash160)
	containerFee := contract.Call(netmapContractAddr, "config", contract.ReadOnly, RegistrationFeeKey).(int)
	balance := contract.Call(balanceContractAddr, "balanceOf", contract.ReadOnly, from).(int)
	if name != "" {
		aliasFee := contract.Call(netmapContractAddr, "config", contract.ReadOnly, AliasFeeKey).(int)
		containerFee += aliasFee
	}

	if balance < containerFee*len(alphabet) {
		panic("insufficient balance to create container")
	}

	if notaryDisabled {
		nodeKey := common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			runtime.Notify("containerPut", container, signature, publicKey, token)
			return
		}

		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{container, signature, publicKey}, []byte("put"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}
	// todo: check if new container with unique container id

	details := common.ContainerFeeTransferDetails(containerID)

	for i := 0; i < len(alphabet); i++ {
		node := alphabet[i]
		to := contract.CreateStandardAccount(node)

		contract.Call(balanceContractAddr, "transferX",
			contract.All,
			from,
			to,
			containerFee,
			details,
		)
	}

	addContainer(ctx, containerID, ownerID, cnr)

	if name != "" {
		if needRegister {
			res := contract.Call(nnsContractAddr, "register", contract.All,
				domain, runtime.GetExecutingScriptHash(), "ops@nspcc.ru",
				defaultRefresh, defaultRetry, defaultExpire, defaultTTL).(bool)
			if !res {
				panic("can't register the domain " + domain)
			}
		}
		contract.Call(nnsContractAddr, "addRecord", contract.All,
			domain, 16 /* TXT */, std.Base58Encode(containerID))

		key := append([]byte(nnsHasAliasKey), containerID...)
		storage.Put(ctx, key, domain)
	}

	if len(token) == 0 { // if container created directly without session
		contract.Call(frostfsIDContractAddr, "addKey", contract.All, ownerID, [][]byte{publicKey})
	}

	runtime.Log("added new container")
	runtime.Notify("PutSuccess", containerID, publicKey)
}

// checkNiceNameAvailable checks if the nice name is available for the container.
// It panics if the name is taken. Returned value specifies if new domain registration is needed.
func checkNiceNameAvailable(nnsContractAddr interop.Hash160, domain string) bool {
	isAvail := contract.Call(nnsContractAddr, "isAvailable",
		contract.ReadStates|contract.AllowCall, domain).(bool)
	if isAvail {
		return true
	}

	owner := contract.Call(nnsContractAddr, "ownerOf",
		contract.ReadStates|contract.AllowCall, domain).(string)
	if owner != string(common.CommitteeAddress()) && owner != string(runtime.GetExecutingScriptHash()) {
		panic("committee or container contract must own registered domain")
	}

	res := contract.Call(nnsContractAddr, "getRecords",
		contract.ReadStates|contract.AllowCall, domain, 16 /* TXT */)
	if res != nil {
		panic("name is already taken")
	}

	return false
}

// Delete method removes a container from the contract storage if it has been
// invoked by Alphabet nodes of the Inner Ring. Otherwise, it produces
// containerDelete notification.
//
// Signature is a RFC6979 signature of the container ID.
// Token is optional and should be a stable marshaled SessionToken structure from
// API.
//
// If the container doesn't exist, it panics with NotFoundError.
func Delete(containerID []byte, signature interop.Signature, token []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	ownerID := getOwnerByID(ctx, containerID)
	if ownerID == nil {
		return
	}

	if notaryDisabled {
		alphabet := common.AlphabetNodes()
		nodeKey := common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			runtime.Notify("containerDelete", containerID, signature, token)
			return
		}

		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{containerID, signature}, []byte("delete"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	key := append([]byte(nnsHasAliasKey), containerID...)
	domain := storage.Get(ctx, key).(string)
	if len(domain) != 0 {
		storage.Delete(ctx, key)
		// We should do `getRecord` first because NNS record could be deleted
		// by other means (expiration, manual), thus leading to failing `deleteRecord`
		// and inability to delete a container. We should also check if we own the record in case.
		nnsContractAddr := storage.Get(ctx, nnsContractKey).(interop.Hash160)
		res := contract.Call(nnsContractAddr, "getRecords", contract.ReadStates|contract.AllowCall, domain, 16 /* TXT */)
		if res != nil && std.Base58Encode(containerID) == string(res.([]interface{})[0].(string)) {
			contract.Call(nnsContractAddr, "deleteRecords", contract.All, domain, 16 /* TXT */)
		}
	}
	removeContainer(ctx, containerID, ownerID)
	runtime.Log("remove container")
	runtime.Notify("DeleteSuccess", containerID)
}

// Get method returns a structure that contains a stable marshaled Container structure,
// the signature, the public key of the container creator and a stable marshaled SessionToken
// structure if it was provided.
//
// If the container doesn't exist, it panics with NotFoundError.
func Get(containerID []byte) Container {
	ctx := storage.GetReadOnlyContext()
	cnt := getContainer(ctx, containerID)
	if len(cnt.value) == 0 {
		panic(NotFoundError)
	}
	return cnt
}

// Owner method returns a 25 byte Owner ID of the container.
//
// If the container doesn't exist, it panics with NotFoundError.
func Owner(containerID []byte) []byte {
	ctx := storage.GetReadOnlyContext()
	owner := getOwnerByID(ctx, containerID)
	if owner == nil {
		panic(NotFoundError)
	}
	return owner
}

// Count method returns the number of registered containers.
func Count() int {
	count := 0
	ctx := storage.GetReadOnlyContext()
	it := storage.Find(ctx, []byte{containerKeyPrefix}, storage.KeysOnly)
	for iterator.Next(it) {
		count++
	}
	return count
}

// ContainersOf iterates over all container IDs owned by the specified owner.
// If owner is nil, it iterates over all containers.
func ContainersOf(owner []byte) iterator.Iterator {
	ctx := storage.GetReadOnlyContext()
	key := []byte{ownerKeyPrefix}
	if len(owner) != 0 {
		key = append(key, owner...)
	}
	return storage.Find(ctx, key, storage.ValuesOnly)
}

// List method returns a list of all container IDs owned by the specified owner.
func List(owner []byte) [][]byte {
	ctx := storage.GetReadOnlyContext()

	if len(owner) == 0 {
		return getAllContainers(ctx)
	}

	var list [][]byte

	it := storage.Find(ctx, append([]byte{ownerKeyPrefix}, owner...), storage.ValuesOnly)
	for iterator.Next(it) {
		id := iterator.Value(it).([]byte)
		list = append(list, id)
	}

	return list
}

// SetEACL method sets a new extended ACL table related to the contract
// if it was invoked by Alphabet nodes of the Inner Ring. Otherwise, it produces
// setEACL notification.
//
// EACL should be a stable marshaled EACLTable structure from API.
// Signature is a RFC6979 signature of the Container.
// PublicKey contains the public key of the signer.
// Token is optional and should be a stable marshaled SessionToken structure from
// API.
//
// If the container doesn't exist, it panics with NotFoundError.
func SetEACL(eACL []byte, signature interop.Signature, publicKey interop.PublicKey, token []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	// V2 format
	// get container ID
	offset := int(eACL[1])
	offset = 2 + offset + 4
	containerID := eACL[offset : offset+32]

	ownerID := getOwnerByID(ctx, containerID)
	if ownerID == nil {
		panic(NotFoundError)
	}

	if notaryDisabled {
		alphabet := common.AlphabetNodes()
		nodeKey := common.InnerRingInvoker(alphabet)
		if len(nodeKey) == 0 {
			runtime.Notify("setEACL", eACL, signature, publicKey, token)
			return
		}

		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{eACL}, []byte("setEACL"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	rule := ExtendedACL{
		value: eACL,
		sig:   signature,
		pub:   publicKey,
		token: token,
	}

	key := append(eACLPrefix, containerID...)

	common.SetSerialized(ctx, key, rule)

	runtime.Log("success")
	runtime.Notify("SetEACLSuccess", containerID, publicKey)
}

// EACL method returns a structure that contains a stable marshaled EACLTable structure,
// the signature, the public key of the extended ACL setter and a stable marshaled SessionToken
// structure if it was provided.
//
// If the container doesn't exist, it panics with NotFoundError.
func EACL(containerID []byte) ExtendedACL {
	ctx := storage.GetReadOnlyContext()

	ownerID := getOwnerByID(ctx, containerID)
	if ownerID == nil {
		panic(NotFoundError)
	}

	return getEACL(ctx, containerID)
}

// PutContainerSize method saves container size estimation in contract
// memory. It can be invoked only by Storage nodes from the network map. This method
// checks witness based on the provided public key of the Storage node.
//
// If the container doesn't exist, it panics with NotFoundError.
func PutContainerSize(epoch int, cid []byte, usedSize int, pubKey interop.PublicKey) {
	ctx := storage.GetContext()

	if getOwnerByID(ctx, cid) == nil {
		panic(NotFoundError)
	}

	common.CheckWitness(pubKey)

	if !isStorageNode(ctx, pubKey) {
		panic("method must be invoked by storage node from network map")
	}

	key := estimationKey(epoch, cid, pubKey)

	s := estimation{
		from: pubKey,
		size: usedSize,
	}

	storage.Put(ctx, key, std.Serialize(s))
	updateEstimations(ctx, epoch, cid, pubKey, false)

	runtime.Log("saved container size estimation")
}

// GetContainerSize method returns the container ID and a slice of container
// estimations. Container estimation includes the public key of the Storage Node
// that registered estimation and value of estimation.
//
// Use the ID obtained from ListContainerSizes method. Estimations are removed
// from contract storage every epoch, see NewEpoch method; therefore, this method
// can return different results during different epochs.
func GetContainerSize(id []byte) containerSizes {
	ctx := storage.GetReadOnlyContext()

	// V2 format
	// this `id` expected to be from `ListContainerSizes`
	// therefore it is not contains postfix, we ignore it in the cut.
	ln := len(id)
	cid := id[ln-containerIDSize : ln]

	return getContainerSizeEstimation(ctx, id, cid)
}

// ListContainerSizes method returns the IDs of container size estimations
// that have been registered for the specified epoch.
func ListContainerSizes(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()

	var buf interface{} = epoch

	key := []byte(estimateKeyPrefix)
	key = append(key, buf.([]byte)...)

	it := storage.Find(ctx, key, storage.KeysOnly)

	uniq := map[string]struct{}{}

	for iterator.Next(it) {
		storageKey := iterator.Value(it).([]byte)

		ln := len(storageKey)
		storageKey = storageKey[:ln-estimatePostfixSize]

		uniq[string(storageKey)] = struct{}{}
	}

	var result [][]byte

	for k := range uniq {
		result = append(result, []byte(k))
	}

	return result
}

// IterateContainerSizes method returns iterator over container size estimations
// that have been registered for the specified epoch.
func IterateContainerSizes(epoch int) iterator.Iterator {
	ctx := storage.GetReadOnlyContext()

	var buf interface{} = epoch

	key := []byte(estimateKeyPrefix)
	key = append(key, buf.([]byte)...)

	return storage.Find(ctx, key, storage.DeserializeValues)
}

// NewEpoch method removes all container size estimations from epoch older than
// epochNum + 3. It can be invoked only by NewEpoch method of the Netmap contract.
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
			panic("method must be invoked by inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	cleanupContainers(ctx, epochNum)
}

// StartContainerEstimation method produces StartEstimation notification.
// It can be invoked only by Alphabet nodes of the Inner Ring.
func StartContainerEstimation(epoch int) {
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
			panic("method must be invoked by inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{epoch}, []byte("startEstimation"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	runtime.Notify("StartEstimation", epoch)
	runtime.Log("notification has been produced")
}

// StopContainerEstimation method produces StopEstimation notification.
// It can be invoked only by Alphabet nodes of the Inner Ring.
func StopContainerEstimation(epoch int) {
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
			panic("method must be invoked by inner ring")
		}
	} else {
		multiaddr := common.AlphabetAddress()
		common.CheckAlphabetWitness(multiaddr)
	}

	if notaryDisabled {
		threshold := len(alphabet)*2/3 + 1
		id := common.InvokeID([]interface{}{epoch}, []byte("stopEstimation"))

		n := common.Vote(ctx, id, nodeKey)
		if n < threshold {
			return
		}

		common.RemoveVotes(ctx, id)
	}

	runtime.Notify("StopEstimation", epoch)
	runtime.Log("notification has been produced")
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

func addContainer(ctx storage.Context, id, owner []byte, container Container) {
	containerListKey := append([]byte{ownerKeyPrefix}, owner...)
	containerListKey = append(containerListKey, id...)
	storage.Put(ctx, containerListKey, id)

	idKey := append([]byte{containerKeyPrefix}, id...)
	common.SetSerialized(ctx, idKey, container)
}

func removeContainer(ctx storage.Context, id []byte, owner []byte) {
	containerListKey := append([]byte{ownerKeyPrefix}, owner...)
	containerListKey = append(containerListKey, id...)
	storage.Delete(ctx, containerListKey)

	storage.Delete(ctx, append([]byte{containerKeyPrefix}, id...))
}

func getAllContainers(ctx storage.Context) [][]byte {
	var list [][]byte

	it := storage.Find(ctx, []byte{containerKeyPrefix}, storage.KeysOnly|storage.RemovePrefix)
	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // it MUST BE `storage.KeysOnly`
		// V2 format
		list = append(list, key)
	}

	return list
}

func getEACL(ctx storage.Context, cid []byte) ExtendedACL {
	key := append(eACLPrefix, cid...)
	data := storage.Get(ctx, key)
	if data != nil {
		return std.Deserialize(data.([]byte)).(ExtendedACL)
	}

	return ExtendedACL{value: []byte{}, sig: interop.Signature{}, pub: interop.PublicKey{}, token: []byte{}}
}

func getContainer(ctx storage.Context, cid []byte) Container {
	data := storage.Get(ctx, append([]byte{containerKeyPrefix}, cid...))
	if data != nil {
		return std.Deserialize(data.([]byte)).(Container)
	}

	return Container{value: []byte{}, sig: interop.Signature{}, pub: interop.PublicKey{}, token: []byte{}}
}

func getOwnerByID(ctx storage.Context, cid []byte) []byte {
	container := getContainer(ctx, cid)
	if len(container.value) == 0 {
		return nil
	}

	return ownerFromBinaryContainer(container.value)
}

func ownerFromBinaryContainer(container []byte) []byte {
	// V2 format
	offset := int(container[1])
	offset = 2 + offset + 4              // version prefix + version size + owner prefix
	return container[offset : offset+25] // offset + size of owner
}

func estimationKey(epoch int, cid []byte, key interop.PublicKey) []byte {
	var buf interface{} = epoch

	hash := crypto.Ripemd160(key)

	result := []byte(estimateKeyPrefix)
	result = append(result, buf.([]byte)...)
	result = append(result, cid...)

	return append(result, hash[:estimatePostfixSize]...)
}

func getContainerSizeEstimation(ctx storage.Context, key, cid []byte) containerSizes {
	var estimations []estimation

	it := storage.Find(ctx, key, storage.ValuesOnly|storage.DeserializeValues)
	for iterator.Next(it) {
		est := iterator.Value(it).(estimation)
		estimations = append(estimations, est)
	}

	return containerSizes{
		cid:         cid,
		estimations: estimations,
	}
}

// isStorageNode looks into _previous_ epoch network map, because storage node
// announces container size estimation of the previous epoch.
func isStorageNode(ctx storage.Context, key interop.PublicKey) bool {
	netmapContractAddr := storage.Get(ctx, netmapContractKey).(interop.Hash160)
	snapshot := contract.Call(netmapContractAddr, "snapshot", contract.ReadOnly, 1).([]storageNode)

	for i := range snapshot {
		// V2 format
		nodeInfo := snapshot[i].info
		nodeKey := nodeInfo[2:35] // offset:2, len:33

		if common.BytesEqual(key, nodeKey) {
			return true
		}
	}

	return false
}

func updateEstimations(ctx storage.Context, epoch int, cid []byte, pub interop.PublicKey, isUpdate bool) {
	h := crypto.Ripemd160(pub)
	estKey := append([]byte(singleEstimatePrefix), cid...)
	estKey = append(estKey, h...)

	var newEpochs []int
	rawList := storage.Get(ctx, estKey).([]byte)

	if rawList != nil {
		epochs := std.Deserialize(rawList).([]int)
		for _, oldEpoch := range epochs {
			if !isUpdate && epoch-oldEpoch > CleanupDelta {
				key := append([]byte(estimateKeyPrefix), convert.ToBytes(oldEpoch)...)
				key = append(key, cid...)
				key = append(key, h[:estimatePostfixSize]...)
				storage.Delete(ctx, key)
			} else {
				newEpochs = append(newEpochs, oldEpoch)
			}
		}
	}

	newEpochs = append(newEpochs, epoch)
	common.SetSerialized(ctx, estKey, newEpochs)
}

func cleanupContainers(ctx storage.Context, epoch int) {
	it := storage.Find(ctx, []byte(estimateKeyPrefix), storage.KeysOnly)
	for iterator.Next(it) {
		k := iterator.Value(it).([]byte)
		// V2 format
		nbytes := k[len(estimateKeyPrefix) : len(k)-containerIDSize-estimatePostfixSize]

		var n interface{} = nbytes

		if epoch-n.(int) > TotalCleanupDelta {
			storage.Delete(ctx, k)
		}
	}
}
