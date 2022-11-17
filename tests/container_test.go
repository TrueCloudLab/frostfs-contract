package tests

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"path"
	"testing"

	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/TrueCloudLab/frostfs-contract/container"
	"github.com/TrueCloudLab/frostfs-contract/nns"
	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/stretchr/testify/require"
)

const containerPath = "../container"

const (
	containerFee      = 0_0100_0000
	containerAliasFee = 0_0050_0000
)

func deployContainerContract(t *testing.T, e *neotest.Executor, addrNetmap, addrBalance, addrNNS util.Uint160) util.Uint160 {
	args := make([]interface{}, 6)
	args[0] = int64(0)
	args[1] = addrNetmap
	args[2] = addrBalance
	args[3] = util.Uint160{} // not needed for now
	args[4] = addrNNS
	args[5] = "frostfs"

	c := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))
	e.DeployContract(t, c, args)
	return c.Hash
}

func newContainerInvoker(t *testing.T) (*neotest.ContractInvoker, *neotest.ContractInvoker, *neotest.ContractInvoker) {
	e := newExecutor(t)

	ctrNNS := neotest.CompileFile(t, e.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))

	e.DeployContract(t, ctrNNS, nil)
	deployNetmapContract(t, e, ctrBalance.Hash, ctrContainer.Hash,
		container.RegistrationFeeKey, int64(containerFee),
		container.AliasFeeKey, int64(containerAliasFee))
	deployBalanceContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)
	deployContainerContract(t, e, ctrNetmap.Hash, ctrBalance.Hash, ctrNNS.Hash)
	return e.CommitteeInvoker(ctrContainer.Hash), e.CommitteeInvoker(ctrBalance.Hash), e.CommitteeInvoker(ctrNetmap.Hash)
}

func setContainerOwner(c []byte, acc neotest.Signer) {
	owner, _ := base58.Decode(address.Uint160ToString(acc.ScriptHash()))
	copy(c[6:], owner)
}

type testContainer struct {
	id                     [32]byte
	value, sig, pub, token []byte
}

func dummyContainer(owner neotest.Signer) testContainer {
	value := randomBytes(100)
	value[1] = 0 // zero offset
	setContainerOwner(value, owner)

	return testContainer{
		id:    sha256.Sum256(value),
		value: value,
		sig:   randomBytes(64),
		pub:   randomBytes(33),
		token: randomBytes(42),
	}
}

func TestContainerCount(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t)

	checkCount := func(t *testing.T, expected int64) {
		s, err := c.TestInvoke(t, "count")
		require.NoError(t, err)
		bi := s.Pop().BigInt()
		require.True(t, bi.IsInt64())
		require.Equal(t, int64(expected), bi.Int64())
	}

	checkCount(t, 0)
	acc1, cnt1 := addContainer(t, c, cBal)
	checkCount(t, 1)

	_, cnt2 := addContainer(t, c, cBal)
	checkCount(t, 2)

	// Same owner.
	cnt3 := dummyContainer(acc1)
	balanceMint(t, cBal, acc1, containerFee*1, []byte{})
	c.Invoke(t, stackitem.Null{}, "put", cnt3.value, cnt3.sig, cnt3.pub, cnt3.token)

	c.Invoke(t, stackitem.Null{}, "delete", cnt1.id[:], cnt1.sig, cnt1.token)
	checkCount(t, 2)

	c.Invoke(t, stackitem.Null{}, "delete", cnt2.id[:], cnt2.sig, cnt2.token)
	checkCount(t, 1)

	c.Invoke(t, stackitem.Null{}, "delete", cnt3.id[:], cnt3.sig, cnt3.token)
	checkCount(t, 0)
}

