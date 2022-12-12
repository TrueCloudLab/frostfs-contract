package tests

import (
	"encoding/binary"
	"path"
	"testing"

	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/TrueCloudLab/frostfs-contract/subnet"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/stretchr/testify/require"
)

const subnetPath = "../subnet"

func deploySubnetContract(t *testing.T, e *neotest.Executor) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, subnetPath, path.Join(subnetPath, "config.yml"))
	args := []interface{}{false}
	e.DeployContract(t, c, args)
	return c.Hash
}

func newSubnetInvoker(t *testing.T) *neotest.ContractInvoker {
	e := newExecutor(t)
	h := deploySubnetContract(t, e)
	return e.CommitteeInvoker(h)
}

func TestSubnet_Version(t *testing.T) {
	e := newSubnetInvoker(t)
	e.Invoke(t, common.Version, "version")
}

func TestSubnet_Put(t *testing.T) {
	e := newSubnetInvoker(t)

	acc := e.NewAccount(t)
	pub, ok := vm.ParseSignatureContract(acc.Script())
	require.True(t, ok)

	id := make([]byte, 5)
	binary.LittleEndian.PutUint32(id, 123)
	info := randomBytes(10)

	e.InvokeFail(t, common.ErrWitnessFailed, "put", id, pub, info)

	cAcc := e.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "put", id, pub, info)

	cBoth := e.WithSigners(e.Committee, acc)
	cBoth.InvokeFail(t, subnet.ErrInvalidSubnetID, "put", []byte{1, 2, 3}, pub, info)
	cBoth.InvokeFail(t, subnet.ErrInvalidOwner, "put", id, pub[10:], info)
	cBoth.Invoke(t, stackitem.Null{}, "put", id, pub, info)
	cAcc.Invoke(t, stackitem.NewBuffer(info), "get", id)
	cBoth.InvokeFail(t, subnet.ErrAlreadyExists, "put", id, pub, info)
}

func TestSubnet_Delete(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	e.InvokeFail(t, common.ErrWitnessFailed, "delete", id)

	cAcc := e.WithSigners(owner)
	cAcc.InvokeFail(t, subnet.ErrInvalidSubnetID, "delete", []byte{1, 1, 1, 1})
	cAcc.Invoke(t, stackitem.Null{}, "delete", []byte{1, 1, 1, 1, 1})
	cAcc.Invoke(t, stackitem.Null{}, "delete", id)
	cAcc.InvokeFail(t, subnet.ErrNotExist, "get", id)
}

func TestSubnet_AddNodeAdmin(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "addNodeAdmin"

	e.InvokeFail(t, subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, admPub)
	e.InvokeFail(t, subnet.ErrInvalidAdmin, method, id, admPub[1:])
	e.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, admPub)

	cAdm := e.WithSigners(adm)
	cAdm.InvokeFail(t, common.ErrOwnerWitnessFailed, method, id, admPub)

	cOwner := e.WithSigners(owner)
	cOwner.Invoke(t, stackitem.Null{}, method, id, admPub)

	cOwner.Invoke(t, stackitem.Null{}, method, id, admPub)
}

func TestSubnet_RemoveNodeAdmin(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "removeNodeAdmin"

	e.InvokeFail(t, subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, admPub)
	e.InvokeFail(t, subnet.ErrInvalidAdmin, method, id, admPub[1:])
	e.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, admPub)

	cAdm := e.WithSigners(adm)
	cAdm.InvokeFail(t, common.ErrOwnerWitnessFailed, method, id, admPub)

	cOwner := e.WithSigners(owner)

	cOwner.Invoke(t, stackitem.Null{}, method, id, admPub)
	cOwner.Invoke(t, stackitem.Null{}, "addNodeAdmin", id, admPub)
	cOwner.Invoke(t, stackitem.Null{}, method, id, admPub)
	cOwner.Invoke(t, stackitem.Null{}, method, id, admPub)
}

func TestSubnet_AddNode(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	node := e.NewAccount(t)
	nodePub, ok := vm.ParseSignatureContract(node.Script())
	require.True(t, ok)

	const method = "addNode"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, nodePub)
	cOwn.InvokeFail(t, subnet.ErrInvalidNode, method, id, nodePub[1:])
	cOwn.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, nodePub)

	cOwn.Invoke(t, stackitem.Null{}, method, id, nodePub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, nodePub)
}

