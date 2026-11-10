// Command sf 是 super-fetch 的 Go 实现（完整对齐 Python 版 CLI）。
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"superfetch/internal/xcore"
	"superfetch/internal/xdb"
	"superfetch/internal/xfetch"
	"superfetch/internal/xparse"
	"superfetch/internal/xrender"
)

type options struct {
	Engine         string
	Sites          *string
	Remember       bool
	Full           bool
	Session        string
	SessionSet     bool
	Wait           int
	Timeout        int
	AutoThreshold  int
	Proxy          string
	Retries        int
	Output         string
	Interactive    bool
	Level          string
	MaxChars       *int
	JSON           bool
	Links          bool
	EvalJS         string
	Fresh          bool
	XHR            *string
	Replace        []string
	Replay         bool
	Embedded       bool
	URLs           []string
	File           string
	Concurrency    int
	DomainDelayMin float64
	DomainDelayMax float64
	Jitter         float64
	Silent         bool
}

func defaultOptions() *options {
	return &options{
		Engine: "auto", Wait: 3, Timeout: 30, AutoThreshold: 400,
		Level: "normal", Concurrency: 5,
		DomainDelayMin: 2.0, DomainDelayMax: 5.0, Jitter: 0.5,
	}
}

func parseArgs(args []string) (*options, error) {
	o := defaultOptions()
	i := 0
	need := func(name string) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s 需要一个值", name)
		}
		i++
		return args[i], nil
	}
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			o.URLs = append(o.URLs, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			o.URLs = append(o.URLs, a)
			continue
		}
		name, inline, hasInline := strings.Cut(a, "=")
		value := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			return need(name)
		}
		optional := func(constVal string) (string, bool) {
			if hasInline {
				return inline, true
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				return args[i], true
			}
			return constVal, true
		}
		switch name {
		case "-h", "--help":
			printHelp()
			os.Exit(0)
		case "--via", "--engine", "-e":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Engine = v
		case "--sites":
			v, _ := optional("")
			o.Sites = &v
		case "--remember":
			o.Remember = true
		case "--full", "-f":
			o.Full = true
		case "--fresh":
			o.Fresh = true
		case "--session", "-s":
			v, _ := optional("session.json")
			o.Session, o.SessionSet = v, true
		case "--wait", "-w":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Wait, _ = strconv.Atoi(v)
		case "--timeout", "-t":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Timeout, _ = strconv.Atoi(v)
		case "--auto-threshold":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.AutoThreshold, _ = strconv.Atoi(v)
		case "--proxy", "-p":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Proxy = v
		case "--retries", "-r":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Retries, _ = strconv.Atoi(v)
		case "--output", "-o":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Output = v
		case "--login", "--interactive", "-i":
			o.Interactive = true
		case "--level", "-L":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Level = v
		case "--max-chars", "-m":
			v, err := value()
			if err != nil {
				return nil, err
			}
			n, _ := strconv.Atoi(v)
			o.MaxChars = &n
		case "--json", "-j":
			o.JSON = true
		case "--links", "-l":
			o.Links = true
		case "--eval":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.EvalJS = v
		case "--discover", "--xhr":
			v, _ := optional("")
			o.XHR = &v
		case "--replace":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Replace = append(o.Replace, v)
		case "--verify", "--replay":
			o.Replay = true
		case "--embedded", "--data":
			o.Embedded = true
		case "--file", "-F":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.File = v
		case "--concurrency", "-c":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Concurrency, _ = strconv.Atoi(v)
		case "--domain-delay-min":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.DomainDelayMin, _ = strconv.ParseFloat(v, 64)
		case "--domain-delay-max":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.DomainDelayMax, _ = strconv.ParseFloat(v, 64)
		case "--jitter":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Jitter, _ = strconv.ParseFloat(v, 64)
		case "--silent":
			o.Silent = true
		default:
			return nil, fmt.Errorf("未知参数: %s", name)
		}
	}
	switch o.Engine {
	case "http":
		o.Engine = "cffi"
	case "browser":
		o.Engine = "playwright"
	case "auto", "cffi", "playwright":
	default:
		stderr("[*] ⚠️ 未知通道 %q，回退 auto（可选：auto|cffi|playwright|http|browser）\n", o.Engine)
		o.Engine = "auto"
	}
	return o, nil
}

