package subscriptionhttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/mihomo"
)

func newSubscriptionHTTPClient() *http.Client {
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, fmt.Errorf("resolve subscription host: %w", err)
		}
		for _, address := range addresses {
			if isUnsafeSubscriptionIP(address.IP) {
				return nil, errors.New("subscription host resolved to a private or local address")
			}
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("too many subscription redirects")
		}
		_, _, err := domain.ValidateSubscriptionInput("redirect", request.URL.String())
		return err
	}
	return client
}

func isUnsafeSubscriptionIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()
}

type Client struct{ HTTP *http.Client }

func New() *Client { return &Client{HTTP: newSubscriptionHTTPClient()} }
func (c *Client) Fetch(ctx context.Context, target string) ([]byte, error) {
	if _, _, err := domain.ValidateSubscriptionInput("subscription", target); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/yaml,text/yaml,text/plain,application/octet-stream")
	request.Header.Set("User-Agent", "clash.meta")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("subscription provider rejected request")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 5<<20+1))
	if err != nil || len(body) > 5<<20 {
		return nil, errors.New("subscription response unavailable")
	}
	return body, nil
}
