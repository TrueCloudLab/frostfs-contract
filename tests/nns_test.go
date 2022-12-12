package tests

import (
	"fmt"
	"math/big"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/TrueCloudLab/frostfs-contract/nns"
	"github.com/nspcc-dev/neo-go/pkg/core/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/stretchr/testify/require"
)

const nnsPath = "../nns"

const msPerYear = 365 * 24 * time.Hour / time.Millisecond

func newNNSInvoker(t *testing.T, addRoot bool) *neotest.ContractInvoker {
	e := newExecutor(t)
	ctr := neotest.CompileFile(t, e.CommitteeHash, nnsPath, path.Join(nnsPath, "config.yml"))
	e.DeployContract(t, ctr, nil)

	c := e.CommitteeInvoker(ctr.Hash)
	if addRoot {
		// Set expiration big enough to pass all tests.
		refresh, retry, expire, ttl := int64(101), int64(102), int64(msPerYear/1000*100), int64(104)
		c.Invoke(t, true, "register",
			"com", c.CommitteeHash,
			"myemail@nspcc.ru", refresh, retry, expire, ttl)
	}
	return c
}

func TestNNSGeneric(t *testing.T) {
	c := newNNSInvoker(t, false)

	c.Invoke(t, "NNS", "symbol")
	c.Invoke(t, 0, "decimals")
	c.Invoke(t, 0, "totalSupply")
}

func TestNNSRegisterTLD(t *testing.T) {
	c := newNNSInvoker(t, false)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)

	c.InvokeFail(t, "invalid domain name format", "register",
		"0com", c.CommitteeHash,
		"email@nspcc.ru", refresh, retry, expire, ttl)

	acc := c.NewAccount(t)
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, "not witnessed by committee", "register",
		"com", acc.ScriptHash(),
		"email@nspcc.ru", refresh, retry, expire, ttl)

	c.Invoke(t, true, "register",
		"com", c.CommitteeHash,
		"email@nspcc.ru", refresh, retry, expire, ttl)

	c.InvokeFail(t, "TLD already exists", "register",
		"com", c.CommitteeHash,
		"email@nspcc.ru", refresh, retry, expire, ttl)
}

