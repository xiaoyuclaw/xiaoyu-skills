package xfetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"superfetch/internal/xcore"
)

// Captured 浏览器里捕获到的一个 XHR/fetch 请求。
type Captured struct {
	URL         string `json:"url"`
	URLFull     string `json:"url_full"`
	Status      int64  `json:"status"`
	Method      string `json:"method"`
	RequestBody string `json:"request_body"`
	ContentType string `json:"content_type"`
	Bytes       int    `json:"bytes"`
	LooksJSON   bool   `json:"looks_json"`
	Preview     string `json:"preview"`
	Body        string `json:"body,omitempty"`
}

// ReplayResult 用 HTTP 通道回放请求的结果。
type ReplayResult struct {
	Status   int     `json:"status"`
	Seconds  float64 `json:"seconds"`
	Bytes    int     `json:"bytes"`
	BodyHead string  `json:"body_head,omitempty"`
	Body     string  `json:"body,omitempty"`
	Error    string  `json:"error,omitempty"`
}

// ProbeResult --discover / --eval 的结构化输出。
type ProbeResult struct {
	URL       string        `json:"url"`
	GotoError string        `json:"goto_error,omitempty"`
	Result    any           `json:"result,omitempty"`
	EvalError string        `json:"eval_error,omitempty"`
	Count     int           `json:"count,omitempty"`
	Captured  []Captured    `json:"captured,omitempty"`
	Replaced  []string      `json:"replaced,omitempty"`
	Replay    *ReplayResult `json:"replay,omitempty"`
}

type reqMeta struct {
	method   string
	postData string
}

// Probe 在浏览器里执行 JS 或捕获接口请求，返回 JSON 字符串。
func Probe(opts BrowserOptions, rawURL, sessionFile, evalJS string, xhrPattern *string, replay bool, replaces []string) (string, error) {
	opts.ForceChromedp = true // eval/discover 需要 CDP，固定走 chromedp
	b, err := LaunchBrowser(opts)
	if err != nil {
		return "", err
	}
	defer b.Close()

	timeoutCtx, stop, err := b.newTab()
	if err != nil {
		return "", err
	}
	defer stop()
	b.applySession(timeoutCtx, sessionFile)

	var (
		mu       sync.Mutex
		captured []Captured
		metas    = map[network.RequestID]reqMeta{}
	)
	if xhrPattern != nil {
		keepBody := replay
		chromedp.ListenTarget(timeoutCtx, func(ev any) {
			switch e := ev.(type) {
			case *network.EventRequestWillBeSent:
				if e.Type != network.ResourceTypeXHR && e.Type != network.ResourceTypeFetch {
					return
				}
				if *xhrPattern != "" && !strings.Contains(e.Request.URL, *xhrPattern) {
					return
				}
				mu.Lock()
				metas[e.RequestID] = reqMeta{method: e.Request.Method, postData: postDataOf(e.Request)}
				mu.Unlock()
			case *network.EventResponseReceived:
				if e.Type != network.ResourceTypeXHR && e.Type != network.ResourceTypeFetch {
					return
				}
				if *xhrPattern != "" && !strings.Contains(e.Response.URL, *xhrPattern) {
					return
				}
				reqID := e.RequestID
				respURL := e.Response.URL
				status := e.Response.Status
				mime := e.Response.MimeType
				go func() {
					var body string
					ctx, cancel := context.WithTimeout(timeoutCtx, 10*time.Second)
					defer cancel()
					if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
						bd, err := network.GetResponseBody(reqID).Do(c)
						if err == nil {
							body = string(bd)
						}
						return err
					})); err != nil {
						body = ""
					}
					mu.Lock()
					meta := metas[reqID]
					delete(metas, reqID)
					mu.Unlock()

					item := Captured{
						URL:         stripQuery(respURL),
						URLFull:     respURL,
						Status:      status,
						Method:      meta.method,
						RequestBody: xcore.TruncateRunes(meta.postData, 4000),
						ContentType: mime,
						Bytes:       len(body),
						LooksJSON:   isJSONBody(body),
						Preview:     truncate(strings.ReplaceAll(body, "\n", " "), 180),
					}
					if keepBody {
						item.Body = xcore.TruncateRunes(body, 20000)
					}
					mu.Lock()
					captured = append(captured, item)
					mu.Unlock()
				}()
			}
		})
	}

	res := ProbeResult{URL: rawURL}
	if err := chromedp.Run(timeoutCtx, chromedp.Navigate(rawURL),
		chromedp.Sleep(time.Duration(maxInt(opts.Wait, 2))*time.Second)); err != nil {
		res.GotoError = err.Error()
	}

	if evalJS != "" {
		var val any
		if err := chromedp.Run(timeoutCtx, chromedp.Evaluate(evalJS, &val)); err != nil {
			res.EvalError = err.Error()
		} else {
			res.Result = val
		}
	}
	if xhrPattern != nil {
		mu.Lock()
		items := append([]Captured(nil), captured...)
		mu.Unlock()
		res.Count = len(items)
		res.Captured = items
		if replay && len(items) > 0 {
			entry := items[0]
			if len(replaces) > 0 {
				entry = applyReplaces(entry, replaces)
				res.Replaced = replaces
			}
			rep := replayCaptured(entry, browserCookies(timeoutCtx))
			res.Replay = &rep
		}
	}

	out, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func postDataOf(r *network.Request) string {
	var b strings.Builder
	for _, entry := range r.PostDataEntries {
		b.WriteString(entry.Bytes)
	}
	return b.String()
}