func printHelp() {
	fmt.Fprint(os.Stderr, `Super Fetch - 高性能抓取基座（Go 版）

通道：
  --via/-e auto|cffi|playwright|http|browser   默认 auto（先 HTTP，按需升浏览器）
通用：--sites [DOMAIN] --remember -f/--full -s/--session -w/--wait -t/--timeout
      --auto-threshold -p/--proxy -r/--retries -o/--output
单 URL：-i/--login -L/--level brief|normal|full -m/--max-chars -j/--json -l/--links
        --eval JS --discover [PATTERN] --replace OLD=NEW --verify --embedded
批量：-F/--file -c/--concurrency --domain-delay-min/max --jitter --silent
`)
}

var progStart = time.Now()

func main() {
	o, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	url := ""
	if len(o.URLs) > 0 {
		url = o.URLs[0]
	}

	// 没指定 -s 时按域名自动匹配登录态
	if !o.SessionSet && url != "" {
		if p, ok := xfetch.AutoMatchSession(url); ok {
			o.Session = p
			o.SessionSet = true
			fmt.Fprintf(os.Stderr, "[*] 🔑 自动匹配登录态: %s\n", filepath.Base(p))
		}
	}

	if o.Sites != nil {
		rows := xdb.ListSites(*o.Sites)
		if len(rows) == 0 {
			fmt.Println("接口库为空")
		}
		for _, r := range rows {
			ep := r.Endpoint
			if len(ep) > 46 {
				ep = ep[:46]
			}
			fmt.Printf("%-26s %-7s %-46s %s ✓%d/✗%d\n", r.Domain, r.Method, ep, r.Status, r.OKCount, r.FailCount)
		}
		return
	}

	sessionPath := ""
	if o.Session != "" {
		sessionPath = xcore.ResolveSessionPath(o.Session)
	}

	isBatch := o.File != "" || len(o.URLs) > 1
	if o.Interactive && (isBatch || len(o.URLs) != 1) {
		printJSON(map[string]any{"error": "批量模式不支持交互模式 (-i)"})
		os.Exit(1)
	}
	if o.Interactive {
		o.Engine = "playwright"
		if !o.SessionSet {
			o.Session = "session.json"
			sessionPath = xcore.ResolveSessionPath(o.Session)
		}
		o.Timeout = maxInt(o.Timeout, 600)
	}

	if isBatch {
		os.Exit(runBatch(o, sessionPath))
	}
	if url == "" {
		fmt.Fprintln(os.Stderr, "错误：请提供 URL（单 URL 模式）或使用 -F/传入多个 URL（批量模式）")
		os.Exit(1)
	}
	if !strings.Contains(url, ":") {
		printJSON(map[string]any{"error": "无效的 URL"})
		os.Exit(1)
	}
	os.Exit(runSingle(o, sessionPath))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func printJSON(v any) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}

func stderr(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
}

// optsToBrowser 把 CLI 参数转成浏览器参数。
func optsToBrowser(o *options, interactive bool) xfetch.BrowserOptions {
	return xfetch.BrowserOptions{
		Headless:    false,
		Show:        interactive,
		Proxy:       o.Proxy,
		Timeout:     o.Timeout,
		Wait:        o.Wait,
		Interactive: interactive,
		NoDaemon:    o.Fresh,
	}
}

// fetchOnce 执行一次抓取。
func fetchOnce(o *options, sessionPath, engine string, timeout int, url string) (*xfetch.Response, error) {
	if engine == "playwright" {
		b, err := xfetch.LaunchBrowser(optsToBrowser(o, o.Interactive))
		if err != nil {
			return nil, err
		}
		defer b.Close()
		html, ctype, err := b.FetchWithBrowser(url, sessionPath)
		if err != nil {
			return nil, err
		}
		return &xfetch.Response{Body: []byte(html), ContentType: ctype}, nil
	}
	return xfetch.FetchOnce(url, o.Proxy, sessionPath, timeout)
}

