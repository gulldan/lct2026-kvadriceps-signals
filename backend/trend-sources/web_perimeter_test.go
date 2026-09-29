package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWebPerimeterRejectsInternalAddresses(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1:443", "127.9.9.9:443", "169.254.169.254:80", "10.1.2.3:443", "172.16.0.1:443",
		"192.168.1.1:443", "0.0.0.0:443", "100.64.0.1:443", "[::1]:443", "[fe80::1]:443", "[fc00::1]:443",
		"[fd12:3456::1]:443", "224.0.0.1:443", "not-an-address", "example.org:443",
	} {
		if publicAddress("tcp", address, nil) == nil {
			t.Fatalf("connected to a non-public address: %s", address)
		}
	}
	for _, address := range []string{"93.184.216.34:443", "[2606:2800:220:1:248:1893:25c8:1946]:443"} {
		if err := publicAddress("tcp", address, nil); err != nil {
			t.Fatalf("public address refused: %s: %v", address, err)
		}
	}
}

func TestWebPanelBoundsAddresses(t *testing.T) {
	panel := []string{"news.mit.edu", ".nasa.gov"}
	for _, address := range []string{
		"https://news.mit.edu/rss/topic/energy", "https://www.nasa.gov/feed/", "https://nasa.gov/x",
	} {
		u, err := url.Parse(address)
		if err != nil || allowedAddress(u, panel) != nil {
			t.Fatalf("configured address refused: %s", address)
		}
	}
	for _, address := range []string{
		"https://evil.example/mit.edu", "http://news.mit.edu/x", "https://user:pass@news.mit.edu/x",
		"https://news.mit.edu.evil.example/x", "javascript:alert(1)", "https://127.0.0.1/x",
	} {
		u, _ := url.Parse(address)
		if allowedAddress(u, panel) == nil {
			t.Fatalf("address outside the panel accepted: %s", address)
		}
	}
	if allowedAddress(&url.URL{Scheme: "https", Host: "news.mit.edu"}, nil) == nil {
		t.Fatal("an empty panel allowed a request")
	}
}

func TestRobotsIsObeyed(t *testing.T) {
	rules := parseRobots(strings.Join([]string{
		"# comment", "User-agent: GreedyBot", "Disallow: /", "",
		"User-agent: *", "Disallow: /private", "Disallow: /search?q=", "Allow: /",
	}, "\n"))
	for path, want := range map[string]bool{
		"/news/story": true, "/private": false, "/private/page": false, "/search?q=x": false, "/": true,
	} {
		u, _ := url.Parse("https://example.org" + path)
		if got := rules.allows(u); got != want {
			t.Fatalf("robots decision for %s: got %v want %v", path, got, want)
		}
	}
	u, _ := url.Parse("https://example.org/anything")
	if !parseRobots("User-agent: *\nDisallow:\n").allows(u) {
		t.Fatal("an empty Disallow forbids nothing")
	}
}

func TestWebRedirectRechecksSameHost(t *testing.T) {
	client := newWebClient(time.Second, []string{"example.org"})
	previous, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.org/", http.NoBody)
	for _, address := range []string{"http://example.org/x", "https://user:pass@example.org/x", "https://outside.example/x"} {
		next, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, address, http.NoBody)
		if client.CheckRedirect(next, []*http.Request{previous}) == nil {
			t.Fatalf("unsafe redirect accepted: %s", address)
		}
	}
}

func TestRobotsGroupsAndExceptions(t *testing.T) {
	rules := parseRobots("User-agent: *\nDisallow: /\n\nUser-agent: rag-test-signalgraph\n" +
		"User-agent: OtherBot\nDisallow: /private\nAllow: /private/open\nDisallow: /*.pdf$\n")
	for path, want := range map[string]bool{"/news": true, "/private/closed": false, "/private/open": true,
		"/a.pdf": false, "/a.pdf?q=1": true} {
		u, _ := url.Parse("https://example.org" + path)
		if got := rules.allows(u); got != want {
			t.Fatalf("%s: got %v want %v", path, got, want)
		}
	}
}

func TestRobotsUnavailableDoesNotAuthorizePages(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusNotFound} {
		s := &service{dir: t.TempDir(), config: defaultConfig(), http: &http.Client{
			Transport: ledgerTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
			}),
		}}
		u, _ := url.Parse("https://example.org/news")
		rules := s.robotsFor(t.Context(), u, &snapshot{})
		if got, want := rules.allows(u), status == http.StatusNotFound; got != want {
			t.Fatalf("robots HTTP %d: got allowed=%v want %v", status, got, want)
		}
		if !rules.allows(u) && !strings.Contains(rules.limitation(u.String()), "unavailable") {
			t.Fatal("an unavailable robots file was described as an explicit site prohibition")
		}
	}
}

func TestWebScopeKindsAreBounded(t *testing.T) {
	base := defaultConfig()
	if len(base.domains()) == 0 {
		t.Fatal("the default configuration has no panel")
	}
	valid := sourceConfig{HistoryMonths: 13, RequestBudget: 600,
		AllowedDomains: []string{"lab.example"},
		Web: []webSource{
			{Name: "one page", URL: "https://lab.example/story", Kind: kindURL, Industry: "Energy"},
			{Name: "list", URL: "https://lab.example/", Kind: kindURLs, Industry: "Energy",
				URLs: []string{"https://lab.example/a", "https://lab.example/b"}, Pages: 2},
			{Name: "sitemap", URL: "https://lab.example/sitemap.xml", Kind: kindSitemap, Industry: "Energy", Pages: 10},
		}}
	if err := validateSourceConfig(valid); err != nil {
		t.Fatalf("a configured panel was refused: %v", err)
	}
	broken := valid
	broken.Web = append([]webSource{{Name: "list without urls", URL: "https://lab.example/",
		Kind: kindURLs, Industry: "Energy"}}, valid.Web...)
	if validateSourceConfig(broken) == nil {
		t.Fatal("kind=urls without a URL list was accepted")
	}
	empty := valid
	empty.AllowedDomains = []string{}
	empty.Web = []webSource{{Name: "no host", URL: "https://", Kind: kindURL, Industry: "Energy"}}
	if validateSourceConfig(empty) == nil {
		t.Fatal("a source without a host was accepted")
	}
}
