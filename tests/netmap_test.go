package tests

import (
	"math/big"
	"math/rand"
	"path"
	"strings"
	"testing"

	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/TrueCloudLab/frostfs-contract/container"
	"github.com/TrueCloudLab/frostfs-contract/netmap"
	"github.com/nspcc-dev/neo-go/pkg/encoding/bigint"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/stretchr/testify/require"
)

const netmapPath = "../netmap"

func deployNetmapContract(t *testing.T, e *neotest.Executor, addrBalance, addrContainer util.Uint160, config ...interface{}) util.Uint160 {
	_, pubs, ok := vm.ParseMultiSigContract(e.Committee.Script())
	require.True(t, ok)

	args := make([]interface{}, 5)
	args[0] = false
	args[1] = addrBalance
	args[2] = addrContainer
	args[3] = []interface{}{pubs[0]}
	args[4] = append([]interface{}{}, config...)

	c := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	e.DeployContract(t, c, args)
	return c.Hash
}

func newNetmapInvoker(t *testing.T, config ...interface{}) *neotest.ContractInvoker {
	e := newExecutor(t)

	ctrNNS := neotest.CompileFile(t, e.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
	ctrNetmap := neotest.CompileFile(t, e.CommitteeHash, netmapPath, path.Join(netmapPath, "config.yml"))
	ctrBalance := neotest.CompileFile(t, e.CommitteeHash, balancePath, path.Join(balancePath, "config.yml"))
	ctrContainer := neotest.CompileFile(t, e.CommitteeHash, containerPath, path.Join(containerPath, "config.yml"))

	e.DeployContract(t, ctrNNS, nil)
	deployContainerContract(t, e, ctrNetmap.Hash, ctrBalance.Hash, ctrNNS.Hash)
	deployBalanceContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)
	deployNetmapContract(t, e, ctrBalance.Hash, ctrContainer.Hash, config...)
	return e.CommitteeInvoker(ctrNetmap.Hash)
}

func TestDeploySetConfig(t *testing.T) {
	c := newNetmapInvoker(t, "SomeKey", "TheValue", container.AliasFeeKey, int64(123))
	c.Invoke(t, "TheValue", "config", "SomeKey")
	c.Invoke(t, stackitem.NewByteArray(bigint.ToBytes(big.NewInt(123))),
		"config", container.AliasFeeKey)
}

type testNodeInfo struct {
	signer neotest.SingleSigner
	pub    []byte
	raw    []byte
	state  netmap.NodeState
}

func dummyNodeInfo(acc neotest.Signer) testNodeInfo {
	ni := make([]byte, 66)
	rand.Read(ni)

	s := acc.(neotest.SingleSigner)
	pub := s.Account().PrivateKey().PublicKey().Bytes()
	copy(ni[2:], pub)
	return testNodeInfo{
		signer: s,
		pub:    pub,
		raw:    ni,
		state:  netmap.NodeStateOnline,
	}
}

func newStorageNode(t *testing.T, c *neotest.ContractInvoker) testNodeInfo {
	return dummyNodeInfo(c.NewAccount(t))
}

func TestAddPeer(t *testing.T) {
	c := newNetmapInvoker(t)

	acc := c.NewAccount(t)
	cAcc := c.WithSigners(acc)
	dummyInfo := dummyNodeInfo(acc)

	acc1 := c.NewAccount(t)
	cAcc1 := c.WithSigners(acc1)
	cAcc1.InvokeFail(t, common.ErrWitnessFailed, "addPeer", dummyInfo.raw)

	h := cAcc.Invoke(t, stackitem.Null{}, "addPeer", dummyInfo.raw)
	aer := cAcc.CheckHalt(t, h)
	require.Equal(t, 0, len(aer.Events))

	dummyInfo.raw[0] ^= 0xFF
	h = cAcc.Invoke(t, stackitem.Null{}, "addPeer", dummyInfo.raw)
	aer = cAcc.CheckHalt(t, h)
	require.Equal(t, 0, len(aer.Events))

	c.InvokeFail(t, common.ErrWitnessFailed, "addPeer", dummyInfo.raw)
	c.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo.raw)
}

