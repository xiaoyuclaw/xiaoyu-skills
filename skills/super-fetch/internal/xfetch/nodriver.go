package xfetch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// FindSystemChrome 找系统真实 Chrome（nodriver 通道用它，而不是 Playwright 的 Chromium）。
func FindSystemChrome() string {
	candidates := []string{
		os.Getenv("CHROME_PATH"),
		"/usr/bin/google-chrome",
		"/opt/google/chrome/chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	}
	for _, c := range candidates {
		if c != "" {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	return ""
}

// FetchWithNodriver 启动真实 Chrome（CDP 远程调试端口，不带自动化参数）抓一页。
func FetchWithNodriver(rawURL, sessionFile string, opts BrowserOptions) (string, string, error) {
	exe := FindSystemChrome()
	if exe == "" {
		return "", "", errors.New("找不到系统 Chrome（可用 CHROME_PATH 指定）")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30
	}
	profile, err := os.MkdirTemp("", "sf-nd-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(profile)

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
		return "", "", err
	}
	// 退出时连同子进程一起收掉，避免残留
	defer func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		_ = cmd.Wait()
	}()

	port, err := waitDevToolsPort(profile, 20*time.Second)
	if err != nil {
		return "", "", err
	}
	ws, err := wsEndpoint(port)
	if err != nil {
		return "", "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.Timeout+30)*time.Second)
	defer cancel()
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ctx, ws)
	defer allocCancel()
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()
	tabCtx, timeoutCancel := context.WithTimeout(tabCtx, time.Duration(opts.Timeout)*time.Second)
	defer timeoutCancel()

	applyNodriverSession(tabCtx, sessionFile)
	var html string
	actions := []chromedp.Action{
		chromedp.Navigate(rawURL),
		chromedp.Sleep(1200 * time.Millisecond),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	}
	if err := chromedp.Run(tabCtx, actions...); err != nil {
		return "", "", err
	}
	saveNodriverSession(tabCtx, sessionFile)
	return html, "text/html", nil
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

func applyNodriverSession(ctx context.Context, path string) {
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
		cp := &network.CookieParam{Name: c.Name, Value: c.Value, Domain: c.Domain,
			Path: c.Path, Secure: c.Secure, HTTPOnly: c.HTTPOnly}
		params = append(params, cp)
	}
	_ = chromedp.Run(ctx, network.SetCookies(params))
}

func saveNodriverSession(ctx context.Context, path string) {
	if path == "" {
		return
	}
	var cookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		cookies, err = network.GetCookies().Do(c)
		return err
	})); err != nil || len(cookies) == 0 {
		return
	}
	st := storageState{Cookies: make([]stateCookie, 0, len(cookies))}
	for _, c := range cookies {
		st.Cookies = append(st.Cookies, stateCookie{Name: c.Name, Value: c.Value,
			Domain: c.Domain, Path: c.Path, HTTPOnly: c.HTTPOnly, Secure: c.Secure,
			Expires: float64(c.Expires)})
	}
	if data, err := json.Marshal(st); err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}

func waitDevToolsPort(profile string, d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) > 0 && lines[0] != "" {
				return lines[0], nil
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	return "", errors.New("等待 Chrome 调试端口超时")
}

func wsEndpoint(port string) (string, error) {
	resp, err := http.Get("http://127.0.0.1:" + port + "/json/version")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(bufio.NewReader(resp.Body)).Decode(&v); err != nil {
		return "", err
	}
	if v.WS == "" {
		return "", fmt.Errorf("拿不到 webSocketDebuggerUrl")
	}
	return v.WS, nil
}
