package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

const (
	webSearchConfigFile  = "ai-web.json"
	webDefaultMaxResults = 6
	webMaxResultsCap     = 10
	webDefaultMaxChars   = 12000
	webMaxCharsCap       = 24000
	webMaxFetchBytes     = 2 << 20 // 2 MiB
	webBrowserUA         = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	webWikiUA            = "BlockNotes/1.0 (+https://github.com/eachann1024/block)"
)

const webSearchPromptHint = "已开启联网搜索与网页读取：遇到实时或外部信息（如天气预报、新闻、汇率、价格、最新版软件、公开资料）或用户提供链接时，直接调用 `webSearch` 搜索，必要时用 `webFetch` 读取靠前网页正文获取精确数据，不要回答“没有工具/无法联网”。"

type webSearchConfig struct {
	Enabled bool `json:"enabled"`
}

// webSearchResult is a single organic result returned by the built-in search service.
type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

type webSearchResponse struct {
	Query   string            `json:"query"`
	Engine  string            `json:"engine"`
	Today   string            `json:"today"`
	Answer  string            `json:"answer,omitempty"`
	Results []webSearchResult `json:"results"`
}

type webFetchResponse struct {
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (c *Controller) loadWebSearchConfigLocked() {
	cfg := webSearchConfig{Enabled: true}
	_ = c.readJSONFile(webSearchConfigFile, &cfg)
	c.webSearch = cfg.Enabled
}

// SetWebSearch toggles built-in web search and web page reading across AI turns.
func (c *Controller) SetWebSearch(on bool) {
	c.mu.Lock()
	c.webSearch = on
	_ = c.writeJSONFile(webSearchConfigFile, webSearchConfig{Enabled: on})
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) webSearchEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.webSearch
}

func (c *Controller) renderWebSearchSettingRow(cUI *ui.Context) {
	on := c.webSearchEnabled()
	bui.SettingsRow(cUI, theme.Tr("联网搜索", "Web Search"), func() {
		if bui.Switch(cUI, "ai-web-search-switch", theme.Tr("联网搜索", "Web Search"), &on) {
			c.SetWebSearch(on)
		}
	})
}

func formatLocalDate() string {
	now := time.Now()
	zhDays := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	return fmt.Sprintf("%s (%s / %s)", now.Format("2006-01-02"), zhDays[now.Weekday()], now.Weekday().String())
}

// ---- Safe HTTP client with SSRF protection ----

var (
	errBlockedWebTarget = errors.New("blocked non-public web address")
	errBingCNRedirect   = errors.New("bing redirected to regional endpoint")
	cgnatPrefix         = netip.MustParsePrefix("100.64.0.0/10")
	thisNetPrefix       = netip.MustParsePrefix("0.0.0.0/8")
)

func isPublicWebIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsUnspecified() || addr.IsInterfaceLocalMulticast() {
		return false
	}
	if addr.Is4() && (cgnatPrefix.Contains(addr) || thisNetPrefix.Contains(addr)) {
		return false
	}
	return true
}

func validatePublicWebURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return nil, errors.New(theme.Tr("无效的网页地址", "Invalid web URL"))
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, errors.New(theme.Tr("仅支持 http/https 网页地址", "Only http and https URLs are supported"))
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return nil, errors.New(theme.Tr("网页地址缺少域名", "URL is missing a host"))
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return nil, errBlockedWebTarget
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicWebIP(ip) {
		return nil, errBlockedWebTarget
	}
	return u, nil
}

func newSafeWebClient(timeout time.Duration, checkRedirect func(req *http.Request, via []*http.Request) error) *http.Client {
	baseDialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 8 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// Allow dialing a user-configured local HTTP/SOCKS proxy; target URLs
			// are independently validated before sending and on every redirect.
			if isConfiguredProxyAddr(addr) {
				return baseDialer.DialContext(ctx, network, addr)
			}
			if ip := net.ParseIP(host); ip != nil {
				if !isPublicWebIP(ip) {
					return nil, errBlockedWebTarget
				}
				return baseDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ipAddr := range ips {
				if !isPublicWebIP(ipAddr.IP) {
					continue
				}
				conn, err := baseDialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, errBlockedWebTarget
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if _, err := validatePublicWebURL(req.URL.String()); err != nil {
				return err
			}
			if checkRedirect != nil {
				return checkRedirect(req, via)
			}
			return nil
		},
	}
}