func fetchWithRetries(o *options, sessionPath, engine, url string, timeout int) (*xfetch.Response, error) {
	var last error
	for i := 0; i <= maxInt(o.Retries, 0); i++ {
		resp, err := fetchOnce(o, sessionPath, engine, timeout, url)
		if err == nil {
			return resp, nil
		}
		last = err
		if i < o.Retries {
			time.Sleep(time.Second)
		}
	}
	return nil, last
}

// definitiveHTTP 这些状态码重试也没意义：资源不存在/无权限/已下架。
func definitiveHTTP(status int) bool {
	switch status {
	case 400, 401, 402, 404, 405, 410, 451:
		return true
	}
	return false
}

func levelCap(level string) int {
	switch level {
	case "brief":
		return 800
	case "full":
		return 0
	default:
		return 5000
	}
}

func runSingle(o *options, sessionPath string) int {
	url := o.URLs[0]
	if !o.JSON {
		stderr("[*] 🚀 任务启动: %s\n", url)
		if sessionPath != "" {
			stderr("[*] 🎫 使用会话: %s\n", filepath.Base(sessionPath))
		}
	}

	if site := xdb.FindSite(url); site != nil && !o.JSON {
		stderr("[*] 📚 接口库中有记录: %s %s（参考，不自动发请求）\n", site.Method, site.Endpoint)
	}

	// --discover / --eval 探针模式
	if o.EvalJS != "" || o.XHR != nil {
		return runProbe(o, sessionPath, url)
	}

	engines := []string{o.Engine}
	if o.Engine == "auto" {
		engines = []string{"cffi", "playwright"}
	}

	var (
		resp        *xfetch.Response
		html        string
		title, md   string
		ns          string
		lastErr     error
		skeletonFix bool
	)

	for idx, engine := range engines {
		r, err := fetchWithRetries(o, sessionPath, engine, url, o.Timeout)
		if err != nil {
			var he *xfetch.HTTPError
			if errors.As(err, &he) && definitiveHTTP(he.Status) {
				printJSON(map[string]any{"error": fmt.Sprintf("HTTP %d", he.Status), "url": url})
				return 1
			}
			lastErr = err
			if !o.JSON {
				stderr("[*] ❌ [%s] 失败: %v\n", engine, err)
			}
			continue
		}
		resp = r

		if r.IsBinary {
			if o.Output != "" {
				if err := os.WriteFile(o.Output, r.Body, 0o644); err == nil {
					stderr("[*] ✅ 文件已保存: %s\n", o.Output)
					return 0
				}
			}
			printJSON(map[string]any{"error": fmt.Sprintf("二进制内容 (%s) 需 -o 参数", r.ContentType)})
			return 1
		}

		html = r.Text()

		if o.Engine != "playwright" {
			if target := xparse.MetaRefreshTarget(html); target != "" {
				if !o.JSON {
					stderr("[*] ↪️ 跟随跳转: %s\n", target)
				}
				if r2, err := fetchWithRetries(o, sessionPath, engine, target, o.Timeout); err == nil {
					if !r2.IsBinary {
						html = r2.Text()
					}
				}
			}
		}

		if strings.HasPrefix(html, `{"error"`) {
			fmt.Println(html)
			return 1
		}

		if o.Embedded {
			return runEmbedded(o, url, html)
		}

		if !o.JSON {
			stderr("[*] 📝 提取内容... [%s]\n", engine)
		}
		title, md, ns = xparse.ExtractPageContent(html, url, o.Full)

		// 骨架屏兜底：正文几乎全是 word 占位符 → 加时重抓一次
		if engine == "playwright" && !skeletonFix && xparse.LooksLikeSkeleton(md) {
			skeletonFix = true
			if !o.JSON {
				stderr("[*] 🦴 正文还是骨架屏，加时重抓…\n")
			}
			oldWait := o.Wait
			o.Wait = maxInt(o.Wait+5, 8)
			if r2, err := fetchWithRetries(o, sessionPath, engine, url, o.Timeout); err == nil && !r2.IsBinary {
				if t2, m2, n2 := xparse.ExtractPageContent(r2.Text(), url, o.Full); !xparse.LooksLikeSkeleton(m2) {
					html, title, md, ns = r2.Text(), t2, m2, n2
				}
			}
			o.Wait = oldWait
		}

		// 提示：内嵌数据比正文富得多
		if !o.JSON && o.Engine != "cffi" {
			if blocks := xparse.ExtractEmbeddedData(html, 200, 3); len(blocks) > 0 {
				total := 0
				for _, b := range blocks {
					total += xcore.RuneLen(b.Raw)
				}
				if total > maxInt(3*xcore.RuneLen(md), 2000) {
					stderr("[*] 💡 页面内嵌 %dKB 结构化数据（正文仅 %d 字），加 --embedded 可直接取\n",
						total/1024, xcore.RuneLen(md))
				}
			}
		}

		blocked := xparse.LooksBlocked(html)
		if blocked && !o.JSON {
			stderr("[*] 🚫 疑似验证/拦截页，强制升级浏览器兜底\n")
		}

		if o.Engine == "auto" && idx == 0 && (blocked || xparse.NeedsPlaywright(html, md, o.AutoThreshold)) {
			if blocks := xparse.ExtractEmbeddedData(html, 200, 3); len(blocks) > 0 {
				if !o.JSON {
					stderr("[*] 💡 内容薄，但发现内嵌数据，免开浏览器\n")
				}
				printJSON(map[string]any{"url": url, "mode": "embedded", "blocks": blocksToJSON(blocks, 0)})
				return 0
			}
			if !o.JSON {
				stderr("[*] 🔄 cffi 只取到 %d 字符，改用 playwright 重试\n", xcore.RuneLen(strings.TrimSpace(md)))
			}
			continue
		}
		break
	}

	if html == "" && resp == nil {
		printJSON(map[string]any{"error": fmt.Sprintf("运行异常: %v", lastErr)})
		return 1
	}

	cap := levelCap(o.Level)
	if o.MaxChars != nil {
		cap = *o.MaxChars
	}
	if o.Level == "brief" {
		o.Links = false
	}
	if o.Links && ns != "" {
		md += dumpLinks(ns, 50)
	}

	header := ""
	if title != "" {
		header = "# " + title + "\n"
	}
	sessionName := "无"
	if sessionPath != "" {
		sessionName = filepath.Base(sessionPath)
	}
	meta := fmt.Sprintf("> [系统提示] 命名空间: %s | 会话: %s\n\n", ns, sessionName)
	fullText := header + meta + md

	if o.JSON {
		content := md
		if cap > 0 {
			content = xcore.TruncateRunes(md, cap)
		}
		printJSON(map[string]any{
			"url": url, "success": true, "title": title, "namespace": ns,
			"content": content, "chars": xcore.RuneLen(md),
			"truncated": cap > 0 && xcore.RuneLen(md) > cap,
		})
		return 0
	}

	if o.Output != "" {
		if err := os.WriteFile(o.Output, []byte(fullText), 0o644); err != nil {
			stderr("[*] ❌ 保存失败: %v\n", err)
			return 1
		}
		stderr("[*] 💾 已保存: %s\n", o.Output)
		return 0
	}

	if cap > 0 && xcore.RuneLen(fullText) > cap {
		stderr("[*] ✂️ 输出已截断: %d/%d 字符（-L full 或 -m 放宽，-o 落盘）\n", cap, xcore.RuneLen(fullText))
		fullText = xcore.TruncateRunes(fullText, cap) + "\n\n...（已截断）"
	}
	fmt.Println(fullText)
	return 0
}

