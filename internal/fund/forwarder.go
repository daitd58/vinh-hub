package fund

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn-sdk/go-sdk/client"
	"github.com/mr-tron/base58"
	mezon "github.com/quangledang23/mezon-sdk-go"
)

// TxSender defines the interface for interacting with MMN node transactions.
type TxSender interface {
	GetAccount(ctx context.Context, addr string) (client.Account, error)
	GetCurrentNonce(ctx context.Context, addr string, tag string) (uint64, error)
	AddTx(ctx context.Context, tx client.SignedTx) (client.AddTxResponse, error)
	Close() error
}

// ZkProver defines the interface for obtaining Zero-Knowledge proofs for token transfers.
type ZkProver interface {
	RequestZkProof(ctx context.Context, endpoint, userID, jwtToken, address, ephemeralPK string) (string, string, error)
}

// DefaultZkProver calls Mezon's ZK prove service (default: https://dong.mezon.ai/zk-api/prove).
type DefaultZkProver struct {
	HTTPClient *http.Client
}

type zkProveResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Proof       string `json:"proof"`
		PublicInput string `json:"public_input"`
	} `json:"data"`
	Message string `json:"message"`
}

func (p *DefaultZkProver) RequestZkProof(ctx context.Context, endpoint, userID, jwtToken, address, ephemeralPK string) (string, string, error) {
	if endpoint == "" {
		endpoint = "https://dong.mezon.ai/zk-api/prove"
	}
	reqBody, err := json.Marshal(map[string]string{
		"user_id":      userID,
		"ephemeral_pk": ephemeralPK,
		"jwt":          jwtToken,
		"address":      address,
		"client_type":  "mezon",
	})
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := p.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("zk prove request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("zk prove HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res zkProveResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", "", fmt.Errorf("decode zk prove response: %w", err)
	}
	if !res.Success {
		return "", "", fmt.Errorf("zk prove error: %s", res.Message)
	}

	return res.Data.Proof, res.Data.PublicInput, nil
}

// Forwarder manages auto-forwarding of incoming funds from the bot wallet to the fund wallet.
type Forwarder struct {
	sender   TxSender
	botAddr  string
	fundAddr string
	wallets  *UserWalletMap
	log      *slog.Logger

	// Key mode (TxTypeTransferByKey)
	botPubKey  ed25519.PublicKey
	botPrivKey ed25519.PrivateKey

	// ZK mode (TxTypeTransferByZk - Native Mezon Bot, no private key required)
	useZK       bool
	botID       string
	botToken    string
	mezonHost   string
	zkEndpoint  string
	zkProver    ZkProver
	cachedToken string
	tokenExpiry time.Time

	mu        sync.Mutex
	lastNonce uint64
}

// DeriveBotAddress computes the on-chain MMN address from a Mezon user/bot ID.
func DeriveBotAddress(botID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(botID)))
	return base58.Encode(sum[:])
}

// ParsePrivateKey parses an Ed25519 private key or seed from hex, base58, or base64 format.
func ParsePrivateKey(raw string) (ed25519.PrivateKey, ed25519.PublicKey, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, "", fmt.Errorf("private key is empty")
	}

	var keyBytes []byte

	// 1. Try Hex
	if hBytes, err := hex.DecodeString(raw); err == nil && len(hBytes) > 0 {
		keyBytes = hBytes
	} else if b58Bytes, err := base58.Decode(raw); err == nil && len(b58Bytes) > 0 {
		// 2. Try Base58
		keyBytes = b58Bytes
	} else if b64Bytes, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b64Bytes) > 0 {
		// 3. Try Base64
		keyBytes = b64Bytes
	} else {
		return nil, nil, "", fmt.Errorf("unable to decode private key (tried hex, base58, base64)")
	}

	var seed []byte
	switch {
	case len(keyBytes) == ed25519.SeedSize: // 32 bytes seed
		seed = keyBytes
	case len(keyBytes) == ed25519.PrivateKeySize: // 64 bytes private key (seed + pub)
		seed = keyBytes[:ed25519.SeedSize]
	case len(keyBytes) > ed25519.SeedSize: // DER or wrapped format with seed at the end
		seed = keyBytes[len(keyBytes)-ed25519.SeedSize:]
	default:
		return nil, nil, "", fmt.Errorf("invalid private key length %d (expected 32 or 64 bytes)", len(keyBytes))
	}

	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)
	addr := base58.Encode(pubKey)
	return privKey, pubKey, addr, nil
}