func TestNewEpoch(t *testing.T) {
	rand.Seed(42)

	const epochCount = netmap.DefaultSnapshotCount * 2

	cNm := newNetmapInvoker(t)
	nodes := make([][]testNodeInfo, epochCount)
	for i := range nodes {
		size := rand.Int()%5 + 1
		arr := make([]testNodeInfo, size)
		for j := 0; j < size; j++ {
			arr[j] = newStorageNode(t, cNm)
		}
		nodes[i] = arr
	}

	for i := 0; i < epochCount; i++ {
		for _, tn := range nodes[i] {
			cNm.WithSigners(tn.signer).Invoke(t, stackitem.Null{}, "addPeer", tn.raw)
			cNm.Invoke(t, stackitem.Null{}, "addPeerIR", tn.raw)
		}

		if i > 0 {
			// Remove random nodes from the previous netmap.
			current := make([]testNodeInfo, 0, len(nodes[i])+len(nodes[i-1]))
			current = append(current, nodes[i]...)

			for j := range nodes[i-1] {
				if rand.Int()%3 == 0 {
					cNm.Invoke(t, stackitem.Null{}, "updateStateIR",
						int64(netmap.NodeStateOffline), nodes[i-1][j].pub)
				} else {
					current = append(current, nodes[i-1][j])
				}
			}
			nodes[i] = current
		}
		cNm.Invoke(t, stackitem.Null{}, "newEpoch", i+1)

		t.Logf("Epoch: %d, Netmap()", i)
		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[i])

		for j := 0; j <= i && j < netmap.DefaultSnapshotCount; j++ {
			t.Logf("Epoch: %d, diff: %d", i, j)
			checkSnapshotAt(t, j, cNm, nodes[i-j])
		}

		_, err = cNm.TestInvoke(t, "snapshot", netmap.DefaultSnapshotCount)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "incorrect diff"))

		_, err = cNm.TestInvoke(t, "snapshot", -1)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "incorrect diff"))
	}
}

