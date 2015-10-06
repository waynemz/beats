package memcache

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/libbeat/common"

	"github.com/elastic/packetbeat/config"
)

type memcacheTest struct {
	mc           *Memcache
	transactions []*transaction
}

func newMemcacheTest(config config.Memcache) *memcacheTest {
	mct := &memcacheTest{}
	mc := &Memcache{}
	mc.InitWithConfig(config, false, nil)
	mc.handler = mct
	mct.mc = mc
	return mct
}

func (mct *memcacheTest) onTransaction(t *transaction) {
	mct.transactions = append(mct.transactions, t)
}

func (mct *memcacheTest) genTransaction(requ, resp *message) *transaction {
	if requ != nil {
		requ.CmdlineTuple = &common.CmdlineTuple{}
	}
	if resp != nil {
		resp.CmdlineTuple = &common.CmdlineTuple{}
	}

	t := newTransaction(requ, resp)
	mct.mc.finishTransaction(t)
	return t
}

func makeBinMessage(
	t *testing.T,
	hdr *binHeader,
	extras []extraFn,
	key valueFn,
	value valueFn,
) *message {
	buf, err := prepareBinMessage(hdr, extras, key, value)
	if err != nil {
		t.Fatalf("generating bin message failed with: %v", err)
	}
	return binParseNoFail(t, buf.Bytes())
}

func makeTransactionEvent(t *testing.T, trans *transaction) common.MapStr {
	event := common.MapStr{}
	err := trans.Event(event)
	if err != nil {
		t.Fatalf("serializing transaction failed with: %v", err)
	}
	return event
}

func Test_TryMergeUnmergeableRespnses(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	msg1 := textParseNoFail(t, "STORED\r\n")
	msg2 := textParseNoFail(t, "0\r\n")
	b, err := tryMergeResponses(mct.mc, msg1, msg2)
	assert.False(t, b)
	assert.Nil(t, err)
}

func Test_TryMergeUnmergeableResponseWithValue(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	msg1 := textParseNoFail(t, "VALUE k 1 5 3\r\nvalue\r\n")
	msg2 := textParseNoFail(t, "0\r\n")
	b, err := tryMergeResponses(mct.mc, msg1, msg2)
	assert.False(t, b)
	assert.Nil(t, err)
}

func Test_TryMergeUnmergeableResponseWithStat(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	msg1 := textParseNoFail(t, "STAT name value\r\n")
	msg2 := textParseNoFail(t, "0\r\n")
	b, err := tryMergeResponses(mct.mc, msg1, msg2)
	assert.False(t, b)
	assert.Nil(t, err)
}

func Test_MergeTextValueResponses(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	msg1 := textParseNoFail(t, "VALUE k 1 6 3\r\nvalue1\r\n")
	msg2 := textParseNoFail(t, "VALUE k 1 6 3\r\nvalue2\r\n")
	msg3 := textParseNoFail(t, "END\r\n")

	b, err := tryMergeResponses(mct.mc, msg1, msg2)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.False(t, msg1.isComplete)

	b, err = tryMergeResponses(mct.mc, msg1, msg3)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.True(t, msg1.isComplete)
}

func Test_MergeTextStatsValueResponses(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	msg1 := textParseNoFail(t, "STAT name1 value1\r\n")
	msg2 := textParseNoFail(t, "STAT name2 value2\r\n")
	msg3 := textParseNoFail(t, "END\r\n")

	b, err := tryMergeResponses(mct.mc, msg1, msg2)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.False(t, msg1.isComplete)

	b, err = tryMergeResponses(mct.mc, msg1, msg3)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.True(t, msg1.isComplete)
}

func Test_MergeBinaryStatsValueResponses(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	msg1 := makeBinMessage(t,
		&binHeader{opcode: opcodeStat, request: false},
		extras(), key("stat1"), value("value1"))
	msg2 := makeBinMessage(t,
		&binHeader{opcode: opcodeStat, request: false},
		extras(), key("stat2"), value("value2"))
	msg3 := makeBinMessage(t,
		&binHeader{opcode: opcodeStat, request: false},
		extras(), noKey, noValue)

	b, err := tryMergeResponses(mct.mc, msg1, msg2)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.False(t, msg1.isComplete)

	b, err = tryMergeResponses(mct.mc, msg1, msg3)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.True(t, msg1.isComplete)
}

