package xfetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"superfetch/internal/xcore"
	"superfetch/internal/xparse"
)

// BrowserOptions 浏览器通道参数。
type BrowserOptions struct {
	Headless      bool
	Show          bool
	Proxy         string
	Timeout       int
	Wait          int
	Interactive   bool
	Width         int
	Height        int
	ExecPath      string
	WaitSelector  string
	Stable        bool
	ForceChromedp bool
	NoDaemon      bool
}

// Browser 一个可复用的浏览器实例。
type Browser struct {
	kind        string // gomoufox | nodriver | chromedp
	allocCtx    context.Context
	allocStop   context.CancelFunc
	browserCtx  context.Context
	browserStop context.CancelFunc
	opts        BrowserOptions
	useGomoufox bool
	startOnce   sync.Once
	startErr    error
}

// FindChrome 依次找 CHROME_PATH、Playwright 缓存、系统 Chrome/Chromium。
func FindChrome() string {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	patterns := []string{
		filepath.Join(home, ".cache/ms-playwright/chromium-*/chrome-linux/chrome"),
		filepath.Join(home, ".cache/ms-playwright/chromium-*/chrome-linux64/chrome"),
		filepath.Join(home, ".cache/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell"),
	}
	var found []string
	for _, pat := range patterns {
		m, _ := filepath.Glob(pat)
		found = append(found, m...)
	}
	if len(found) > 0 {
		sort.Strings(found)
		return found[len(found)-1]
	}
	for _, cand := range []string{
		"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable",
		"/usr/bin/chromium", "/usr/bin/chromium-browser",
		"/snap/bin/chromium",
	} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	if p, err := exec.LookPath("google-chrome"); err == nil {
		return p
	}
	if p, err := exec.LookPath("chromium"); err == nil {
		return p
	}
	return ""
}

func ensureDisplay() {
	if os.Getenv("DISPLAY") == "" {
		if _, err := os.Stat("/tmp/.X11-unix/X0"); err == nil {
			os.Setenv("DISPLAY", ":0")
		}
	}
}

// LaunchBrowser 启动浏览器（永不 headless，除非显式要求）。
func LaunchBrowser(opts BrowserOptions) (*Browser, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 30
	}
	// gomoufox / nodriver：不需要常驻 chromedp 分配器，按需启动
	if !opts.Interactive && !opts.ForceChromedp {
		if kind := BackendKind(); kind != "chromedp" {
			return &Browser{opts: opts, kind: kind, useGomoufox: kind == "gomoufox"}, nil
		}
	}
	return newChromedpBrowser(opts)
}

// fallbackToChromedp Camoufox 起不来时切换到 Chromium 通道。
func (b *Browser) fallbackToChromedp() error {
	nb, err := newChromedpBrowser(b.opts)
	if err != nil {
		return err
	}
	b.allocCtx, b.allocStop = nb.allocCtx, nb.allocStop
	b.browserCtx, b.browserStop = nb.browserCtx, nb.browserStop
	b.useGomoufox = false
	b.kind = "chromedp"
	return nil
}

// BackendKind 返回当前浏览器后端：gomoufox（默认）| nodriver | chromedp。
func BackendKind() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SF_BROWSER"))) {
	case "nodriver":
		return "nodriver"
	case "chromium", "chromedp", "patchright", "playwright":
		return "chromedp"
	case "gomoufox", "camoufox":
		return "gomoufox"
	}
	return "nodriver"
}

func newChromedpBrowser(opts BrowserOptions) (*Browser, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 30
	}
	if opts.Width == 0 {
		opts.Width = 1280
	}
	if opts.Height == 0 {
		opts.Height = 900
	}
	exe := opts.ExecPath
	if exe == "" {
		exe = FindChrome()
	}
	if exe == "" {
		return nil, errors.New("找不到 Chrome/Chromium，可用 CHROME_PATH 指定")
	}
	ensureDisplay()

	pos := "-2400,-2400"
	if opts.Show || opts.Interactive {
		pos = "120,80"
	}

	profile, _ := os.MkdirTemp("", "sf-cd-")
	flags := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(exe),
		chromedp.UserDataDir(profile),
		chromedp.Flag("headless", opts.Headless),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("window-position", pos),
		chromedp.Flag("window-size", fmt.Sprintf("%d,%d", opts.Width, opts.Height)),
		chromedp.Flag("lang", "zh-CN"),
	}
	if opts.Proxy != "" {
		flags = append(flags, chromedp.ProxyServer(opts.Proxy))
	}

	allocCtx, allocStop := chromedp.NewExecAllocator(context.Background(), flags...)
	browserCtx, browserStop := chromedp.NewContext(allocCtx)
	return &Browser{
		allocCtx: allocCtx, allocStop: allocStop,
		browserCtx: browserCtx, browserStop: browserStop,
		opts: opts,
	}, nil
}

