package fund

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn-sdk/go-sdk/client"
	mmnpb "github.com/mezonai/mmn-sdk/go-sdk/proto"
	"github.com/mr-tron/base58"
)

type mockTxSender struct {
	nonce   uint64
	sentTxs []client.SignedTx
}

func (m *mockTxSender) GetAccount(_ context.Context, addr string) (client.Account, error) {
	return client.Account{
		Address: addr,
		Balance: uint256.NewInt(1000000000),
		Nonce:   m.nonce,
	}, nil
}

func (m *mockTxSender) AddTx(_ context.Context, tx client.SignedTx) (client.AddTxResponse, error) {
	m.sentTxs = append(m.sentTxs, tx)
	m.nonce++
	return client.AddTxResponse{
		Ok:     true,
		TxHash: fmt.Sprintf("mock_fwd_tx_hash_%d", tx.Tx.Nonce),
	}, nil
}

func (m *mockTxSender) Close() error {
	return nil
}

type mockZkProver struct {
	called bool
}

func (p *mockZkProver) RequestZkProof(_ context.Context, _, _, _, _, _ string) (string, string, error) {
	p.called = true
	return "mock_zk_proof_abc", "mock_zk_pub_xyz", nil
}

func TestParsePrivateKey(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	expectedAddr := base58.Encode(pub)
	seed := priv.Seed()

	// 1. Hex Seed (32 bytes = 64 hex chars)
	hexSeed := hex.EncodeToString(seed)
	_, _, addr1, err := ParsePrivateKey(hexSeed)
	if err != nil {
		t.Errorf("ParsePrivateKey hex seed error: %v", err)
	}
	if addr1 != expectedAddr {
		t.Errorf("addr1 = %s, want %s", addr1, expectedAddr)
	}

	// 2. Hex Full Private Key (64 bytes = 128 hex chars)
	hexFull := hex.EncodeToString(priv)
	_, _, addr2, err := ParsePrivateKey(hexFull)
	if err != nil {
		t.Errorf("ParsePrivateKey hex full error: %v", err)
	}
	if addr2 != expectedAddr {
		t.Errorf("addr2 = %s, want %s", addr2, expectedAddr)
	}

	// 3. Base58 Seed
	b58Seed := base58.Encode(seed)
	_, _, addr3, err := ParsePrivateKey(b58Seed)
	if err != nil {
		t.Errorf("ParsePrivateKey base58 seed error: %v", err)
	}
	if addr3 != expectedAddr {
		t.Errorf("addr3 = %s, want %s", addr3, expectedAddr)
	}

	// 4. Invalid
	if _, _, _, err := ParsePrivateKey(""); err == nil {
		t.Error("expected error for empty key")
	}
	if _, _, _, err := ParsePrivateKey("invalid_key_xyz!"); err == nil {
		t.Error("expected error for invalid key")
	}
}

func TestParseForwardInfo(t *testing.T) {
	wallets := NewUserWalletMap(map[string]string{
		"a.nguyenvan": "addr-user-123",
	})
	botAddr := "addr-bot-789"

	// Case 1: Pattern "<user> transfer via bot: <content>"
	m1 := Movement{
		Incoming: true,
		Sender:   botAddr,
		Content:  "a.nguyenvan transfer via bot: 1 xúc xích",
	}
	fwd1, ok1 := ParseForwardInfo(m1, wallets)
	if !ok1 || fwd1.OriginalUser != "a.nguyenvan" || fwd1.Content != "1 xúc xích" {
		t.Errorf("Case 1 failed: got %+v, %v", fwd1, ok1)
	}

	// Case 2: Pattern "<user> transfer via bot" (no content)
	m2 := Movement{
		Incoming: true,
		Sender:   botAddr,
		Content:  "a.nguyenvan transfer via bot",
	}
	fwd2, ok2 := ParseForwardInfo(m2, wallets)
	if !ok2 || fwd2.OriginalUser != "a.nguyenvan" || fwd2.Content != "" {
		t.Errorf("Case 2 failed: got %+v, %v", fwd2, ok2)
	}

	// Case 3: Pattern with mapped address "<userAddr> transfer via bot: test"
	m3 := Movement{
		Incoming: true,
		Sender:   botAddr,
		Content:  "addr-user-123 transfer via bot: test note",
	}
	fwd3, ok3 := ParseForwardInfo(m3, wallets)
	if !ok3 || fwd3.OriginalUser != "a.nguyenvan" || fwd3.Content != "test note" {
		t.Errorf("Case 3 failed: got %+v, %v", fwd3, ok3)
	}

	// Case 4: ExtraInfo JSON
	m4 := Movement{
		Incoming:  true,
		Sender:    botAddr,
		ExtraInfo: `{"type":"forward_via_bot","original_user":"b.le","original_content":"tra sua"}`,
	}
	fwd4, ok4 := ParseForwardInfo(m4, wallets)
	if !ok4 || fwd4.OriginalUser != "b.le" || fwd4.Content != "tra sua" {
		t.Errorf("Case 4 failed: got %+v, %v", fwd4, ok4)
	}

	// Case 5: Direct transfer without bot (should NOT match)
	m5 := Movement{
		Incoming: true,
		Sender:   "addr-user-123",
		Content:  "tiền bánh mì",
	}
	if _, ok5 := ParseForwardInfo(m5, wallets); ok5 {
		t.Errorf("Case 5 should not match forward info: %+v", m5)
	}
}