func dumpLinks(ns string, limit int) string {
	rows := xdb.LinksByNamespace(ns, limit)
	if len(rows) == 0 {
		return ""
	}
	lines := []string{"", "---", "## 链接对照", ""}
	for _, r := range rows {
		lines = append(lines, "- @"+r.ID+" → "+r.URL)
	}
	if len(rows) == limit {
		lines = append(lines, fmt.Sprintf("- ……（最多显示 %d 条，用 sfl 查全部）", limit))
	}
	return "\n" + strings.Join(lines, "\n")
}

func runEmbedded(o *options, url, html string) int {
	blocks := xparse.ExtractEmbeddedData(html, 200, 3)
	cap := map[string]int{"brief": 600, "normal": 6000, "full": 0}[o.Level]
	if o.MaxChars != nil {
		cap = *o.MaxChars
	}
	items := blocksToJSON(blocks, cap)
	out := map[string]any{"url": url, "mode": "embedded", "blocks": items}
	if len(blocks) == 0 {
		out["hint"] = "HTML 里没有内嵌 JSON，试 --discover 找接口"
	}
	if o.Output != "" {
		data, _ := json.MarshalIndent(out, "", "  ")
		if err := os.WriteFile(o.Output, data, 0o644); err == nil {
			stderr("[*] 💾 已保存: %s\n", o.Output)
			return 0
		}
	}
	if o.JSON {
		printJSON(out)
		return 0
	}
	var views []xrender.EmbeddedView
	for _, it := range items {
		v := xrender.EmbeddedView{
			Kind:      it.Kind,
			Chars:     it.Chars,
			Truncated: it.Truncated,
			Preview:   it.Preview,
			Outline:   it.Outline,
		}
		views = append(views, v)
	}
	fmt.Println(xrender.EmbeddedMarkdown(views))
	return 0
}