func TestContainerPut(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t)

	acc := c.NewAccount(t)
	cnt := dummyContainer(acc)

	putArgs := []interface{}{cnt.value, cnt.sig, cnt.pub, cnt.token}
	c.InvokeFail(t, "insufficient balance to create container", "put", putArgs...)

	balanceMint(t, cBal, acc, containerFee*1, []byte{})

	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "put", putArgs...)

	c.Invoke(t, stackitem.Null{}, "put", putArgs...)

	t.Run("with nice names", func(t *testing.T) {
		ctrNNS := neotest.CompileFile(t, c.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
		nnsHash := ctrNNS.Hash

		balanceMint(t, cBal, acc, containerFee*1, []byte{})

		putArgs := []interface{}{cnt.value, cnt.sig, cnt.pub, cnt.token, "mycnt", ""}
		t.Run("no fee for alias", func(t *testing.T) {
			c.InvokeFail(t, "insufficient balance to create container", "putNamed", putArgs...)
		})

		balanceMint(t, cBal, acc, containerAliasFee*1, []byte{})
		c.Invoke(t, stackitem.Null{}, "putNamed", putArgs...)

		expected := stackitem.NewArray([]stackitem.Item{
			stackitem.NewByteArray([]byte(base58.Encode(cnt.id[:]))),
		})
		cNNS := c.CommitteeInvoker(nnsHash)
		cNNS.Invoke(t, expected, "resolve", "mycnt.frostfs", int64(nns.TXT))

		t.Run("name is already taken", func(t *testing.T) {
			c.InvokeFail(t, "name is already taken", "putNamed", putArgs...)
		})

		c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)
		cNNS.Invoke(t, stackitem.Null{}, "resolve", "mycnt.frostfs", int64(nns.TXT))

		t.Run("register in advance", func(t *testing.T) {
			cnt.value[len(cnt.value)-1] = 10
			cnt.id = sha256.Sum256(cnt.value)

			cNNS.Invoke(t, true, "register",
				"cdn", c.CommitteeHash,
				"whateveriwant@world.com", int64(0), int64(0), int64(100_000), int64(0))

			cNNS.Invoke(t, true, "register",
				"domain.cdn", c.CommitteeHash,
				"whateveriwant@world.com", int64(0), int64(0), int64(100_000), int64(0))

			balanceMint(t, cBal, acc, (containerFee+containerAliasFee)*1, []byte{})

			putArgs := []interface{}{cnt.value, cnt.sig, cnt.pub, cnt.token, "domain", "cdn"}
			c2 := c.WithSigners(c.Committee, acc)
			c2.Invoke(t, stackitem.Null{}, "putNamed", putArgs...)

			expected = stackitem.NewArray([]stackitem.Item{
				stackitem.NewByteArray([]byte(base58.Encode(cnt.id[:])))})
			cNNS.Invoke(t, expected, "resolve", "domain.cdn", int64(nns.TXT))
		})
	})
}

func addContainer(t *testing.T, c, cBal *neotest.ContractInvoker) (neotest.Signer, testContainer) {
	acc := c.NewAccount(t)
	cnt := dummyContainer(acc)

	balanceMint(t, cBal, acc, containerFee*1, []byte{})
	c.Invoke(t, stackitem.Null{}, "put", cnt.value, cnt.sig, cnt.pub, cnt.token)
	return acc, cnt
}

func TestContainerDelete(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t)

	acc, cnt := addContainer(t, c, cBal)
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "delete",
		cnt.id[:], cnt.sig, cnt.token)

	c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.Invoke(t, stackitem.Null{}, "delete", cnt.id[:], cnt.sig, cnt.token)
	})

	c.InvokeFail(t, container.NotFoundError, "get", cnt.id[:])
}

func TestContainerOwner(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t)

	acc, cnt := addContainer(t, c, cBal)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.InvokeFail(t, container.NotFoundError, "owner", id[:])
	})

	owner, _ := base58.Decode(address.Uint160ToString(acc.ScriptHash()))
	c.Invoke(t, stackitem.NewBuffer(owner), "owner", cnt.id[:])
}

func TestContainerGet(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t)

	_, cnt := addContainer(t, c, cBal)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		c.InvokeFail(t, container.NotFoundError, "get", id[:])
	})

	expected := stackitem.NewStruct([]stackitem.Item{
		stackitem.NewByteArray(cnt.value),
		stackitem.NewByteArray(cnt.sig),
		stackitem.NewByteArray(cnt.pub),
		stackitem.NewByteArray(cnt.token),
	})
	c.Invoke(t, expected, "get", cnt.id[:])
}

