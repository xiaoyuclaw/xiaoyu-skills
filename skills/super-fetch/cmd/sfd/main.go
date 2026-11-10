// sfd 是常驻浏览器 daemon：一个后端一个进程，sf 通过 unix socket 复用。
//
//	sfd --backend gomoufox|nodriver|chromedp [--sock PATH] [--dsh-pid PID] [--idle SEC]
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
)

type request struct {
	URL      string  `json:"url"`
	Session  string  `json:"session"`
	Timeout  int     `json:"timeout"`
	Wait     float64 `json:"wait"`
	Selector string  `json:"selector"`
}

type response struct {
	HTML  string `json:"html"`
	Error string `json:"error"`
}

// reapStaleBrowsers 清掉上次异常退出留下的浏览器进程（按 profile 前缀认领）。
func reapStaleBrowsers() {
	entries, err := os.ReadDir("/tmp")
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "sf-nd-") && !strings.HasPrefix(name, "sf-cd-") {
			continue
		}
		out, _ := exec.Command("pgrep", "-f", "--", "/tmp/"+name).Output()
		for _, f := range strings.Fields(string(out)) {
			if pid, err := strconv.Atoi(f); err == nil && pid != os.Getpid() {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		_ = os.RemoveAll("/tmp/" + name)
	}
}

func logf(format string, a ...any) { fmt.Fprintf(os.Stderr, "[sfd] "+format+"\n", a...) }

func dshAlive(pid int) bool {
	if pid <= 0 {
		return true
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func headlessMode() camoufoxcfg.HeadlessMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SF_HEADLESS"))) {
	case "false":
		return camoufoxcfg.HeadlessFalse
	case "true":
		return camoufoxcfg.HeadlessTrue
	}
	return camoufoxcfg.HeadlessVirtual
}

func main() {
	sock := flag.String("sock", defaultSock("gomoufox"), "unix socket path")
	backend := flag.String("backend", "gomoufox", "gomoufox|nodriver|chromedp")
	dshPID := flag.Int("dsh-pid", 0, "dsh host pid; daemon exits when it dies")
	idle := flag.Int("idle", 900, "idle timeout seconds")
	flag.Parse()

	lockPath := *sock + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		logf("打开锁失败: %v", err)
		return
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		logf("已有 daemon 在跑，退出")
		return
	}
	defer lf.Close()

	_ = os.WriteFile(*sock+".pid", []byte(fmt.Sprintf("%d", os.Getpid())), 0o600)
	defer os.Remove(*sock)
	defer os.Remove(*sock + ".pid")
	defer os.Remove(*sock + ".lock")

	ctx := context.Background()
	var handler func(request) (string, error)
	var cleanup func()

	// 先清上次的残留，再启动新浏览器（顺序反了会把刚启动的浏览器一起杀掉）
	reapStaleBrowsers()

	// 先把浏览器启动好（慢的 gomoufox 要 30s+），再对外监听
	switch *backend {
	case "gomoufox":
		handler, cleanup = newGomoufox(ctx)
	case "nodriver":
		handler, cleanup = newNodriver(ctx)
	default:
		handler, cleanup = newChromedp(ctx)
	}
	defer cleanup()
	if handler == nil {
		logf("后端启动失败")
		os.Remove(*sock + ".pid")
		return
	}
	logf("%s 浏览器已常驻", *backend)

	_ = os.Remove(*sock)
	ln, err := net.Listen("unix", *sock)
	if err != nil {
		logf("监听失败: %v", err)
		return
	}
	_ = os.Chmod(*sock, 0o600)
	logf("监听 %s（backend=%s, dsh_pid=%d, idle=%ds）", *sock, *backend, *dshPID, *idle)

	shutdown := func(reason string) {
		logf("%s，关闭", reason)
		cleanup()
		os.Remove(*sock)
		os.Remove(*sock + ".pid")
		os.Remove(*sock + ".lock")
		os.Exit(0)
	}

	// SIGTERM/SIGINT：先关浏览器再退出（客户端切换后端时会发 SIGTERM）
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigc
		shutdown("收到退出信号")
	}()

	lastUsed := time.Now()
	go func() {
		for {
			time.Sleep(10 * time.Second)
			if !dshAlive(*dshPID) {
				shutdown("DSH 已退出")
			}
			if int(time.Since(lastUsed).Seconds()) > *idle {
				shutdown("空闲超时")
			}
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		lastUsed = time.Now()
		go func(c net.Conn) {
			defer c.Close()
			line, err := bufio.NewReader(c).ReadBytes('\n')
			if err != nil {
				return
			}
			var req request
			if json.Unmarshal(line, &req) != nil {
				writeJSON(c, response{Error: "bad request"})
				return
			}
			logf("请求 url=%s session=%q selector=%q", req.URL, req.Session, req.Selector)
			html, err := handler(req)
			logf("结果 err=%v len=%d", err, len(html))
			if err != nil {
				writeJSON(c, response{Error: err.Error()})
			} else {
				writeJSON(c, response{HTML: html})
			}
			lastUsed = time.Now()
		}(conn)
	}
}

