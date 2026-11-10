// Package xfetch 提供两个抓取通道：TLS 指纹 HTTP 与真实浏览器。
package xfetch

import (
	"bytes"
	"fmt"
	http_cookiejar "github.com/bogdanfinn/fhttp/cookiejar"
	"io"
	"net/url"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"golang.org/x/net/html/charset"
	"unicode/utf8"

	"superfetch/internal/xcore"
)

// BinaryPrefixes 这些内容类型不解析，直接返回字节。
var BinaryPrefixes = []string{
	"application/pdf", "image/", "audio/", "video/", "application/zip",
}

// IsBinaryType 判断 content-type 是否二进制。
func IsBinaryType(ct string) bool {
	ct = strings.ToLower(ct)
	for _, p := range BinaryPrefixes {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	return false
}

// HTTPError HTTP 状态码错误。404/401 这类是确定性失败，调用方不该再开浏览器重试。
type HTTPError struct {
	Status int
	URL    string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d", e.Status) }

// Response 一次抓取的原始结果。
type Response struct {
	Body        []byte
	ContentType string
	IsBinary    bool
	Status      int
}

// Text 按响应头/页面 meta 的字符集解码正文。
func (r *Response) Text() string {
	if !r.IsBinary {
		// 响应头/meta 都没写 charset 时，charset.NewReader 会退到 windows-1252，
		// UTF-8 页面就会变成 "æœ" 这种乱码；先按 UTF-8 试一次。
		declared := strings.Contains(strings.ToLower(r.ContentType), "charset=")
		if !declared && utf8.Valid(r.Body) {
			return string(r.Body)
		}
		reader, err := charset.NewReader(bytes.NewReader(r.Body), r.ContentType)
		if err == nil {
			if b, err := io.ReadAll(reader); err == nil {
				return string(b)
			}
		}
	}
	return string(r.Body)
}

// Client 复用的 HTTP 客户端（对应 Python 的 AsyncSession）。
type Client struct {
	http    tls_client.HttpClient
	proxy   string
	timeout int
}

// NewClient 创建一个带 Chrome 指纹的客户端。
func NewClient(proxy string, timeout int) (*Client, error) {
	if timeout <= 0 {
		timeout = 30
	}
	jar, _ := http_cookiejar.New(nil)
	opts := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(timeout),
		tls_client.WithClientProfile(profiles.Chrome_152),
		tls_client.WithCookieJar(jar),
		tls_client.WithRandomTLSExtensionOrder(),
	}
	if proxy != "" {
		opts = append(opts, tls_client.WithProxyUrl(proxy))
	}
	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), opts...)
	if err != nil {
		return nil, err
	}
	return &Client{http: client, proxy: proxy, timeout: timeout}, nil
}

// Close 释放连接。
func (c *Client) Close() {
	if c != nil && c.http != nil {
		c.http.CloseIdleConnections()
	}
}

