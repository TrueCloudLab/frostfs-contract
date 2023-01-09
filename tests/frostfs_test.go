package tests

import (
	"bytes"
	"path"
	"sort"
	"testing"

	"github.com/TrueCloudLab/frostfs-contract/frostfs"
	"github.com/nspcc-dev/neo-go/pkg/core/native/nativenames"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/stretchr/testify/require"
)

const frostfsPath = "../frostfs"

func deployFrostFSContract(t *testing.T, e *neotest.Executor, addrProc util.Uint160,
	pubs keys.PublicKeys, config ...interface{}) util.Uint160 {
	args := make([]interface{}, 5)
	args[0] = false
	args[1] = addrProc

	arr := make([]interface{}, len(pubs))
	for i := range pubs {
		arr[i] = pubs[i].Bytes()
	}
	args[2] = arr
	args[3] = append([]interface{}{}, config...)

	c := neotest.CompileFile(t, e.CommitteeHash, frostfsPath, path.Join(frostfsPath, "config.yml"))
	e.DeployContract(t, c, args)
	return c.Hash
}

func newFrostFSInvoker(t *testing.T, n int, config ...interface{}) (*neotest.ContractInvoker, neotest.Signer, keys.PublicKeys) {
	e := newExecutor(t)

	accounts := make([]*wallet.Account, n)
	for i := 0; i < n; i++ {
		acc, err := wallet.NewAccount()
		require.NoError(t, err)

		accounts[i] = acc
	}

	sort.Slice(accounts, func(i, j int) bool {
		p1 := accounts[i].PrivateKey().PublicKey()
		p2 := accounts[j].PrivateKey().PublicKey()
		return p1.Cmp(p2) == -1
	})

	pubs := make(keys.PublicKeys, n)
	for i := range accounts {
		pubs[i] = accounts[i].PrivateKey().PublicKey()
	}

	m := smartcontract.GetMajorityHonestNodeCount(len(accounts))
	for i := range accounts {
		require.NoError(t, accounts[i].ConvertMultisig(m, pubs.Copy()))
	}

	alphabet := neotest.NewMultiSigner(accounts...)
	h := deployFrostFSContract(t, e, util.Uint160{}, pubs, config...)

	gasHash, err := e.Chain.GetNativeContractScriptHash(nativenames.Gas)
	require.NoError(t, err)

	vc := e.CommitteeInvoker(gasHash).WithSigners(e.Validator)
	vc.Invoke(t, true, "transfer",
		e.Validator.ScriptHash(), alphabet.ScriptHash(),
		int64(10_0000_0000), nil)

	return e.CommitteeInvoker(h).WithSigners(alphabet), alphabet, pubs
}

func TestFrostFS_AlphabetList(t *testing.T) {
	const alphabetSize = 4

	e, _, pubs := newFrostFSInvoker(t, alphabetSize)
	arr := make([]stackitem.Item, len(pubs))
	for i := range arr {
		arr[i] = stackitem.NewStruct([]stackitem.Item{
			stackitem.NewByteArray(pubs[i].Bytes()),
		})
	}

	e.Invoke(t, stackitem.NewArray(arr), "alphabetList")
}

func TestFrostFS_InnerRingCandidate(t *testing.T) {
	e, _, _ := newFrostFSInvoker(t, 4, frostfs.CandidateFeeConfigKey, int64(10))

	const candidateCount = 3

	accs := make([]neotest.Signer, candidateCount)
	for i := range accs {
		accs[i] = e.NewAccount(t)
	}

	arr := make([]stackitem.Item, candidateCount)
	pubs := make([][]byte, candidateCount)
	sort.Slice(accs, func(i, j int) bool {
		s1 := accs[i].Script()
		s2 := accs[j].Script()
		return bytes.Compare(s1, s2) == -1
	})

	for i, acc := range accs {
		cAcc := e.WithSigners(acc)
		pub, ok := vm.ParseSignatureContract(acc.Script())
		require.True(t, ok)
		cAcc.Invoke(t, stackitem.Null{}, "innerRingCandidateAdd", pub)
		cAcc.InvokeFail(t, "candidate already in the list", "innerRingCandidateAdd", pub)

		pubs[i] = pub
		arr[i] = stackitem.NewStruct([]stackitem.Item{stackitem.NewBuffer(pub)})
	}

	e.Invoke(t, stackitem.NewArray(arr), "innerRingCandidates")

	cAcc := e.WithSigners(accs[1])
	cAcc.Invoke(t, stackitem.Null{}, "innerRingCandidateRemove", pubs[1])
	e.Invoke(t, stackitem.NewArray([]stackitem.Item{arr[0], arr[2]}), "innerRingCandidates")

	cAcc = e.WithSigners(accs[2])
	cAcc.Invoke(t, stackitem.Null{}, "innerRingCandidateRemove", pubs[2])
	e.Invoke(t, stackitem.NewArray([]stackitem.Item{arr[0]}), "innerRingCandidates")

	cAcc = e.WithSigners(accs[0])
	cAcc.Invoke(t, stackitem.Null{}, "innerRingCandidateRemove", pubs[0])
	e.Invoke(t, stackitem.NewArray([]stackitem.Item{}), "innerRingCandidates")
}