type embeddedItem struct {
	Kind      string      `json:"kind"`
	Chars     int         `json:"chars"`
	Data      any         `json:"data"`
	Truncated bool        `json:"truncated,omitempty"`
	Outline   [][2]string `json:"outline,omitempty"`
	Preview   string      `json:"preview,omitempty"`
}

func blocksToJSON(blocks []xparse.EmbeddedBlock, cap int) []embeddedItem {
	out := make([]embeddedItem, 0, len(blocks))
	for _, b := range blocks {
		raw, _ := json.Marshal(b.Data)
		chars := xcore.RuneLen(string(raw))
		item := embeddedItem{Kind: b.Kind, Chars: chars, Outline: xparse.Outline(b.Data, 12)}
		if cap > 0 && chars > cap {
			item.Truncated = true
			item.Preview = xcore.TruncateRunes(string(raw), cap)
		} else {
			item.Data = b.Data
		}
		out = append(out, item)
	}
	return out
}

func runProbe(o *options, sessionPath, url string) int {
	out, err := xfetch.Probe(optsToBrowser(o, false), url, sessionPath, o.EvalJS, o.XHR, o.Replay, o.Replace)
	if err != nil {
		printJSON(map[string]any{"error": err.Error()})
		return 1
	}
	if o.Remember {
		rememberProbe(out, sessionPath, url)
	}
	if o.JSON {
		fmt.Println(out)
		return 0
	}
	var res xfetch.ProbeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		fmt.Println(out)
		return 0
	}
	var parts []string
	if res.Captured != nil {
		parts = append(parts, xrender.DiscoverTable(res.Captured, res.Count))
	}
	if res.Result != nil {
		parts = append(parts, xrender.EvalBlock(res.Result, 800))
	}
	if res.Replay != nil {
		method, u := "", ""
		if len(res.Captured) > 0 {
			method, u = res.Captured[0].Method, res.Captured[0].URL
		}
		parts = append(parts, xrender.ReplayBlock(res.Replay, method, u))
	}
	fmt.Println(strings.Join(parts, "\n\n"))
	return 0
}

