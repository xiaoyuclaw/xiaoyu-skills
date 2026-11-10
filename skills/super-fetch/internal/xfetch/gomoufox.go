package xfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
)

// GomoufoxEnabled 判断是否走 Camoufox（gomoufox）通道。
//
//	SF_BROWSER=gomoufox  强制走
//	SF_BROWSER=chromium  强制走 chromedp
//	未设置              装了 gomoufox 运行时就用，否则回退 chromedp
func GomoufoxEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SF_BROWSER"))) {
	case "gomoufox", "camoufox":
		return true
	case "chromium", "chromedp", "patchright", "playwright":
		return false
	}
	return gomoufoxInstalled()
}

func gomoufoxInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	hits, _ := filepath.Glob(filepath.Join(home, ".cache/gomoufox/venv/runtime/*/*/linux-amd64/manifest.json"))
	return len(hits) > 0
}

// loadStorageState 读 Playwright storageState JSON（字段和 gomoufox.Cookie 完全一致）。
func loadStorageState(path string) *gomoufox.StorageState {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var st gomoufox.StorageState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	return &st
}

// FetchWithGomoufox 用 Camoufox（Firefox 内核）抓一个页面。
func FetchWithGomoufox(rawURL, sessionFile string, opts BrowserOptions) (string, string, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 30
	}
	if opts.Width == 0 {
		opts.Width = 1280
	}
	if opts.Height == 0 {
		opts.Height = 900
	}

	// 优先走常驻 daemon：浏览器只启动一次
	if html, ok := FetchViaDaemon("gomoufox", rawURL, sessionFile, opts.WaitSelector, opts.Timeout, 1.5); ok {
		return html, "text/html", nil
	}

	// 浏览器冷启动 3–11 秒，超时给足
	bootCtx, bootCancel := context.WithTimeout(context.Background(), time.Duration(opts.Timeout+90)*time.Second)
	defer bootCancel()

	debugTiming := os.Getenv("SF_DEBUG_TIME") == "1"
	mark := time.Now()
	tick := func(name string) {
		if debugTiming {
			fmt.Fprintf(os.Stderr, "[time] %-14s %5.2fs\n", name, time.Since(mark).Seconds())
			mark = time.Now()
		}
	}
	var newOpts []gomoufox.Option
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SF_HEADLESS"))) {
	case "false":
		newOpts = append(newOpts, gomoufox.WithHeadless(camoufoxcfg.HeadlessFalse))
	case "true":
		newOpts = append(newOpts, gomoufox.WithHeadless(camoufoxcfg.HeadlessTrue))
	default:
		newOpts = append(newOpts, gomoufox.WithHeadless(camoufoxcfg.HeadlessVirtual))
	}
	b, err := gomoufox.New(bootCtx, newOpts...)
	if err != nil {
		return "", "", fmt.Errorf("gomoufox 启动失败: %w", err)
	}
	defer b.Close()
	tick("浏览器启动")

	copts := []gomoufox.ContextOption{
		gomoufox.WithViewport(opts.Width, 900),
		gomoufox.WithContextLocale("zh-CN"),
		gomoufox.WithTimezoneID("Asia/Shanghai"),
	}
	if st := loadStorageState(sessionFile); st != nil {
		normalizeStorageState(st)
		copts = append(copts, gomoufox.WithStorageState(st))
	}
	page, err := b.NewPage(bootCtx, copts...)
	if err != nil {
		return "", "", fmt.Errorf("gomoufox 新建页面失败: %w", err)
	}
	defer page.Close()
	tick("new_page")

	if _, err := page.Goto(bootCtx, rawURL,
		gomoufox.WaitUntil("domcontentloaded"),
		gomoufox.WithTimeout(time.Duration(opts.Timeout)*time.Second),
	); err != nil {
		return "", "", err
	}

	tick("goto")
	if opts.WaitSelector != "" {
		waitSelectorGomoufox(bootCtx, page, opts.WaitSelector, 6*time.Second)
	}
	if opts.Stable {
		time.Sleep(600 * time.Millisecond)
	}

	tick("等待选择器")
	html, err := page.Content(bootCtx)
	if err != nil {
		return "", "", err
	}

	if sessionFile != "" {
		if cookies, err := page.Cookies(bootCtx); err == nil && len(cookies) > 0 {
			st := gomoufox.StorageState{Cookies: cookies}
			if data, err := json.Marshal(st); err == nil {
				_ = os.WriteFile(sessionFile, data, 0o600)
			}
		}
	}
	return html, "text/html", nil
}

// normalizeStorageState 把 sameSite 规范成合法值（Playwright 导出的空串会被 gomoufox 拒绝）。
func normalizeStorageState(st *gomoufox.StorageState) {
	for i := range st.Cookies {
		switch strings.ToLower(st.Cookies[i].SameSite) {
		case "strict":
			st.Cookies[i].SameSite = "Strict"
		case "lax":
			st.Cookies[i].SameSite = "Lax"
		case "none", "no_restriction":
			st.Cookies[i].SameSite = "None"
		default:
			st.Cookies[i].SameSite = "Lax"
		}
	}
}

func waitSelectorGomoufox(ctx context.Context, page *gomoufox.Page, sel string, d time.Duration) {
	deadline := time.Now().Add(d)
	expr := "!!document.querySelector(" + strconv.Quote(sel) + ")"
	for time.Now().Before(deadline) {
		if v, err := page.Evaluate(ctx, expr); err == nil {
			if ok, _ := v.(bool); ok {
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
}
