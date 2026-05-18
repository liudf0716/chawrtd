package ws

import "strings"

var sensitiveLogKeys = map[string]struct{}{
	"password":    {},
	"passwd":      {},
	"token":       {},
	"secret":      {},
	"key":         {},
	"api_key":     {},
	"apikey":      {},
	"access_key":  {},
	"private_key": {},
}

// SanitizeDataForLog redacts known sensitive fields recursively for safe logging.
func SanitizeDataForLog(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]interface{}, len(val))
		for k, vv := range val {
			if isSensitiveLogKey(k) {
				out[k] = "***REDACTED***"
				continue
			}
			out[k] = SanitizeDataForLog(vv)
		}
		return out
	case []any:
		out := make([]interface{}, len(val))
		for i := range val {
			out[i] = SanitizeDataForLog(val[i])
		}
		return out
	default:
		return v
	}
}

func isSensitiveLogKey(key string) bool {
	_, ok := sensitiveLogKeys[strings.ToLower(strings.TrimSpace(key))]
	return ok
}