// SetSessionCookies 把会话文件里的 Cookie 注入到该域。
func (c *Client) SetSessionCookies(sessionFile, rawURL string) {
	if sessionFile == "" {
		return
	}
	state, err := LoadStorageState(sessionFile)
	if err != nil {
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	var cookies []*http.Cookie
	for _, ck := range state.Cookies {
		// 对齐 Python 版：只按名字取值，不校验 domain。
		cookies = append(cookies, &http.Cookie{Name: ck.Name, Value: ck.Value, Path: "/"})
	}
	if len(cookies) > 0 {
		c.http.SetCookies(u, cookies)
	}
}

// Fetch 抓一个 URL。sessionFile 非空时按其 Cookie 发送。
func (c *Client) Fetch(rawURL, sessionFile string) (*Response, error) {
	return c.FetchWithReferer(rawURL, sessionFile, "")
}

// FetchWithReferer 带 Referer 抓取（翻页需要）。
func (c *Client) FetchWithReferer(rawURL, sessionFile, referer string) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range xcore.RealHeaders {
		req.Header.Set(k, v)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	} else {
		req.Header.Set("Referer", fmt.Sprintf("%s://%s/", u.Scheme, u.Host))
	}
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Priority", "u=0, i")
	// 头部顺序本身也是指纹：按 Chrome 的发送顺序声明
	req.Header[http.HeaderOrderKey] = []string{
		"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
		"upgrade-insecure-requests", "user-agent", "accept",
		"sec-fetch-site", "sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest",
		"referer", "accept-encoding", "accept-language", "priority", "cookie",
	}
	req.Header[http.PHeaderOrderKey] = []string{":method", ":authority", ":scheme", ":path"}

	if sessionFile != "" {
		if state, err := LoadStorageState(sessionFile); err == nil && len(state.Cookies) > 0 {
			var cookies []*http.Cookie
			for _, ck := range state.Cookies {
				cookies = append(cookies, &http.Cookie{Name: ck.Name, Value: ck.Value, Path: "/"})
			}
			c.http.SetCookies(u, cookies)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, &HTTPError{Status: resp.StatusCode, URL: rawURL}
	}
	ct := resp.Header.Get("content-type")
	return &Response{Status: resp.StatusCode, Body: body, ContentType: ct, IsBinary: IsBinaryType(ct)}, nil
}

// FetchOnce 便捷函数：单次抓取（内部建客户端）。
func FetchOnce(rawURL, proxy, sessionFile string, timeout int) (*Response, error) {
	c, err := NewClient(proxy, timeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.Fetch(rawURL, sessionFile)
}

// Warmup 访问首页拿身份 Cookie（搜索预热用）。
func Warmup(homeURL, proxy string, timeout int) (map[string]string, error) {
	c, err := NewClient(proxy, timeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if _, err := c.Fetch(homeURL, ""); err != nil {
		return nil, err
	}
	time.Sleep(800 * time.Millisecond)
	u, _ := url.Parse(homeURL)
	jar := c.http.GetCookieJar()
	out := map[string]string{}
	if jar != nil && u != nil {
		for _, ck := range jar.Cookies(u) {
			out[ck.Name] = ck.Value
		}
	}
	return out, nil
}

// RawResponse 原始响应（回放用）。
type RawResponse struct {
	StatusCode int
	BodyData   []byte
	Header     http.Header
}

// SetRawCookies 注入名字→值的 Cookie（回放用）。
func (c *Client) SetRawCookies(u *url.URL, jar map[string]string) {
	if u == nil {
		return
	}
	var cookies []*http.Cookie
	for k, v := range jar {
		cookies = append(cookies, &http.Cookie{Name: k, Value: v, Path: "/"})
	}
	if len(cookies) > 0 {
		c.http.SetCookies(u, cookies)
	}
}

// RawRequest 按给定方法/请求体回放一次请求。
func (c *Client) RawRequest(method, target, body string) (*RawResponse, error) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	if u, err := url.Parse(target); err == nil {
		req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &RawResponse{StatusCode: resp.StatusCode, BodyData: data, Header: resp.Header}, nil
}

// CookiesFor 取某 URL 的 Cookie（名→值）。
func (c *Client) CookiesFor(rawURL string) map[string]string {
	out := map[string]string{}
	u, err := url.Parse(rawURL)
	if err != nil {
		return out
	}
	jar := c.http.GetCookieJar()
	if jar == nil {
		return out
	}
	for _, ck := range jar.Cookies(u) {
		out[ck.Name] = ck.Value
	}
	return out
}

// SetCookiesFromMap 把名→值注入为某 URL 的 Cookie。
func (c *Client) SetCookiesFromMap(rawURL string, jar map[string]string) {
	u, err := url.Parse(rawURL)
	if err != nil || len(jar) == 0 {
		return
	}
	var cookies []*http.Cookie
	for k, v := range jar {
		cookies = append(cookies, &http.Cookie{Name: k, Value: v, Path: "/"})
	}
	c.http.SetCookies(u, cookies)
}

// PostForm 提交表单（ddg 这类用 POST 翻页的站点）。
func (c *Client) PostForm(rawURL string, form map[string]string, referer string) (*Response, error) {
	values := url.Values{}
	for k, v := range form {
		values.Set(k, v)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range xcore.RealHeaders {
		req.Header.Set(k, v)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	ct := resp.Header.Get("content-type")
	return &Response{Body: body, ContentType: ct, IsBinary: IsBinaryType(ct)}, nil
}
