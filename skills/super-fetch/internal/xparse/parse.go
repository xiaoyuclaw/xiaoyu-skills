// Package xparse 把 HTML 抽取成结构化 Markdown（对齐 Python 版 fetch_parser.py）。
package xparse

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"superfetch/internal/xcore"
	"superfetch/internal/xdb"
)

// MakeSoup 用 goquery 解析 HTML。
func MakeSoup(input string) (*goquery.Document, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(input))
	if err != nil {
		return nil, err
	}
	return doc, nil
}

var noiseTags = []string{"script", "style", "noscript", "svg", "canvas"}

// noiseKeywords 用于 class/id 过滤。
var noiseKeywords = []string{
	"nav", "navigation", "navbar", "footer", "sidebar",
	"ad-", "-ad-", "ads-", "-ads-",
	"cookie", "consent",
}

// contentKeywords 正文加分。
var contentKeywords = []string{"article", "content", "main", "post", "entry"}

// CleanNoiseElements 移除注释与最确定的噪音标签。
func CleanNoiseElements(s *goquery.Selection) {
	removeComments(s)
	for _, tag := range noiseTags {
		s.Find(tag).Remove()
	}
}

func removeComments(s *goquery.Selection) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for child := n.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == html.CommentNode {
				n.RemoveChild(child)
			} else {
				walk(child)
			}
			child = next
		}
	}
	walk(s.Nodes[0])
}

// ContentScore 保守的正文评分：宁可多留。
func ContentScore(s *goquery.Selection) float64 {
	text := s.Text()
	textLen := float64(xcore.RuneLen(text))
	if textLen < 20 {
		return 0
	}
	pCount := float64(s.Find("p").Length())

	linkTextLen := 0.0
	s.Find("a").Each(func(_ int, a *goquery.Selection) { linkTextLen += float64(xcore.RuneLen(a.Text())) })
	linkDensity := 1.0
	if textLen > 0 {
		linkDensity = linkTextLen / textLen
	}
	if linkDensity > 0.8 {
		return 0
	}

	identity := ""
	if cls, ok := s.Attr("class"); ok {
		identity += " " + cls
	}
	if id, ok := s.Attr("id"); ok {
		identity += " " + id
	}
	identity = strings.ToLower(identity)

	noisePenalty := 1.0
	for _, kw := range noiseKeywords {
		if strings.Contains(identity, kw) {
			noisePenalty *= 0.3
			break
		}
	}
	contentBonus := 1.0
	for _, kw := range contentKeywords {
		if strings.Contains(identity, kw) {
			contentBonus = 1.5
			break
		}
	}

	base := textLen * (1 - linkDensity*0.5)
	if pCount >= 1 {
		base *= 1.1
	}
	if pCount >= 3 {
		base *= 1.2
	}
	return base * contentBonus * noisePenalty
}

func domTextLen(s *goquery.Selection) int {
	return xcore.RuneLen(s.Text())
}

// maxBlockLen body 直接子节点里最大的那块文本量。
func maxBlockLen(doc *goquery.Document) int {
	body := doc.Find("body").First()
	if body.Length() == 0 {
		body = doc.Selection
	}
	best := 0
	body.Children().Each(func(_ int, c *goquery.Selection) {
		if n := xcore.RuneLen(c.Text()); n > best {
			best = n
		}
	})
	if best == 0 {
		return domTextLen(doc.Selection)
	}
	return best
}

// BestContentNode 挑正文容器：语义标签 → 常见容器 → 评分 → body。
func BestContentNode(doc *goquery.Document) *goquery.Selection {
	maxBlock := maxBlockLen(doc)
	threshold := maxBlock / 4
	if threshold < 50 {
		threshold = 50
	}
	bigEnough := func(s *goquery.Selection) bool {
		return xcore.RuneLen(s.Text()) >= threshold
	}

	// 策略 1：语义化标签
	for _, sel := range []string{"article", "main", "[role=\"main\"]"} {
		if el := doc.Find(sel).First(); el.Length() > 0 && bigEnough(el) {
			return el
		}
	}
	// 策略 2：常见正文容器
	common := []string{
		".post-content", ".entry-content", ".article-content",
		"#post-content", "#entry-content", "#article-content",
		".content-body", ".post-body", ".article-body",
		".content", "#content",
	}
	for _, sel := range common {
		if el := doc.Find(sel).First(); el.Length() > 0 && bigEnough(el) {
			return el
		}
	}
	// 策略 3：评分
	type cand struct {
		score float64
		sel   *goquery.Selection
	}
	var cands []cand
	doc.Find("div, article, section, main, body").Each(func(_ int, s *goquery.Selection) {
		if sc := ContentScore(s); sc > 0 {
			cands = append(cands, cand{sc, s})
		}
	})
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	for _, c := range cands {
		if bigEnough(c.sel) {
			return c.sel
		}
	}
	// 策略 4：body
	if body := doc.Find("body").First(); body.Length() > 0 {
		return body
	}
	return doc.Selection
}

