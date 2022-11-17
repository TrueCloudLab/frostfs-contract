package tests

import (
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/neotest/chain"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

func iteratorToArray(iter *storage.Iterator) []stackitem.Item {
	stackItems := make([]stackitem.Item, 0)
	for iter.Next() {
		stackItems = append(stackItems, iter.Value())
	}
	return stackItems
}

func newExecutor(t *testing.T) *neotest.Executor {
	bc, acc := chain.NewSingle(t)
	return neotest.NewExecutor(t, bc, acc, acc)
}