// SetWaitSelector 设置导航后等待出现的 CSS 选择器。
// ensureStarted 懒启动：真正要用浏览器时才拉起进程（daemon 命中就不必启动）。
func (b *Browser) ensureStarted() error {
	b.startOnce.Do(func() {
		if b.browserCtx == nil {
			b.startErr = errors.New("no browser context")
			return
		}
		b.startErr = chromedp.Run(b.browserCtx)
	})
	return b.startErr
}

func (b *Browser) SetWaitSelector(sel string) { b.opts.WaitSelector = sel }

// SetStable 设置是否等正文长度稳定后再取内容。
func (b *Browser) SetStable(v bool) { b.opts.Stable = v }

// Close 关闭浏览器。
func (b *Browser) Close() {
	if b == nil {
		return
	}
	if b.browserStop != nil {
		b.browserStop()
	}
	if b.allocStop != nil {
		b.allocStop()
	}
}

// NewTab 开一个新标签页并注入 stealth（供其它命令复用）。
func (b *Browser) NewTab() (context.Context, context.CancelFunc, error) { return b.newTab() }

// newTab 开一个新标签页并注入 stealth。
func (b *Browser) newTab() (context.Context, context.CancelFunc, error) {
	if err := b.ensureStarted(); err != nil {
		return nil, nil, fmt.Errorf("浏览器启动失败: %w", err)
	}
	tabCtx, cancel := chromedp.NewContext(b.browserCtx)
	if err := chromedp.Run(tabCtx); err != nil {
		cancel()
		return nil, nil, err
	}
	// 每个新文档都注入反检测脚本
	err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(xcore.StealthJS).Do(ctx)
		return err
	}))
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return tabCtx, cancel, nil
}

func (b *Browser) applySession(timeoutCtx context.Context, sessionFile string) {
	if sessionFile == "" {
		return
	}
	state, err := LoadStorageState(sessionFile)
	if err != nil || len(state.Cookies) == 0 && len(state.Origins) == 0 {
		return
	}
	_ = chromedp.Run(timeoutCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		for _, ck := range state.Cookies {
			p := network.SetCookie(ck.Name, ck.Value).WithPath(ck.Path)
			if ck.Domain != "" {
				p = p.WithDomain(ck.Domain)
			}
			if ck.Expires > 0 {
				exp := cdp.TimeSinceEpoch(time.Unix(int64(ck.Expires), 0))
				p = p.WithExpires(&exp)
			}
			_ = p.Do(ctx)
		}
		for _, o := range state.Origins {
			if len(o.LocalStorage) == 0 {
				continue
			}
			script := buildLocalStorageScript(o)
			_, _ = page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
		}
		return nil
	}))
}

func buildLocalStorageScript(o Origin) string {
	items, _ := json.Marshal(o.LocalStorage)
	return fmt.Sprintf(`(() => {
  if (location.origin !== %q) return;
  const items = %s;
  for (const it of items) { try { localStorage.setItem(it.name, it.value); } catch (e) {} }
})();`, o.Origin, string(items))
}