func removeBase64Images(s *goquery.Selection) {
	s.Find("img").Each(func(_ int, img *goquery.Selection) {
		if src, ok := img.Attr("src"); ok && strings.HasPrefix(src, "data:") {
			img.Remove()
		}
	})
}

// ToMarkdown 把 DOM 节点转 Markdown。
func ToMarkdown(sel *goquery.Selection) (string, error) {
	if sel.Length() == 0 || len(sel.Nodes) == 0 {
		return "", fmt.Errorf("empty selection")
	}
	out, err := htmltomarkdown.ConvertNode(sel.Nodes[0])
	if err != nil {
		return "", err
	}
	return string(out), nil
}

var (
	reMultiNewline = regexp.MustCompile(`\n{3,}`)
	reTagBlock     = regexp.MustCompile(`(?is)<(?:style|script|link)[^>]*>.*?</(?:style|script|link)>`)
	linkURLPattern = regexp.MustCompile(`"linkurl"\s*:\s*"([^"]+)"`)
)

// ExtractPageContent 解析 HTML 并转成 Markdown，返回 (title, markdown, namespace)。
func ExtractPageContent(input, pageURL string, fullMode bool) (string, string, string) {
	doc, err := MakeSoup(input)
	if err != nil {
		return "Untitled Page", strings.TrimSpace(input), ""
	}
	title := "Untitled Page"
	if t := doc.Find("title").First(); t.Length() > 0 {
		if s := strings.TrimSpace(t.Text()); s != "" {
			title = s
		}
	}

	var root *goquery.Selection
	if fullMode {
		if body := doc.Find("body").First(); body.Length() > 0 {
			root = body
		} else {
			root = doc.Selection
		}
	} else {
		CleanNoiseElements(doc.Selection)
		root = BestContentNode(doc)
	}

	for _, tag := range []string{"script", "style", "noscript", "svg", "canvas", "link"} {
		root.Find(tag).Remove()
	}
	removeBase64Images(root)

	ns := xdb.ProcessLinksToIDs(root, pageURL)
	processTextareaLinks(root, ns)

	md, err := ToMarkdown(root)
	if err != nil {
		md = strings.TrimSpace(root.Text())
	}
	md = reTagBlock.ReplaceAllString(md, "")
	md = reMultiNewline.ReplaceAllString(md, "\n\n")
	return title, strings.TrimSpace(md), ns
}

// processTextareaLinks 处理 textarea 里 "linkurl":"..." 的内嵌链接。
func processTextareaLinks(root *goquery.Selection, ns string) {
	counter := 0
	type taEdit struct {
		node  *html.Node
		pairs map[string]string // encoded -> @ns-id
	}
	var edits []taEdit

	root.Find("textarea").Each(func(_ int, ta *goquery.Selection) {
		if len(ta.Nodes) == 0 || ta.Nodes[0].FirstChild == nil {
			return
		}
		content := ta.Nodes[0].FirstChild.Data
		if ta.Nodes[0].FirstChild.Type != html.TextNode {
			return
		}
		pairs := map[string]string{}
		for _, m := range linkURLPattern.FindAllStringSubmatch(content, -1) {
			encoded := m[1]
			decoded := decodeURLTwice(encoded)
			counter++
			id := fmt.Sprintf("%s-%d", ns, counter)
			pairs[encoded] = "@" + id
			xdb.SaveLinks([]xdb.LinkPair{{ID: id, URL: decoded}})
		}
		if len(pairs) > 0 {
			edits = append(edits, taEdit{ta.Nodes[0], pairs})
		}
	})

	for _, e := range edits {
		content := e.node.FirstChild.Data
		for encoded, ref := range e.pairs {
			content = strings.ReplaceAll(content, `"linkurl":"`+encoded+`"`, `"linkurl":"`+ref+`"`)
			content = strings.ReplaceAll(content, `"linkurl": "`+encoded+`"`, `"linkurl": "`+ref+`"`)
		}
		e.node.FirstChild.Data = content
	}
}

