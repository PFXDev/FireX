package server

import (
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// secretKey matches the inbound parameter names that carry credentials:
// REALITY and TLS private keys, passwords, seeds and VLESS decryption. It
// mirrors concealed() in web/src/lib/inbound-parameters.ts, which masks the
// same fields for super admins until they choose to reveal them.
var secretKey = regexp.MustCompile(`(?i)private|password|secret|token|seed|^key$|^auth$|^decryption$`)

const redactedValue = "••••••••"

// redactForViewer masks credentials in inbound parameters when the caller
// may only read configuration. A super admin gets the value unchanged, since
// they can already rewrite and deploy it.
func redactForViewer(c *gin.Context, value any) any {
	if isSuperAdmin(currentAdmin(c)) {
		return value
	}
	return redactSecrets(value, "")
}

func redactSecrets(value any, key string) any {
	// "none" is the literal for no decryption and carries no credential.
	if strings.EqualFold(key, "decryption") && value == "none" {
		return value
	}
	if key != "" && secretKey.MatchString(key) && !emptyValue(value) {
		return redactedValue
	}
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for name, item := range v {
			out[name] = redactSecrets(item, name)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactSecrets(item, "")
		}
		return out
	}
	return value
}

func emptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case bool:
		return !v
	case float64:
		return v == 0
	}
	return false
}