func isConfiguredProxyAddr(addr string) bool {
	for _, sample := range []string{"https://example.com", "http://example.com"} {
		req, err := http.NewRequest(http.MethodGet, sample, nil)
		if err != nil {
			continue
		}
		p, err := http.ProxyFromEnvironment(req)
		if err != nil || p == nil {
			continue
		}
		pHost := p.Host
		if _, _, err := net.SplitHostPort(pHost); err != nil {
			if strings.EqualFold(p.Scheme, "https") {
				pHost = net.JoinHostPort(pHost, "443")
			} else {
				pHost = net.JoinHostPort(pHost, "80")
			}
		}
		if strings.EqualFold(pHost, addr) {
			return true
		}
	}
	return false
}

func webGetBytes(ctx context.Context, client *http.Client, rawURL, ua string) (int, *url.URL, string, []byte, error) {
	u, err := validatePublicWebURL(rawURL)
	if err != nil {
		return 0, nil, "", nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, nil, "", nil, err
	}
	if ua == "" {
		ua = webBrowserUA
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/json;q=0.8,text/plain;q=0.8,*/*;q=0.5")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, webMaxFetchBytes))
	if err != nil {
		return resp.StatusCode, resp.Request.URL, resp.Header.Get("Content-Type"), nil, err
	}
	return resp.StatusCode, resp.Request.URL, resp.Header.Get("Content-Type"), body, nil
}

// ---- Built-in multi-engine search service ----

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

func looksLikeBotChallenge(body string) bool {
	low := strings.ToLower(body)
	for _, marker := range []string{
		"verify you are human",
		"please complete the captcha",
		"g-recaptcha",
		"h-captcha",
		"cf-turnstile",
		"anomaly-modal",
		"人机验证",
		"安全验证",
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

func executeWebSearch(ctx context.Context, query string, maxResults int) (webSearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return webSearchResponse{}, errors.New(theme.Tr("请提供搜索关键词", "Enter a search query"))
	}
	if maxResults <= 0 {
		maxResults = webDefaultMaxResults
	}
	if maxResults > webMaxResultsCap {
		maxResults = webMaxResultsCap
	}

	type engineFn struct {
		name string
		run  func(context.Context, string, int) (string, []webSearchResult, error)
	}
	var engines []engineFn
	if hasCJK(query) {
		engines = []engineFn{
			{"toutiao", searchToutiao},
			{"so360", search360},
			{"bing", searchBingGlobal},
			{"duckduckgo", searchDuckDuckGo},
			{"wikipedia", searchWikipedia},
		}
	} else {
		engines = []engineFn{
			{"bing", searchBingGlobal},
			{"duckduckgo", searchDuckDuckGo},
			{"so360", search360},
			{"toutiao", searchToutiao},
			{"wikipedia", searchWikipedia},
		}
	}

	out := webSearchResponse{
		Query: query,
		Today: formatLocalDate(),
	}
	seen := make(map[string]bool)
	var usedEngines []string
	var lastErr error

	for _, eng := range engines {
		if ctx.Err() != nil {
			break
		}
		if len(out.Results) >= maxResults {
			break
		}
		// Once a primary web search engine already delivered a direct answer or
		// sufficient results, skip fallback engines like Wikipedia.
		if eng.name == "wikipedia" && (len(out.Results) > 0 || out.Answer != "") {
			break
		}
		if len(out.Results) >= 4 && (out.Answer != "" || !hasCJK(query)) {
			break
		}
		subCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		ans, items, err := eng.run(subCtx, query, maxResults)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		added := false
		if out.Answer == "" && strings.TrimSpace(ans) != "" {
			out.Answer = strings.TrimSpace(ans)
			added = true
		}
		for _, it := range items {
			norm := strings.TrimRight(strings.TrimSpace(it.URL), "/")
			if norm == "" || seen[norm] {
				continue
			}
			seen[norm] = true
			out.Results = append(out.Results, it)
			added = true
			if len(out.Results) >= maxResults {
				break
			}
		}
		if added {
			usedEngines = append(usedEngines, eng.name)
		}
	}

	if len(out.Results) == 0 && out.Answer == "" {
		if lastErr != nil {
			return webSearchResponse{}, fmt.Errorf(theme.Tr("联网搜索失败：%v", "Web search failed: %v"), lastErr)
		}
		return webSearchResponse{}, errors.New(theme.Tr("未找到相关搜索结果", "No web search results found"))
	}
	out.Engine = strings.Join(usedEngines, "+")
	return out, nil
}

// ---- Engine: Toutiao Search (so.toutiao.com) ----

func searchToutiao(ctx context.Context, query string, maxResults int) (string, []webSearchResult, error) {
	u := "https://so.toutiao.com/search?" + url.Values{
		"dvpf":    {"pc"},
		"source":  {"input"},
		"keyword": {query},
	}.Encode()
	client := newSafeWebClient(8*time.Second, nil)
	status, _, ct, body, err := webGetBytes(ctx, client, u, webBrowserUA)
	if err != nil {
		return "", nil, err
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("toutiao status %d", status)
	}
	text := decodeWebBytes(ct, body)
	if looksLikeBotChallenge(text) {
		return "", nil, errors.New("toutiao challenge")
	}

	doc, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return "", nil, err
	}

	var results []webSearchResult
	seen := make(map[string]bool)
	var answer string

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Organic results are embedded as clean JSON payloads.
			if n.Data == "script" && strings.EqualFold(htmlAttr(n, "type"), "application/json") {
				if raw := strings.TrimSpace(nodeRawText(n)); raw != "" {
					if res, ok := parseToutiaoJSONCard(raw); ok && !seen[res.URL] {
						seen[res.URL] = true
						results = append(results, res)
					}
				}
				return
			}
			// Direct answer cards (e.g., live weather, currency rates, facts) appear in
			// div.result-content blocks without standard web-result boilerplate.
			if answer == "" && n.Data == "div" && hasHTMLClass(n, "result-content") {
				if cand := extractToutiaoDirectAnswer(n); cand != "" {
					answer = cand
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	// Avoid duplicating an organic result's title as a direct answer box.
	if answer != "" && len(results) > 0 && strings.HasPrefix(answer, results[0].Title) {
		answer = ""
	}
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return answer, results, nil
}

func parseToutiaoJSONCard(raw string) (webSearchResult, bool) {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &envelope) != nil || envelope.Data == nil {
		return webSearchResult{}, false
	}
	data := envelope.Data
	tk, _ := data["template_key"].(string)
	switch tk {
	case "SearchBar", "SearchFilter", "BottomBar":
		return webSearchResult{}, false
	}
	link := firstNonEmptyString(
		jsonString(data["url"]),
		nestedJSONString(data, "display", "url"),
		nestedJSONString(data, "display", "info", "url"),
	)
	if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
		return webSearchResult{}, false
	}
	if strings.Contains(link, "so.toutiao.com/") {
		return webSearchResult{}, false
	}
	title := firstNonEmptyString(
		jsonString(data["title"]),
		nestedJSONString(data, "display", "title"),
		nestedJSONString(data, "display", "title", "text"),
	)
	snippet := firstNonEmptyString(
		nestedJSONString(data, "display", "summary"),
		nestedJSONString(data, "display", "summary", "text"),
		nestedJSONString(data, "display", "abstract"),
		nestedJSONString(data, "display", "content"),
	)
	title = cleanInlineHTMLText(title)
	snippet = cleanInlineHTMLText(snippet)
	if title == "" {
		return webSearchResult{}, false
	}
	return webSearchResult{
		Title:   truncateWebRunes(title, 160),
		URL:     link,
		Snippet: truncateWebRunes(snippet, 320),
	}, true
}

func extractToutiaoDirectAnswer(n *html.Node) string {
	// Skip header bar, trending sidebar, and regular organic result cards.
	if nodeContainsToutiaoWebTemplate(n) {
		return ""
	}
	txt := collapseSpaces(extractVisibleText(n, false))
	if utf8.RuneCountInString(txt) < 20 {
		return ""
	}
	for _, noise := range []string{"今日头条首页", "头条热榜", "相关搜索", "不限时间", "换一换", "反馈建议"} {
		if strings.Contains(txt, noise) {
			return ""
		}
	}
	// Accept rich structured cards (weather, exchange rate, calculator, calendar, etc.).
	for _, signal := range []string{"°C", "℃", "天气", "湿度", "日出", "汇率", "兑", "昨收", "今开", "等于", "平方公里", "海拔", "邮编", "区号"} {
		if strings.Contains(txt, signal) {
			return truncateWebRunes(txt, 1400)
		}
	}
	return ""
}

func nodeContainsToutiaoWebTemplate(n *html.Node) bool {
	if n.Type == html.ElementNode && n.Data == "script" && strings.EqualFold(htmlAttr(n, "type"), "application/json") {
		if strings.Contains(nodeRawText(n), "toutiao_web") {
			return true
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if nodeContainsToutiaoWebTemplate(c) {
			return true
		}
	}
	return false
}

// ---- Engine: 360 Search (www.so.com) ----

func search360(ctx context.Context, query string, maxResults int) (string, []webSearchResult, error) {
	u := "https://www.so.com/s?" + url.Values{"q": {query}}.Encode()
	client := newSafeWebClient(8*time.Second, nil)
	status, _, ct, body, err := webGetBytes(ctx, client, u, webBrowserUA)
	if err != nil {
		return "", nil, err
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("so360 status %d", status)
	}
	text := decodeWebBytes(ct, body)
	if looksLikeBotChallenge(text) {
		return "", nil, errors.New("so360 challenge")
	}

	doc, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return "", nil, err
	}

	var results []webSearchResult
	seen := make(map[string]bool)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= maxResults {
			return
		}
		if n.Type == html.ElementNode && n.Data == "li" && hasHTMLClass(n, "res-list") {
			if res, ok := parse360Item(n); ok && !seen[res.URL] {
				seen[res.URL] = true
				results = append(results, res)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return "", results, nil
}

func parse360Item(li *html.Node) (webSearchResult, bool) {
	h3 := findFirstElement(li, "h3")
	if h3 == nil {
		return webSearchResult{}, false
	}
	a := findFirstElement(h3, "a")
	if a == nil {
		return webSearchResult{}, false
	}
	title := collapseSpaces(extractVisibleText(a, false))
	if title == "" {
		return webSearchResult{}, false
	}
	link := strings.TrimSpace(htmlAttr(a, "data-mdurl"))
	if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
		link = strings.TrimSpace(htmlAttr(a, "href"))
	}
	if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
		return webSearchResult{}, false
	}
	if strings.Contains(link, "e.so.com/") || strings.Contains(link, "fanyi.so.com/") || strings.Contains(link, "www.so.com/link?") {
		return webSearchResult{}, false
	}

	var snippet string
	if p := findElementByClass(li, "p", "res-desc"); p != nil {
		snippet = collapseSpaces(extractVisibleText(p, false))
	}
	if snippet == "" {
		full := collapseSpaces(extractVisibleText(li, false))
		snippet = strings.TrimSpace(strings.TrimPrefix(full, title))
	}
	return webSearchResult{
		Title:   truncateWebRunes(title, 160),
		URL:     link,
		Snippet: truncateWebRunes(snippet, 320),
	}, true
}

// ---- Engine: Bing International (www.bing.com) ----

func searchBingGlobal(ctx context.Context, query string, maxResults int) (string, []webSearchResult, error) {
	u := "https://www.bing.com/search?" + url.Values{
		"q":       {query},
		"setlang": {"en-US"},
		"cc":      {"US"},
	}.Encode()
	client := newSafeWebClient(7*time.Second, func(req *http.Request, _ []*http.Request) error {
		if strings.EqualFold(req.URL.Hostname(), "cn.bing.com") {
			return errBingCNRedirect
		}
		return nil
	})
	status, finalURL, ct, body, err := webGetBytes(ctx, client, u, webBrowserUA)
	if err != nil {
		return "", nil, err
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("bing status %d", status)
	}
	if finalURL != nil && strings.EqualFold(finalURL.Hostname(), "cn.bing.com") {
		return "", nil, errBingCNRedirect
	}
	text := decodeWebBytes(ct, body)
	if looksLikeBotChallenge(text) {
		return "", nil, errors.New("bing challenge")
	}

	doc, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return "", nil, err
	}
	var answer string
	var results []webSearchResult
	seen := make(map[string]bool)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "li" {
			if hasHTMLClass(n, "b_algo") && len(results) < maxResults {
				if h2 := findFirstElement(n, "h2"); h2 != nil {
					if a := findFirstElement(h2, "a"); a != nil {
						title := collapseSpaces(extractVisibleText(a, false))
						link := strings.TrimSpace(htmlAttr(a, "href"))
						if title != "" && (strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://")) && !seen[link] {
							var snip string
							if capDiv := findElementByClass(n, "div", "b_caption"); capDiv != nil {
								if p := findFirstElement(capDiv, "p"); p != nil {
									snip = collapseSpaces(extractVisibleText(p, false))
								}
							}
							seen[link] = true
							results = append(results, webSearchResult{
								Title:   truncateWebRunes(title, 160),
								URL:     link,
								Snippet: truncateWebRunes(snip, 320),
							})
						}
					}
				}
				return
			}
			if answer == "" && hasHTMLClass(n, "b_ans") && !hasHTMLClass(n, "b_ad") {
				if txt := collapseSpaces(extractVisibleText(n, false)); utf8.RuneCountInString(txt) >= 24 {
					answer = truncateWebRunes(txt, 1000)
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return answer, results, nil
}

// ---- Engine: DuckDuckGo HTML (only when HTTP 200 without challenge) ----

func searchDuckDuckGo(ctx context.Context, query string, maxResults int) (string, []webSearchResult, error) {
	u := "https://html.duckduckgo.com/html/?" + url.Values{"q": {query}}.Encode()
	client := newSafeWebClient(7*time.Second, nil)
	status, _, ct, body, err := webGetBytes(ctx, client, u, webBrowserUA)
	if err != nil {
		return "", nil, err
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("ddg status %d", status)
	}
	text := decodeWebBytes(ct, body)
	if looksLikeBotChallenge(text) {
		return "", nil, errors.New("ddg challenge")
	}

	doc, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return "", nil, err
	}
	var results []webSearchResult
	seen := make(map[string]bool)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= maxResults {
			return
		}
		if n.Type == html.ElementNode && n.Data == "div" && hasHTMLClass(n, "result") {
			a := findElementByClass(n, "a", "result__a")
			if a != nil {
				title := collapseSpaces(extractVisibleText(a, false))
				link := unwrapDDGURL(htmlAttr(a, "href"))
				var snip string
				if sn := findElementByClass(n, "", "result__snippet"); sn != nil {
					snip = collapseSpaces(extractVisibleText(sn, false))
				}
				if title != "" && link != "" && !seen[link] {
					seen[link] = true
					results = append(results, webSearchResult{
						Title:   truncateWebRunes(title, 160),
						URL:     link,
						Snippet: truncateWebRunes(snip, 320),
					})
				}
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return "", results, nil
}

func unwrapDDGURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if parsed, err := url.Parse(raw); err == nil {
		if target := parsed.Query().Get("uddg"); strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			return target
		}
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return ""
}

// ---- Engine: Wikipedia API fallback ----

func searchWikipedia(ctx context.Context, query string, maxResults int) (string, []webSearchResult, error) {
	lang := "en"
	if hasCJK(query) {
		lang = "zh"
	}
	u := fmt.Sprintf("https://%s.wikipedia.org/w/api.php?", lang) + url.Values{
		"action":   {"query"},
		"list":     {"search"},
		"srsearch": {query},
		"srlimit":  {fmt.Sprintf("%d", maxResults)},
		"format":   {"json"},
	}.Encode()
	client := newSafeWebClient(7*time.Second, nil)
	status, _, _, body, err := webGetBytes(ctx, client, u, webWikiUA)
	if err != nil {
		return "", nil, err
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("wikipedia status %d", status)
	}
	var payload struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, err
	}
	out := make([]webSearchResult, 0, len(payload.Query.Search))
	for _, item := range payload.Query.Search {
		link := fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", lang, url.PathEscape(strings.ReplaceAll(item.Title, " ", "_")))
		out = append(out, webSearchResult{
			Title:   item.Title,
			URL:     link,
			Snippet: truncateWebRunes(cleanInlineHTMLText(item.Snippet), 320),
		})
	}
	return "", out, nil
}

// ---- General Web Page Fetcher (webFetch) ----

func executeWebFetch(ctx context.Context, rawURL string, maxChars int) (webFetchResponse, error) {
	if maxChars <= 0 {
		maxChars = webDefaultMaxChars
	}
	if maxChars > webMaxCharsCap {
		maxChars = webMaxCharsCap
	}
	client := newSafeWebClient(12*time.Second, nil)
	status, finalURL, ct, body, err := webGetBytes(ctx, client, rawURL, webBrowserUA)
	if err != nil {
		if errors.Is(err, errBlockedWebTarget) {
			return webFetchResponse{}, errors.New(theme.Tr("不允许访问本机或内网地址", "Access to local or private network addresses is blocked"))
		}
		return webFetchResponse{}, fmt.Errorf(theme.Tr("网页读取失败：%v", "Failed to fetch web page: %v"), err)
	}
	if status < 200 || status >= 300 {
		return webFetchResponse{}, fmt.Errorf(theme.Tr("网页返回 HTTP %d", "Web page returned HTTP %d"), status)
	}
	resURL := rawURL
	if finalURL != nil {
		resURL = finalURL.String()
	}

	lowCT := strings.ToLower(ct)
	if strings.Contains(lowCT, "image/") || strings.Contains(lowCT, "video/") || strings.Contains(lowCT, "audio/") || strings.Contains(lowCT, "application/pdf") || strings.Contains(lowCT, "application/octet-stream") {
		return webFetchResponse{}, fmt.Errorf(theme.Tr("不支持直接读取二进制内容（%s）", "Unsupported binary content type (%s)"), ct)
	}

	decoded := decodeWebBytes(ct, body)
	var title, content string
	if strings.Contains(lowCT, "text/html") || strings.Contains(lowCT, "application/xhtml") || looksLikeHTML(decoded) {
		title, content = extractHTMLPageText(decoded)
	} else {
		content = strings.TrimSpace(decoded)
	}
	truncated := false
	if utf8.RuneCountInString(content) > maxChars {
		content = truncateWebRunes(content, maxChars)
		truncated = true
	}
	return webFetchResponse{
		URL:       resURL,
		Title:     title,
		Content:   content,
		Truncated: truncated,
	}, nil
}

// ---- HTML & charset helpers ----

var metaCharsetRe = regexp.MustCompile(`(?i)charset\s*=\s*["']?([a-z0-9\-_]+)`)

func decodeWebBytes(contentType string, raw []byte) string {
	charset := extractCharset(contentType)
	if charset == "" {
		head := raw
		if len(head) > 4096 {
			head = head[:4096]
		}
		if m := metaCharsetRe.FindSubmatch(head); len(m) == 2 {
			charset = strings.ToLower(strings.TrimSpace(string(m[1])))
		}
	}
	switch charset {
	case "gbk", "gb2312", "gb18030", "gb_2312", "x-gbk":
		if dec, err := io.ReadAll(transform.NewReader(bytes.NewReader(raw), simplifiedchinese.GB18030.NewDecoder())); err == nil {
			return string(dec)
		}
	case "big5", "big5-hkscs":
		if dec, err := io.ReadAll(transform.NewReader(bytes.NewReader(raw), traditionalchinese.Big5.NewDecoder())); err == nil {
			return string(dec)
		}
	}
	return string(raw)
}

func extractCharset(contentType string) string {
	if m := metaCharsetRe.FindStringSubmatch(contentType); len(m) == 2 {
		return strings.ToLower(strings.TrimSpace(m[1]))
	}
	return ""
}

func looksLikeHTML(s string) bool {
	head := strings.ToLower(strings.TrimSpace(truncateWebRunes(s, 400)))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html") || strings.Contains(head, "<body") || strings.Contains(head, "<head")
}

func extractHTMLPageText(rawHTML string) (string, string) {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return "", collapseMultilineText(rawHTML)
	}
	var title string
	if t := findFirstElement(doc, "title"); t != nil {
		title = collapseSpaces(extractVisibleText(t, false))
	}
	// Prefer <main> or <article> when substantial, otherwise use <body> skipping chrome.
	if mainNode := findFirstElement(doc, "main"); mainNode != nil {
		if txt := collapseMultilineText(extractVisibleText(mainNode, true)); utf8.RuneCountInString(txt) >= 200 {
			return title, txt
		}
	}
	if artNode := findFirstElement(doc, "article"); artNode != nil {
		if txt := collapseMultilineText(extractVisibleText(artNode, true)); utf8.RuneCountInString(txt) >= 200 {
			return title, txt
		}
	}
	bodyNode := findFirstElement(doc, "body")
	if bodyNode == nil {
		bodyNode = doc
	}
	if txt := collapseMultilineText(extractVisibleText(bodyNode, true)); utf8.RuneCountInString(txt) >= 150 {
		return title, txt
	}
	return title, collapseMultilineText(extractVisibleText(bodyNode, false))
}

func extractVisibleText(n *html.Node, skipChrome bool) string {
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		switch cur.Type {
		case html.ElementNode:
			switch cur.Data {
			case "script", "style", "noscript", "svg", "iframe", "template", "object", "embed", "canvas":
				return
			case "nav", "header", "footer", "aside":
				if skipChrome {
					return
				}
			}
			isBlock := isBlockTag(cur.Data)
			if isBlock && buf.Len() > 0 {
				buf.WriteByte('\n')
			}
			for c := cur.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			if isBlock {
				buf.WriteByte('\n')
			}
		case html.TextNode:
			if t := cur.Data; t != "" {
				buf.WriteString(t)
				buf.WriteByte(' ')
			}
		default:
			for c := cur.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	walk(n)
	return buf.String()
}

func isBlockTag(tag string) bool {
	switch tag {
	case "p", "div", "section", "article", "main", "li", "tr", "br", "hr",
		"h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "pre", "dt", "dd":
		return true
	}
	return false
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func collapseMultilineText(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := collapseSpaces(line)
		if clean == "" {
			continue
		}
		out = append(out, clean)
	}
	return strings.Join(out, "\n")
}

func cleanInlineHTMLText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.ContainsAny(s, "<&") {
		return collapseSpaces(s)
	}
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return collapseSpaces(s)
	}
	return collapseSpaces(extractVisibleText(doc, false))
}

func nodeRawText(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

func htmlAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func hasHTMLClass(n *html.Node, want string) bool {
	for _, cls := range strings.Fields(htmlAttr(n, "class")) {
		if cls == want {
			return true
		}
	}
	return false
}

func findFirstElement(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func findElementByClass(n *html.Node, tag, cls string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && (tag == "" || n.Data == tag) && hasHTMLClass(n, cls) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElementByClass(c, tag, cls); found != nil {
			return found
		}
	}
	return nil
}

func jsonString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func nestedJSONString(m map[string]any, keys ...string) string {
	var cur any = m
	for _, k := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[k]
	}
	return jsonString(cur)
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateWebRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	b.Grow(max)
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String()) + "…"
}