func TestUpdateSnapshotCount(t *testing.T) {
	rand.Seed(42)

	require.True(t, netmap.DefaultSnapshotCount > 5) // sanity check, adjust tests if false.

	prepare := func(t *testing.T, cNm *neotest.ContractInvoker, epochCount int) [][]testNodeInfo {
		nodes := make([][]testNodeInfo, epochCount)
		nodes[0] = []testNodeInfo{newStorageNode(t, cNm)}
		cNm.Invoke(t, stackitem.Null{}, "addPeerIR", nodes[0][0].raw)
		cNm.Invoke(t, stackitem.Null{}, "newEpoch", 1)
		for i := 1; i < len(nodes); i++ {
			sn := newStorageNode(t, cNm)
			nodes[i] = append(nodes[i-1], sn)
			cNm.Invoke(t, stackitem.Null{}, "addPeerIR", sn.raw)
			cNm.Invoke(t, stackitem.Null{}, "newEpoch", i+1)
		}
		return nodes
	}

	t.Run("increase size, extend with nil", func(t *testing.T) {
		// Before: S-old .. S
		// After : S-old .. S nil nil ...
		const epochCount = netmap.DefaultSnapshotCount / 2

		cNm := newNetmapInvoker(t)
		nodes := prepare(t, cNm, epochCount)

		const newCount = netmap.DefaultSnapshotCount + 3
		cNm.Invoke(t, stackitem.Null{}, "updateSnapshotCount", newCount)

		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[epochCount-1])
		for i := 0; i < epochCount; i++ {
			checkSnapshotAt(t, i, cNm, nodes[epochCount-i-1])
		}
		for i := epochCount; i < newCount; i++ {
			checkSnapshotAt(t, i, cNm, nil)
		}
		_, err = cNm.TestInvoke(t, "snapshot", int64(newCount))
		require.Error(t, err)
	})
	t.Run("increase size, copy old snapshots", func(t *testing.T) {
		// Before: S-x .. S S-old ...
		// After : S-x .. S nil nil S-old ...
		const epochCount = netmap.DefaultSnapshotCount + netmap.DefaultSnapshotCount/2

		cNm := newNetmapInvoker(t)
		nodes := prepare(t, cNm, epochCount)

		const newCount = netmap.DefaultSnapshotCount + 3
		cNm.Invoke(t, stackitem.Null{}, "updateSnapshotCount", newCount)

		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[epochCount-1])
		for i := 0; i < newCount-3; i++ {
			checkSnapshotAt(t, i, cNm, nodes[epochCount-i-1])
		}
		for i := newCount - 3; i < newCount; i++ {
			checkSnapshotAt(t, i, cNm, nil)
		}
		_, err = cNm.TestInvoke(t, "snapshot", int64(newCount))
		require.Error(t, err)
	})
	t.Run("decrease size, small decrease", func(t *testing.T) {
		// Before: S-x .. S S-old ... ...
		// After : S-x .. S S-new ...
		const epochCount = netmap.DefaultSnapshotCount + netmap.DefaultSnapshotCount/2

		cNm := newNetmapInvoker(t)
		nodes := prepare(t, cNm, epochCount)

		const newCount = netmap.DefaultSnapshotCount/2 + 2
		cNm.Invoke(t, stackitem.Null{}, "updateSnapshotCount", newCount)

		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[epochCount-1])
		for i := 0; i < newCount; i++ {
			checkSnapshotAt(t, i, cNm, nodes[epochCount-i-1])
		}
		_, err = cNm.TestInvoke(t, "snapshot", int64(newCount))
		require.Error(t, err)
	})
	t.Run("decrease size, big decrease", func(t *testing.T) {
		// Before: S-x   ... ... S S-old ... ...
		// After : S-new ...  S
		const epochCount = netmap.DefaultSnapshotCount + netmap.DefaultSnapshotCount/2

		cNm := newNetmapInvoker(t)
		nodes := prepare(t, cNm, epochCount)

		const newCount = netmap.DefaultSnapshotCount/2 - 2
		cNm.Invoke(t, stackitem.Null{}, "updateSnapshotCount", newCount)

		s, err := cNm.TestInvoke(t, "netmap")
		require.NoError(t, err)
		require.Equal(t, 1, s.Len())
		checkSnapshot(t, s, nodes[epochCount-1])
		for i := 0; i < newCount; i++ {
			checkSnapshotAt(t, i, cNm, nodes[epochCount-i-1])
		}
		_, err = cNm.TestInvoke(t, "snapshot", int64(newCount))
		require.Error(t, err)
	})
}

func checkSnapshotAt(t *testing.T, epoch int, cNm *neotest.ContractInvoker, nodes []testNodeInfo) {
	s, err := cNm.TestInvoke(t, "snapshot", int64(epoch))
	require.NoError(t, err)
	require.Equal(t, 1, s.Len())
	checkSnapshot(t, s, nodes)
}

func checkSnapshot(t *testing.T, s *vm.Stack, nodes []testNodeInfo) {
	arr, ok := s.Pop().Value().([]stackitem.Item)
	require.True(t, ok, "expected array")
	require.Equal(t, len(nodes), len(arr), "expected %d nodes", len(nodes))

	actual := make([]netmap.Node, len(nodes))
	expected := make([]netmap.Node, len(nodes))
	for i := range nodes {
		n, ok := arr[i].Value().([]stackitem.Item)
		require.True(t, ok, "expected node struct")
		require.Equalf(t, 2, len(n), "expected %d field(s)", 2)

		require.IsType(t, []byte{}, n[0].Value())

		state, err := n[1].TryInteger()
		require.NoError(t, err)

		actual[i].BLOB = n[0].Value().([]byte)
		actual[i].State = netmap.NodeState(state.Int64())
		expected[i].BLOB = nodes[i].raw
		expected[i].State = nodes[i].state
	}

	require.ElementsMatch(t, expected, actual, "snapshot is different")
}

