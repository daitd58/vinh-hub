package fund

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	mmnpb "github.com/mezonai/mmn-sdk/go-sdk/proto"
)

type fakeQueue struct {
	queued map[string]string
	fail   error
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{queued: make(map[string]string)}
}

func (q *fakeQueue) Enqueue(_ context.Context, _, _, _, body, dedupeKey string) error {
	if q.fail != nil {
		return q.fail
	}
	if _, seen := q.queued[dedupeKey]; seen {
		return ErrAlreadyQueued
	}
	q.queued[dedupeKey] = body
	return nil
}

const fundWallet = "2pCcGfr1e2czUEEXQj7Sc5CavjVM13iKWGDVsGSh5dFP"

func testStream(q Queue) *Stream {
	return &Stream{
		Address:   fundWallet,
		ChannelID: "chan-1",
		ThreadID:  "thread-1",
		Decimals:  6,
		Queue:     q,
		Timezone:  time.UTC,
	}
}

func event(hash, from, to, amount, note string, status mmnpb.TransactionStatus) *mmnpb.TransactionStatusInfo {
	return &mmnpb.TransactionStatusInfo{
		TxHash:    hash,
		Sender:    from,
		Recipient: to,
		Amount:    amount,
		TextData:  note,
		Status:    status,
		Timestamp: 1789368887,
	}
}

func TestMoneyArrivingAndLeavingAreBothAnnounced(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)

	s.handle(context.Background(),
		event("in", "a-donor", fundWallet, "20000000000", "Transfer 20000 from donor note",
			mmnpb.TransactionStatus_CONFIRMED))
	s.handle(context.Background(),
		event("out", fundWallet, "a-recipient", "2000000000000", "chi quỹ dự án",
			mmnpb.TransactionStatus_CONFIRMED))

	if len(q.queued) != 2 {
		t.Fatalf("queued %d, want both directions", len(q.queued))
	}

	gotIn := q.queued["fund:in"]
	if !strings.Contains(gotIn, "Nhận vào quỹ") || !strings.Contains(gotIn, "+20.000 VNĐ") {
		t.Errorf("incoming reads %q", gotIn)
	}
	if !strings.Contains(gotIn, "Người gửi: a-donor") {
		t.Errorf("sender should be included in incoming: %q", gotIn)
	}
	if !strings.Contains(gotIn, "Nội dung: Transfer 20000 from donor note") {
		t.Errorf("content should be included in incoming: %q", gotIn)
	}

	gotOut := q.queued["fund:out"]
	if !strings.Contains(gotOut, "Chi từ quỹ") || !strings.Contains(gotOut, "−2.000.000 VNĐ") {
		t.Errorf("outgoing reads %q", gotOut)
	}
	if !strings.Contains(gotOut, "Người nhận: a-recipient") {
		t.Errorf("recipient should be included in outgoing: %q", gotOut)
	}
	if !strings.Contains(gotOut, "Nội dung: chi quỹ dự án") {
		t.Errorf("content should be included in outgoing: %q", gotOut)
	}
}

func TestMovementWithMappedUser(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)

	// Sender with known mapping: dai.trinhduc -> DzSGqEnhbpdLnsAPqyhAD97UxTKeHWmCaMm3nENQasbW
	senderAddr := "DzSGqEnhbpdLnsAPqyhAD97UxTKeHWmCaMm3nENQasbW"
	s.handle(context.Background(),
		event("tx-user", senderAddr, fundWallet, "80000000", "sua chua",
			mmnpb.TransactionStatus_CONFIRMED))

	got := q.queued["fund:tx-user"]
	if !strings.Contains(got, "Người gửi: dai.trinhduc") {
		t.Errorf("expected mapped username 'dai.trinhduc', got: %q", got)
	}
	if !strings.Contains(got, "Nội dung: sua chua") {
		t.Errorf("expected content line, got: %q", got)
	}
}

func TestMovementWithoutContentOmitContentLine(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)

	s.handle(context.Background(),
		event("tx-nocontent", "DzSGqEnhbpdLnsAPqyhAD97UxTKeHWmCaMm3nENQasbW", fundWallet, "80000000", "",
			mmnpb.TransactionStatus_CONFIRMED))

	got := q.queued["fund:tx-nocontent"]
	if strings.Contains(got, "Nội dung:") {
		t.Errorf("empty content should omit 'Nội dung:' line, got: %q", got)
	}
}

func TestOneAnnouncementPerLifetimeNotPerStatus(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)

	for _, st := range []mmnpb.TransactionStatus{
		mmnpb.TransactionStatus_CONFIRMED,
		mmnpb.TransactionStatus_FINALIZED,
	} {
		s.handle(context.Background(),
			event("same-tx", "a-donor", fundWallet, "50000000", "", st))
	}

	if len(q.queued) != 1 {
		t.Errorf("%d messages for one transaction, expected 1", len(q.queued))
	}
}