// FetchWithBrowser 用浏览器抓页面，返回 HTML 与 contentType。
func (b *Browser) FetchWithBrowser(rawURL, sessionFile string) (string, string, error) {
	if !b.useGomoufox && b.kind == "chromedp" {
		if err := b.ensureStarted(); err != nil {
			return "", "", err
		}
	}
	// nodriver/chromedp 的 -s 先走进程内（daemon 里 CDP cookie 读写不稳）；
	// gomoufox 的 session 在 daemon 里可靠，保持热启动
	if !b.useGomoufox && !b.opts.NoDaemon {
		if html, ok := FetchViaDaemon(b.kind, rawURL, sessionFile, b.opts.WaitSelector, b.opts.Timeout, 1.0); ok {
			return html, "text/html", nil
		}
	}
	if b.kind == "nodriver" {
		html, ctype, err := FetchWithNodriver(rawURL, sessionFile, b.opts)
		if err == nil {
			return html, ctype, nil
		}
		fmt.Fprintf(os.Stderr, "[*] ⚠️ nodriver 失败，回退 Chromium: %v\n", err)
		if err := b.fallbackToChromedp(); err != nil {
			return "", "", err
		}
	}
	if b.useGomoufox {
		html, ctype, err := FetchWithGomoufox(rawURL, sessionFile, b.opts)
		if err == nil {
			return html, ctype, nil
		}
		// Camoufox 失败（运行时缺失/协议错误）就回退 chromedp 一次
		if os.Getenv("SF_BROWSER") != "" {
			return "", "", err
		}
		fmt.Fprintf(os.Stderr, "[*] ⚠️ Camoufox 失败，回退 Chromium: %v\n", err)
		if err := b.fallbackToChromedp(); err != nil {
			return "", "", err
		}
	}
	timeoutCtx, stop, err := b.newTab()
	if err != nil {
		return "", "", err
	}
	defer stop()

	b.applySession(timeoutCtx, sessionFile)

	if b.opts.Interactive {
		return b.interactiveFetch(timeoutCtx, rawURL, sessionFile)
	}

	var html string
	err = chromedp.Run(timeoutCtx,
		chromedp.Navigate(rawURL),
		chromedp.Sleep(time.Duration(maxInt(b.opts.Wait, 1))*time.Second),
	)
	if err != nil {
		// 导航超时不致命，尽量取当前内容
		fmt.Fprintf(os.Stderr, "[*] 页面加载超时或部分失败（继续）: %.80s\n", err.Error())
	}
	if b.opts.WaitSelector != "" {
		b.waitSelector(timeoutCtx, b.opts.WaitSelector, 4*time.Second)
	}
	if b.opts.Stable {
		waitStable(timeoutCtx, 1*time.Second, 5*time.Second)
	}
	b.waitRendered(timeoutCtx)
	if err := chromedp.Run(timeoutCtx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)); err != nil {
		return "", "", err
	}
	var ctype string
	_ = chromedp.Run(timeoutCtx, chromedp.Evaluate(`document.contentType || 'text/html'`, &ctype))
	if ctype == "" {
		ctype = "text/html"
	}
	if sessionFile != "" {
		_ = b.SaveSession(sessionFile, timeoutCtx)
	}
	return html, ctype, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// waitSelector 等选择器出现（最多 d 秒，超时不致命）。
func (b *Browser) waitSelector(ctx context.Context, sel string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		var ok bool
		js := fmt.Sprintf("!!document.querySelector(%q)", sel)
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &ok)); err != nil {
			return
		}
		if ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitStable 等正文长度稳定下来，避免页面还没渲染就抓走。
func waitStable(ctx context.Context, minWait, maxWait time.Duration) {
	start := time.Now()
	time.Sleep(minWait)
	last := -1
	stable := 0
	for time.Since(start) < maxWait {
		var n int
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`document.body ? document.body.innerText.length : 0`, &n)); err != nil {
			return
		}
		if n == last && n > 0 {
			stable++
			if stable >= 2 {
				return
			}
		} else {
			stable = 0
			last = n
		}
		time.Sleep(350 * time.Millisecond)
	}
}

// waitRendered 骨架屏兜底：正文还是一排 "word" 就多等几轮。
func (b *Browser) waitRendered(timeoutCtx context.Context) {
	for i := 0; i < 3; i++ {
		var txt string
		if err := chromedp.Run(timeoutCtx, chromedp.Evaluate(
			`document.body ? document.body.innerText.slice(0, 20000) : ''`, &txt)); err != nil {
			return
		}
		if !xparse.LooksLikeSkeleton(txt) {
			return
		}
		time.Sleep(3 * time.Second)
	}
}

const interactiveButtonJS = `
(() => {
    function createButton() {
        if (document.getElementById('_claw_btn')) return;
        const btn = document.createElement('div');
        btn.id = '_claw_btn';
        btn.innerHTML = '✅ 完成操作，点击抓取内容';
        Object.assign(btn.style, {
            position: 'fixed', zIndex: '2147483647', padding: '12px 24px',
            background: '#00c853', color: 'white', borderRadius: '30px',
            cursor: 'pointer', boxShadow: '0 4px 15px rgba(0,0,0,0.5)',
            top: '20px', right: '20px', fontWeight: 'bold',
            userSelect: 'none', fontFamily: 'sans-serif',
            display: 'block', visibility: 'visible', opacity: '1'
        });
        let isDragging = false, hasMoved = false, startX, startY, initX, initY;
        btn.addEventListener('mousedown', (e) => {
            isDragging = true; hasMoved = false;
            startX = e.clientX; startY = e.clientY;
            initX = btn.offsetLeft; initY = btn.offsetTop;
        });
        window.addEventListener('mousemove', (e) => {
            if (!isDragging) return;
            const dx = e.clientX - startX, dy = e.clientY - startY;
            if (Math.abs(dx) > 5 || Math.abs(dy) > 5) hasMoved = true;
            btn.style.left = (initX + dx) + 'px';
            btn.style.top = (initY + dy) + 'px';
            btn.style.right = 'auto';
        });
        window.addEventListener('mouseup', () => { isDragging = false; });
        btn.addEventListener('click', (e) => {
            if (hasMoved) return;
            window._fetch_interactive_done = true;
            btn.style.background = '#ff9100';
            btn.innerHTML = '⏳ 正在同步并解析...';
        });
        (document.body || document.documentElement).appendChild(btn);
    }
    createButton();
    const observer = new MutationObserver(() => {
        if (!document.getElementById('_claw_btn')) createButton();
    });
    observer.observe(document.documentElement, { childList: true, subtree: true });
    setInterval(createButton, 1000);
})();
`

