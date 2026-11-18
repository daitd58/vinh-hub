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
	{UserID: "1821743062989148160", Username: "vangiachu", DisplayName: "gia.chuvan", ClanNick: "gia.chuvan"},
	{UserID: "1840671876997713920", Username: "ha.tranngan", DisplayName: "ha.tranngan", ClanNick: "ha.tranngan"},
	{UserID: "1807983162756698112", Username: "hai.nguyensinh", DisplayName: "hai.nguyensinh", ClanNick: "hai.nguyensinh"},
	{UserID: "1946197154334773248", Username: "dphau29082002", DisplayName: "hau.duongphuc", ClanNick: "hau.duongphuc"},
	{UserID: "1783441990394843136", Username: "hieu.nguyendinh", DisplayName: "hieu.nguyendinh", ClanNick: "hieu.nguyendinh"},
	{UserID: "1783443441674358784", Username: "hoang.nguyendinh", DisplayName: "hoang.nguyendinh", ClanNick: "hoang.nguyendinh"},
	{UserID: "1996036237500092416", Username: "huyhoang160420", DisplayName: "hoang.nguyenhuy1", ClanNick: "hoang.nguyenhuy1"},
	{UserID: "1831540707106492416", Username: "hoang.nguyenviet2", DisplayName: "hoang.nguyenviet2", ClanNick: "hoang.nguyenviet2"},
	{UserID: "1826814768338440192", Username: "huy.lexuan", DisplayName: "huy.lexuan", ClanNick: "huy.lexuan"},
	{UserID: "1780085418822209536", Username: "khanh.phamvan", DisplayName: "khanh.phamvan", ClanNick: "khanh.phamvan"},
	{UserID: "1826067167154540544", Username: "phu444080", DisplayName: "KOMU", ClanNick: "KOMU"},
	{UserID: "1827994776956309504", Username: "man.maihong", DisplayName: "man.maihong", ClanNick: "man.maihong"},
	{UserID: "1782303705618452480", Username: "nga.nguyenthithanh", DisplayName: "nga.nguyenthithanh", ClanNick: "nga.nguyenthithanh"},
	{UserID: "1783441497253744640", Username: "ngoc.maithuy", DisplayName: "ngoc.maithuy", ClanNick: "ngoc.maithuy"},
	{UserID: "1783442196159008768", Username: "nguyen.hoangphuong", DisplayName: "nguyen.hoangphuong", ClanNick: "nguyen.hoangphuong"},
	{UserID: "1831634827619602432", Username: "quyen.nguyenta", DisplayName: "quyen.nguyenta", ClanNick: "quyen.nguyenta"},
	{UserID: "1795277119710826496", Username: "son.nguyenhoai", DisplayName: "son.nguyenhoai", ClanNick: "son.nguyenhoai"},
	{UserID: "1810223235401256960", Username: "son.nguyentruong1", DisplayName: "son.nguyentruong1", ClanNick: "son.nguyentruong1"},
	{UserID: "1826122746413715456", Username: "tai.cumanhtuan", DisplayName: "tai.cumanhtuan", ClanNick: "tai.cumanhtuan"},
	{UserID: "1783441673976549376", Username: "thanh.levan", DisplayName: "thanh.levan", ClanNick: "thanh.levan"},
	{UserID: "1840669672182124544", Username: "thin.tranhoang", DisplayName: "thin.tranhoang", ClanNick: "thin.tranhoang"},
	{UserID: "1783443471286145024", Username: "thinh.nguyenvan", DisplayName: "thinh.nguyenvan", ClanNick: "thinh.nguyenvan"},
	{UserID: "1983278895796850688", Username: "sunnlessmt09", DisplayName: "thong.nguyendinhminh", ClanNick: "thong.nguyendinhminh"},
	{UserID: "1986634093076418560", Username: "toannguyen03.arc", DisplayName: "toan.nguyendinh", ClanNick: "toan.nguyendinh"},
	{UserID: "1952194848102551552", Username: "tridc1803", DisplayName: "tri.dangcao", ClanNick: "tri.dangcao"},
	{UserID: "1831524472864116736", Username: "trinh.nguyenthimai", DisplayName: "trinh.nguyenthimai", ClanNick: "trinh.nguyenthimai"},
	{UserID: "1783441468711505920", Username: "tuan.nguyenhuu", DisplayName: "tuan.nguyenhuu", ClanNick: "tuan.nguyenhuu"},
	{UserID: "1833682843671203840", Username: "tuan.nguyenngocanh", DisplayName: "tuan.nguyenngocanh", ClanNick: "tuan.nguyenngocanh"},
	{UserID: "1782303543722512384", Username: "tuan.phamminh2", DisplayName: "tuan.phamminh2", ClanNick: "tuan.phamminh2"},
	{UserID: "1783444369026584576", Username: "vinh.nguyenquoc3", DisplayName: "vinh.nguyenquoc3", ClanNick: "vinh.nguyenquoc3"},
	{UserID: "1783442595926511616", Username: "hang.nguyenthu", DisplayName: "hang.nguyenthu", ClanNick: "hang.nguyenthu"},
	{UserID: "1831538106453463040", Username: "thanh.maihoai", DisplayName: "thanh.maihoai", ClanNick: "thanh.maihoai"},
}