func TestNNSRegister(t *testing.T) {
	c := newNNSInvoker(t, false)

	accTop := c.NewAccount(t)
	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c1 := c.WithSigners(c.Committee, accTop)
	c1.Invoke(t, true, "register",
		"com", accTop.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	acc := c.NewAccount(t)
	c2 := c.WithSigners(c.Committee, acc)
	c2.InvokeFail(t, "not witnessed by admin", "register",
		"testdomain.com", acc.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	c3 := c.WithSigners(accTop, acc)
	t.Run("domain names with hyphen", func(t *testing.T) {
		c3.InvokeFail(t, "invalid domain name format", "register",
			"-testdomain.com", acc.ScriptHash(),
			"myemail@nspcc.ru", refresh, retry, expire, ttl)
		c3.InvokeFail(t, "invalid domain name format", "register",
			"testdomain-.com", acc.ScriptHash(),
			"myemail@nspcc.ru", refresh, retry, expire, ttl)
		c3.Invoke(t, true, "register",
			"test-domain.com", acc.ScriptHash(),
			"myemail@nspcc.ru", refresh, retry, expire, ttl)
	})
	c3.Invoke(t, true, "register",
		"testdomain.com", acc.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	b := c.TopBlock(t)
	expected := stackitem.NewArray([]stackitem.Item{stackitem.NewBuffer(
		[]byte(fmt.Sprintf("testdomain.com myemail@nspcc.ru %d %d %d %d %d",
			b.Timestamp, refresh, retry, expire, ttl)))})
	c.Invoke(t, expected, "getRecords", "testdomain.com", int64(nns.SOA))

	cAcc := c.WithSigners(acc)
	cAcc.Invoke(t, stackitem.Null{}, "addRecord",
		"testdomain.com", int64(nns.TXT), "first TXT record")
	cAcc.InvokeFail(t, "record already exists", "addRecord",
		"testdomain.com", int64(nns.TXT), "first TXT record")
	cAcc.Invoke(t, stackitem.Null{}, "addRecord",
		"testdomain.com", int64(nns.TXT), "second TXT record")

	expected = stackitem.NewArray([]stackitem.Item{
		stackitem.NewByteArray([]byte("first TXT record")),
		stackitem.NewByteArray([]byte("second TXT record"))})
	c.Invoke(t, expected, "getRecords", "testdomain.com", int64(nns.TXT))

	cAcc.Invoke(t, stackitem.Null{}, "setRecord",
		"testdomain.com", int64(nns.TXT), int64(0), "replaced first")

	expected = stackitem.NewArray([]stackitem.Item{
		stackitem.NewByteArray([]byte("replaced first")),
		stackitem.NewByteArray([]byte("second TXT record"))})
	c.Invoke(t, expected, "getRecords", "testdomain.com", int64(nns.TXT))
}

func TestTLDRecord(t *testing.T) {
	c := newNNSInvoker(t, true)
	c.Invoke(t, stackitem.Null{}, "addRecord",
		"com", int64(nns.A), "1.2.3.4")

	result := []stackitem.Item{stackitem.NewByteArray([]byte("1.2.3.4"))}
	c.Invoke(t, result, "resolve", "com", int64(nns.A))
}

func TestNNSRegisterMulti(t *testing.T) {
	c := newNNSInvoker(t, true)

	newArgs := func(domain string, account neotest.Signer) []interface{} {
		return []interface{}{
			domain, account.ScriptHash(), "doesnt@matter.com",
			int64(101), int64(102), int64(103), int64(104),
		}
	}
	acc := c.NewAccount(t)
	cBoth := c.WithSigners(c.Committee, acc)
	args := newArgs("neo.com", acc)
	cBoth.Invoke(t, true, "register", args...)

	c1 := c.WithSigners(acc)
	t.Run("parent domain is missing", func(t *testing.T) {
		msg := "one of the parent domains is not registered"
		args[0] = "testnet.fs.neo.com"
		c1.InvokeFail(t, msg, "register", args...)
	})

	args[0] = "fs.neo.com"
	c1.Invoke(t, true, "register", args...)

	args[0] = "testnet.fs.neo.com"
	c1.Invoke(t, true, "register", args...)

	acc2 := c.NewAccount(t)
	c2 := c.WithSigners(c.Committee, acc2)
	args = newArgs("mainnet.fs.neo.com", acc2)
	c2.InvokeFail(t, "not witnessed by admin", "register", args...)

	c1.Invoke(t, stackitem.Null{}, "addRecord",
		"something.mainnet.fs.neo.com", int64(nns.A), "1.2.3.4")
	c1.Invoke(t, stackitem.Null{}, "addRecord",
		"another.fs.neo.com", int64(nns.A), "4.3.2.1")

	c2 = c.WithSigners(acc, acc2)
	c2.InvokeFail(t, "parent domain has conflicting records: something.mainnet.fs.neo.com",
		"register", args...)

	c1.Invoke(t, stackitem.Null{}, "deleteRecords",
		"something.mainnet.fs.neo.com", int64(nns.A))
	c2.Invoke(t, true, "register", args...)

	c2 = c.WithSigners(acc2)
	c2.Invoke(t, stackitem.Null{}, "addRecord",
		"cdn.mainnet.fs.neo.com", int64(nns.A), "166.15.14.13")
	result := stackitem.NewArray([]stackitem.Item{
		stackitem.NewByteArray([]byte("166.15.14.13"))})
	c2.Invoke(t, result, "resolve", "cdn.mainnet.fs.neo.com", int64(nns.A))
}

func TestNNSUpdateSOA(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	refresh *= 2
	retry *= 2
	expire *= 2
	ttl *= 2
	c.Invoke(t, stackitem.Null{}, "updateSOA",
		"testdomain.com", "newemail@nspcc.ru", refresh, retry, expire, ttl)

	b := c.TopBlock(t)
	expected := stackitem.NewArray([]stackitem.Item{stackitem.NewBuffer(
		[]byte(fmt.Sprintf("testdomain.com newemail@nspcc.ru %d %d %d %d %d",
			b.Timestamp, refresh, retry, expire, ttl)))})
	c.Invoke(t, expected, "getRecords", "testdomain.com", int64(nns.SOA))
}

func TestNNSGetAllRecords(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	c.Invoke(t, stackitem.Null{}, "addRecord", "testdomain.com", int64(nns.TXT), "first TXT record")
	c.Invoke(t, stackitem.Null{}, "addRecord", "testdomain.com", int64(nns.A), "1.2.3.4")

	b := c.TopBlock(t)
	expSOA := fmt.Sprintf("testdomain.com myemail@nspcc.ru %d %d %d %d %d",
		b.Timestamp, refresh, retry, expire, ttl)

	s, err := c.TestInvoke(t, "getAllRecords", "testdomain.com")
	require.NoError(t, err)

	iter := s.Pop().Value().(*storage.Iterator)
	require.True(t, iter.Next())
	require.Equal(t, stackitem.NewStruct([]stackitem.Item{
		stackitem.Make("testdomain.com"), stackitem.Make(int64(nns.A)),
		stackitem.Make("1.2.3.4"), stackitem.Make(new(big.Int)),
	}), iter.Value())

	require.True(t, iter.Next())
	require.Equal(t, stackitem.NewStruct([]stackitem.Item{
		stackitem.Make("testdomain.com"), stackitem.Make(int64(nns.SOA)),
		stackitem.NewBuffer([]byte(expSOA)), stackitem.Make(new(big.Int)),
	}), iter.Value())

	require.True(t, iter.Next())
	require.Equal(t, stackitem.NewStruct([]stackitem.Item{
		stackitem.Make("testdomain.com"), stackitem.Make(int64(nns.TXT)),
		stackitem.Make("first TXT record"), stackitem.Make(new(big.Int)),
	}), iter.Value())

	require.False(t, iter.Next())
}

func TestExpiration(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(msPerYear/1000*10), int64(104)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	checkProperties := func(t *testing.T, expiration uint64) {
		expected := stackitem.NewMapWithValue([]stackitem.MapElement{
			{Key: stackitem.Make("name"), Value: stackitem.Make("testdomain.com")},
			{Key: stackitem.Make("expiration"), Value: stackitem.Make(expiration)}})
		s, err := c.TestInvoke(t, "properties", "testdomain.com")
		require.NoError(t, err)
		require.Equal(t, expected.Value(), s.Top().Item().Value())
	}

	top := c.TopBlock(t)
	expiration := top.Timestamp + uint64(expire*1000)
	checkProperties(t, expiration)

	b := c.NewUnsignedBlock(t)
	b.Timestamp = expiration - 2 // test invoke is done with +1 timestamp
	require.NoError(t, c.Chain.AddBlock(c.SignBlock(b)))
	checkProperties(t, expiration)

	b = c.NewUnsignedBlock(t)
	b.Timestamp = expiration - 1
	require.NoError(t, c.Chain.AddBlock(c.SignBlock(b)))

	_, err := c.TestInvoke(t, "properties", "testdomain.com")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "name has expired"))

	c.InvokeFail(t, "name has expired", "getAllRecords", "testdomain.com")
	c.InvokeFail(t, "name has expired", "ownerOf", "testdomain.com")
}

func TestNNSSetAdmin(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	acc := c.NewAccount(t)
	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, "not witnessed by admin", "addRecord",
		"testdomain.com", int64(nns.TXT), "won't be added")

	c1 := c.WithSigners(c.Committee, acc)
	c1.Invoke(t, stackitem.Null{}, "setAdmin", "testdomain.com", acc.ScriptHash())

	cAcc.Invoke(t, stackitem.Null{}, "addRecord",
		"testdomain.com", int64(nns.TXT), "will be added")
}

