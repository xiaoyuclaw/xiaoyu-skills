package xparse

import (
	"os"
	"strings"
	"testing"

	"superfetch/internal/xcore"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sf-test-*")
	if err == nil {
		os.Setenv("SUPER_FETCH_HOME", dir)
	}
	code := m.Run()
	if dir != "" {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

const testHTML = `<!doctype html><html><head><title>测试页面</title></head>
<body>
<nav class="navbar"><a href="/x">导航</a></nav>
<article>
  <h1>标题</h1>
  <p>第一段文字，足够长以便通过正文阈值判断，包含 <a href="/a">链接A</a>。</p>
  <p>第二段文字，也足够长，继续补充内容以便评分算法挑中这个 article 节点。</p>
  <img src="data:image/png;base64,AAAA">
  <img src="/pic.png">
</article>
<footer class="footer"><a href="/about">关于</a></footer>
</body></html>`

func TestExtractPageContent(t *testing.T) {
	title, md, ns := ExtractPageContent(testHTML, "https://example.com/page", false)
	if title != "测试页面" {
		t.Fatalf("title = %q", title)
	}
	if ns == "" || len(ns) != 4 {
		t.Fatalf("namespace = %q", ns)
	}
	if !strings.Contains(md, "第一段文字") {
		t.Fatalf("markdown 缺少正文: %q", md)
	}
	if strings.Contains(md, "base64") {
		t.Fatalf("base64 图片没有被移除")
	}
	if !strings.Contains(md, "@"+ns+"-") {
		t.Fatalf("链接没有换成代号: %q", md)
	}
	if strings.Contains(md, "/nav") || strings.Contains(md, "导航") {
		t.Fatalf("导航噪音没有被清理: %q", md)
	}
}

func TestLooksLikeSkeleton(t *testing.T) {
	skel := strings.Repeat("word ", 40)
	if !LooksLikeSkeleton(skel) {
		t.Fatal("骨架屏没有被识别")
	}
	if LooksLikeSkeleton("这是正常的正文内容，只有几个词 word。") {
		t.Fatal("正常正文被误判为骨架屏")
	}
}

func TestMetaRefreshTarget(t *testing.T) {
	h := `<meta http-equiv="Refresh" content="0;url=https://a.example/x">`
	if got := MetaRefreshTarget(h); got != "https://a.example/x" {
		t.Fatalf("got %q", got)
	}
}

func TestOutlineSortedKeys(t *testing.T) {
	data := map[string]any{"b": "x", "a": map[string]any{"c": 1.0}}
	rows := Outline(data, 12)
	if len(rows) == 0 || rows[0][0] != "a" {
		t.Fatalf("outline 顺序不对: %v", rows)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := xcore.TruncateRunes("中文测试", 2); got != "中文" {
		t.Fatalf("got %q", got)
	}
	if got := xcore.RuneLen("中文abc"); got != 5 {
		t.Fatalf("RuneLen = %d", got)
	}
}
