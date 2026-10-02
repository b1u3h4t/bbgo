package batch

import "strings"

// IsRateLimitError reports whether err is an exchange rate-limit or IP-ban response
// (e.g. Binance -1003 / HTTP 418 / HTTP 429).
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, s := range []string{
		"code=-1003",
		`"code":-1003`,
		"status code: 418",
		"status code: 429",
		"Too many requests",
		"banned until",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
