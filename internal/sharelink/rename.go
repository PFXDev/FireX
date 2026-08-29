package sharelink

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
)

// Rename rewrites the display name carried by a share link so client apps show
// FireX's node name rather than the panel's remark. The name lives in the URI
// fragment for every scheme except vmess, which keeps it inside its base64 JSON.
// A link that cannot be rewritten is returned unchanged.
func Rename(raw, name string) string {
	raw = strings.TrimSpace(raw)
	if name == "" {
		return raw
	}
	if strings.HasPrefix(strings.ToLower(raw), "vmess://") {
		return renameVMess(raw, name)
	}
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = raw[:i]
	}
	return raw + "#" + url.PathEscape(name)
}

func renameVMess(raw, name string) string {
	payload := raw[len("vmess://"):]
	if i := strings.IndexByte(payload, '#'); i >= 0 {
		payload = payload[:i]
	}
	decoded, err := decodeBase64(payload)
	if err != nil {
		return raw
	}
	var obj map[string]any
	if err := json.Unmarshal(decoded, &obj); err != nil {
		return raw
	}
	obj["ps"] = name
	encoded, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(encoded)
}