// NewForwarder creates a key-based auto-forwarder (TxTypeTransferByKey).
func NewForwarder(endpoint string, useTLS bool, fundAddr string, rawPrivateKey string, botAddr string, wallets *UserWalletMap, logger *slog.Logger) (*Forwarder, error) {
	if rawPrivateKey == "" {
		return nil, fmt.Errorf("private key is required")
	}
	if fundAddr == "" {
		return nil, fmt.Errorf("fund wallet address is required")
	}

	privKey, pubKey, derivedAddr, err := ParsePrivateKey(rawPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse bot private key: %w", err)
	}

	activeBotAddr := derivedAddr
	if botAddr != "" && botAddr != derivedAddr {
		if logger != nil {
			logger.Warn("configured BOT_WALLET_ADDRESS does not match address derived from BOT_PRIVATE_KEY; using derived address",
				"configured", botAddr, "derived", derivedAddr)
		}
	}

	c, err := client.NewClient(client.Config{
		Endpoint: endpoint,
		UseTLS:   useTLS,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to node %s: %w", endpoint, err)
	}

	return &Forwarder{
		sender:     c,
		botAddr:    activeBotAddr,
		botPubKey:  pubKey,
		botPrivKey: privKey,
		fundAddr:   fundAddr,
		wallets:    wallets,
		log:        logger,
	}, nil
}

// NewZKForwarder creates a native Mezon ZK auto-forwarder (TxTypeTransferByZk).
// No private key is required; it authenticates via Mezon session and uses Mezon ZK proof service.
func NewZKForwarder(endpoint string, useTLS bool, zkEndpoint, mezonHost, botID, botToken, fundAddr, botAddr string, wallets *UserWalletMap, logger *slog.Logger) (*Forwarder, error) {
	if botID == "" || botToken == "" {
		return nil, fmt.Errorf("bot ID and bot token are required for ZK forwarder")
	}
	if fundAddr == "" {
		return nil, fmt.Errorf("fund wallet address is required")
	}

	derivedAddr := DeriveBotAddress(botID)
	activeBotAddr := derivedAddr
	if botAddr != "" && botAddr != derivedAddr {
		if logger != nil {
			logger.Info("using custom BOT_WALLET_ADDRESS for ZK forwarder", "custom", botAddr, "derived", derivedAddr)
		}
		activeBotAddr = botAddr
	}

	c, err := client.NewClient(client.Config{
		Endpoint: endpoint,
		UseTLS:   useTLS,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to node %s: %w", endpoint, err)
	}

	if zkEndpoint == "" {
		zkEndpoint = "https://dong.mezon.ai/zk-api/prove"
	}
	if mezonHost == "" {
		mezonHost = "gw.mezon.ai"
	}

	return &Forwarder{
		sender:     c,
		botAddr:    activeBotAddr,
		fundAddr:   fundAddr,
		wallets:    wallets,
		log:        logger,
		useZK:      true,
		botID:      botID,
		botToken:   botToken,
		mezonHost:  mezonHost,
		zkEndpoint: zkEndpoint,
		zkProver:   &DefaultZkProver{},
	}, nil
}

// NewForwarderWithSender creates a Forwarder with an explicit TxSender (useful for testing).
func NewForwarderWithSender(sender TxSender, fundAddr string, privKey ed25519.PrivateKey, botAddr string, wallets *UserWalletMap, logger *slog.Logger) *Forwarder {
	pubKey := privKey.Public().(ed25519.PublicKey)
	derivedAddr := base58.Encode(pubKey)
	activeAddr := derivedAddr
	if botAddr != "" {
		activeAddr = botAddr
	}

	return &Forwarder{
		sender:     sender,
		botAddr:    activeAddr,
		botPubKey:  pubKey,
		botPrivKey: privKey,
		fundAddr:   fundAddr,
		wallets:    wallets,
		log:        logger,
	}
}

// NewZKForwarderWithSender creates a ZK Forwarder with mock sender and prover (useful for testing).
func NewZKForwarderWithSender(sender TxSender, prover ZkProver, fundAddr, botAddr, botID, idToken string, wallets *UserWalletMap, logger *slog.Logger) *Forwarder {
	return &Forwarder{
		sender:      sender,
		zkProver:    prover,
		botAddr:     botAddr,
		fundAddr:    fundAddr,
		wallets:     wallets,
		log:         logger,
		useZK:       true,
		botID:       botID,
		cachedToken: idToken,
		tokenExpiry: time.Now().Add(24 * time.Hour),
	}
}

// BotAddress returns the address of the bot wallet.
func (f *Forwarder) BotAddress() string {
	return f.botAddr
}

// FundAddress returns the destination fund wallet address.
func (f *Forwarder) FundAddress() string {
	return f.fundAddr
}

// IsZKMode reports whether the forwarder is operating in native Mezon ZK proof mode.
func (f *Forwarder) IsZKMode() bool {
	return f.useZK
}

// Close closes the underlying connection.
func (f *Forwarder) Close() error {
	if f.sender != nil {
		return f.sender.Close()
	}
	return nil
}

func (f *Forwarder) getIdToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.cachedToken != "" && time.Now().Before(f.tokenExpiry) {
		return f.cachedToken, nil
	}

	host := f.mezonHost
	if host == "" {
		host = "gw.mezon.ai"
	}
	basePath := "https://" + host
	api := mezon.NewMezonApi(f.botToken, basePath, 10*time.Second)
	sess, err := api.MezonAuthenticate(f.botID, f.botToken)
	if err != nil {
		return "", fmt.Errorf("mezon authenticate: %w", err)
	}
	if sess.IdToken == "" {
		return "", fmt.Errorf("empty id_token returned from mezon authenticate")
	}

	f.cachedToken = sess.IdToken
	f.tokenExpiry = time.Now().Add(45 * time.Minute)
	return f.cachedToken, nil
}

// Forward forwards an incoming movement from the bot wallet to the fund wallet on-chain.
func (f *Forwarder) Forward(ctx context.Context, m Movement) (string, error) {
	if m.Raw == nil || m.Raw.Sign() <= 0 {
		return "", fmt.Errorf("invalid or non-positive amount")
	}

	u256Amount, overflow := uint256.FromBig(m.Raw)
	if overflow || u256Amount == nil || u256Amount.IsZero() {
		return "", fmt.Errorf("amount overflow or zero")
	}

	senderName := m.Sender
	if f.wallets != nil {
		if u, ok := f.wallets.LookupUser(m.Sender); ok {
			senderName = u
		}
	}

	var forwardText string
	if m.Content != "" {
		forwardText = fmt.Sprintf("%s transfer via bot: %s", senderName, m.Content)
	} else {
		forwardText = fmt.Sprintf("%s transfer via bot", senderName)
	}

	extraInfo := map[string]string{
		"type":             "forward_via_bot",
		"original_sender":   m.Sender,
		"original_user":     senderName,
		"original_content":  m.Content,
		"original_tx":       m.Hash,
	}

	nonce, err := f.nextNonce(ctx)
	if err != nil {
		return "", fmt.Errorf("get nonce: %w", err)
	}

	if f.useZK {
		return f.forwardZK(ctx, u256Amount, nonce, forwardText, extraInfo)
	}
	return f.forwardKey(ctx, u256Amount, nonce, forwardText, extraInfo)
}

func (f *Forwarder) forwardZK(ctx context.Context, amount *uint256.Int, nonce uint64, forwardText string, extraInfo map[string]string) (string, error) {
	// 1. Generate ephemeral Ed25519 keypair
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", fmt.Errorf("generate ephemeral key: %w", err)
	}
	ephemeralPK := base58.Encode(pub)

	// 2. Get ID Token (JWT)
	idToken, err := f.getIdToken(ctx)
	if err != nil {
		return "", fmt.Errorf("get id token: %w", err)
	}

	// 3. Request ZK proof from Mezon ZK prover service
	prover := f.zkProver
	if prover == nil {
		prover = &DefaultZkProver{}
	}
	proof, pubInput, err := prover.RequestZkProof(ctx, f.zkEndpoint, f.botID, idToken, f.botAddr, ephemeralPK)
	if err != nil {
		return "", fmt.Errorf("obtain zk proof: %w", err)
	}

	// 4. Build Tx (TxTypeTransferByZk = 0)
	unsigned, err := client.BuildTransferTx(
		client.TxTypeTransferByZk,
		f.botAddr,
		f.fundAddr,
		amount,
		nonce,
		uint64(time.Now().Unix()),
		forwardText,
		extraInfo,
		proof,
		pubInput,
	)
	if err != nil {
		return "", fmt.Errorf("build zk transfer tx: %w", err)
	}

	// 5. Sign with ephemeral private key
	signedTx, err := client.SignTx(unsigned, pub, priv.Seed())
	if err != nil {
		return "", fmt.Errorf("sign zk transfer tx: %w", err)
	}

	// 6. Submit to node
	res, err := f.sender.AddTx(ctx, signedTx)
	if err != nil {
		f.mu.Lock()
		f.lastNonce = 0
		f.mu.Unlock()
		return "", fmt.Errorf("submit zk transfer tx: %w", err)
	}

	return res.TxHash, nil
}

