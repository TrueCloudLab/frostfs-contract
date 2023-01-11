package tests

import (
	"bytes"
	"path"
	"sort"
	"testing"

	"github.com/TrueCloudLab/frostfs-contract/container"
	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/stretchr/testify/require"
)

const frostfsidPath = "../frostfsid"

func deployFrostFSIDContract(t *testing.T, e *neotest.Executor, addrNetmap, addrContainer util.Uint160) util.Uint160 {
	args := make([]interface{}, 5)
	args[0] = false
	args[1] = addrNetmap
	args[2] = addrContainer

	c := neotest.CompileFile(t, e.CommitteeHash, frostfsidPath, path.Join(frostfsidPath, "config.yml"))
	e.DeployContract(t, c, args)
	return c.Hash
}

func newFrostFSIDInvoker(t *testing.T) *neotest.ContractInvoker {
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
	h := deployFrostFSIDContract(t, e, ctrNetmap.Hash, ctrContainer.Hash)
	return e.CommitteeInvoker(h)
}

func TestFrostFSID_AddKey(t *testing.T) {
	e := newFrostFSIDInvoker(t)

	pubs := make([][]byte, 6)
	for i := range pubs {
		p, err := keys.NewPrivateKey()
		require.NoError(t, err)
		pubs[i] = p.PublicKey().Bytes()
	}
	acc := e.NewAccount(t)
	owner, _ := base58.Decode(address.Uint160ToString(acc.ScriptHash()))
	e.Invoke(t, stackitem.Null{}, "addKey", owner,
		[]interface{}{pubs[0], pubs[1]})

	sort.Slice(pubs[:2], func(i, j int) bool {
		return bytes.Compare(pubs[i], pubs[j]) == -1
	})
	arr := []stackitem.Item{
		stackitem.NewBuffer(pubs[0]),
		stackitem.NewBuffer(pubs[1]),
	}
	e.Invoke(t, stackitem.NewArray(arr), "key", owner)

	t.Run("multiple addKey per block", func(t *testing.T) {
		tx1 := e.PrepareInvoke(t, "addKey", owner, []interface{}{pubs[2]})
		tx2 := e.PrepareInvoke(t, "addKey", owner, []interface{}{pubs[3], pubs[4]})
		e.AddNewBlock(t, tx1, tx2)
		e.CheckHalt(t, tx1.Hash(), stackitem.Null{})
		e.CheckHalt(t, tx2.Hash(), stackitem.Null{})

		sort.Slice(pubs[:5], func(i, j int) bool {
			return bytes.Compare(pubs[i], pubs[j]) == -1
		})
		arr = []stackitem.Item{
			stackitem.NewBuffer(pubs[0]),
			stackitem.NewBuffer(pubs[1]),
			stackitem.NewBuffer(pubs[2]),
			stackitem.NewBuffer(pubs[3]),
			stackitem.NewBuffer(pubs[4]),
		}
		e.Invoke(t, stackitem.NewArray(arr), "key", owner)
	})

	e.Invoke(t, stackitem.Null{}, "removeKey", owner,
		[]interface{}{pubs[1], pubs[5]})
	arr = []stackitem.Item{
		stackitem.NewBuffer(pubs[0]),
		stackitem.NewBuffer(pubs[2]),
		stackitem.NewBuffer(pubs[3]),
		stackitem.NewBuffer(pubs[4]),
	}
	e.Invoke(t, stackitem.NewArray(arr), "key", owner)

	t.Run("multiple removeKey per block", func(t *testing.T) {
		tx1 := e.PrepareInvoke(t, "removeKey", owner, []interface{}{pubs[2]})
		tx2 := e.PrepareInvoke(t, "removeKey", owner, []interface{}{pubs[0], pubs[4]})
		e.AddNewBlock(t, tx1, tx2)
		e.CheckHalt(t, tx1.Hash(), stackitem.Null{})
		e.CheckHalt(t, tx2.Hash(), stackitem.Null{})

		arr = []stackitem.Item{stackitem.NewBuffer(pubs[3])}
		e.Invoke(t, stackitem.NewArray(arr), "key", owner)
	})
}
