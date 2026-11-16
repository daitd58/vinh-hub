package vinhhub

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	_ "unsafe" // for go:linkname

	mezon "github.com/quangledang23/mezon-sdk-go"
	"github.com/quangledang23/mezon-sdk-go/api"
)

// apiNameIndex links into mezon-sdk-go's internal table of socket API names to indices.
//
//go:linkname apiNameIndex github.com/quangledang23/mezon-sdk-go.apiNameIndex
var apiNameIndex map[string]int32

func init() {
	if apiNameIndex != nil {
		if _, ok := apiNameIndex["GenerateCDNSignature"]; !ok {
			apiNameIndex["GenerateCDNSignature"] = 212
		}
	}
}

const cdnHost = "cdn.mezon.ai"

// NeedsSignature returns true if rawURL is hosted on cdn.mezon.ai and has a channel folder prefix.
func NeedsSignature(rawURL string) bool {
	_, ok := ParseCDNChannel(rawURL)
	return ok
}

// ParseCDNChannel extracts the 64-bit snowflake channel ID from a Mezon CDN URL.
// Format: https://cdn.mezon.ai/<16_hex_digits_channel_id>/<filename>?...
func ParseCDNChannel(rawURL string) (int64, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host != cdnHost {
		return 0, false
	}
	p := u.EscapedPath()
	if len(p) < 18 || p[0] != '/' || p[17] != '/' {
		return 0, false
	}
	id, err := strconv.ParseUint(p[1:17], 16, 64)
	if err != nil || id == 0 || id > 1<<63-1 {
		return 0, false
	}
	return int64(id), true
}

// FileNameFromURL extracts the base filename from a URL or returns a default name.
func FileNameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "vinhhub_menu.png"
	}
	base := path.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		return "vinhhub_menu.png"
	}
	// Strip query or fragment if present
	if idx := strings.IndexAny(base, "?#"); idx != -1 {
		base = base[:idx]
	}
	return base
}

// SignCDNURL generates a fresh signature for Mezon CDN URLs using the connected MezonClient socket.
// If rawURL is not on cdn.mezon.ai, it is returned untouched without error.
func SignCDNURL(client *mezon.MezonClient, rawURL string) (string, error) {
	channelID, ok := ParseCDNChannel(rawURL)
	if !ok {
		return rawURL, nil
	}
	if client == nil || client.Socket() == nil {
		return rawURL, fmt.Errorf("mezon client socket is not connected")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, nil
	}

	mezonAPI := mezon.NewMezonApi("", "https://gw.mezon.ai", 60*time.Second)
	mezonAPI.AttachSocket(client.Socket())

	var resp api.GenerateCDNSignatureResponse
	if err := mezonAPI.Call("", "GenerateCDNSignature", &api.GenerateCDNSignatureRequest{
		ChannelId: channelID,
	}, &resp); err != nil {
		return "", fmt.Errorf("sign cdn channel %d: %w", channelID, err)
	}
	if resp.Signature == "" {
		return "", fmt.Errorf("empty cdn signature returned for channel %d", channelID)
	}

	u.RawQuery = resp.Signature
	return u.String(), nil
}