type eacl struct {
	value []byte
	sig   []byte
	pub   []byte
	token []byte
}

func dummyEACL(containerID [32]byte) eacl {
	e := make([]byte, 50)
	copy(e[6:], containerID[:])
	return eacl{
		value: e,
		sig:   randomBytes(64),
		pub:   randomBytes(33),
		token: randomBytes(42),
	}
}

func TestContainerSetEACL(t *testing.T) {
	c, cBal, _ := newContainerInvoker(t)

	acc, cnt := addContainer(t, c, cBal)

	t.Run("missing container", func(t *testing.T) {
		id := cnt.id
		id[0] ^= 0xFF
		e := dummyEACL(id)
		c.InvokeFail(t, container.NotFoundError, "setEACL", e.value, e.sig, e.pub, e.token)
	})

	e := dummyEACL(cnt.id)
	setArgs := []interface{}{e.value, e.sig, e.pub, e.token}
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "setEACL", setArgs...)

	c.Invoke(t, stackitem.Null{}, "setEACL", setArgs...)

	expected := stackitem.NewStruct([]stackitem.Item{
		stackitem.NewByteArray(e.value),
		stackitem.NewByteArray(e.sig),
		stackitem.NewByteArray(e.pub),
		stackitem.NewByteArray(e.token),
	})
	c.Invoke(t, expected, "eACL", cnt.id[:])
}

func TestContainerSizeEstimation(t *testing.T) {
	c, cBal, cNm := newContainerInvoker(t)

	_, cnt := addContainer(t, c, cBal)
	nodes := []testNodeInfo{
		newStorageNode(t, c),
		newStorageNode(t, c),
		newStorageNode(t, c),
	}
	for i := range nodes {
		cNm.WithSigners(nodes[i].signer).Invoke(t, stackitem.Null{}, "addPeer", nodes[i].raw)
		cNm.Invoke(t, stackitem.Null{}, "addPeerIR", nodes[i].raw)
	}

	// putContainerSize retrieves storage nodes from the previous snapshot,
	// so epoch must be incremented twice.
	cNm.Invoke(t, stackitem.Null{}, "newEpoch", int64(1))
	cNm.Invoke(t, stackitem.Null{}, "newEpoch", int64(2))

	t.Run("must be witnessed by key in the argument", func(t *testing.T) {
		c.WithSigners(nodes[1].signer).InvokeFail(t, common.ErrWitnessFailed, "putContainerSize",
			int64(2), cnt.id[:], int64(123), nodes[0].pub)
	})

	c.WithSigners(nodes[0].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
		int64(2), cnt.id[:], int64(123), nodes[0].pub)
	estimations := []estimation{{nodes[0].pub, 123}}
	checkEstimations(t, c, 2, cnt, estimations...)

	c.WithSigners(nodes[1].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
		int64(2), cnt.id[:], int64(42), nodes[1].pub)
	estimations = append(estimations, estimation{nodes[1].pub, int64(42)})
	checkEstimations(t, c, 2, cnt, estimations...)

	t.Run("add estimation for a different epoch", func(t *testing.T) {
		c.WithSigners(nodes[2].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
			int64(1), cnt.id[:], int64(777), nodes[2].pub)
		checkEstimations(t, c, 1, cnt, estimation{nodes[2].pub, 777})
		checkEstimations(t, c, 2, cnt, estimations...)
	})

	c.WithSigners(nodes[2].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
		int64(3), cnt.id[:], int64(888), nodes[2].pub)
	checkEstimations(t, c, 3, cnt, estimation{nodes[2].pub, 888})

	// Remove old estimations.
	for i := int64(1); i <= container.CleanupDelta; i++ {
		cNm.Invoke(t, stackitem.Null{}, "newEpoch", 2+i)
		checkEstimations(t, c, 2, cnt, estimations...)
		checkEstimations(t, c, 3, cnt, estimation{nodes[2].pub, 888})
	}

	epoch := int64(2 + container.CleanupDelta + 1)
	cNm.Invoke(t, stackitem.Null{}, "newEpoch", epoch)
	checkEstimations(t, c, 2, cnt, estimations...) // not yet removed
	checkEstimations(t, c, 3, cnt, estimation{nodes[2].pub, 888})

	c.WithSigners(nodes[1].signer).Invoke(t, stackitem.Null{}, "putContainerSize",
		epoch, cnt.id[:], int64(999), nodes[1].pub)

	checkEstimations(t, c, 2, cnt, estimations[:1]...)
	checkEstimations(t, c, epoch, cnt, estimation{nodes[1].pub, int64(999)})

	// Estimation from node 0 should be cleaned during epoch tick.
	for i := int64(1); i <= container.TotalCleanupDelta-container.CleanupDelta; i++ {
		cNm.Invoke(t, stackitem.Null{}, "newEpoch", epoch+i)
	}
	checkEstimations(t, c, 2, cnt)
	checkEstimations(t, c, epoch, cnt, estimation{nodes[1].pub, int64(999)})
}

