package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
)

// SetSerialized serializes data and puts it into contract storage.
func SetSerialized(ctx storage.Context, key interface{}, value interface{}) {
	data := std.Serialize(value)
	storage.Put(ctx, key, data)
}