func (f *Forwarder) forwardKey(ctx context.Context, amount *uint256.Int, nonce uint64, forwardText string, extraInfo map[string]string) (string, error) {
	unsigned, err := client.BuildTransferTx(
		client.TxTypeTransferByKey,
		f.botAddr,
		f.fundAddr,
		amount,
		nonce,
		uint64(time.Now().Unix()),
		forwardText,
		extraInfo,
		"",
		"",
	)
	if err != nil {
		return "", fmt.Errorf("build transfer tx: %w", err)
	}

	signedTx, err := client.SignTx(unsigned, f.botPubKey, f.botPrivKey.Seed())
	if err != nil {
		return "", fmt.Errorf("sign transfer tx: %w", err)
	}

	res, err := f.sender.AddTx(ctx, signedTx)
	if err != nil {
		f.mu.Lock()
		f.lastNonce = 0
		f.mu.Unlock()
		return "", fmt.Errorf("submit transfer tx: %w", err)
	}

	return res.TxHash, nil
}

func (f *Forwarder) nextNonce(ctx context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Query current pending nonce from the node (preferred for transaction sequencing)
	currentNonce, err := f.sender.GetCurrentNonce(ctx, f.botAddr, "pending")
	if err != nil {
		if latest, lerr := f.sender.GetCurrentNonce(ctx, f.botAddr, "latest"); lerr == nil {
			currentNonce = latest
		} else if acc, aerr := f.sender.GetAccount(ctx, f.botAddr); aerr == nil {
			currentNonce = acc.Nonce
		} else {
			return 0, fmt.Errorf("query account nonce: %w", err)
		}
	}

	if currentNonce > f.lastNonce {
		f.lastNonce = currentNonce
	}
	f.lastNonce++
	return f.lastNonce, nil
}
