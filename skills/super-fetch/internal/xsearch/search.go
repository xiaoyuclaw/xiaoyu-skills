// Package xsearch 是 super-search 的 Go 实现：带 Cookie 预热的搜索引擎抓取。
package xsearch

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"superfetch/internal/xcore"
	"superfetch/internal/xdb"
	"superfetch/internal/xfetch"
	"superfetch/internal/xparse"
)

// Engine 一个搜索引擎的配置。
type Engine struct {
	Home              string
	Search            string
	Domain            string
	Sel               string
	Start             string
	Blocks            []string
	TitleSel          string
	SnippetSel        string
	URLAttrs          []string
	NoWarmup          bool
	Unwrap            string
	AllowInternal     bool
	DropInternalPaths []string
	RequireSelector   string
	DropCardIDs       []string
	PageKey           string
	PageStep          int
	PageExtra         string
	PageKind          string
}

// Engines 支持的搜索引擎。
var Engines = map[string]Engine{
	"bing": {
		Home: "https://www.bing.com/", Search: "https://www.bing.com/search?q={q}", Domain: ".bing.com", Unwrap: "bing", PageKey: "first", PageStep: 10, PageExtra: "FORM=PERE",
		Sel: "#b_results", Start: `\n\d+\. \[`,
		Blocks: []string{"li.b_algo", "#b_results > li.b_algo"}, TitleSel: "h2 a",
		SnippetSel: ".b_caption p, .b_snippet, .b_algoSlug, p",
	},
	"baidu": {
		Home: "https://www.baidu.com/", Search: "https://www.baidu.com/s?wd={q}", Domain: ".baidu.com",
		Sel: "#content_left", Start: `### \[`,
		Blocks:   []string{"#content_left .c-container", "#content_left div.result"},
		URLAttrs: []string{"mu"},
		TitleSel: "h3 a", SnippetSel: ".c-abstract, [class*='content-right'], .c-span-last, p", PageKey: "pn", PageStep: 10,
	},
	"sogou": {
		Home: "https://www.sogou.com/", Search: "https://www.sogou.com/web?query={q}", Domain: ".sogou.com",
		Sel: "#main", Start: `### `,
		Blocks: []string{".vrwrap", ".rb"}, TitleSel: "h3 a",
		SnippetSel: ".str_info, .text-layout, .space-txt, .str-text-info, p", PageKey: "page", PageStep: 1,
	},
	"ddg": {
		Home: "https://html.duckduckgo.com/html/", Search: "https://html.duckduckgo.com/html/?q={q}", Domain: ".duckduckgo.com",
		Sel: ".results", Start: `### `,
		PageKind: "ddg-post",
		Blocks:   []string{".result"}, TitleSel: ".result__a",
		SnippetSel: ".result__snippet", NoWarmup: true,
	},
	"shenma": {
		Home: "https://m.sm.cn/", Search: "https://m.sm.cn/s?q={q}",
		Domain: ".sm.cn", Sel: ".sc", Start: `### `,
		Blocks: []string{".sc"}, TitleSel: ".qk-title-text", SnippetSel: ".qk-paragraph",
		NoWarmup: true, PageKey: "page", PageStep: 1,
	},
	"toutiao": {
		Home: "https://so.toutiao.com/", Search: "https://so.toutiao.com/search?keyword={q}",
		Domain: ".toutiao.com", Sel: ".result-content", Start: `### `,
		Blocks: []string{".result-content"}, TitleSel: ".l-card-title, a.l-view.l-text",
		SnippetSel: ".l-paragraph, [class*='l-paragraph'], [class*='l-natural_summary']",
		Unwrap:     "toutiao", AllowInternal: true, NoWarmup: true, PageKey: "offset", PageStep: 10,
		DropInternalPaths: []string{"/search"}, RequireSelector: ".l-source", DropCardIDs: []string{"hotlist", "video"},
	},
	"360": {
		Home: "https://www.so.com/", Search: "https://www.so.com/s?q={q}", Domain: ".so.com",
		Sel: "#main", Start: `### \[`,
		Blocks: []string{"li.res-list", ".res-list"}, TitleSel: "h3 a",
		SnippetSel: ".res-desc, .summary, p", PageKey: "pn", PageStep: 1,
	},
}

var captchaMarks = []string{"验证码", "安全验证", "滑块验证", "人机验证", "拖动左侧滑块",
	"请完成下方验证", "unusual traffic"}