func (b *Browser) interactiveFetch(timeoutCtx context.Context, rawURL, sessionFile string) (string, string, error) {
	// 预注入按钮
	_ = chromedp.Run(timeoutCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(interactiveButtonJS).Do(ctx)
		return err
	}))
	if err := chromedp.Run(timeoutCtx, chromedp.Navigate(rawURL)); err != nil {
		fmt.Fprintf(os.Stderr, "[*] 页面加载超时或部分失败（继续）: %.80s\n", err.Error())
	}
	_ = chromedp.Run(timeoutCtx, chromedp.Evaluate(interactiveButtonJS, nil))
	fmt.Fprintln(os.Stderr, "[*] 浏览器已打开，请在页面完成操作（如登录、过验证码）后，点击页面右上角的绿色按钮。")

	// 附加到该标签的上下文不能随手 cancel —— chromedp 里 cancel 会关掉对应标签页。
	// 每个 target 只附加一次，保留到整个流程结束。
	type tabHandle struct {
		ctx    context.Context
		cancel context.CancelFunc
	}
	attached := map[string]*tabHandle{}
	defer func() {
		for _, h := range attached {
			h.cancel()
		}
	}()

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		ids := b.pageTargetIDs(timeoutCtx)
		if len(ids) == 0 {
			fmt.Fprintln(os.Stderr, "[*] 浏览器已被关闭，尝试保存会话...")
			_ = b.SaveSession(sessionFile, timeoutCtx)
			return "", "text/html", nil
		}
		for _, id := range ids {
			h, ok := attached[id]
			if !ok {
				tctx, cancel := chromedp.NewContext(b.browserCtx, chromedp.WithTargetID(target.ID(id)))
				h = &tabHandle{ctx: tctx, cancel: cancel}
				attached[id] = h
			}
			var done bool
			if err := chromedp.Run(h.ctx, chromedp.Evaluate(`window._fetch_interactive_done === true`, &done)); err == nil && done {
				var html string
				if e := chromedp.Run(h.ctx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)); e == nil {
					_ = b.SaveSession(sessionFile, h.ctx)
					return html, "text/html", nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	// 超时：取当前页内容并保存会话
	var html string
	_ = chromedp.Run(timeoutCtx, chromedp.OuterHTML("html", &html, chromedp.ByQuery))
	_ = b.SaveSession(sessionFile, timeoutCtx)
	return html, "text/html", nil
}

func (b *Browser) pageTargetIDs(tab context.Context) []string {
	var infos []*target.Info
	ctx, cancel := context.WithTimeout(tab, 5*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		infos, err = target.GetTargets().Do(c)
		return err
	})); err != nil {
		return nil
	}
	var out []string
	for _, t := range infos {
		if t.Type == "page" && !strings.HasPrefix(t.URL, "devtools://") {
			out = append(out, string(t.TargetID))
		}
	}
	return out
}

// SaveSession 把当前 Cookie 与 localStorage 存成 Playwright storageState。
func (b *Browser) SaveSession(sessionFile string, ctx context.Context) error {
	if sessionFile == "" {
		return nil
	}
	state := &StorageState{Cookies: []Cookie{}, Origins: []Origin{}}
	var cookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		cookies, err = network.GetCookies().Do(c)
		return err
	})); err != nil {
		return err
	}
	for _, ck := range cookies {
		state.Cookies = append(state.Cookies, Cookie{
			Name: ck.Name, Value: ck.Value, Domain: ck.Domain, Path: ck.Path,
			Expires: ck.Expires, HTTPOnly: ck.HTTPOnly, Secure: ck.Secure,
			SameSite: strings.Title(string(ck.SameSite)), //nolint:staticcheck
		})
	}
	// 当前页面的 localStorage
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`JSON.stringify({origin: location.origin, items: Object.keys(localStorage).map(k => ({name: k, value: localStorage.getItem(k)}))})`,
		&raw)); err == nil && raw != "" {
		var parsed struct {
			Origin string             `json:"origin"`
			Items  []LocalStorageItem `json:"items"`
		}
		if json.Unmarshal([]byte(raw), &parsed) == nil && parsed.Origin != "" && len(parsed.Items) > 0 {
			state.Origins = append(state.Origins, Origin{Origin: parsed.Origin, LocalStorage: parsed.Items})
		}
	}
	return SaveStorageState(sessionFile, state)
}
