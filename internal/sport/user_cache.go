package sport

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// UserProfile holds identity mappings for a clan member.
type UserProfile struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	ClanNick    string `json:"clan_nick"`
}

// BestDisplayName returns ClanNick, fallback to DisplayName, fallback to Username.
func (p *UserProfile) BestDisplayName() string {
	if p == nil {
		return ""
	}
	if nick := strings.TrimSpace(p.ClanNick); nick != "" {
		return nick
	}
	if disp := strings.TrimSpace(p.DisplayName); disp != "" {
		return disp
	}
	return strings.TrimSpace(p.Username)
}

// UserRegistry manages clan nicknames, usernames, and common aliases.
type UserRegistry struct {
	mu            sync.RWMutex
	byID          map[string]*UserProfile
	byUsername    map[string]*UserProfile
	byDisplayName map[string]*UserProfile
	aliases       map[string]string
	filePath      string
}

// Default known badminton clan member mappings for immediate availability.
var defaultUserProfiles = []UserProfile{
	{Username: "son.nguyentruong1", ClanNick: "Sơn Nguyễn Trường", DisplayName: "Sơn Nguyễn Trường"},
	{Username: "dai.trinhduc", ClanNick: "Trịnh Đức Đại", DisplayName: "Đức Đại"},
	{Username: "anh.leduc1", ClanNick: "Lê Đức Anh", DisplayName: "Lê Đức Anh"},
	{Username: "dphau29082002", ClanNick: "Đới Phi Hậu", DisplayName: "Hậu Đới"},
	{Username: "hau.duongphuc", ClanNick: "Đới Phi Hậu", DisplayName: "Đới Phi Hậu"},
	{Username: "hoang.nguyenviet2", ClanNick: "Hoàng Nguyễn Việt", DisplayName: "Việt Hoàng"},
	{Username: "ntdon1502", ClanNick: "Nguyễn Thành Đôn", DisplayName: "Đôn Nguyễn"},
	{Username: "don.nguyenthanh", ClanNick: "Nguyễn Thành Đôn", DisplayName: "Nguyễn Thành Đôn"},
	{Username: "tai.cumanhtuan", ClanNick: "Tài Cù Mạnh Tuấn", DisplayName: "Tài Cù Mạnh Tuấn"},
	{Username: "toannguyen03.arc", ClanNick: "Nguyễn Đình Toàn", DisplayName: "Toàn Nguyễn"},
	{Username: "toan.nguyendinh", ClanNick: "Nguyễn Đình Toàn", DisplayName: "Nguyễn Đình Toàn"},
	{Username: "tuan.phamminh2", ClanNick: "Phạm Minh Tuấn", DisplayName: "Tuấn Phạm Minh"},
	{Username: "chinh.nguyenduc", ClanNick: "Nguyễn Đức Chính", DisplayName: "Chính Nguyễn Đức"},
	{Username: "hoang.nguyendinh", ClanNick: "Nguyễn Đình Hoàng", DisplayName: "Hoàng Nguyễn Đình"},
	{Username: "tridc1803", ClanNick: "Đặng Cao Trí", DisplayName: "Trí Đặng Cao"},
	{Username: "tri.dangcao", ClanNick: "Đặng Cao Trí", DisplayName: "Đặng Cao Trí"},
	{Username: "son.lesytruong", ClanNick: "Lê Sỹ Trường Sơn", DisplayName: "Lê Sỹ Trường Sơn"},
	{Username: "son.nguyenhoai", ClanNick: "Nguyễn Hoài Sơn", DisplayName: "Nguyễn Hoài Sơn"},
	{Username: "ha.tranngan", ClanNick: "Trần Ngân Hà", DisplayName: "Hà Trần Ngân"},
	{Username: "hieu.nguyendinh", ClanNick: "Nguyễn Đình Hiếu", DisplayName: "Hiếu Nguyễn Đình"},
}

// Known aliases mapping nicknames/emails to canonical usernames.
var defaultAliases = map[string]string{
	"son.nt":            "son.nguyentruong1",
	"son nt":            "son.nguyentruong1",
	"sơn nt":            "son.nguyentruong1",
	"sơn nguyễn trường": "son.nguyentruong1",
	"son nguyentruong":  "son.nguyentruong1",
	"dai.td":            "dai.trinhduc",
	"đức đại":           "dai.trinhduc",
	"trịnh đức đại":     "dai.trinhduc",
	"don.nt":            "ntdon1502",
	"đôn nguyễn":        "ntdon1502",
	"nguyễn thành đôn":  "ntdon1502",
	"hau.dp":            "dphau29082002",
	"hậu đới":           "dphau29082002",
	"đới phi hậu":       "dphau29082002",
	"hoang.nv":          "hoang.nguyenviet2",
	"việt hoàng":        "hoang.nguyenviet2",
	"hoàng nguyễn việt": "hoang.nguyenviet2",
	"tai.cm":            "tai.cumanhtuan",
	"tài cù mạnh tuấn":  "tai.cumanhtuan",
	"chinh.nd":          "chinh.nguyenduc",
	"chính nguyễn đức":  "chinh.nguyenduc",
	"tuan.pm":           "tuan.phamminh2",
	"tuấn phạm minh":    "tuan.phamminh2",
	"phạm minh tuấn":    "tuan.phamminh2",
	"hoang.nd":          "hoang.nguyendinh",
	"hoàng nguyễn đình": "hoang.nguyendinh",
	"toan.nd":           "toannguyen03.arc",
	"toàn nguyễn":       "toannguyen03.arc",
	"nguyễn đình toàn":  "toannguyen03.arc",
	"tri.dc":            "tridc1803",
	"trí đặng cao":      "tridc1803",
	"đặng cao trí":      "tridc1803",
}