func TestNNSIsAvailable(t *testing.T) {
	c := newNNSInvoker(t, false)

	c.Invoke(t, true, "isAvailable", "com")
	c.InvokeFail(t, "TLD not found", "isAvailable", "domain.com")

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register",
		"com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	c.Invoke(t, false, "isAvailable", "com")
	c.Invoke(t, true, "isAvailable", "domain.com")

	acc := c.NewAccount(t)
	c1 := c.WithSigners(c.Committee, acc)
	c1.Invoke(t, true, "register",
		"domain.com", acc.ScriptHash(),
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	c.Invoke(t, false, "isAvailable", "domain.com")
}

func TestNNSRenew(t *testing.T) {
	c := newNNSInvoker(t, true)

	acc := c.NewAccount(t)
	c1 := c.WithSigners(c.Committee, acc)
	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c1.Invoke(t, true, "register",
		"testdomain.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	const msPerYear = 365 * 24 * time.Hour / time.Millisecond
	b := c.TopBlock(t)
	ts := b.Timestamp + uint64(expire*1000) + uint64(msPerYear)

	cAcc := c.WithSigners(acc)
	cAcc.InvokeFail(t, "not witnessed by admin", "renew", "testdomain.com")
	c1.Invoke(t, ts, "renew", "testdomain.com")
	expected := stackitem.NewMapWithValue([]stackitem.MapElement{
		{Key: stackitem.Make("name"), Value: stackitem.Make("testdomain.com")},
		{Key: stackitem.Make("expiration"), Value: stackitem.Make(ts)}})
	cAcc.Invoke(t, expected, "properties", "testdomain.com")
}

func TestNNSResolve(t *testing.T) {
	c := newNNSInvoker(t, true)

	refresh, retry, expire, ttl := int64(101), int64(102), int64(103), int64(104)
	c.Invoke(t, true, "register",
		"test.com", c.CommitteeHash,
		"myemail@nspcc.ru", refresh, retry, expire, ttl)

	c.Invoke(t, stackitem.Null{}, "addRecord",
		"test.com", int64(nns.TXT), "expected result")

	records := stackitem.NewArray([]stackitem.Item{stackitem.Make("expected result")})
	c.Invoke(t, records, "resolve", "test.com", int64(nns.TXT))
	c.Invoke(t, records, "resolve", "test.com.", int64(nns.TXT))
	c.InvokeFail(t, "invalid domain name format", "resolve", "test.com..", int64(nns.TXT))
}
