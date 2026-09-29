package fund

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// DefaultUserWallets maps usernames to their known wallet addresses.
var DefaultUserWallets = map[string]string{
	"anh.leduc1":          "imBbCvQQYAbzVTHt5ZPMNYYisv1xrWgDYDDNxUfjwe7",
	"chinh.nguyenduc":      "8btLpUVkMeN4UdhL49p23QLKNtjSrKnUiyTabBHFVesd",
	"dai.trinhduc":        "DzSGqEnhbpdLnsAPqyhAD97UxTKeHWmCaMm3nENQasbW",
	"dat.dangvan":         "BHqruUmQUhKX9rJSYXtntQaUVSzKdWzth5fxVeWNunkZ",
	"don.nguyenthanh":     "HcMdZEv11tEL1AQQ6s13ZwcqNpUN95hAVXwgu3NZKT5D",
	"duc.phamhuyen":       "5uqukmZvS9bA6oTicob4Xq1JMkFPmRDZ7qW9pcRVuakQ",
	"dung.nguyencong":     "BqqrnMdXhSCFHrrevj1nejDLWSjX86Nz5jmDCq7QPqgb",
	"dung.luongthithao":   "3LuMSX2MQ3W6WLPaiRVur9331tzQmLN8oNxkLGk9MERE",
	"gia.chuvan":          "7tPjKTpzaReThtihFNHwoVYx1iSLMAjmVM26FnR1zGDP",
	"ha.tranngan":         "3EKNZRtamWG93L2qs9gZCmEi52bEvj3PnsDj8NM6QRH7",
	"hai.nguyensinh":      "EmJWZe77qoLLZRRax34vZMPrVevgWtqq2wULsstiV8NK",
	"hang.nguyenthu":      "3GSr72Jf2sGcfHwDMG3FHPNc7NTyPivYEeYgyhk7DP5Z",
	"hau.duongphuc":       "2P9r2Sq5LSHPpLdx4ndr35oQNVGPvrBXhyVEsZ3JQ54b",
	"hiep.ngoxuan":        "BmdaNsAgj6eYchBeFczKP2Fvvpk5X2Nih7btmX1U5JZX",
	"hieu.nguyendinh":     "2rancZc6UutnnKPnSBQB8LqrLkFhPM5V3bQQKoVdFpDe",
	"hoang.nguyendinh":    "2MV1S7otjTSHRDxcUro43t2g2nVwigsFiaprb1TwzntU",
	"hoang.nguyenviet2":   "EwEYmXz6FtwKRHdM5Mrt9asu7UNnNSsuAt2FEoqLEXK6",
	"huy.lexuan":          "8GnRq4GBkcRSAEKL3wKnHMADZJnXWGK25nfybEcF2tHp",
	"khanh.phamvan":       "7hdLpKe6Pq6uW7dntF6UzEjd6KbxdBmEDqZ8cweu9P3m",
	"lehongha638":         "7busaz7MMpJX5egJ7rKYGbyh4KphMcChb5CUWQtoVTUj",
	"lichduongthanh":      "9qkKFDgfeRHiCC2pJEkoR3QHpDoHpZw5YTvXWP4cbFWk",
	"man.maihong":         "3d4UBU557Mdwkyvv8WPh1ErjZNcoS5PqfzSMKbh65vkN",
	"nga.nguyenthithanh":  "GoY8YLAVvSZff4tSvGCUaN7fNvbaSZe5Gk5eCj533K5d",
	"ngoc.maithuy":        "C6SiVpVQskeijZactQXxDsA6Ke78C9c8Dio9mZnM9PAh",
	"ngtlong":             "9iTfuWTCmfL1FcepbqrE1tkUekSgPW9ASvDqNUv4r12j",
	"nguyen.hoangphuong":  "Gyy2k4xc72WMpJwCEHb2cRwpmJZZUtefHHTqpxToFWMG",
	"nguyentran":          "HMRrnAU2bXHef5BrQm2oyR54Ycnnri9fxYqnh4sKEGHP",
	"obitanz123":          "EifW3vRy6Q3aWPyyp1smGRyvxwd1YNKw1akMxvXaCF1m",
	"phong.nguyennam":     "HicZqrwoEbuJfkW8WT8DE86XhwnqZoRVBujSqeQ8NLm4",
	"quan.nguyenxuan":     "731jcPAHVuDsiwp3cFeZdNVvdt7bf4o4Uu5uX7RzYr4i",
	"quy.trandinh":        "2DpkbA47DPXAT5BLhnLBwzov4rucbLE6d2J1anUWnGwf",
	"quyen.nguyenta":      "oc1gy2vMKZJqjVLN7kbY3RhgD65AYBUA5JXnyCdQipA",
	"son.lesytruong":      "6x7nuT9vzoeWCgCooqEhe1pxjsBuKK6yZVtwaGaPiHC",
	"son.nguyenhoai":      "9BZPNGxqHT1G3M9RUvJ1p7msXDd6QY93zYoxMkSfHkqj",
	"son.nguyentruong1":   "7Lw59Pf6EzvqL1TsnBnvFM5sUQMvgQvCXRWVgo2Cgmrb",
	"tai.cumanhtuan":      "5F758XLD4KgWHEEFmxkCGNMyGs7bqWyLmogZqZtBvGQJ",
	"thanh.levan":         "6v5TmsX8wmbx8t7fbjLumq6u9grp4K4ugXZQsLmyBp8c",
	"thanh.maihoai":       "BPJLbu1cuCnTDLqtohaNavMxoC2ezE4bMQcnzHnaMWXo",
	"thaodungc3pbc":       "3LuMSX2MQ3W6WLPaiRVur9331tzQmLN8oNxkLGk9MERE",
	"thin.tranhoang":      "CYfQBR7WJZ7U7iwThMhUY9yvgS9AUyXAHQMUTCNjoV4x",
	"thinh.nguyenvan":     "9ELNdjaTSjUZYriWMhPCuNkVumTxvzsixEh8xF24XWXd",
	"thong.nguyendinhminh": "B6NZU3jj9NA5qiqq6gfwt7DyZCmTNCaw9qZ27B7Hj8em",
	"tinh.phamthe":        "4CxbGpNQ3pwM32jPqjtdBBaNurTd2tPmSKvVJU2ZGddd",
	"toan.nguyendinh":     "25iNCHzfU9Vzd2s1JbYNoZVp3tgr5DHkDvMgNJzd4Ytw",
	"tri.dangcao":         "3CFtSno7DqFng5a1GbY9oNnkbHsBrCuXWTPpzGRhuJQv",
	"trinh.nguyenthimai":  "EpUZEbFSpLmkKQo2qSaGX6aMy81GZ5MGAY5MzonGWDKq",
	"tuan.nguyenhuu":      "EqW5EYzwBmKVj1k5fzSEbwZqkLU4e16veeGjhTpPMbC2",
	"tuan.nguyenngocanh":  "DCMyxfPMT8rYsSe7RbJ43RSxSV2X4twR43GQLUS8Qeu6",
	"tuan.phamminh2":      "6rDZgawocWwQsZd7Qn77oJKTV2wzodQgRKnafLFS2MgW",
	"vinh.nguyenquoc3":    "6mEaUo1L92pAHK7jNdAiSmZ2iA2CMXCf3bN5AFcpKwmZ",
	"vinh.tranhuu":        "AoENDHTeGcsrG5X2xMV8jqE46YiNTZkrrSeoHhJvCgiz",
}

