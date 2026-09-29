package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/temoto/robotstxt"
)

// publicAddress проверяет само соединение, поэтому переадресация, DNS на внутренний хост
// и подмена адреса между проверкой и соединением тоже отсекаются.
func publicAddress(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("unparsable source address %q", address)
	}
	ip := net.ParseIP(host)
	switch {
	case ip == nil:
		return fmt.Errorf("unresolved source address %q", host)
	case ip.IsLoopback(), ip.IsPrivate(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(), ip.IsMulticast(), ip.IsUnspecified():
		return fmt.Errorf("blocked internal source address %s", host)
	case ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64:
		return fmt.Errorf("blocked carrier-grade NAT source address %s", host)
	case len(ip) == net.IPv6len && ip.To4() == nil && ip[0]&0xfe == 0xfc:
		return fmt.Errorf("blocked unique-local source address %s", host)
	}
	return nil
}

// newWebClient не шлёт учётных данных и не обходит стену авторизации: закрытая страница остаётся вне сбора.
func newWebClient(timeout time.Duration, allowed []string) *http.Client {
	transport := &http.Transport{
		// Проверка соединения должна видеть адрес назначения, а не прокси из окружения.
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, Control: publicAddress}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	// Прокси оператора — единственный путь к непубличному адресу: прокси сам соединяется с источником, поэтому
	// проверка переезжает с соединения на URL, который allowedAddress проверяет перед каждым запросом.
	if proxy, ok, err := sourceProxy(); err == nil && ok {
		transport.Proxy = http.ProxyURL(proxy)
		transport.DialContext = (&net.Dialer{Timeout: 15 * time.Second}).DialContext
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("source redirect chain is too long")
			}
			// Переадресация на тот же хост тоже обязана пройти правило HTTPS и учётных данных.
			return allowedAddress(req.URL, allowed)
		},
	}
}

// allowedAddress при пустой панели не пропускает ничего.
func allowedAddress(u *url.URL, allowed []string) error {
	if u == nil || u.Scheme != schemeHTTPS || u.Host == "" || u.User != nil {
		return errors.New("source address must be an HTTPS URL without credentials")
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range allowed {
		domain = strings.ToLower(strings.TrimPrefix(domain, "."))
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return nil
		}
	}
	return fmt.Errorf("source address %s is outside the configured domains", host)
}

type robotsRules struct {
	group *robotstxt.Group
	deny  bool
}

func parseRobots(body string) robotsRules {
	parsed, err := robotstxt.FromString(body)
	if err != nil {
		return robotsRules{deny: true}
	}
	return robotsRules{group: parsed.FindGroup("rag-test-signalgraph")}
}

func (r robotsRules) allows(u *url.URL) bool {
	return !r.deny && (r.group == nil || r.group.Test(u.RequestURI()))
}

func (r robotsRules) limitation(address string) string {
	if r.deny {
		return "robots.txt unavailable; page deferred: " + address
	}
	return "robots.txt forbids " + address
}

// robotsFor: недоступный robots.txt запрещает обход (RFC 9309 §2.3.1.4), прочий 4xx кроме 429 — разрешает.
func (s *service) robotsFor(ctx context.Context, u *url.URL, data *snapshot) robotsRules {
	host := strings.ToLower(u.Host)
	if rules, ok := s.robots[host]; ok {
		return rules
	}
	if s.robots == nil {
		s.robots = map[string]robotsRules{}
	}
	rules := robotsRules{deny: true}
	body, err := s.fetch(ctx, "https://"+u.Host+"/robots.txt", data)
	if err == nil {
		rules = parseRobots(string(body))
	} else if status, ok := errors.AsType[*sourceHTTPError](err); ok &&
		status.Status >= 400 && status.Status < 500 && status.Status != http.StatusTooManyRequests {
		rules = robotsRules{}
	}
	s.robots[host] = rules
	return rules
}

func sourceProxy() (*url.URL, bool, error) {
	raw := strings.TrimSpace(os.Getenv("SOURCE_PROXY_URL"))
	if raw == "" {
		return nil, false, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || !slices.Contains([]string{"http", schemeHTTPS, "socks5", "socks5h"}, u.Scheme) {
		return nil, false, errors.New("SOURCE_PROXY_URL must be an http, https or socks5 URL with a host")
	}
	return u, true, nil
}
