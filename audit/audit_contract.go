package audit

import (
	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/contract"
	"github.com/nspcc-dev/neo-go/pkg/interop/iterator"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/management"
	"github.com/nspcc-dev/neo-go/pkg/interop/runtime"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

type (
	auditHeader struct {
		epoch int
		cid   []byte
		from  interop.PublicKey
	}
)

// Audit key is a combination of the epoch, the container ID and the public key of the node that
// has executed the audit. Together, it shouldn't be more than 64 bytes. We can't shrink
// epoch and container ID since we iterate over these values. But we can shrink
// public key by using first bytes of the hashed value.

// V2 format
const maxKeySize = 24 // 24 + 32 (container ID length) + 8 (epoch length) = 64

func (a auditHeader) ID() []byte {
	var buf interface{} = a.epoch

	hashedKey := crypto.Sha256(a.from)
	shortedKey := hashedKey[:maxKeySize]

	return append(buf.([]byte), append(a.cid, shortedKey...)...)
}

const (
	netmapContractKey = "netmapScriptHash"

	notaryDisabledKey = "notary"
)

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
	})

	if len(args.addrNetmap) != interop.Hash160Len {
		panic("incorrect length of contract script hash")
	}

	storage.Put(ctx, netmapContractKey, args.addrNetmap)

	// initialize the way to collect signatures
	storage.Put(ctx, notaryDisabledKey, args.notaryDisabled)
	if args.notaryDisabled {
		runtime.Log("audit contract notary disabled")
	}

	runtime.Log("audit contract initialized")
}

// Update method updates contract source code and manifest. It can be invoked
// only by committee.
func Update(script []byte, manifest []byte, data interface{}) {
	if !common.HasUpdateAccess() {
		panic("only committee can update contract")
	}

	contract.Call(interop.Hash160(management.Hash), "update",
		contract.All, script, manifest, common.AppendVersion(data))
	runtime.Log("audit contract updated")
}

// Put method stores a stable marshalled `DataAuditResult` structure. It can be
// invoked only by Inner Ring nodes.
//
// Inner Ring nodes perform audit of containers and produce `DataAuditResult`
// structures. They are stored in audit contract and used for settlements
// in later epochs.
func Put(rawAuditResult []byte) {
	ctx := storage.GetContext()
	notaryDisabled := storage.Get(ctx, notaryDisabledKey).(bool)

	var innerRing []interop.PublicKey

	if notaryDisabled {
		netmapContract := storage.Get(ctx, netmapContractKey).(interop.Hash160)
		innerRing = common.InnerRingNodesFromNetmap(netmapContract)
	} else {
		innerRing = common.InnerRingNodes()
	}

	hdr := newAuditHeader(rawAuditResult)
	presented := false

	for i := range innerRing {
		ir := innerRing[i]
		if common.BytesEqual(ir, hdr.from) {
			presented = true

			break
		}
	}

	if !runtime.CheckWitness(hdr.from) || !presented {
		panic("put access denied")
	}

	storage.Put(ctx, hdr.ID(), rawAuditResult)

	runtime.Log("audit: result has been saved")
}

// Get method returns a stable marshaled DataAuditResult structure.
//
// The ID of the DataAuditResult can be obtained from listing methods.
func Get(id []byte) []byte {
	ctx := storage.GetReadOnlyContext()
	return storage.Get(ctx, id).([]byte)
}

// List method returns a list of all available DataAuditResult IDs from
// the contract storage.
func List() [][]byte {
	ctx := storage.GetReadOnlyContext()
	it := storage.Find(ctx, []byte{}, storage.KeysOnly)

	return list(it)
}

// ListByEpoch method returns a list of DataAuditResult IDs generated during
// the specified epoch.
func ListByEpoch(epoch int) [][]byte {
	ctx := storage.GetReadOnlyContext()
	var buf interface{} = epoch
	it := storage.Find(ctx, buf.([]byte), storage.KeysOnly)

	return list(it)
}

// ListByCID method returns a list of DataAuditResult IDs generated during
// the specified epoch for the specified container.
func ListByCID(epoch int, cid []byte) [][]byte {
	ctx := storage.GetReadOnlyContext()

	var buf interface{} = epoch

	prefix := append(buf.([]byte), cid...)
	it := storage.Find(ctx, prefix, storage.KeysOnly)

	return list(it)
}

// ListByNode method returns a list of DataAuditResult IDs generated in
// the specified epoch for the specified container by the specified Inner Ring node.
func ListByNode(epoch int, cid []byte, key interop.PublicKey) [][]byte {
	ctx := storage.GetReadOnlyContext()
	hdr := auditHeader{
		epoch: epoch,
		cid:   cid,
		from:  key,
	}

	it := storage.Find(ctx, hdr.ID(), storage.KeysOnly)

	return list(it)
}

func list(it iterator.Iterator) [][]byte {
	var result [][]byte

	ignore := [][]byte{
		[]byte(netmapContractKey),
		[]byte(notaryDisabledKey),
	}

loop:
	for iterator.Next(it) {
		key := iterator.Value(it).([]byte) // iterator MUST BE `storage.KeysOnly`
		for _, ignoreKey := range ignore {
			if common.BytesEqual(key, ignoreKey) {
				continue loop
			}
		}

		result = append(result, key)
	}

	return result
}

// Version returns the version of the contract.
func Version() int {
	return common.Version
}

// readNext reads the length from the first byte, and then reads data (max 127 bytes).
func readNext(input []byte) ([]byte, int) {
	var buf interface{} = input[0]
	ln := buf.(int)

	return input[1 : 1+ln], 1 + ln
}

func newAuditHeader(input []byte) auditHeader {
	// V2 format
	offset := int(input[1])
	offset = 2 + offset + 1 // version prefix + version len + epoch prefix

	var buf interface{} = input[offset : offset+8] // [ 8 integer bytes ]
	epoch := buf.(int)

	offset = offset + 8

	// cid is a nested structure with raw bytes
	// [ cid struct prefix (wireType + len = 2 bytes), cid value wireType (1 byte), ... ]
	cid, cidOffset := readNext(input[offset+2+1:])

	// key is a raw byte
	// [ public key wireType (1 byte), ... ]
	key, _ := readNext(input[offset+2+1+cidOffset+1:])

	return auditHeader{
		epoch,
		cid,
		key,
	}
}