func decodeURLTwice(s string) string {
	d, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	if strings.Contains(d, "%") {
		if d2, err := url.QueryUnescape(d); err == nil {
			return d2
		}
	}
	return d
}

// LooksLikeSkeleton 正文是不是还在骨架屏（一排 "word" 占位符）。
func LooksLikeSkeleton(text string) bool {
	toks := strings.Fields(text)
	if len(toks) < 20 {
		return false
	}
	bad := 0
	for _, t := range toks {
		if strings.ToLower(strings.Trim(t, "[](){}*_,.;:!?@#\"'`-")) == "word" {
			bad++
		}
	}
	return bad >= 20 && float64(bad) >= float64(len(toks))*0.3
}

var blockMarks = []string{"安全验证", "访问过于频繁", "Just a moment", "cf-challenge", "请输入验证码", "验证码"}

// LooksBlocked 页面是不是验证/拦截页（短页面 + 关键词才判定）。
func LooksBlocked(content string) bool {
	if xcore.RuneLen(content) >= 30000 {
		return false
	}
	for _, k := range blockMarks {
		if strings.Contains(content, k) {
			return true
		}
	}
	return false
}

var reMetaRefresh = regexp.MustCompile(`(?i)http-equiv=["']?refresh["']?[^>]*content=["'][^"']*url=([^"'>\s]+)`)

// MetaRefreshTarget 识别 <meta http-equiv="Refresh"> 的跳转目标。
func MetaRefreshTarget(input string) string {
	m := reMetaRefresh.FindStringSubmatch(input)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// NeedsPlaywright 内容薄且页面明显靠 JS 渲染时才值得上浏览器。
func NeedsPlaywright(input, markdown string, threshold int) bool {
	if xcore.RuneLen(strings.TrimSpace(markdown)) >= threshold {
		return false
	}
	if len(input) > 200_000 {
		return true
	}
	low := strings.ToLower(input)
	if strings.Count(low, "<script") >= 3 {
		return true
	}
	if strings.Contains(low, `id="app"`) || strings.Contains(low, `id='app'`) || strings.Contains(low, `id="root"`) {
		if xcore.RuneLen(strings.TrimSpace(markdown)) < 300 {
			return true
		}
	}
	return false
}

// Outline 把嵌套结构压成若干行 key-path（对齐 render.outline）。
func Outline(data any, maxRows int) [][2]string {
	if maxRows <= 0 {
		maxRows = 12
	}
	var rows [][2]string
	var walk func(node any, path string, depth int)
	walk = func(node any, path string, depth int) {
		if len(rows) >= maxRows || depth > 1 {
			return
		}
		switch v := node.(type) {
		case map[string]any:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				p := k
				if path != "" {
					p = path + "." + k
				}
				child := v[k]
				rows = append(rows, [2]string{p, shapeOf(child)})
				if len(rows) >= maxRows {
					return
				}
				switch child.(type) {
				case map[string]any, []any:
					walk(firstOf(child), p, depth+1)
				}
			}
		case []any:
			if len(v) > 0 {
				walk(v[0], path+"[0]", depth+1)
			}
		}
	}
	walk(data, "", 0)
	return rows
}

func firstOf(node any) any {
	switch v := node.(type) {
	case []any:
		if len(v) > 0 {
			return v[0]
		}
		return nil
	case map[string]any:
		return v
	}
	return nil
}

func shapeOf(v any) string {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 6 {
			keys = keys[:6]
		}
		return "object{" + strings.Join(keys, ", ") + "}"
	case []any:
		inner := ""
		if len(x) > 0 {
			inner = " " + shapeOf(x[0])
		}
		return fmt.Sprintf("array[%d]%s", len(x), inner)
	case string:
		return "str[" + strconv.Itoa(xcore.RuneLen(x)) + "]"
	case float64:
		return "float64"
	case bool:
		return "bool"
	case nil:
		return "NoneType"
	default:
		return fmt.Sprintf("%T", v)
	}
}