func isJSONBody(body string) bool {
	t := strings.TrimSpace(body)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

func stripQuery(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host + u.Path
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func applyReplaces(entry Captured, replaces []string) Captured {
	for _, pair := range replaces {
		oldVal, newVal, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		entry.RequestBody = strings.ReplaceAll(entry.RequestBody, oldVal, newVal)
		entry.URLFull = strings.ReplaceAll(entry.URLFull, oldVal, newVal)
		entry.URL = strings.ReplaceAll(entry.URL, oldVal, newVal)
	}
	return entry
}

func browserCookies(ctx context.Context) map[string]string {
	out := map[string]string{}
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		var e error
		cookies, e = network.GetCookies().Do(c)
		return e
	}))
	if err != nil {
		return out
	}
	for _, ck := range cookies {
		out[ck.Name] = ck.Value
	}
	return out
}

// replayCaptured 用 TLS 指纹通道回放浏览器抓到的请求。
func replayCaptured(entry Captured, cookies map[string]string) ReplayResult {
	client, err := NewClient("", 30)
	if err != nil {
		return ReplayResult{Error: err.Error()}
	}
	defer client.Close()

	method := entry.Method
	if method == "" {
		method = "GET"
	}
	target := entry.URLFull
	if target == "" {
		target = entry.URL
	}
	if u, err := url.Parse(target); err == nil && len(cookies) > 0 {
		client.SetRawCookies(u, cookies)
	}

	start := time.Now()
	if method == http.MethodGet || method == "" {
		// GET 用标准抓取通道：带完整 Chrome 头序，服务端不会当成爬虫排队
		r, err := client.Fetch(target, "")
		secs := time.Since(start).Seconds()
		if err != nil {
			return ReplayResult{Error: err.Error(), Seconds: round2(secs)}
		}
		return ReplayResult{
			Status: r.Status, Seconds: round2(secs), Bytes: len(r.Body),
			BodyHead: truncate(string(r.Body), 3000), Body: string(r.Body),
		}
	}
	resp, err := client.RawRequest(method, target, entry.RequestBody)
	secs := time.Since(start).Seconds()
	if err != nil {
		return ReplayResult{Error: err.Error(), Seconds: round2(secs)}
	}
	return ReplayResult{
		Status:   resp.StatusCode,
		Seconds:  round2(secs),
		Bytes:    len(resp.BodyData),
		BodyHead: truncate(string(resp.BodyData), 3000),
		Body:     string(resp.BodyData),
	}
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