func rememberProbe(rawJSON, sessionPath, pageURL string) {
	var res xfetch.ProbeResult
	if err := json.Unmarshal([]byte(rawJSON), &res); err != nil || len(res.Captured) == 0 {
		return
	}
	c := res.Captured[0]
	endpoint := c.URLFull
	if endpoint == "" {
		endpoint = c.URL
	}
	u, err := parseURL(pageURL)
	if err != nil {
		return
	}
	var keys []string
	if state, err := xfetch.LoadStorageState(sessionPath); err == nil {
		seen := map[string]bool{}
		for _, ck := range state.Cookies {
			if !seen[ck.Name] {
				seen[ck.Name] = true
				keys = append(keys, ck.Name)
			}
		}
	}
	if err := xdb.SaveSite(u.host, u.path, c.Method, endpoint, c.RequestBody, keys, "html", pageURL); err == nil {
		stderr("[*] 📚 已记入接口库: %s %s\n", c.Method, u.path)
	}
}

type urlParts struct {
	host string
	path string
}

func parseURL(raw string) (*urlParts, error) {
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return nil, err
	}
	return &urlParts{host: parsed.Host, path: parsed.Path}, nil
}

// ---------------------------------------------------------------- 批量

func readURLFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var urls []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}
	return urls
}

type domainLimiter struct {
	mu   sync.Mutex
	next map[string]time.Time
}

func newDomainLimiter() *domainLimiter {
	return &domainLimiter{next: map[string]time.Time{}}
}

func (l *domainLimiter) wait(domain string, minD, maxD float64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	delay := minD
	if maxD > minD {
		delay += rand.Float64() * (maxD - minD)
	}
	now := time.Now()
	last := l.next[domain]
	wait := time.Duration(0)
	if now.Before(last) {
		wait = last.Sub(now)
		l.next[domain] = last.Add(time.Duration(delay * float64(time.Second)))
	} else {
		l.next[domain] = now.Add(time.Duration(delay * float64(time.Second)))
	}
	return wait
}