// Known aliases mapping nicknames/emails to canonical usernames.
var defaultAliases = map[string]string{
	// son.nguyentruong1
	"son.nt":            "son.nguyentruong1",
	"son nt":            "son.nguyentruong1",
	"sơn nt":            "son.nguyentruong1",
	"sơn nguyễn trường": "son.nguyentruong1",
	"son nguyentruong":  "son.nguyentruong1",
	"nguyễn trường sơn": "son.nguyentruong1",
	"nguyen truong son": "son.nguyentruong1",

	// dai.trinhduc
	"dai.td":        "dai.trinhduc",
	"đức đại":       "dai.trinhduc",
	"duc dai":       "dai.trinhduc",
	"trịnh đức đại": "dai.trinhduc",
	"trinh duc dai": "dai.trinhduc",

	// don.nguyenthanh
	"don.nt":           "don.nguyenthanh",
	"đôn nguyễn":       "don.nguyenthanh",
	"don nguyen":       "don.nguyenthanh",
	"nguyễn thành đôn": "don.nguyenthanh",
	"nguyen thanh don": "don.nguyenthanh",
	"ntdon1502":        "don.nguyenthanh",

	// hau.duongphuc
	"hau.dp":         "hau.duongphuc",
	"hậu đới":        "hau.duongphuc",
	"hau doi":        "hau.duongphuc",
	"đới phi hậu":    "hau.duongphuc",
	"doi phi hau":    "hau.duongphuc",
	"dphau29082002":  "hau.duongphuc",
	"dương phúc hậu": "hau.duongphuc",
	"duong phuc hau": "hau.duongphuc",

	// hoang.nguyenviet2
	"hoang.nv":          "hoang.nguyenviet2",
	"việt hoàng":        "hoang.nguyenviet2",
	"viet hoang":        "hoang.nguyenviet2",
	"hoàng nguyễn việt": "hoang.nguyenviet2",
	"hoang nguyen viet": "hoang.nguyenviet2",
	"nguyễn hoàng":      "hoang.nguyenviet2",
	"nguyen hoang":      "hoang.nguyenviet2",
	"đỗ thánh":          "hoang.nguyenviet2",
	"do thanh":          "hoang.nguyenviet2",

	// tai.cumanhtuan
	"tai.cm":           "tai.cumanhtuan",
	"tài cù mạnh tuấn": "tai.cumanhtuan",
	"tai cu manh tuan": "tai.cumanhtuan",
	"cù mạnh tuấn":     "tai.cumanhtuan",
	"cu manh tuan":     "tai.cumanhtuan",
	"俊才":               "tai.cumanhtuan",

	// chinh.nguyenduc
	"chinh.nd":         "chinh.nguyenduc",
	"chính nguyễn đức": "chinh.nguyenduc",
	"chinh nguyen duc": "chinh.nguyenduc",
	"nguyễn đức chính": "chinh.nguyenduc",
	"nguyen duc chinh": "chinh.nguyenduc",

	// tuan.phamminh2
	"tuan.pm":         "tuan.phamminh2",
	"tuấn phạm minh":  "tuan.phamminh2",
	"tuan pham minh":  "tuan.phamminh2",
	"phạm minh tuấn":  "tuan.phamminh2",
	"pham minh tuan":  "tuan.phamminh2",
	"tuấn phạm":       "tuan.phamminh2",
	"tuan pham":       "tuan.phamminh2",
	"tuan.phamminh2☕": "tuan.phamminh2",

	// hoang.nguyendinh
	"hoang.nd":          "hoang.nguyendinh",
	"hoàng nguyễn đình": "hoang.nguyendinh",
	"hoang nguyen dinh": "hoang.nguyendinh",
	"nguyễn đình hoàng": "hoang.nguyendinh",
	"nguyen dinh hoang": "hoang.nguyendinh",
	"em hoàng":          "hoang.nguyendinh",
	"em hoang":          "hoang.nguyendinh",

	// toan.nguyendinh
	"toan.nd":          "toan.nguyendinh",
	"toàn nguyễn":      "toan.nguyendinh",
	"toan nguyen":      "toan.nguyendinh",
	"nguyễn đình toàn": "toan.nguyendinh",
	"nguyen dinh toan": "toan.nguyendinh",
	"toannguyen03.arc": "toan.nguyendinh",

	// tri.dangcao
	"tri.dc":       "tri.dangcao",
	"trí đặng cao": "tri.dangcao",
	"tri dang cao": "tri.dangcao",
	"đặng cao trí": "tri.dangcao",
	"dang cao tri": "tri.dangcao",
	"tridc1803":    "tri.dangcao",

	// anh.leduc1
	"anh.ld":     "anh.leduc1",
	"lê đức anh": "anh.leduc1",
	"le duc anh": "anh.leduc1",
	"đức anh":    "anh.leduc1",
	"duc anh":    "anh.leduc1",

	// gia.chuvan
	"gia.cv":      "gia.chuvan",
	"chu văn gia": "gia.chuvan",
	"chu van gia": "gia.chuvan",
	"chu gia":     "gia.chuvan",
	"vangiachu":   "gia.chuvan",

	// hai.nguyensinh
	"hai.ns":          "hai.nguyensinh",
	"nguyễn sinh hải": "hai.nguyensinh",
	"nguyen sinh hai": "hai.nguyensinh",
	"sinh hải":        "hai.nguyensinh",
	"sinh hai":        "hai.nguyensinh",

	// dung.nguyencong
	"dung.nc":          "dung.nguyencong",
	"nguyễn công dũng": "dung.nguyencong",
	"nguyen cong dung": "dung.nguyencong",
	"công dũng":        "dung.nguyencong",
	"cong dung":        "dung.nguyencong",

	// hieu.nguyendinh
	"hieu.nd":          "hieu.nguyendinh",
	"nguyễn đình hiếu": "hieu.nguyendinh",
	"nguyen dinh hieu": "hieu.nguyendinh",

	// hoang.nguyenhuy1
	"hoang.nh":        "hoang.nguyenhuy1",
	"nguyễn huy hoàng": "hoang.nguyenhuy1",
	"nguyen huy hoang": "hoang.nguyenhuy1",
	"huyhoang160420":  "hoang.nguyenhuy1",

	// huy.lexuan
	"huy.lx":      "huy.lexuan",
	"lê xuân huy": "huy.lexuan",
	"le xuan huy": "huy.lexuan",

	// khanh.phamvan
	"khanh.pv":       "khanh.phamvan",
	"phạm văn khánh": "khanh.phamvan",
	"pham van khanh": "khanh.phamvan",

	// man.maihong
	"man.mh":       "man.maihong",
	"mai hồng mẫn": "man.maihong",
	"mai hong man": "man.maihong",

	// nga.nguyenthithanh
	"nga.ntt":              "nga.nguyenthithanh",
	"nguyễn thị thanh nga": "nga.nguyenthithanh",
	"nguyen thi thanh nga": "nga.nguyenthithanh",
	"nguyen nga":           "nga.nguyenthithanh",

	// ngoc.maithuy
	"ngoc.mt":      "ngoc.maithuy",
	"mai thúy ngọc": "ngoc.maithuy",
	"mai thuy ngoc": "ngoc.maithuy",

	// nguyen.hoangphuong
	"nguyen.hp":           "nguyen.hoangphuong",
	"hoàng phương nguyên": "nguyen.hoangphuong",
	"hoang phuong nguyen": "nguyen.hoangphuong",

	// quyen.nguyenta
	"quyen.nt":        "quyen.nguyenta",
	"nguyễn tạ quyền": "quyen.nguyenta",
	"nguyen ta quyen": "quyen.nguyenta",

	// thinh.nguyenvan
	"thinh.nv":         "thinh.nguyenvan",
	"nguyễn văn thịnh": "thinh.nguyenvan",
	"nguyen van thinh": "thinh.nguyenvan",
	"cam vinh":         "thinh.nguyenvan",

	// thong.nguyendinhminh
	"thong.ndm":              "thong.nguyendinhminh",
	"nguyễn đình minh thông": "thong.nguyendinhminh",
	"nguyen dinh minh thong": "thong.nguyendinhminh",
	"sunnlessmt09":           "thong.nguyendinhminh",

	// trinh.nguyenthimai
	"trinh.ntm":            "trinh.nguyenthimai",
	"nguyễn thị mai trinh": "trinh.nguyenthimai",
	"nguyen thi mai trinh": "trinh.nguyenthimai",
	"him":                  "trinh.nguyenthimai",

	// tuan.nguyenhuu
	"tuan.nh":         "tuan.nguyenhuu",
	"nguyễn hữu tuấn": "tuan.nguyenhuu",
	"nguyen huu tuan": "tuan.nguyenhuu",
	"tuấng":           "tuan.nguyenhuu",

	// tuan.nguyenngocanh
	"tuan.nna":             "tuan.nguyenngocanh",
	"nguyễn ngọc anh tuấn": "tuan.nguyenngocanh",
	"nguyen ngoc anh tuan": "tuan.nguyenngocanh",

	// vinh.nguyenquoc3
	"vinh.nq":          "vinh.nguyenquoc3",
	"nguyễn quốc vinh": "vinh.nguyenquoc3",
	"nguyen quoc vinh": "vinh.nguyenquoc3",

	// dat.dangvan
	"dat.dv":       "dat.dangvan",
	"đặng văn đạt": "dat.dangvan",
	"dang van dat": "dat.dangvan",

	// dung.luongthithao
	"dung.ltt":            "dung.luongthithao",
	"lương thị thảo dung": "dung.luongthithao",
	"luong thi thao dung": "dung.luongthithao",
	"thaodungc3pbc":       "dung.luongthithao",

	// ha.tranngan
	"ha.tn":       "ha.tranngan",
	"trần ngân hà": "ha.tranngan",
	"tran ngan ha": "ha.tranngan",

	// son.nguyenhoai
	"son.nh":          "son.nguyenhoai",
	"nguyễn hoài sơn": "son.nguyenhoai",
	"nguyen hoai son": "son.nguyenhoai",
	"son nguyen hoai": "son.nguyenhoai",

	// thanh.levan
	"thanh.lv":     "thanh.levan",
	"lê văn thành": "thanh.levan",
	"le van thanh": "thanh.levan",

	// thin.tranhoang
	"thin.th":         "thin.tranhoang",
	"trần hoàng thìn": "thin.tranhoang",
	"tran hoang thin": "thin.tranhoang",

	// hang.nguyenthu
	"hang.nt":        "hang.nguyenthu",
	"nguyễn thu hằng": "hang.nguyenthu",
	"nguyen thu hang": "hang.nguyenthu",

	// thanh.maihoai
	"thanh.mh":      "thanh.maihoai",
	"mai hoài thanh": "thanh.maihoai",
	"mai hoai thanh": "thanh.maihoai",
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

	// Sanitize clanNick & displayName through aliases
	if target, ok := r.aliases[strings.ToLower(clanNick)]; ok {
		clanNick = target
	}
	if target, ok := r.aliases[strings.ToLower(displayName)]; ok {
		displayName = target
	}

	// Check if this userID or username belongs to a known default profile
	var defaultP *UserProfile
	for _, dp := range defaultUserProfiles {
		if (userID != "" && dp.UserID == userID) || (username != "" && strings.EqualFold(dp.Username, username)) {
			defaultP = &dp
			break
		}
	}
	if defaultP != nil {
		clanNick = defaultP.ClanNick
		displayName = defaultP.ClanNick
	}

	var existing *UserProfile
	if userID != "" {
		existing = r.byID[userID]
	}
	if existing == nil && username != "" {
		existing = r.byUsername[strings.ToLower(username)]
	}

	if existing != nil {
		if defaultP != nil {
			existing.ClanNick = defaultP.ClanNick
			existing.DisplayName = defaultP.ClanNick
		} else {
			if clanNick != "" {
				existing.ClanNick = clanNick
			}
			if displayName != "" {
				existing.DisplayName = displayName
			}
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
		if defaultP != nil {
			newP.ClanNick = defaultP.ClanNick
			newP.DisplayName = defaultP.ClanNick
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

	var result string
	if userID != "" {
		if p, ok := r.byID[userID]; ok {
			if disp := p.BestDisplayName(); disp != "" {
				result = disp
			}
		}
	}

	if result == "" {
		clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(username, "@")))
		if target, ok := r.aliases[clean]; ok {
			clean = strings.ToLower(target)
		}

		if p, ok := r.byUsername[clean]; ok {
			if disp := p.BestDisplayName(); disp != "" {
				result = disp
			}
		}

		if result == "" {
			if p, ok := r.byDisplayName[clean]; ok {
				if disp := p.BestDisplayName(); disp != "" {
					result = disp
				}
			}
		}

		if result == "" {
			if clean != "" {
				result = clean
			} else if username != "" {
				result = strings.TrimPrefix(username, "@")
			} else {
				result = userID
			}
		}
	}

	// Always sanitize final result through aliases
	cleanResult := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(result, "@")))
	if target, ok := r.aliases[cleanResult]; ok {
		return target
	}
	return result
}

