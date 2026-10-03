package mihomo

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

func ValidateSubscriptionInput(name, rawURL string) (string, *url.URL, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 || len(rawURL) > 4096 {
		return "", nil, ErrSubscriptionInvalid
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", nil, ErrSubscriptionInvalid
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return "", nil, ErrSubscriptionInvalid
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()) {
		return "", nil, ErrSubscriptionInvalid
	}
	return name, parsed, nil
}

var ErrSubscriptionInvalid = errors.New("Mihomo subscription request is invalid")