var (
	reTitle  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reScript = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>`)
	reTags   = regexp.MustCompile(`<[^>]+>`)
)

// IsBlocked 只看标题与正文文本——脚本里常有 challenge/captcha 字样。
func IsBlocked(html string) bool {
	title := ""
	if m := reTitle.FindStringSubmatch(html); len(m) > 1 {
		title = m[1]
	}
	for _, k := range []string{"验证", "安全", "人机"} {
		if strings.Contains(title, k) {
			return true
		}
	}
	text := reScript.ReplaceAllString(html, " ")
	text = reTags.ReplaceAllString(text, " ")
	for _, k := range captchaMarks {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

// StatePath 浏览器会话状态文件。
func StatePath(engine string) string {
	return filepath.Join(xcore.DataDir(), "pwstate-"+engine+".json")
}

// CookieJarPath cffi 预热得到的 Cookie 文件。
func CookieJarPath(engine string) string {
	return filepath.Join(xcore.DataDir(), "cookies-"+engine+".json")
}

// HasDisplay 是否有可用显示器。
func HasDisplay() bool {
	if os.Getenv("DISPLAY") == "" && fileExists("/tmp/.X11-unix/X0") {
		os.Setenv("DISPLAY", ":0")
	}
	return os.Getenv("DISPLAY") != "" && fileExists("/tmp/.X11-unix/X0")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Warmup 用 HTTP 通道访问首页拿身份 Cookie。
func Warmup(engine string, timeout int) map[string]string {
	cfg, ok := Engines[engine]
	if !ok {
		return nil
	}
	client, err := xfetch.NewClient("", timeout)
	if err != nil {
		return nil
	}
	defer client.Close()
	if _, err := client.Fetch(cfg.Home, ""); err != nil {
		return nil
	}
	time.Sleep(800 * time.Millisecond)
	cookies := client.CookiesFor(cfg.Home)
	if len(cookies) > 0 {
		saveJSON(CookieJarPath(engine), cookies)
	}
	return cookies
}

func saveJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o600); err == nil {
		xcore.SecureFile(path)
	}
}

func loadCookies(engine string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(CookieJarPath(engine))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

// CffiFetch 用 HTTP 通道抓结果页。
func CffiFetch(engine, query string, timeout int) (string, error) {
	return CffiFetchURL(engine, query, "", timeout)
}

// CffiFetchURL 抓指定地址（翻页用）；override 为空时按 query 拼。
func CffiFetchURL(engine, query, override string, timeout int) (string, error) {
	cfg := Engines[engine]
	searchURL := override
	if searchURL == "" {
		searchURL = strings.ReplaceAll(cfg.Search, "{q}", neturl.QueryEscape(query))
	}
	client, err := xfetch.NewClient("", timeout)
	if err != nil {
		return "", err
	}
	defer client.Close()

	if !cfg.NoWarmup {
		cookies := loadCookies(engine)
		if len(cookies) > 0 {
			client.SetCookiesFromMap(searchURL, cookies)
		} else if _, err := client.Fetch(cfg.Home, ""); err == nil {
			time.Sleep(time.Second)
			cookies = client.CookiesFor(cfg.Home)
			client.SetCookiesFromMap(searchURL, cookies)
		}
	}
	resp, err := client.Fetch(searchURL, "")
	if err != nil {
		return "", err
	}
	text := resp.Text()
	// 撞验证页多半是 Cookie 脏了：清掉重新预热再来一次。
	if IsBlocked(text) {
		_ = os.Remove(CookieJarPath(engine))
		client.Close()
		client, err = xfetch.NewClient("", timeout)
		if err != nil {
			return text, nil
		}
		defer client.Close()
		_, werr := client.Fetch(cfg.Home, "")
		time.Sleep(time.Second)
		fresh := client.CookiesFor(cfg.Home)
		if werr == nil {
			if len(fresh) > 0 {
				client.SetCookiesFromMap(searchURL, fresh)
			}
			if r2, err2 := client.Fetch(searchURL, ""); err2 == nil {
				text = r2.Text()
				resp = r2
			}
		}
	}
	saveJSON(CookieJarPath(engine), client.CookiesFor(searchURL))
	return text, nil
}

// Searcher 复用浏览器与上下文的搜索器。
type Searcher struct {
	b       *xfetch.Browser
	timeout int
}

// NewSearcher 启动搜索专用浏览器。
func NewSearcher(timeout int, headless, show bool) (*Searcher, error) {
	b, err := xfetch.LaunchBrowser(xfetch.BrowserOptions{
		Headless: headless, Show: show, Timeout: timeout,
		Width: 1366, Height: 900,
	})
	if err != nil {
		return nil, err
	}
	return &Searcher{b: b, timeout: timeout}, nil
}

// Close 关闭浏览器。
func (s *Searcher) Close() {
	if s.b != nil {
		s.b.Close()
	}
}

// Fetch 用浏览器抓一个引擎的结果页。
// FetchURL 用浏览器抓任意地址（翻页用）。
func (s *Searcher) FetchURL(engine, purl string) (string, error) {
	cfg := Engines[engine]
	s.b.SetWaitSelector(cfg.Sel)
	s.b.SetStable(true)
	html, _, err := s.b.FetchWithBrowser(purl, StatePath(engine))
	return html, err
}

func (s *Searcher) Fetch(engine, query string) (string, error) {
	cfg := Engines[engine]
	searchURL := strings.ReplaceAll(cfg.Search, "{q}", neturl.QueryEscape(query))
	state := StatePath(engine)

	if _, err := os.Stat(state); err != nil {
		jar := Warmup(engine, s.timeout)
		if len(jar) > 0 {
			st := &xfetch.StorageState{Cookies: []xfetch.Cookie{}, Origins: []xfetch.Origin{}}
			for k, v := range jar {
				st.Cookies = append(st.Cookies, xfetch.Cookie{
					Name: k, Value: v, Domain: cfg.Domain, Path: "/", Expires: -1, SameSite: "Lax",
				})
			}
			_ = xfetch.SaveStorageState(state, st)
		}
	}

	s.b.SetWaitSelector(cfg.Sel)
	s.b.SetStable(true)
	html, _, err := s.b.FetchWithBrowser(searchURL, state)
	if err != nil {
		return "", err
	}
	if !IsBlocked(html) {
		// 保存会话：借用最后一次抓取的页面上下文不可得，这里仅保留文件
	}
	return html, nil
}

// Result 一次成功的搜索结果。
type Result struct {
	Engine    string  `json:"engine"`
	Via       string  `json:"via"`
	Query     string  `json:"query"`
	Title     string  `json:"title"`
	Namespace string  `json:"namespace"`
	Content   string  `json:"content"`
	Items     []Item  `json:"items,omitempty"`
	Elapsed   float64 `json:"elapsed"`
}

// QuotedURL 把查询词拼进搜索地址。
func QuotedURL(engine, query string) string {
	return strings.ReplaceAll(Engines[engine].Search, "{q}", neturl.QueryEscape(query))
}

// Extract 从结果页 HTML 抽取标题与 Markdown。
func Extract(engine, html string, limit int) (string, string, string) {
	cfg := Engines[engine]
	doc, err := xparse.MakeSoup(html)
	if err != nil {
		return "", "", ""
	}
	title := ""
	if t := doc.Find("title").First(); t.Length() > 0 {
		title = strings.TrimSpace(t.Text())
	}
	xparse.CleanNoiseElements(doc.Selection)
	sel := cfg.Sel
	if sel == "" {
		sel = "body"
	}
	node := doc.Find(sel).First()
	if node.Length() == 0 {
		node = doc.Find("body").First()
	}
	if node.Length() == 0 {
		node = doc.Selection
	}
	ns := xdb.ProcessLinksToIDs(node, cfg.Home)
	md, err := xparse.ToMarkdown(node)
	if err != nil {
		md = node.Text()
	}
	if cfg.Start != "" {
		// markdownify 与 html-to-markdown 的行首换行处理不同：
		// 先按原正则找，找不到再按行首匹配一次。
		cut := -1
		if re, err := regexp.Compile(cfg.Start); err == nil {
			if loc := re.FindStringIndex(md); loc != nil {
				cut = loc[0]
			}
		}
		if cut < 0 {
			fallback := "(?m)^" + strings.TrimPrefix(cfg.Start, `\n`)
			if re, err := regexp.Compile(fallback); err == nil {
				if loc := re.FindStringIndex(md); loc != nil {
					cut = loc[0]
				}
			}
		}
		if cut > 0 {
			md = md[cut:]
		}
	}
	if limit > 0 {
		md = xcore.TruncateRunes(md, limit)
	}
	return title, md, ns
}

// ExtractItems 先按引擎专属选择器抽，抽不到再用通用启发式。
// 只抽结构，不依赖 Markdown 渲染出来的标题层级。
func ExtractItems(engine, htmlStr string, limit int) []Item {
	cfg, ok := Engines[engine]
	if !ok {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	doc, err := xparse.MakeSoup(htmlStr)
	if err != nil {
		return nil
	}
	for _, tag := range []string{"script", "style", "noscript", "svg", "canvas"} {
		doc.Find(tag).Remove()
	}

	home := homeHost(engine)
	titleSels := splitSels(cfg.TitleSel, "h2 a, h3 a, h1 a, h4 a, a")
	snipSels := splitSels(cfg.SnippetSel, "p")

	var blocks []*goquery.Selection
	for _, sel := range cfg.Blocks {
		if sel == "" {
			continue
		}
		if found := doc.Find(sel); found.Length() > 0 {
			blocks = make([]*goquery.Selection, 0, found.Length())
			found.Each(func(_ int, b *goquery.Selection) { blocks = append(blocks, b) })
			break
		}
	}

	var items []Item
	for _, b := range blocks {
		if isAdBlock(b) {
			continue
		}
		if cfg.RequireSelector != "" && b.Find(cfg.RequireSelector).Length() == 0 {
			continue
		}
		cardID, _ := b.Attr("data-test-card-id")
		skip := false
		for _, k := range cfg.DropCardIDs {
			if strings.Contains(cardID, k) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		titleEl := firstBySelectors(b, titleSels)
		if titleEl == nil {
			continue
		}
		anchor := titleEl
		if goquery.NodeName(anchor) != "a" {
			if up := anchor.ParentsFiltered("a").First(); up.Length() > 0 {
				anchor = up
			} else if sib := b.Find("a[href]").First(); sib.Length() > 0 {
				anchor = sib
			}
		}
		href, _ := anchor.Attr("href")
		abs := unwrapByEngine(cfg, resolveHref(href, cfg.Home))
		// 百度/360 的链接是站内跳转：块上有真实地址属性时优先用它
		if !isExternal(abs, home) {
			for _, attr := range cfg.URLAttrs {
				if cand, ok := b.Attr(attr); ok && isExternal(cand, home) {
					abs = cand
					break
				}
			}
		}
		if abs == "" {
			continue
		}
		if !isExternal(abs, home) && !cfg.AllowInternal {
			continue
		}
		if isInternalAggregate(abs, home, cfg) {
			continue
		}
		title := strings.TrimSpace(titleEl.Text())
		if xcore.RuneLen(title) < 4 || isJunkTitle(title) {
			continue
		}
		snippet := ""
		if el := firstBySelectors(b, snipSels); el != nil {
			snippet = strings.TrimSpace(el.Text())
		}
		if snippet == "" {
			snippet = strings.TrimSpace(strings.Replace(b.Text(), title, "", 1))
		}
		items = append(items, Item{
			Title:   xcore.TruncateRunes(title, 120),
			URL:     abs,
			Snippet: xcore.TruncateRunes(collapseSpaces(snippet), 220),
		})
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		items = genericItems(doc, home, limit)
	}
	return dedupeItems(items)
}

// genericItems 通用兜底：找「重复 ≥3 次、内含指向外站标题链接」的兄弟块。
func genericItems(doc *goquery.Document, home string, limit int) []Item {
	type member struct {
		el *goquery.Selection
		a  *goquery.Selection
	}
	var best []member
	for _, tag := range []string{"li", "article", "div", "section"} {
		groups := map[string][]member{}
		doc.Find(tag).Each(func(_ int, el *goquery.Selection) {
			h := el.Find("h1, h2, h3, h4").First()
			if h.Length() == 0 {
				return
			}
			a := h.Find("a[href]").First()
			if a.Length() == 0 {
				return
			}
			href, _ := a.Attr("href")
			if !isExternal(href, home) {
				return
			}
			if xcore.RuneLen(strings.TrimSpace(a.Text())) < 4 {
				return
			}
			cls, _ := el.Attr("class")
			id, _ := el.Attr("id")
			key := tag + "|" + cls + "|" + id
			groups[key] = append(groups[key], member{el, a})
		})
		for _, members := range groups {
			if len(members) >= 3 && len(members) > len(best) {
				best = members
			}
		}
		if len(best) > 0 {
			break
		}
	}

	var items []Item
	for _, m := range best {
		if len(items) >= limit {
			break
		}
		href, _ := m.a.Attr("href")
		title := strings.TrimSpace(m.a.Text())
		full := strings.TrimSpace(m.el.Text())
		items = append(items, Item{
			Title:   xcore.TruncateRunes(title, 120),
			URL:     href,
			Snippet: xcore.TruncateRunes(collapseSpaces(strings.Replace(full, title, "", 1)), 220),
		})
	}
	return items
}

func splitSels(raw, fallback string) []string {
	if strings.TrimSpace(raw) == "" {
		raw = fallback
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstBySelectors(node *goquery.Selection, sels []string) *goquery.Selection {
	for _, sel := range sels {
		if el := node.Find(sel).First(); el.Length() > 0 {
			return el
		}
	}
	return nil
}

func resolveHref(href, base string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "javascript:") ||
		strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") {
		return ""
	}
	b, err := neturl.Parse(base)
	if err != nil {
		return ""
	}
	r, err := neturl.Parse(href)
	if err != nil {
		return ""
	}
	return b.ResolveReference(r).String()
}

func homeHost(engine string) string {
	cfg := Engines[engine]
	if u, err := neturl.Parse(cfg.Home); err == nil {
		return strings.Replace(u.Hostname(), "www.", "", 1)
	}
	return ""
}

func isExternal(href, home string) bool {
	u, err := neturl.Parse(href)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.Replace(u.Hostname(), "www.", "", 1)
	return host != "" && !strings.Contains(host, home)
}

func collapseSpaces(s string) string { return reSpaces.ReplaceAllString(s, " ") }

var adMarkers = []string{"b_ad", "result--ad", "ec-tuiguang", "ec_ad", "e-ads", "ad--", "sponsored", "cpc"}

var junkTitles = []string{"大家还在搜", "大家都在搜", "相关搜索", "其他人还搜了", "为您推荐", "进一步探索"}

func isAdBlock(b *goquery.Selection) bool {
	cls, _ := b.Attr("class")
	cls = strings.ToLower(cls)
	for _, m := range adMarkers {
		if strings.Contains(cls, m) {
			return true
		}
	}
	if _, ok := b.Attr("data-tuiguang"); ok {
		return true
	}
	text := strings.TrimSpace(b.Text())
	for _, p := range []string{"广告", "推广", "赞助"} {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

func isJunkTitle(title string) bool {
	for _, k := range junkTitles {
		if strings.Contains(title, k) {
			return true
		}
	}
	return false
}

// unwrapToutiao 解开头条 /search/jump?...&url=<zlink?h5_url=真实地址>，纯字符串操作。
func unwrapToutiao(href string) string {
	u, err := neturl.Parse(href)
	if err != nil {
		return ""
	}
	inner := u.Query().Get("url")
	if inner == "" {
		return ""
	}
	iu, err := neturl.Parse(inner)
	if err != nil {
		return ""
	}
	h5 := iu.Query().Get("h5_url")
	real := h5
	if real == "" {
		real = inner
	}
	if strings.HasPrefix(real, "http://") || strings.HasPrefix(real, "https://") {
		return real
	}
	return ""
}

// unwrapBing 解 bing 的 ck/a?u=a1<base64url> 跳转。
func unwrapBing(href string) string {
	u, err := neturl.Parse(href)
	if err != nil || !strings.Contains(u.Host, "bing.com") || !strings.HasPrefix(u.Path, "/ck/a") {
		return href
	}
	raw := u.Query().Get("u")
	if raw == "" {
		return href
	}
	raw = strings.TrimPrefix(raw, "a1")
	if pad := len(raw) % 4; pad != 0 {
		raw += strings.Repeat("=", 4-pad)
	}
	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		return href
	}
	return string(decoded)
}

func unwrapByEngine(cfg Engine, href string) string {
	switch cfg.Unwrap {
	case "bing":
		return unwrapBing(href)
	case "toutiao":
		if real := unwrapToutiao(href); real != "" {
			return real
		}
		return href
	default:
		return UnwrapRedirect(href)
	}
}

func isInternalAggregate(href, home string, cfg Engine) bool {
	u, err := neturl.Parse(href)
	if err != nil {
		return false
	}
	if !strings.Contains(strings.Replace(u.Hostname(), "www.", "", 1), home) {
		return false
	}
	for _, p := range cfg.DropInternalPaths {
		if strings.HasPrefix(u.Path, p) {
			return true
		}
	}
	return false
}

// UnwrapRedirect 把 ddg 的 /l/?uddg=... 跳转还原成真实地址。
func UnwrapRedirect(href string) string {
	u, err := neturl.Parse(href)
	if err != nil {
		return href
	}
	if strings.Contains(u.Host, "duckduckgo.com") && strings.HasPrefix(u.Path, "/l/") {
		if v := u.Query().Get("uddg"); v != "" {
			return v
		}
	}
	return href
}

func dedupeItems(items []Item) []Item {
	seen := map[string]bool{}
	out := items[:0]
	for _, it := range items {
		key, _, _ := strings.Cut(it.URL, "#")
		if key == "" || it.Title == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out
}

// CffiFetchPages 用同一个客户端串行抓多页（bing 需要先有首页 Cookie + Referer）。
func CffiFetchPages(engine, query string, pages, timeout int) []string {
	cfg := Engines[engine]
	client, err := xfetch.NewClient("", timeout)
	if err != nil {
		return nil
	}
	defer client.Close()
	base := strings.ReplaceAll(cfg.Search, "{q}", neturl.QueryEscape(query))
	var out []string

	if cfg.PageKind == "ddg-post" {
		r, err := client.FetchWithReferer(base, "", "")
		if err != nil {
			return nil
		}
		html := r.Text()
		out = append(out, html)
		for p := 2; p <= pages; p++ {
			form := ddgNextForm(html)
			if form == nil {
				break
			}
			r2, err := client.PostForm("https://html.duckduckgo.com/html/", form, base)
			if err != nil {
				break
			}
			html = r2.Text()
			out = append(out, html)
			time.Sleep(800 * time.Millisecond)
		}
		return out
	}

	prev := ""
	for p := 1; p <= pages; p++ {
		u := base
		if p > 1 {
			u = PageURL(engine, query, p)
		}
		r, err := client.FetchWithReferer(u, "", prev)
		if err != nil {
			break
		}
		out = append(out, r.Text())
		prev = u
		time.Sleep(800 * time.Millisecond)
	}
	return out
}

// ddgNextForm 从 ddg 结果页里抠「下一页」表单。
func ddgNextForm(htmlStr string) map[string]string {
	doc, err := xparse.MakeSoup(htmlStr)
	if err != nil {
		return nil
	}
	var out map[string]string
	doc.Find("form").EachWithBreak(func(_ int, f *goquery.Selection) bool {
		if f.Find("input[name=vqd]").Length() == 0 {
			return true
		}
		out = map[string]string{}
		f.Find("input").Each(func(_ int, in *goquery.Selection) {
			name, _ := in.Attr("name")
			if name == "" {
				return
			}
			v, _ := in.Attr("value")
			out[name] = v
		})
		return false
	})
	return out
}

// PageURL 拼第 page 页的地址（page 从 1 开始）。
func PageURL(engine, query string, page int) string {
	cfg, ok := Engines[engine]
	if !ok || page <= 1 || cfg.PageKey == "" {
		return QuotedURL(engine, query)
	}
	base := QuotedURL(engine, query)
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	offset := cfg.PageStep * (page - 1)
	if cfg.PageKey == "first" {
		offset++
	}
	out := base + sep + cfg.PageKey + "=" + strconv.Itoa(offset)
	if cfg.PageExtra != "" {
		out += "&" + cfg.PageExtra
	}
	return out
}

// LinkTable 把命名空间的 @代号 映射成真实地址。
func LinkTable(ns string, limit int) string {
	if ns == "" {
		return ""
	}
	rows := xdb.LinksByNamespace(ns, limit)
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n---\n## 链接对照\n\n")
	for _, r := range rows {
		b.WriteString("- @" + r.ID + " → " + r.URL + "\n")
	}
	return b.String()
}

// Run 按顺序尝试引擎，返回第一个成功的结果。
func Run(engines []string, query, via string, limit, timeout int, headless, show bool, pages, count int) (*Result, error) {
	var searcher *Searcher
	defer func() {
		if searcher != nil {
			searcher.Close()
		}
	}()

	if (via == "playwright" || via == "auto") && HasDisplay() {
		s, err := NewSearcher(timeout, headless, show)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[*] ⚠️ playwright 启动失败，回退 cffi: %.100s\n", err.Error())
			if via == "playwright" {
				via = "cffi"
			}
		} else {
			searcher = s
		}
	}

	for _, engine := range engines {
		if _, ok := Engines[engine]; !ok {
			fmt.Fprintf(os.Stderr, "[*] ⚠️ 未知引擎 %s，跳过\n", engine)
			continue
		}
		start := time.Now()
		html, used, err := fetchOne(engine, query, via, timeout, searcher, headless)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[*] ❌ %s (%s): %.120s\n", engine, via, err.Error())
			continue
		}

		if IsBlocked(html) {
			// 状态可能被污染：清掉重试一次
			_ = os.Remove(StatePath(engine))
			_ = os.Remove(CookieJarPath(engine))
			if h2, u2, err2 := fetchOne(engine, query, via, timeout, searcher, headless); err2 == nil {
				html, used = h2, u2
			}
		}
		if IsBlocked(html) && via == "auto" && searcher != nil {
			fmt.Fprintf(os.Stderr, "[*] ↻ %s: cffi 撞风控，改用 playwright\n", engine)
			if h3, err3 := searcher.Fetch(engine, query); err3 == nil {
				html, used = h3, "playwright"
			}
		}
		if IsBlocked(html) {
			fmt.Fprintf(os.Stderr, "[*] ⚠️ %s: 仍被风控，换下一个\n", engine)
			continue
		}

		title, md, ns := Extract(engine, html, 0)
		engineItems := ExtractItems(engine, html, 20)
		if engine == "toutiao" && len(engines) > 1 && len(engineItems) < 2 {
			fmt.Fprintf(os.Stderr, "[*] ⚠️ %s: 只抽到 %d 条，换下一个\n", engine, len(engineItems))
			continue
		}
		if xcore.RuneLen(strings.TrimSpace(md)) < 200 && len(engineItems) < 3 {
			if via == "auto" && used == "cffi" && searcher != nil {
				fmt.Fprintf(os.Stderr, "[*] ↻ %s: cffi 只拿到 %d 字符，改用 playwright\n", engine, xcore.RuneLen(strings.TrimSpace(md)))
				if h4, err4 := searcher.Fetch(engine, query); err4 == nil {
					title, md, ns = Extract(engine, h4, 0)
					used = "playwright"
				}
			}
			if xcore.RuneLen(strings.TrimSpace(md)) < 200 {
				fmt.Fprintf(os.Stderr, "[*] ⚠️ %s: 内容过少（%d 字符），换下一个\n", engine, xcore.RuneLen(strings.TrimSpace(md)))
				continue
			}
		}
		items := engineItems
		// 翻页：默认只抓首页（快），--pages N 才继续拼
		if pages > 1 {
			seen := map[string]bool{}
			for _, it := range items {
				if k, _, _ := strings.Cut(it.URL, "#"); k != "" {
					seen[k] = true
				}
			}
			chain := CffiFetchPages(engine, query, pages, timeout)
			for pno, phtml := range chain {
				if pno == 0 {
					continue
				}
				more := ExtractItems(engine, phtml, 20)
				if len(more) == 0 {
					break
				}
				added := 0
				for _, it := range more {
					k, _, _ := strings.Cut(it.URL, "#")
					if k != "" && !seen[k] {
						seen[k] = true
						items = append(items, it)
						added++
					}
				}
				fmt.Fprintf(os.Stderr, "[*] 📄 %s 第 %d 页: 新增 %d 条\n", engine, pno+1, added)
				time.Sleep(800 * time.Millisecond)
			}
		}
		if count > 0 && len(items) > count {
			items = items[:count]
		}
		elapsed := time.Since(start).Seconds()
		channel := used
		if via == "auto" {
			channel = "auto→" + used
		}
		fmt.Fprintf(os.Stderr, "[*] ✅ %s (%s): %.2fs / %d 字符\n", engine, channel, elapsed, xcore.RuneLen(md))
		content := md
		if limit > 0 {
			content = xcore.TruncateRunes(md, limit)
		}
		return &Result{
			Engine: engine, Via: channel, Query: query, Title: title,
			Namespace: ns, Content: content, Items: items, Elapsed: round2(elapsed),
		}, nil
	}
	return nil, nil
}

func fetchOne(engine, query, via string, timeout int, searcher *Searcher, headless bool) (string, string, error) {
	if via == "cffi" {
		h, err := CffiFetch(engine, query, timeout)
		return h, "cffi", err
	}
	if via == "auto" {
		if h, err := CffiFetch(engine, query, timeout); err == nil && !IsBlocked(h) {
			return h, "cffi", nil
		}
	}
	if searcher == nil {
		return "", "", fmt.Errorf("没有可用的浏览器")
	}
	h, err := searcher.Fetch(engine, query)
	return h, "playwright", err
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

// ---------------------------------------------------------------- 渲染

var reHead = regexp.MustCompile(`^###\s+\[?(.+?)\]?(?:\((@[\w\-]+)\))?\s*$`)
var (
	reMDImage = regexp.MustCompile(`!\[[^\]]*\]\(@[\w\-]+\)`)
	reMDLink  = regexp.MustCompile(`\[([^\]]*)\]\(@[\w\-]+\)`)
	reMDEmph  = regexp.MustCompile(`[*_]{1,2}`)
	reSpaces  = regexp.MustCompile(`\s+`)
)

// Item 一条搜索结果。
type Item struct {
	Title   string
	Code    string
	URL     string
	Snippet string
	Body    []string
}

// Clean 去掉图片/链接代号/强调符号。
func Clean(s string) string {
	s = reMDImage.ReplaceAllString(s, "")
	s = reMDLink.ReplaceAllString(s, "$1")
	s = reMDEmph.ReplaceAllString(s, "")
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// ParseItems 从 Markdown 里解析条目。
func ParseItems(markdown string) []Item {
	var items []Item
	var cur *Item
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := reHead.FindStringSubmatch(trimmed); m != nil {
			if cur != nil {
				items = append(items, *cur)
				cur = nil
			}
			title := Clean(m[1])
			if title != "" {
				cur = &Item{Title: title, Code: m[2]}
			}
		} else if cur != nil && trimmed != "" {
			cur.Body = append(cur.Body, trimmed)
		}
	}
	if cur != nil {
		items = append(items, *cur)
	}
	var out []Item
	for _, it := range items {
		if it.Title != "" {
			out = append(out, it)
		}
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// MDResults 渲染人类可读的结果列表；没有条目时直接把 Markdown 交给模型读。
func MDResults(r *Result, level string) string {
	n := map[string]int{"brief": 5, "normal": 10, "full": 20}[level]
	snip := map[string]int{"brief": 50, "normal": 110, "full": 400}[level]
	if n == 0 {
		n = 10
	}
	items := r.Items
	if len(items) == 0 {
		items = parseLegacyItems(r.Content)
	}
	head := fmt.Sprintf("## %s ｜ %s ｜ %s ｜ %.2fs", r.Engine, r.Query, r.Via, r.Elapsed)
	if len(items) == 0 {
		body := strings.TrimSpace(r.Content)
		if body == "" {
			return head + "\n\n（没拿到内容：试 --via browser，或换引擎）"
		}
		return head + "\n\n" + body + LinkTable(r.Namespace, 40)
	}
	out := []string{head, ""}
	limit := n
	if len(items) < limit {
		limit = len(items)
	}
	for i := 0; i < limit; i++ {
		it := items[i]
		src := it.Snippet
		if src == "" && len(it.Body) > 0 {
			src = strings.Join(it.Body, " ")
		}
		body := xcore.TruncateRunes(Clean(src), snip)
		tail := ""
		if it.Code != "" {
			tail = " [" + it.Code + "]"
		}
		out = append(out, fmt.Sprintf("%d. **%s**%s", i+1, xcore.TruncateRunes(it.Title, 80), tail))
		if body != "" {
			out = append(out, "   "+body)
		}
	}
	if len(items) > n {
		out = append(out, "")
		out = append(out, fmt.Sprintf("…还有 %d 条，用 -L full 或 -j", len(items)-n))
	}
	return strings.Join(out, "\n")
}

// parseLegacyItems 老路径：从 Markdown 里解析条目（DOM 抽取失败时兜底）。
func parseLegacyItems(markdown string) []Item { return ParseItems(markdown) }

// StructuredItems 供 -j 输出的结构化条目。
func StructuredItems(r *Result, limit, snip int) []map[string]string {
	if len(r.Items) > 0 {
		items := r.Items
		if len(items) > limit {
			items = items[:limit]
		}
		out := []map[string]string{}
		for _, it := range items {
			out = append(out, map[string]string{
				"title":   xcore.TruncateRunes(it.Title, 120),
				"snippet": xcore.TruncateRunes(Clean(it.Snippet), snip),
				"code":    it.Code,
				"url":     it.URL,
			})
		}
		return out
	}
	parsed := ParseItems(r.Content)
	if len(parsed) > limit {
		parsed = parsed[:limit]
	}
	out := []map[string]string{}
	for _, it := range parsed {
		out = append(out, map[string]string{
			"title":   xcore.TruncateRunes(it.Title, 120),
			"snippet": xcore.TruncateRunes(Clean(strings.Join(it.Body, " ")), snip),
			"code":    it.Code,
			"url":     it.URL,
		})
	}
	return out
}

var _ = goquery.NodeName