// NewUserRegistry initializes a user registry with defaults and loads persisted data if available.
func NewUserRegistry(filePath string) *UserRegistry {
	r := &UserRegistry{
		byID:          make(map[string]*UserProfile),
		byUsername:    make(map[string]*UserProfile),
		byDisplayName: make(map[string]*UserProfile),
		aliases:       make(map[string]string),
		filePath:      filePath,
	}

	// Load defaults
	for _, p := range defaultUserProfiles {
		clone := p
		r.indexProfileLocked(&clone)
	}
	for k, v := range defaultAliases {
		r.aliases[strings.ToLower(k)] = v
	}

	if filePath != "" {
		_ = r.loadFromFile()
	}

	return r
}

func (r *UserRegistry) indexProfileLocked(p *UserProfile) {
	if p.UserID != "" {
		r.byID[p.UserID] = p
	}
	if p.Username != "" {
		r.byUsername[strings.ToLower(p.Username)] = p
	}
	if p.ClanNick != "" {
		r.byDisplayName[strings.ToLower(p.ClanNick)] = p
	}
	if p.DisplayName != "" {
		r.byDisplayName[strings.ToLower(p.DisplayName)] = p
	}
}

// Register adds or updates a user profile.
func (r *UserRegistry) Register(userID, username, clanNick, displayName string) {
	userID = strings.TrimSpace(userID)
	username = strings.TrimSpace(username)
	clanNick = strings.TrimSpace(clanNick)
	displayName = strings.TrimSpace(displayName)

	if userID == "" && username == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var existing *UserProfile
	if userID != "" {
		existing = r.byID[userID]
	}
	if existing == nil && username != "" {
		existing = r.byUsername[strings.ToLower(username)]
	}

	if existing != nil {
		if clanNick != "" {
			existing.ClanNick = clanNick
		}
		if displayName != "" {
			existing.DisplayName = displayName
		}
		if username != "" {
			existing.Username = username
		}
		if userID != "" {
			existing.UserID = userID
		}
		r.indexProfileLocked(existing)
	} else {
		newP := &UserProfile{
			UserID:      userID,
			Username:    username,
			DisplayName: displayName,
			ClanNick:    clanNick,
		}
		r.indexProfileLocked(newP)
	}

	if r.filePath != "" {
		_ = r.saveToFileLocked()
	}
}

// ResolveDisplayName resolves a userID or username/alias to their preferred Clan Nickname.
func (r *UserRegistry) ResolveDisplayName(userID, username string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if userID != "" {
		if p, ok := r.byID[userID]; ok {
			if disp := p.BestDisplayName(); disp != "" {
				return disp
			}
		}
	}

	clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(username, "@")))
	if clean == "" {
		if userID != "" {
			return userID
		}
		return ""
	}

	// Check alias
	if target, ok := r.aliases[clean]; ok {
		clean = strings.ToLower(target)
	}

	if p, ok := r.byUsername[clean]; ok {
		if disp := p.BestDisplayName(); disp != "" {
			return disp
		}
	}

	if p, ok := r.byDisplayName[clean]; ok {
		if disp := p.BestDisplayName(); disp != "" {
			return disp
		}
	}

	if username != "" {
		return strings.TrimPrefix(username, "@")
	}
	return userID
}

// AddAlias maps an alias string (e.g. "son.nt") to a canonical username (e.g. "son.nguyentruong1").
func (r *UserRegistry) AddAlias(alias, canonicalUsername string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[strings.ToLower(strings.TrimSpace(alias))] = strings.TrimSpace(canonicalUsername)
}

func (r *UserRegistry) loadFromFile() error {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return err
	}
	var list []UserProfile
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range list {
		r.indexProfileLocked(&list[i])
	}
	return nil
}

func (r *UserRegistry) saveToFileLocked() error {
	var list []UserProfile
	seen := make(map[*UserProfile]bool)
	for _, p := range r.byID {
		if !seen[p] {
			seen[p] = true
			list = append(list, *p)
		}
	}
	for _, p := range r.byUsername {
		if !seen[p] {
			seen[p] = true
			list = append(list, *p)
		}
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.filePath, data, 0644)
}