func Test_MergeTextValueResponsesNoLimits(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{MaxValues: -1, MaxBytesPerValue: 1000})
	msg1 := textParseNoFail(t, "VALUE k1 1 6 3\r\nvalue1\r\n")
	msg2 := textParseNoFail(t, "VALUE k2 1 6 3\r\nvalue2\r\n")
	msg3 := textParseNoFail(t, "END\r\n")

	b, err := tryMergeResponses(mct.mc, msg1, msg2)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.False(t, msg1.isComplete)

	b, err = tryMergeResponses(mct.mc, msg1, msg3)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.True(t, msg1.isComplete)

	msg := msg1
	assert.Equal(t, "k1", msg.keys[0].String())
	assert.Equal(t, "k2", msg.keys[1].String())
	assert.Equal(t, uint32(2), msg.count_values)
	assert.Equal(t, "value1", msg.values[0].String())
	assert.Equal(t, "value2", msg.values[1].String())
}

func Test_MergeTextValueResponsesWithLimits(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{MaxValues: 1, MaxBytesPerValue: 1000})
	msg1 := textParseNoFail(t, "VALUE k1 1 6 3\r\nvalue1\r\n")
	msg2 := textParseNoFail(t, "VALUE k2 1 6 3\r\nvalue2\r\n")
	msg3 := textParseNoFail(t, "END\r\n")

	b, err := tryMergeResponses(mct.mc, msg1, msg2)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.False(t, msg1.isComplete)

	b, err = tryMergeResponses(mct.mc, msg1, msg3)
	assert.True(t, b)
	assert.Nil(t, err)
	assert.True(t, msg1.isComplete)

	msg := msg1
	assert.Equal(t, "k1", msg.keys[0].String())
	assert.Equal(t, "k2", msg.keys[1].String())
	assert.Equal(t, uint32(2), msg.count_values)
	assert.Equal(t, 1, len(msg.values))
	assert.Equal(t, "value1", msg.values[0].String())
}

func Test_TransactionComplete(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	trans := mct.genTransaction(
		textParseNoFail(t, "set k 0 0 5\r\nvalue\r\n"),
		textParseNoFail(t, "STORED\r\n"),
	)
	assert.Equal(t, common.OK_STATUS, trans.Status)
	assert.Equal(t, uint64(20), trans.BytesOut)
	assert.Equal(t, uint64(8), trans.BytesIn)
	assert.Equal(t, trans, mct.transactions[0])

	event := makeTransactionEvent(t, trans)
	assert.Equal(t, "memcache", event["type"])
	assert.Equal(t, common.OK_STATUS, event["status"])
}

func Test_TransactionRequestNoReply(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	trans := mct.genTransaction(
		textParseNoFail(t, "set k 0 0 5 noreply\r\nvalue\r\n"),
		nil,
	)
	assert.Equal(t, common.OK_STATUS, trans.Status)
	assert.Equal(t, uint64(28), trans.BytesOut)
	assert.Equal(t, uint64(0), trans.BytesIn)
	assert.Equal(t, trans, mct.transactions[0])

	event := makeTransactionEvent(t, trans)
	assert.Equal(t, "memcache", event["type"])
	assert.Equal(t, common.OK_STATUS, event["status"])
}

func Test_TransactionLostResponse(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	trans := mct.genTransaction(
		textParseNoFail(t, "set k 0 0 5\r\nvalue\r\n"),
		nil,
	)
	assert.Equal(t, common.SERVER_ERROR_STATUS, trans.Status)
	assert.Equal(t, uint64(20), trans.BytesOut)
	assert.Equal(t, uint64(0), trans.BytesIn)
	assert.Equal(t, trans, mct.transactions[0])

	event := makeTransactionEvent(t, trans)
	assert.Equal(t, "memcache", event["type"])
	assert.Equal(t, common.SERVER_ERROR_STATUS, event["status"])
}

func Test_TransactionLostRequest(t *testing.T) {
	mct := newMemcacheTest(config.Memcache{})
	trans := mct.genTransaction(
		nil,
		textParseNoFail(t, "STORED\r\n"),
	)
	assert.Equal(t, common.CLIENT_ERROR_STATUS, trans.Status)
	assert.Equal(t, uint64(0), trans.BytesOut)
	assert.Equal(t, uint64(8), trans.BytesIn)
	assert.Equal(t, trans, mct.transactions[0])

	event := makeTransactionEvent(t, trans)
	assert.Equal(t, "memcache", event["type"])
	assert.Equal(t, common.CLIENT_ERROR_STATUS, event["status"])
}