func TestUpdateStateIR(t *testing.T) {
	cNm := newNetmapInvoker(t)

	acc := cNm.NewAccount(t)
	pub := acc.(neotest.SingleSigner).Account().PrivateKey().PublicKey().Bytes()

	t.Run("can't move online, need addPeerIR", func(t *testing.T) {
		cNm.InvokeFail(t, "peer is missing", "updateStateIR", int64(netmap.NodeStateOnline), pub)
	})

	dummyInfo := dummyNodeInfo(acc)
	cNm.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo.raw)

	acc1 := cNm.NewAccount(t)
	dummyInfo1 := dummyNodeInfo(acc1)
	cNm.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo1.raw)

	t.Run("must be signed by the alphabet", func(t *testing.T) {
		cAcc := cNm.WithSigners(acc)
		cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "updateStateIR", int64(netmap.NodeStateOffline), pub)
	})
	t.Run("invalid state", func(t *testing.T) {
		cNm.InvokeFail(t, "unsupported state", "updateStateIR", int64(42), pub)
	})

	checkNetmapCandidates(t, cNm, 2)

	// Move the first node offline.
	cNm.Invoke(t, stackitem.Null{}, "updateStateIR", int64(netmap.NodeStateOffline), pub)
	checkNetmapCandidates(t, cNm, 1)

	checkState := func(expected netmap.NodeState) {
		arr := checkNetmapCandidates(t, cNm, 1)
		nn := arr[0].Value().([]stackitem.Item)
		state, err := nn[1].TryInteger()
		require.NoError(t, err)
		require.Equal(t, int64(expected), state.Int64())
	}

	// Move the second node in the maintenance state.
	pub1 := acc1.(neotest.SingleSigner).Account().PrivateKey().PublicKey().Bytes()
	t.Run("maintenance -> add peer", func(t *testing.T) {
		cNm.Invoke(t, stackitem.Null{}, "updateStateIR", int64(netmap.NodeStateMaintenance), pub1)
		checkState(netmap.NodeStateMaintenance)
		cNm.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo1.raw)
		checkState(netmap.NodeStateOnline)
	})
	t.Run("maintenance -> online", func(t *testing.T) {
		cNm.Invoke(t, stackitem.Null{}, "updateStateIR", int64(netmap.NodeStateMaintenance), pub1)
		checkState(netmap.NodeStateMaintenance)
		cNm.Invoke(t, stackitem.Null{}, "updateStateIR", int64(netmap.NodeStateOnline), pub1)
		checkState(netmap.NodeStateOnline)
	})
}

func TestUpdateState(t *testing.T) {
	cNm := newNetmapInvoker(t)

	accs := []neotest.Signer{cNm.NewAccount(t), cNm.NewAccount(t)}
	pubs := make([][]byte, len(accs))
	for i := range accs {
		dummyInfo := dummyNodeInfo(accs[i])
		cNm.Invoke(t, stackitem.Null{}, "addPeerIR", dummyInfo.raw)
		pubs[i] = accs[i].(neotest.SingleSigner).Account().PrivateKey().PublicKey().Bytes()
	}

	t.Run("missing witness", func(t *testing.T) {
		cAcc := cNm.WithSigners(accs[0])
		cNm.InvokeFail(t, common.ErrWitnessFailed, "updateState", int64(2), pubs[0])
		cAcc.InvokeFail(t, common.ErrAlphabetWitnessFailed, "updateState", int64(2), pubs[0])
		cAcc.InvokeFail(t, common.ErrWitnessFailed, "updateState", int64(2), pubs[1])
	})

	checkNetmapCandidates(t, cNm, 2)

	cBoth := cNm.WithSigners(accs[0], cNm.Committee)

	cBoth.Invoke(t, stackitem.Null{}, "updateState", int64(2), pubs[0])
	checkNetmapCandidates(t, cNm, 1)

	t.Run("remove already removed node", func(t *testing.T) {
		cBoth.Invoke(t, stackitem.Null{}, "updateState", int64(2), pubs[0])
		checkNetmapCandidates(t, cNm, 1)
	})

	cBoth = cNm.WithSigners(accs[1], cNm.Committee)
	cBoth.Invoke(t, stackitem.Null{}, "updateState", int64(2), pubs[1])
	checkNetmapCandidates(t, cNm, 0)
}

func checkNetmapCandidates(t *testing.T, c *neotest.ContractInvoker, size int) []stackitem.Item {
	s, err := c.TestInvoke(t, "netmapCandidates")
	require.NoError(t, err)
	require.Equal(t, 1, s.Len())

	arr, ok := s.Pop().Value().([]stackitem.Item)
	require.True(t, ok)
	require.Equal(t, size, len(arr))
	return arr
}