func runBatch(o *options, sessionPath string) int {
	urls := append([]string{}, o.URLs...)
	if o.File != "" {
		urls = append(urls, readURLFile(o.File)...)
	}
	seen := map[string]bool{}
	var valid []string
	for _, u := range urls {
		if seen[u] {
			continue
		}
		seen[u] = true
		if strings.Contains(u, ":") {
			valid = append(valid, u)
		} else if !o.Silent {
			stderr("[*] ⚠️ 跳过无效 URL: %s\n", u)
		}
	}
	if len(valid) == 0 {
		printJSON(map[string]any{"error": "请提供 URL（参数或 -F 文件）"})
		return 1
	}
	if !o.Silent {
		stderr("[*] 📋 批量抓取任务启动\n")
		stderr("[*] 🔗 URL 数量: %d\n", len(valid))
		stderr("[*] 🚦 并发数: %d\n", o.Concurrency)
		stderr("[*] 🔧 引擎: %s\n", o.Engine)
		stderr("[*] ⏱️  域名间隔: %.1f-%.1fs\n", o.DomainDelayMin, o.DomainDelayMax)
		if sessionPath != "" {
			stderr("[*] 🎫 使用会话: %s\n", sessionPath)
		}
	}

	type result struct {
		URL        string `json:"url"`
		Success    bool   `json:"success"`
		Title      string `json:"title"`
		Content    string `json:"content"`
		Namespace  string `json:"namespace"`
		Error      string `json:"error"`
		htmlLen    int
		scripts    int
		definitive bool
	}

	limiter := newDomainLimiter()
	results := make([]result, len(valid))
	sem := make(chan struct{}, maxInt(o.Concurrency, 1))
	var wg sync.WaitGroup

	engineFor := func() string {
		if o.Engine == "auto" {
			return "cffi"
		}
		return o.Engine
	}

	for i, u := range valid {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			domain := hostOf(u)
			if wait := limiter.wait(domain, o.DomainDelayMin, o.DomainDelayMax); wait > 0 {
				if !o.Silent {
					stderr("[*] ⏳ 对 %s 限流，等待 %.2fs: %s\n", domain, wait.Seconds(), u)
				}
				time.Sleep(wait)
			}
			if o.Jitter > 0 {
				time.Sleep(time.Duration(rand.Float64() * o.Jitter * float64(time.Second)))
			}
			sem <- struct{}{}
			defer func() { <-sem }()

			engine := engineFor()
			if !o.Silent {
				stderr("[*] 🚀 [%s] 开始抓取: %s\n", engine, u)
			}
			res, err := fetchWithRetries(o, sessionPath, engine, u, o.Timeout)
			if err != nil {
				def := false
				var he *xfetch.HTTPError
				if errors.As(err, &he) && definitiveHTTP(he.Status) {
					def = true
				}
				results[i] = result{URL: u, Error: err.Error(), definitive: def}
				if !o.Silent {
					stderr("[*] ❌ [%s] 抓取异常: %s - %v\n", engine, u, err)
				}
				return
			}
			if res.IsBinary {
				results[i] = result{URL: u, Error: fmt.Sprintf("不支持的二进制内容: %s", res.ContentType)}
				return
			}
			html := res.Text()
			title, md, ns := xparse.ExtractPageContent(html, u, o.Full)
			results[i] = result{
				URL: u, Success: true, Title: title, Content: md, Namespace: ns,
				htmlLen: len(html), scripts: strings.Count(strings.ToLower(html), "<script"),
			}
			if !o.Silent {
				stderr("[*] ✅ [%s] 抓取完成: %s\n", engine, u)
			}
		}(i, u)
	}
	wg.Wait()

	// auto：内容薄的用浏览器补抓
	if o.Engine == "auto" {
		var weak []int
		for i, r := range results {
			if r.definitive {
				continue // 404/410 之类，浏览器也救不回来
			}
			if !r.Success || (xcore.RuneLen(strings.TrimSpace(r.Content)) < o.AutoThreshold && (r.htmlLen > 200_000 || r.scripts >= 3)) {
				if r.URL != "" {
					weak = append(weak, i)
				}
			}
		}
		if len(weak) > 0 {
			if !o.Silent {
				stderr("[*] 🔄 %d 个页面内容不足，改用 playwright 重试\n", len(weak))
			}
			var wg2 sync.WaitGroup
			sem2 := make(chan struct{}, maxInt(o.Concurrency, 1))
			for _, idx := range weak {
				wg2.Add(1)
				go func(idx int) {
					defer wg2.Done()
					sem2 <- struct{}{}
					defer func() { <-sem2 }()
					u := results[idx].URL
					res, err := fetchWithRetries(o, sessionPath, "playwright", u, o.Timeout)
					if err != nil || res.IsBinary {
						return
					}
					html := res.Text()
					title, md, ns := xparse.ExtractPageContent(html, u, o.Full)
					results[idx] = result{
						URL: u, Success: true, Title: title, Content: md, Namespace: ns,
						htmlLen: len(html), scripts: strings.Count(strings.ToLower(html), "<script"),
					}
				}(idx)
			}
			wg2.Wait()
		}
	}

	success := 0
	for _, r := range results {
		if r.Success {
			success++
		}
	}
	payload := map[string]any{
		"total": len(results), "success": success, "failed": len(results) - success,
		"results": results,
	}
	if o.Output != "" {
		data, _ := json.MarshalIndent(payload, "", "  ")
		if err := os.WriteFile(o.Output, data, 0o644); err == nil && !o.Silent {
			stderr("[*] 💾 结果已保存到: %s\n", o.Output)
		}
	} else {
		printJSON(payload)
	}
	if !o.Silent {
		stderr("\n[*] 📊 完成统计:\n")
		stderr("[*]   总计: %d\n", len(results))
		stderr("[*]   成功: %d ✅\n", success)
		stderr("[*]   失败: %d ❌\n", len(results)-success)
	}
	return 0
}

func hostOf(u string) string {
	s := u
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}