func TestMovementText_ForwardViaBot(t *testing.T) {
	wallets := NewUserWalletMap(map[string]string{
		"a.nguyenvan": "addr-user-123",
	})
	botAddr := "addr-bot-789"

	m := Movement{
		Incoming:   true,
		Sender:     botAddr,
		Content:    "a.nguyenvan transfer via bot: 1 xúc xích",
		Raw:        big.NewInt(20000000000), // 20,000 VND (6 decimals)
		At:         time.Date(2026, 11, 16, 15, 22, 0, 0, time.UTC),
		BotAddress: botAddr,
	}

	text := MovementText(m, 6, time.UTC, wallets)

	if !strings.Contains(text, "Nhận vào quỹ +20.000 VNĐ") {
		t.Errorf("expected amount line, got %q", text)
	}
	if !strings.Contains(text, "Người gửi: a.nguyenvan (qua bot)") {
		t.Errorf("expected mapped sender line 'Người gửi: a.nguyenvan (qua bot)', got %q", text)
	}
	if !strings.Contains(text, "Nội dung: 1 xúc xích") {
		t.Errorf("expected content line 'Nội dung: 1 xúc xích', got %q", text)
	}
}

func TestForwarder_Forward(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	botAddr := base58.Encode(pub)
	fundAddr := "2pCcGfr1e2czUEEXQj7Sc5CavjVM13iKWGDVsGSh5dFP"

	wallets := NewUserWalletMap(map[string]string{
		"dai.trinhduc": "addr-dai-999",
	})

	mockSender := &mockTxSender{nonce: 5}
	fwd := NewForwarderWithSender(mockSender, fundAddr, priv, botAddr, wallets, nil)

	m := Movement{
		Hash:      "in_user_tx_1",
		Sender:    "addr-dai-999",
		Recipient: botAddr,
		Raw:       big.NewInt(50000000000), // 50,000 VND
		Content:   "1 xúc xích",
	}

	txHash, err := fwd.Forward(context.Background(), m)
	if err != nil {
		t.Fatalf("fwd.Forward error: %v", err)
	}
	if !strings.HasPrefix(txHash, "mock_fwd_tx_hash_") {
		t.Errorf("unexpected txHash: %s", txHash)
	}

	if len(mockSender.sentTxs) != 1 {
		t.Fatalf("expected 1 sent tx, got %d", len(mockSender.sentTxs))
	}

	sent := mockSender.sentTxs[0]
	if sent.Tx.Sender != botAddr {
		t.Errorf("sent.Tx.Sender = %s, want %s", sent.Tx.Sender, botAddr)
	}
	if sent.Tx.Recipient != fundAddr {
		t.Errorf("sent.Tx.Recipient = %s, want %s", sent.Tx.Recipient, fundAddr)
	}
	if sent.Tx.Nonce != 6 {
		t.Errorf("sent.Tx.Nonce = %d, want 6", sent.Tx.Nonce)
	}
	if sent.Tx.TextData != "dai.trinhduc transfer via bot: 1 xúc xích" {
		t.Errorf("sent.Tx.TextData = %q, want 'dai.trinhduc transfer via bot: 1 xúc xích'", sent.Tx.TextData)
	}

	// Verify cryptographic signature
	if !client.Verify(sent.Tx, sent.Sig) {
		t.Errorf("signature verification failed on forwarded tx")
	}
}