func TestOnlyTransactionsThatReachedABlockAreAnnounced(t *testing.T) {
	for _, st := range []mmnpb.TransactionStatus{
		mmnpb.TransactionStatus_PENDING,
		mmnpb.TransactionStatus_FAILED,
	} {
		q := newFakeQueue()
		testStream(q).handle(context.Background(),
			event("h", "a-donor", fundWallet, "50000000", "", st))
		if len(q.queued) != 0 {
			t.Errorf("%v should not have been announced", st)
		}
	}
}

func TestOtherWalletsAreIgnored(t *testing.T) {
	q := newFakeQueue()
	testStream(q).handle(context.Background(),
		event("h", "stranger-a", "stranger-b", "50000000", "",
			mmnpb.TransactionStatus_CONFIRMED))
	if len(q.queued) != 0 {
		t.Error("a transfer between two strangers was announced")
	}
}

func TestHeartbeatsAreNotAnnounced(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)

	s.handle(context.Background(), &mmnpb.TransactionStatusInfo{
		TextData: "HeartBeat",
		Amount:   "0",
		Status:   mmnpb.TransactionStatus_PENDING,
	})
	s.handle(context.Background(),
		event("hb", "", fundWallet, "0", "HeartBeat", mmnpb.TransactionStatus_CONFIRMED))

	if len(q.queued) != 0 {
		t.Errorf("heartbeats were announced: %v", q.queued)
	}
}

func TestUnreadableEventIsSkippedNotCrashed(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)

	s.handle(context.Background(), nil)
	s.handle(context.Background(),
		event("", "a-donor", fundWallet, "1", "", mmnpb.TransactionStatus_CONFIRMED))
	s.handle(context.Background(),
		event("h", "a-donor", fundWallet, "invalid-amount", "", mmnpb.TransactionStatus_CONFIRMED))

	if len(q.queued) != 0 {
		t.Errorf("unusable events were announced: %v", q.queued)
	}
}

func TestQueueFailureDoesNotStopStream(t *testing.T) {
	q := newFakeQueue()
	q.fail = errors.New("temporary queue error")
	s := testStream(q)

	s.handle(context.Background(),
		event("tx1", "a-donor", fundWallet, "1000000", "", mmnpb.TransactionStatus_CONFIRMED))

	q.fail = nil
	s.handle(context.Background(),
		event("tx2", "a-donor", fundWallet, "2000000", "", mmnpb.TransactionStatus_CONFIRMED))

	if _, ok := q.queued["fund:tx2"]; !ok {
		t.Error("event after temporary queue failure was lost")
	}
}

func TestBalanceLineAppended(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)

	source := &fakeSource{balance: micro(46_000_000)}
	r := New(Options{Source: source, Wallet: fundWallet})
	s.Balance = r

	s.handle(context.Background(),
		event("tx-bal", "a-donor", fundWallet, "80000000", "", mmnpb.TransactionStatus_CONFIRMED))

	got := q.queued["fund:tx-bal"]
	if !strings.Contains(got, "Quỹ hiện có 46.000.000 VNĐ") {
		t.Errorf("expected balance line, got: %q", got)
	}
}

func TestBalanceLineOmittedWhenReaderFails(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)

	source := &fakeSource{err: errors.New("indexer down")}
	r := New(Options{Source: source, Wallet: fundWallet})
	s.Balance = r

	s.handle(context.Background(),
		event("tx-nobal", "a-donor", fundWallet, "80000000000", "", mmnpb.TransactionStatus_CONFIRMED))

	got := q.queued["fund:tx-nobal"]
	if strings.Contains(got, "Quỹ hiện có") {
		t.Errorf("balance line should be omitted on error, got: %q", got)
	}
	if !strings.Contains(got, "Nhận vào quỹ +80.000 VNĐ") {
		t.Errorf("movement line missing: %q", got)
	}
}

type fakeStream struct {
	send     []*mmnpb.TransactionStatusInfo
	i        int
	released chan struct{}
}

func (f *fakeStream) Recv() (*mmnpb.TransactionStatusInfo, error) {
	if f.i < len(f.send) {
		f.i++
		return f.send[f.i-1], nil
	}
	<-f.released
	return nil, errors.New("stream cancelled")
}

func TestSilentStreamIsGivenUpOn(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)
	s.Silence = 50 * time.Millisecond

	stream := &fakeStream{released: make(chan struct{})}
	gaveUp := make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- s.read(context.Background(), stream, func() {
			close(gaveUp)
			close(stream.released)
		})
	}()

	select {
	case <-gaveUp:
	case <-time.After(2 * time.Second):
		t.Fatal("silent stream timeout was never triggered")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("read loop did not return after silence deadline")
	}
}

