package tests

import (
	"path"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

const processingPath = "../processing"

func deployProcessingContract(t *testing.T, e *neotest.Executor, addrFrostFS util.Uint160) util.Uint160 {
	c := neotest.CompileFile(t, e.CommitteeHash, processingPath, path.Join(processingPath, "config.yml"))

	args := make([]interface{}, 1)
	args[0] = addrFrostFS

	e.DeployContract(t, c, args)
	return c.Hash
}

func newProcessingInvoker(t *testing.T) (*neotest.ContractInvoker, neotest.Signer) {
	frostfsInvoker, irMultiAcc, _ := newFrostFSInvoker(t, 2)
	hash := deployProcessingContract(t, frostfsInvoker.Executor, frostfsInvoker.Hash)

	return frostfsInvoker.CommitteeInvoker(hash), irMultiAcc
}

func TestVerify_Processing(t *testing.T) {
	c, irMultiAcc := newProcessingInvoker(t)

	const method = "verify"

	cIR := c.WithSigners(irMultiAcc)

	cIR.Invoke(t, stackitem.NewBool(true), method)
	c.Invoke(t, stackitem.NewBool(false), method)
}