// ---------- gomoufox ----------

func newGomoufox(ctx context.Context) (func(request) (string, error), func()) {
	b, err := gomoufox.New(ctx, gomoufox.WithHeadless(headlessMode()))
	if err != nil {
		logf("gomoufox 启动失败: %v", err)
		return nil, func() {}
	}
	fetch := func(req request) (string, error) {
		timeout := req.Timeout
		if timeout <= 0 {
			timeout = 30
		}
		opts := []gomoufox.ContextOption{
			gomoufox.WithViewport(1280, 900),
			gomoufox.WithContextLocale("zh-CN"),
			gomoufox.WithTimezoneID("Asia/Shanghai"),
		}
		if req.Session != "" {
			if data, err := os.ReadFile(req.Session); err == nil {
				var st gomoufox.StorageState
				if json.Unmarshal(data, &st) == nil {
					normalizeCookies(&st)
					opts = append(opts, gomoufox.WithStorageState(&st))
				}
			}
		}
		page, err := b.NewPage(ctx, opts...)
		if err != nil {
			return "", err
		}
		defer page.Close()
		if _, err := page.Goto(ctx, req.URL, gomoufox.WaitUntil("domcontentloaded"),
			gomoufox.WithTimeout(time.Duration(timeout)*time.Second)); err != nil {
			return "", err
		}
		if req.Wait > 0 {
			time.Sleep(time.Duration(req.Wait * float64(time.Second)))
		}
		html, err := page.Content(ctx)
		if err == nil && req.Session != "" {
			if cookies, cerr := page.Cookies(ctx); cerr == nil && len(cookies) > 0 {
				st := gomoufox.StorageState{Cookies: cookies}
				if data, jerr := json.Marshal(st); jerr == nil {
					_ = os.WriteFile(req.Session, data, 0o600)
				}
			}
		}
		return html, err
	}
	return fetch, func() { b.Close() }
}

func normalizeCookies(st *gomoufox.StorageState) {
	for i := range st.Cookies {
		switch strings.ToLower(st.Cookies[i].SameSite) {
		case "strict":
			st.Cookies[i].SameSite = "Strict"
		case "none", "no_restriction":
			st.Cookies[i].SameSite = "None"
		default:
			st.Cookies[i].SameSite = "Lax"
		}
	}
}

// ---------- chromedp ----------

func newChromedp(ctx context.Context) (func(request) (string, error), func()) {
	exe := os.Getenv("CHROME_PATH")
	if exe == "" {
		home, _ := os.UserHomeDir()
		for _, pat := range []string{
			filepath.Join(home, ".cache/ms-playwright/chromium-*/chrome-linux/chrome"),
			filepath.Join(home, ".cache/ms-playwright/chromium-*/chrome-linux64/chrome"),
		} {
			if hits, _ := filepath.Glob(pat); len(hits) > 0 {
				exe = hits[0]
				break
			}
		}
	}
	profile, _ := os.MkdirTemp("", "sf-cd-")
	flags := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(exe),
		chromedp.UserDataDir(profile),
		chromedp.Flag("headless", os.Getenv("SF_HEADLESS") == "true"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("window-position", "-2400,-2400"),
		chromedp.Flag("window-size", "1280,900"),
		chromedp.Flag("lang", "zh-CN"),
	}
	allocCtx, allocStop := chromedp.NewExecAllocator(context.Background(), flags...)
	browserCtx, browserStop := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(browserCtx); err != nil {
		logf("chromedp 启动失败: %v", err)
		allocStop()
		return nil, func() {}
	}
	fetch := func(req request) (string, error) { return fetchTab(browserCtx, req) }
	return fetch, func() { browserStop(); allocStop(); os.RemoveAll(profile) }
}

// ---------- nodriver（系统 Chrome + 远程调试端口） ----------

func newNodriver(ctx context.Context) (func(request) (string, error), func()) {
	exe := findSystemChrome()
	if exe == "" {
		logf("找不到系统 Chrome")
		return nil, func() {}
	}
	profile, err := os.MkdirTemp("", "sf-nd-")
	if err != nil {
		return nil, func() {}
	}
	args := []string{
		"--remote-debugging-port=0",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-blink-features=AutomationControlled",
		"--disable-dev-shm-usage",
		"--no-sandbox",
		"--window-position=-32000,-32000",
		"--window-size=1280,900",
		"--ozone-platform=x11",
		"--lang=zh-CN",
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logf("Chrome 启动失败: %v", err)
		return nil, func() {}
	}
	kill := func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		_ = cmd.Wait()
		os.RemoveAll(profile)
	}

	port, err := waitPort(profile, 20*time.Second)
	if err != nil {
		logf("等调试端口失败: %v", err)
		kill()
		return nil, func() {}
	}
	ws, err := wsURL(port)
	if err != nil {
		logf("取 ws 地址失败: %v", err)
		kill()
		return nil, func() {}
	}
	allocCtx, allocStop := chromedp.NewRemoteAllocator(context.Background(), ws)
	browserCtx, browserStop := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(browserCtx); err != nil {
		logf("连接 Chrome 失败: %v", err)
		allocStop()
		kill()
		return nil, func() {}
	}
	fetch := func(req request) (string, error) { return fetchTab(browserCtx, req) }
	return fetch, func() { browserStop(); allocStop(); kill() }
}

type stateCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

type storageState struct {
	Cookies []stateCookie    `json:"cookies"`
	Origins []map[string]any `json:"origins"`
}

func applySession(ctx context.Context, path string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var st storageState
	if json.Unmarshal(data, &st) != nil || len(st.Cookies) == 0 {
		return
	}
	params := make([]*network.CookieParam, 0, len(st.Cookies))
	for _, c := range st.Cookies {
		cp := &network.CookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
		}
		if c.Expires > 0 {
			v := cdp.TimeSinceEpoch(time.Unix(int64(c.Expires), 0))
			cp.Expires = &v
		}
		params = append(params, cp)
	}
	_ = chromedp.Run(ctx, network.SetCookies(params))
}

func saveSession(ctx context.Context, path string) {
	if path == "" {
		logf("saveSession: 没有 session 路径")
		return
	}
	var cookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		cookies, err = network.GetCookies().Do(c)
		return err
	})); err != nil || len(cookies) == 0 {
		logf("saveSession: 取 cookie 失败 err=%v count=%d", err, len(cookies))
		return
	}
	st := storageState{Cookies: make([]stateCookie, 0, len(cookies))}
	for _, c := range cookies {
		sc := stateCookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
			HTTPOnly: c.HTTPOnly, Secure: c.Secure}
		if c.Expires > 0 {
			sc.Expires = float64(c.Expires)
		}
		st.Cookies = append(st.Cookies, sc)
	}
	if data, err := json.Marshal(st); err == nil {
		if werr := os.WriteFile(path, data, 0o600); werr != nil {
			logf("saveSession: 写文件失败 %v", werr)
		}
	}
}

func fetchTab(browserCtx context.Context, req request) (string, error) {
	tabCtx, cancelTab := chromedp.NewContext(browserCtx)
	defer cancelTab()
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	reqCtx, cancel := context.WithTimeout(tabCtx, time.Duration(timeout+15)*time.Second)
	defer cancel()

	applySession(reqCtx, req.Session)

	// 导航给 15s：拿不到 load 事件也继续（bing 这类站会一直挂着），后面靠 selector/wait 兜
	navCtx, navCancel := context.WithTimeout(reqCtx, 15*time.Second)
	defer navCancel() // 不能立即 cancel：会让标签页脱离目标，后续 Evaluate 卡死
	navErr := chromedp.Run(navCtx, chromedp.Navigate(req.URL))
	if navErr != nil {
		logf("导航未完成（继续）: %v", navErr)
	}
	if req.Wait > 0 {
		time.Sleep(time.Duration(req.Wait * float64(time.Second)))
	}
	waitForSelector(reqCtx, req.Selector, 6*time.Second)

	var html string
	err := chromedp.Run(reqCtx, chromedp.Evaluate(
		"document.documentElement ? document.documentElement.outerHTML : ''", &html))
	if err == nil && req.Session != "" {
		saveSession(reqCtx, req.Session)
	}
	return html, err
}

func findSystemChrome() string {
	for _, c := range []string{
		os.Getenv("CHROME_PATH"),
		"/usr/bin/google-chrome",
		"/opt/google/chrome/chrome",
		"/usr/bin/google-chrome-stable",
	} {
		if c != "" {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	return ""
}

func waitPort(profile string, d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort")); err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) > 0 && lines[0] != "" {
				return lines[0], nil
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	return "", fmt.Errorf("等待调试端口超时")
}

func wsURL(port string) (string, error) {
	resp, err := http.Get("http://127.0.0.1:" + port + "/json/version")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	if v.WS == "" {
		return "", fmt.Errorf("没有 webSocketDebuggerUrl")
	}
	return v.WS, nil
}

func waitForSelector(ctx context.Context, sel string, d time.Duration) {
	if sel == "" {
		return
	}
	expr := fmt.Sprintf("!!document.querySelector(%q)", sel)
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok)); err == nil && ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func writeJSON(c net.Conn, v response) {
	data, _ := json.Marshal(v)
	_, _ = c.Write(append(data, '\n'))
}

func defaultSock(backend string) string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, fmt.Sprintf("sf-%s-%d.sock", backend, os.Getuid()))
}