func TestConsecutiveIncomingTransactionsUpdateBalance(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)
	s.RetryDelay = 1 * time.Millisecond
	s.MaxRetries = 1

	source := &fakeSource{balance: micro(2_766_348)}
	r := New(Options{Source: source, Wallet: fundWallet})
	s.Balance = r
	s.SetLastBalance(2_766_348)

	// Tx 1: +30.000
	source.balance = micro(2_796_348)
	s.handle(context.Background(),
		event("tx1", "duc.phamhuyen", fundWallet, "30000000000", "dong gop", mmnpb.TransactionStatus_CONFIRMED))

	// Tx 2: +10.000
	source.balance = micro(2_806_348)
	s.handle(context.Background(),
		event("tx2", "dai.trinhduc", fundWallet, "10000000000", "ung ho", mmnpb.TransactionStatus_CONFIRMED))

	// Tx 3: +1
	source.balance = micro(2_806_349)
	s.handle(context.Background(),
		event("tx3", "tai.cumanhtuan", fundWallet, "1000000", "test 1d", mmnpb.TransactionStatus_CONFIRMED))

	msg1 := q.queued["fund:tx1"]
	if !strings.Contains(msg1, "Quỹ hiện có 2.796.348 VNĐ") {
		t.Errorf("tx1 balance line wrong, got: %s", msg1)
	}

	msg2 := q.queued["fund:tx2"]
	if !strings.Contains(msg2, "Quỹ hiện có 2.806.348 VNĐ") {
		t.Errorf("tx2 balance line wrong, got: %s", msg2)
	}

	msg3 := q.queued["fund:tx3"]
	if !strings.Contains(msg3, "Quỹ hiện có 2.806.349 VNĐ") {
		t.Errorf("tx3 balance line wrong, got: %s", msg3)
	}
}

type retrySource struct {
	calls   int
	step1   *big.Int
	step2   *big.Int
	switchAt int
}

func (r *retrySource) WalletBalance(context.Context, string) (*big.Int, error) {
	r.calls++
	if r.calls >= r.switchAt {
		return r.step2, nil
	}
	return r.step1, nil
}

func TestBalanceRetryPicksUpUpdatedBalance(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)
	s.RetryDelay = 1 * time.Millisecond
	s.MaxRetries = 3

	// Source returns old balance for 1st call, then updated on 2nd call
	source := &retrySource{
		step1:    micro(2_766_348),
		step2:    micro(2_796_348),
		switchAt: 2,
	}
	r := New(Options{Source: source, Wallet: fundWallet})
	s.Balance = r
	s.SetLastBalance(2_766_348)

	s.handle(context.Background(),
		event("lag-tx1", "duc.phamhuyen", fundWallet, "30000000000", "", mmnpb.TransactionStatus_CONFIRMED))

	msg1 := q.queued["fund:lag-tx1"]
	if !strings.Contains(msg1, "Quỹ hiện có 2.796.348 VNĐ") {
		t.Errorf("lag-tx1 should pick up updated balance 2.796.348, got: %s", msg1)
	}
	if source.calls < 2 {
		t.Errorf("expected at least 2 calls (retry), got %d", source.calls)
	}
}

func TestDuplicateStatusEventsDoNotDuplicateProcessing(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)

	source := &fakeSource{balance: micro(3_000_000)}
	r := New(Options{Source: source, Wallet: fundWallet})
	s.Balance = r

	// First event: CONFIRMED
	s.handle(context.Background(),
		event("tx-dup", "a-donor", fundWallet, "10000000000", "test", mmnpb.TransactionStatus_CONFIRMED))
	firstCalls := source.calls

	// Second event: FINALIZED for same tx hash
	s.handle(context.Background(),
		event("tx-dup", "a-donor", fundWallet, "10000000000", "test", mmnpb.TransactionStatus_FINALIZED))

	if source.calls != firstCalls {
		t.Errorf("duplicate event should be ignored immediately without querying balance: calls went from %d to %d", firstCalls, source.calls)
	}
}

func TestConsecutiveOutgoingTransactionsDecreaseBalance(t *testing.T) {
	q := newFakeQueue()
	s := testStream(q)
	s.RetryDelay = 1 * time.Millisecond
	s.MaxRetries = 1

	source := &fakeSource{balance: micro(5_000_000)}
	r := New(Options{Source: source, Wallet: fundWallet})
	s.Balance = r
	s.SetLastBalance(5_000_000)

	source.balance = micro(4_000_000)
	s.handle(context.Background(),
		event("out-tx1", fundWallet, "vendor-a", "1000000000000", "chi hoat dong", mmnpb.TransactionStatus_CONFIRMED))

	msg := q.queued["fund:out-tx1"]
	if !strings.Contains(msg, "Quỹ hiện có 4.000.000 VNĐ") {
		t.Errorf("out-tx1 balance line wrong, got: %s", msg)
	}
}