func TestSubnet_RemoveNode(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	node := e.NewAccount(t)
	nodePub, ok := vm.ParseSignatureContract(node.Script())
	require.True(t, ok)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "removeNode"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, nodePub)
	cOwn.InvokeFail(t, subnet.ErrInvalidNode, method, id, nodePub[1:])
	cOwn.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, nodePub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, nodePub)

	cOwn.Invoke(t, stackitem.Null{}, "addNode", id, nodePub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, nodePub)

	cAdm := cOwn.WithSigners(adm)

	cOwn.Invoke(t, stackitem.Null{}, "addNodeAdmin", id, admPub)
	cAdm.Invoke(t, stackitem.Null{}, method, id, nodePub)
}

func TestSubnet_NodeAllowed(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	node := e.NewAccount(t)
	nodePub, ok := vm.ParseSignatureContract(node.Script())
	require.True(t, ok)

	const method = "nodeAllowed"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, nodePub)
	cOwn.InvokeFail(t, subnet.ErrInvalidNode, method, id, nodePub[1:])
	cOwn.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, nodePub)
	cOwn.Invoke(t, stackitem.NewBool(false), method, id, nodePub)

	cOwn.Invoke(t, stackitem.Null{}, "addNode", id, nodePub)
	cOwn.Invoke(t, stackitem.NewBool(true), method, id, nodePub)
}

func TestSubnet_AddClientAdmin(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "addClientAdmin"

	groupId := randomBytes(5)

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, groupId, admPub)
	cOwn.InvokeFail(t, subnet.ErrInvalidAdmin, method, id, groupId, admPub[1:])
	cOwn.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, groupId, admPub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, admPub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, admPub)
}

func TestSubnet_RemoveClientAdmin(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "removeClientAdmin"

	groupId := randomBytes(5)

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, groupId, admPub)
	cOwn.InvokeFail(t, subnet.ErrInvalidAdmin, method, id, groupId, admPub[1:])
	cOwn.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, groupId, admPub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, admPub)
	cOwn.Invoke(t, stackitem.Null{}, "addClientAdmin", id, groupId, admPub)
	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, admPub)
}

func TestSubnet_AddUser(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	user := randomBytes(27)

	groupId := randomBytes(5)

	const method = "addUser"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, groupId, user)
	cOwn.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, groupId, user)

	cOwn.Invoke(t, stackitem.Null{}, "addClientAdmin", id, groupId, admPub)

	cAdm := e.WithSigners(adm)
	cAdm.Invoke(t, stackitem.Null{}, method, id, groupId, user)
	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, user)
}

func TestSubnet_RemoveUser(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	groupId := randomBytes(5)
	user := randomBytes(27)

	adm := e.NewAccount(t)
	admPub, ok := vm.ParseSignatureContract(adm.Script())
	require.True(t, ok)

	const method = "removeUser"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, subnet.ErrInvalidSubnetID, method, []byte{0, 0, 0, 0}, groupId, user)
	cOwn.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, groupId, user)

	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, user)
	cOwn.Invoke(t, stackitem.Null{}, "addUser", id, groupId, user)
	cOwn.Invoke(t, stackitem.Null{}, method, id, groupId, user)

	cAdm := cOwn.WithSigners(adm)

	cOwn.Invoke(t, stackitem.Null{}, "addClientAdmin", id, groupId, admPub)
	cAdm.Invoke(t, stackitem.Null{}, method, id, groupId, user)
}

func TestSubnet_UserAllowed(t *testing.T) {
	e := newSubnetInvoker(t)

	id, owner := createSubnet(t, e)

	groupId := randomBytes(5)

	user := randomBytes(27)

	const method = "userAllowed"

	cOwn := e.WithSigners(owner)
	cOwn.InvokeFail(t, subnet.ErrNotExist, method, []byte{0, 0, 0, 0, 0}, user)

	cOwn.Invoke(t, stackitem.NewBool(false), method, id, user)
	cOwn.Invoke(t, stackitem.Null{}, "addUser", id, groupId, user)
	cOwn.Invoke(t, stackitem.NewBool(true), method, id, user)
}

func createSubnet(t *testing.T, e *neotest.ContractInvoker) (id []byte, owner neotest.Signer) {
	var (
		ok  bool
		pub []byte
	)

	owner = e.NewAccount(t)
	pub, ok = vm.ParseSignatureContract(owner.Script())
	require.True(t, ok)

	id = make([]byte, 5)
	binary.LittleEndian.PutUint32(id, 123)
	info := randomBytes(10)

	cBoth := e.WithSigners(e.Committee, owner)
	cBoth.Invoke(t, stackitem.Null{}, "put", id, pub, info)

	return
}