// UserWalletMap provides bidirectional mapping between usernames and wallet addresses.
type UserWalletMap struct {
	mu           sync.RWMutex
	userToWallet map[string]string
	walletToUser map[string]string
}

// DefaultWallets is initialized with DefaultUserWallets.
var DefaultWallets = NewUserWalletMap(DefaultUserWallets)

// NewUserWalletMap creates a UserWalletMap from a map of usernames to wallet addresses.
func NewUserWalletMap(usersToWallets map[string]string) *UserWalletMap {
	m := &UserWalletMap{
		userToWallet: make(map[string]string),
		walletToUser: make(map[string]string),
	}
	for user, wallet := range usersToWallets {
		m.Add(user, wallet)
	}
	return m
}

// Add associates a username with a wallet address.
func (m *UserWalletMap) Add(user, wallet string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := strings.TrimSpace(user)
	w := strings.TrimSpace(wallet)
	if u == "" || w == "" {
		return
	}
	m.userToWallet[u] = w
	// If multiple users share the same wallet, first one set stays or overwrites.
	if _, exists := m.walletToUser[w]; !exists {
		m.walletToUser[w] = u
	}
}

// LookupUser finds the username corresponding to a wallet address.
func (m *UserWalletMap) LookupUser(wallet string) (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.walletToUser[strings.TrimSpace(wallet)]
	return user, ok
}

// LookupWallet finds the wallet address corresponding to a username.
func (m *UserWalletMap) LookupWallet(user string) (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	wallet, ok := m.userToWallet[strings.TrimSpace(user)]
	return wallet, ok
}

// All returns a copy of the user to wallet map.
func (m *UserWalletMap) All() map[string]string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.userToWallet))
	for k, v := range m.userToWallet {
		out[k] = v
	}
	return out
}

// LoadUserWalletMap reads a JSON mapping file of user -> wallet address,
// merging it with DefaultUserWallets.
func LoadUserWalletMap(path string) (*UserWalletMap, error) {
	m := NewUserWalletMap(DefaultUserWallets)
	if path == "" {
		return m, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, fmt.Errorf("read wallet map file %s: %w", path, err)
	}

	var custom map[string]string
	if err := json.Unmarshal(data, &custom); err != nil {
		return nil, fmt.Errorf("decode wallet map file %s: %w", path, err)
	}

	for u, w := range custom {
		m.Add(u, w)
	}
	return m, nil
}