// AddAlias maps an alias string (e.g. "son.nt") to a canonical username (e.g. "son.nguyentruong1").
func (r *UserRegistry) AddAlias(alias, canonicalUsername string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[strings.ToLower(strings.TrimSpace(alias))] = strings.TrimSpace(canonicalUsername)
}

// LookupUser searches by userID, username, clan nickname, or alias.
func (r *UserRegistry) LookupUser(query string) (userID, clanNick string, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "@")))
	if clean == "" {
		return "", "", false
	}

	// 1. By UserID
	if p, ok := r.byID[clean]; ok {
		return p.UserID, p.BestDisplayName(), true
	}

	// 2. Alias mapping
	if target, ok := r.aliases[clean]; ok {
		clean = strings.ToLower(target)
	}

	// 3. By Username
	if p, ok := r.byUsername[clean]; ok {
		return p.UserID, p.BestDisplayName(), true
	}

	// 4. By DisplayName / ClanNick
	if p, ok := r.byDisplayName[clean]; ok {
		return p.UserID, p.BestDisplayName(), true
	}

	return "", "", false
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

	defaultByID := make(map[string]*UserProfile)
	for _, dp := range defaultUserProfiles {
		defaultByID[dp.UserID] = &dp
	}

	for i := range list {
		p := &list[i]
		if def, ok := defaultByID[p.UserID]; ok {
			p.ClanNick = def.ClanNick
			p.DisplayName = def.DisplayName
		} else {
			if target, ok := r.aliases[strings.ToLower(strings.TrimSpace(p.ClanNick))]; ok {
				p.ClanNick = target
			}
			if target, ok := r.aliases[strings.ToLower(strings.TrimSpace(p.DisplayName))]; ok {
				p.DisplayName = target
			}
		}
		r.indexProfileLocked(p)
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