type estimation struct {
	from []byte
	size int64
}

func checkEstimations(t *testing.T, c *neotest.ContractInvoker, epoch int64, cnt testContainer, estimations ...estimation) {
	// Check that listed estimations match expected
	listEstimations := getListEstimations(t, c, epoch, cnt)
	requireEstimationsMatch(t, estimations, listEstimations)

	// Check that iterated estimations match expected
	iterEstimations := getIterEstimations(t, c, epoch)
	requireEstimationsMatch(t, estimations, iterEstimations)
}

func getListEstimations(t *testing.T, c *neotest.ContractInvoker, epoch int64, cnt testContainer) []estimation {
	s, err := c.TestInvoke(t, "listContainerSizes", epoch)
	require.NoError(t, err)

	var id []byte

	// When there are no estimations, listContainerSizes can also return nothing.
	item := s.Top().Item()
	switch it := item.(type) {
	case stackitem.Null:
		require.Equal(t, stackitem.Null{}, it)
		return make([]estimation, 0)
	case *stackitem.Array:
		id, err = it.Value().([]stackitem.Item)[0].TryBytes()
		require.NoError(t, err)
	default:
		require.FailNow(t, "invalid return type for listContainerSizes")
	}

	s, err = c.TestInvoke(t, "getContainerSize", id)
	require.NoError(t, err)

	// Here and below we assume that all estimations in the contract are related to our container
	sizes := s.Top().Array()
	require.Equal(t, cnt.id[:], sizes[0].Value())

	return convertStackToEstimations(sizes[1].Value().([]stackitem.Item))
}

func getIterEstimations(t *testing.T, c *neotest.ContractInvoker, epoch int64) []estimation {
	iterStack, err := c.TestInvoke(t, "iterateContainerSizes", epoch)
	require.NoError(t, err)
	iter := iterStack.Pop().Value().(*storage.Iterator)

	// Iterator contains pairs: key + estimation (as stack item), we extract estimations only
	pairs := iteratorToArray(iter)
	estimationItems := make([]stackitem.Item, len(pairs))
	for i, pair := range pairs {
		pairItems := pair.Value().([]stackitem.Item)
		estimationItems[i] = pairItems[1]
	}

	return convertStackToEstimations(estimationItems)
}

func convertStackToEstimations(stackItems []stackitem.Item) []estimation {
	estimations := make([]estimation, 0, len(stackItems))
	for _, item := range stackItems {
		value := item.Value().([]stackitem.Item)
		from := value[0].Value().([]byte)
		size := value[1].Value().(*big.Int)

		estimation := estimation{from: from, size: size.Int64()}
		estimations = append(estimations, estimation)
	}
	return estimations
}

func requireEstimationsMatch(t *testing.T, expected []estimation, actual []estimation) {
	require.Equal(t, len(expected), len(actual))
	for _, e := range expected {
		found := false
		for _, a := range actual {
			if found = bytes.Equal(e.from, a.from); found {
				require.Equal(t, e.size, a.size)
				break
			}
		}
		require.True(t, found, "expected estimation from %x to be present", e.from)
	}
}