func TestStream_BotIncomingAutoForward(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	botAddr := base58.Encode(pub)
	fundAddr := "2pCcGfr1e2czUEEXQj7Sc5CavjVM13iKWGDVsGSh5dFP"

	wallets := NewUserWalletMap(map[string]string{
		"a.nguyenvan": "addr-user-abc",
	})

	mockSender := &mockTxSender{nonce: 10}
	fwd := NewForwarderWithSender(mockSender, fundAddr, priv, botAddr, wallets, nil)

	q := newFakeQueue()
	s := &Stream{
		Address:    fundAddr,
		BotAddress: botAddr,
		Forwarder:  fwd,
		ChannelID:  "chan-1",
		ThreadID:   "thread-1",
		Decimals:   6,
		Queue:      q,
		Wallets:    wallets,
		Timezone:   time.UTC,
	}

	// 1. User sends money to Bot
	userTx := event("tx-user-to-bot", "addr-user-abc", botAddr, "10000000000", "1 xúc xích", mmnpb.TransactionStatus_CONFIRMED)
	s.handle(context.Background(), userTx)

	if len(mockSender.sentTxs) != 1 {
		t.Fatalf("expected forwarder to send 1 tx, got %d", len(mockSender.sentTxs))
	}
	if mockSender.sentTxs[0].Tx.TextData != "a.nguyenvan transfer via bot: 1 xúc xích" {
		t.Errorf("unexpected forwarded text: %q", mockSender.sentTxs[0].Tx.TextData)
	}

	// Duplicate event (e.g. FINALIZED) for the same user tx should NOT forward again
	s.handle(context.Background(), userTx)
	if len(mockSender.sentTxs) != 1 {
		t.Errorf("expected duplicate tx to be skipped, got %d forwarded txs", len(mockSender.sentTxs))
	}

	// 2. Now the forwarded transaction lands on the Fund wallet
	fwdTx := event("tx-fwd-to-fund", botAddr, fundAddr, "10000000000", "a.nguyenvan transfer via bot: 1 xúc xích", mmnpb.TransactionStatus_CONFIRMED)
	s.handle(context.Background(), fwdTx)

	if len(q.queued) != 1 {
		t.Fatalf("expected 1 announcement queued in Mezon outbox, got %d", len(q.queued))
	}

	announcement := q.queued["fund:tx-fwd-to-fund"]
	if !strings.Contains(announcement, "Người gửi: a.nguyenvan (qua bot)") {
		t.Errorf("expected mapped announcement sender 'Người gửi: a.nguyenvan (qua bot)', got %q", announcement)
	}
	if !strings.Contains(announcement, "Nội dung: 1 xúc xích") {
		t.Errorf("expected announcement content 'Nội dung: 1 xúc xích', got %q", announcement)
	}
}

func TestZKForwarder_Forward(t *testing.T) {
	botID := "1840703905160237056"
	botAddr := DeriveBotAddress(botID)
	fundAddr := "2pCcGfr1e2czUEEXQj7Sc5CavjVM13iKWGDVsGSh5dFP"

	wallets := NewUserWalletMap(map[string]string{
		"a.nguyenvan": "addr-user-abc",
	})

	mockSender := &mockTxSender{nonce: 20}
	mockProver := &mockZkProver{}

	fwd := NewZKForwarderWithSender(mockSender, mockProver, fundAddr, botAddr, botID, "mock-jwt-token", wallets, nil)
	if !fwd.IsZKMode() {
		t.Error("expected IsZKMode() to be true")
	}

	m := Movement{
		Hash:      "in_user_tx_zk",
		Sender:    "addr-user-abc",
		Recipient: botAddr,
		Raw:       big.NewInt(30000000000), // 30,000 VND
		Content:   "1 xúc xích",
	}

	txHash, err := fwd.Forward(context.Background(), m)
	if err != nil {
		t.Fatalf("ZK Forward error: %v", err)
	}
	if !strings.HasPrefix(txHash, "mock_fwd_tx_hash_") {
		t.Errorf("unexpected txHash: %s", txHash)
	}
	if !mockProver.called {
		t.Errorf("expected ZkProver to be called")
	}

	if len(mockSender.sentTxs) != 1 {
		t.Fatalf("expected 1 sent tx, got %d", len(mockSender.sentTxs))
	}

	sent := mockSender.sentTxs[0]
	if sent.Tx.Type != client.TxTypeTransferByZk {
		t.Errorf("sent.Tx.Type = %d, want %d (TxTypeTransferByZk)", sent.Tx.Type, client.TxTypeTransferByZk)
	}
	if sent.Tx.ZkProof != "mock_zk_proof_abc" {
		t.Errorf("sent.Tx.ZkProof = %q, want 'mock_zk_proof_abc'", sent.Tx.ZkProof)
	}
	if sent.Tx.TextData != "a.nguyenvan transfer via bot: 1 xúc xích" {
		t.Errorf("sent.Tx.TextData = %q, want 'a.nguyenvan transfer via bot: 1 xúc xích'", sent.Tx.TextData)
	}
}
