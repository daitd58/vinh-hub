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

// BestDisplayName returns ClanNick if set, fallback to Username.
func (p *UserProfile) BestDisplayName() string {
	if p == nil {
		return ""
	}
	if nick := strings.TrimSpace(p.ClanNick); nick != "" {
		return nick
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

// Default known badminton clan member mappings fetched directly from Mezon MCP.
// Keeps usernames/clan nicknames as-is without translating to real names.
var defaultUserProfiles = []UserProfile{
	{UserID: "1783438504739475456", Username: "anh.leduc1", DisplayName: "anh.leduc1", ClanNick: "anh.leduc1"},
	{UserID: "1823998475507863552", Username: "chinh.nguyenduc", DisplayName: "chinh.nguyenduc", ClanNick: "chinh.nguyenduc"},
	{UserID: "1783440675396653056", Username: "dai.trinhduc", DisplayName: "dai.trinhduc", ClanNick: "dai.trinhduc"},
	{UserID: "1840678415796015104", Username: "dat.dangvan", DisplayName: "dat.dangvan", ClanNick: "dat.dangvan"},
	{UserID: "1946167706600345600", Username: "ntdon1502", DisplayName: "don.nguyenthanh", ClanNick: "don.nguyenthanh"},
	{UserID: "2087103424536514560", Username: "thaodungc3pbc", DisplayName: "dung.luongthithao", ClanNick: "dung.luongthithao"},
	{UserID: "1929365076809093120", Username: "dung.nguyencong", DisplayName: "dung.nguyencong", ClanNick: "dung.nguyencong"},
	{UserID: "1821743062989148160", Username: "vangiachu", DisplayName: "Chu Gia", ClanNick: "gia.chuvan"},
	{UserID: "1840671876997713920", Username: "ha.tranngan", DisplayName: "", ClanNick: "ha.tranngan"},
	{UserID: "1807983162756698112", Username: "hai.nguyensinh", DisplayName: "Sinh Hải", ClanNick: "hai.nguyensinh"},
	{UserID: "1946197154334773248", Username: "dphau29082002", DisplayName: "hau.duongphuc", ClanNick: "hau.duongphuc"},
	{UserID: "1783441990394843136", Username: "hieu.nguyendinh", DisplayName: "hieu.nguyendinh", ClanNick: "hieu.nguyendinh"},
	{UserID: "1783443441674358784", Username: "hoang.nguyendinh", DisplayName: "Em Hoàng", ClanNick: "hoang.nguyendinh"},
	{UserID: "1996036237500092416", Username: "huyhoang160420", DisplayName: "hoang.nguyenhuy1", ClanNick: "hoang.nguyenhuy1"},
	{UserID: "1831540707106492416", Username: "hoang.nguyenviet2", DisplayName: "Nguyễn Hoàng", ClanNick: "hoang.nguyenviet2"},
	{UserID: "1826814768338440192", Username: "huy.lexuan", DisplayName: "huy.lexuan", ClanNick: "huy.lexuan"},
	{UserID: "1780085418822209536", Username: "khanh.phamvan", DisplayName: "khanh.phamvan", ClanNick: "khanh.phamvan"},
	{UserID: "1826067167154540544", Username: "phu444080", DisplayName: "phu.nguyenthien", ClanNick: "KOMU"},
	{UserID: "1827994776956309504", Username: "man.maihong", DisplayName: "Mai Hong Man", ClanNick: "man.maihong"},
	{UserID: "1782303705618452480", Username: "nga.nguyenthithanh", DisplayName: "Nguyen Nga", ClanNick: "nga.nguyenthithanh"},
	{UserID: "1783441497253744640", Username: "ngoc.maithuy", DisplayName: "", ClanNick: "ngoc.maithuy"},
	{UserID: "1783442196159008768", Username: "nguyen.hoangphuong", DisplayName: "Hoang Phuong Nguyen", ClanNick: "nguyen.hoangphuong"},
	{UserID: "1831634827619602432", Username: "quyen.nguyenta", DisplayName: "", ClanNick: "quyen.nguyenta"},
	{UserID: "1795277119710826496", Username: "son.nguyenhoai", DisplayName: "Son Nguyen Hoai", ClanNick: "son.nguyenhoai"},
	{UserID: "1810223235401256960", Username: "son.nguyentruong1", DisplayName: "son.nt", ClanNick: "son.nguyentruong1"},
	{UserID: "1826122746413715456", Username: "tai.cumanhtuan", DisplayName: "俊才", ClanNick: "tai.cumanhtuan"},
	{UserID: "1783441673976549376", Username: "thanh.levan", DisplayName: "", ClanNick: "thanh.levan"},
	{UserID: "1840669672182124544", Username: "thin.tranhoang", DisplayName: "thin.tranhoang", ClanNick: "thin.tranhoang"},
	{UserID: "1783443471286145024", Username: "thinh.nguyenvan", DisplayName: "Cam Vinh", ClanNick: "thinh.nguyenvan"},
	{UserID: "1983278895796850688", Username: "sunnlessmt09", DisplayName: "thong.nguyendinhminh", ClanNick: "thong.nguyendinhminh"},
	{UserID: "1986634093076418560", Username: "toannguyen03.arc", DisplayName: "toan.nguyendinh", ClanNick: "toan.nguyendinh"},
	{UserID: "1952194848102551552", Username: "tridc1803", DisplayName: "tri.dangcao", ClanNick: "tri.dangcao"},
	{UserID: "1831524472864116736", Username: "trinh.nguyenthimai", DisplayName: "him", ClanNick: "trinh.nguyenthimai"},
	{UserID: "1783441468711505920", Username: "tuan.nguyenhuu", DisplayName: "Tuấng", ClanNick: "tuan.nguyenhuu"},
	{UserID: "1833682843671203840", Username: "tuan.nguyenngocanh", DisplayName: "tuan.nguyenngocanh", ClanNick: "tuan.nguyenngocanh"},
	{UserID: "1782303543722512384", Username: "tuan.phamminh2", DisplayName: "tuan.phamminh2☕", ClanNick: "tuan.phamminh2"},
	{UserID: "1783444369026584576", Username: "vinh.nguyenquoc3", DisplayName: "Nguyen Quoc Vinh", ClanNick: "vinh.nguyenquoc3"},
	{UserID: "1783442595926511616", Username: "hang.nguyenthu", DisplayName: "hang.nguyenthu", ClanNick: "hang.nguyenthu"},
	{UserID: "1831538106453463040", Username: "thanh.maihoai", DisplayName: "thanh.maihoai", ClanNick: "thanh.maihoai"},
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
	"don.nt":            "don.nguyenthanh",
	"đôn nguyễn":        "don.nguyenthanh",
	"nguyễn thành đôn":  "don.nguyenthanh",
	"ntdon1502":         "don.nguyenthanh",
	"hau.dp":            "hau.duongphuc",
	"hậu đới":           "hau.duongphuc",
	"đới phi hậu":       "hau.duongphuc",
	"dphau29082002":     "hau.duongphuc",
	"dương phúc hậu":    "hau.duongphuc",
	"hoang.nv":          "hoang.nguyenviet2",
	"việt hoàng":        "hoang.nguyenviet2",
	"hoàng nguyễn việt": "hoang.nguyenviet2",
	"nguyễn hoàng":      "hoang.nguyenviet2",
	"đỗ thánh":          "hoang.nguyenviet2",
	"tai.cm":            "tai.cumanhtuan",
	"tài cù mạnh tuấn":  "tai.cumanhtuan",
	"chinh.nd":          "chinh.nguyenduc",
	"chính nguyễn đức":  "chinh.nguyenduc",
	"tuan.pm":           "tuan.phamminh2",
	"tuấn phạm minh":    "tuan.phamminh2",
	"phạm minh tuấn":    "tuan.phamminh2",
	"hoang.nd":          "hoang.nguyendinh",
	"hoàng nguyễn đình": "hoang.nguyendinh",
	"toan.nd":           "toan.nguyendinh",
	"toàn nguyễn":       "toan.nguyendinh",
	"nguyễn đình toàn":  "toan.nguyendinh",
	"toannguyen03.arc":  "toan.nguyendinh",
	"tri.dc":            "tri.dangcao",
	"trí đặng cao":      "tri.dangcao",
	"đặng cao trí":      "tri.dangcao",
	"tridc1803":         "tri.dangcao",
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

	if clean != "" {
		return clean
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
