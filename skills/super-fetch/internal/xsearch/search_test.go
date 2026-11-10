package xsearch

import (
	"strings"
	"testing"

	"superfetch/internal/xparse"
)

func TestGenericItemsFallback(t *testing.T) {
	// 没有任何已知类名，只能靠通用启发式：重复出现的兄弟块 + 外链标题
	var b strings.Builder
	for i := 0; i < 3; i++ {
		b.WriteString(`<div class="unknown-block"><h2><a href="https://example.org/` + string(rune('a'+i)) + `">这是第` + string(rune('一'+i)) + `条结果标题</a></h2><p>摘要文字</p></div>`)
	}
	b.WriteString(`<div class="footer"><a href="https://example.org/x">不是结果</a></div>`)

	doc, err := xparse.MakeSoup(`<html><body>` + b.String() + `</body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	items := genericItems(doc, "bing.com", 20)
	if len(items) != 3 {
		t.Fatalf("genericItems got %d, want 3: %+v", len(items), items)
	}
	if items[0].URL == "" || items[0].Title == "" {
		t.Fatalf("条目缺字段: %+v", items[0])
	}
}

func TestExtractItemsByEngineSelector(t *testing.T) {
	html := `<html><body><ol id="b_results">
	  <li class="b_algo"><h2><a href="https://rust-lang.org/zh-CN/">Rust 程序设计语言</a></h2><div class="b_caption"><p>生产环境中的 Rust</p></div></li>
	  <li class="b_algo"><h2><a href="https://www.runoob.com/rust/">Rust 教程</a></h2><div class="b_caption"><p>菜鸟教程</p></div></li>
	  <li class="b_algo"><h2><a href="https://example.com/rust">Rust 官网</a></h2><div class="b_caption"><p>示例</p></div></li>
	</ol></body></html>`
	items := ExtractItems("bing", html, 20)
	if len(items) != 3 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	if items[0].Title != "Rust 程序设计语言" || items[0].URL != "https://rust-lang.org/zh-CN/" {
		t.Fatalf("首条不对: %+v", items[0])
	}
	if !strings.Contains(items[0].Snippet, "生产环境") {
		t.Fatalf("摘要不对: %q", items[0].Snippet)
	}
}

func TestDedupeItems(t *testing.T) {
	in := []Item{{Title: "a", URL: "https://x.com/1"}, {Title: "a2", URL: "https://x.com/1#frag"}, {Title: "b", URL: "https://x.com/2"}}
	out := dedupeItems(in)
	if len(out) != 2 {
		t.Fatalf("dedupe got %d", len(out))
	}
}

func TestMDResultsFallsBackToMarkdown(t *testing.T) {
	r := &Result{Engine: "bing", Query: "q", Via: "cffi", Elapsed: 0.5,
		Content: "## [标题](@ab12-1)\n\n正文摘要", Namespace: ""}
	out := MDResults(r, "brief")
	if !strings.Contains(out, "正文摘要") {
		t.Fatalf("没有回退到 markdown: %q", out)
	}
	if strings.Contains(out, "没解析出条目") {
		t.Fatalf("不应再出现旧提示: %q", out)
	}
}

func TestAdBlocksFiltered(t *testing.T) {
	html := `<html><body><ol id="b_results">
	  <li class="b_algo"><h2><a href="https://good.example/1">正常结果一号</a></h2><p>摘要一</p></li>
	  <li class="b_algo b_ad"><h2><a href="https://ad.example/1">这是广告结果</a></h2><p>广告摘要</p></li>
	  <li class="b_algo" data-tuiguang="x"><h2><a href="https://ad2.example/1">推广结果二号</a></h2><p>推广摘要</p></li>
	  <li class="b_algo"><h2><a href="https://good.example/2">正常结果二号</a></h2><p>摘要二</p></li>
	</ol></body></html>`
	items := ExtractItems("bing", html, 20)
	if len(items) != 2 {
		t.Fatalf("广告没被过滤: %+v", items)
	}
	for _, it := range items {
		if strings.Contains(it.URL, "ad.example") || strings.Contains(it.URL, "ad2.example") {
			t.Fatalf("混进广告: %+v", it)
		}
	}
}

func TestUnwrapDuckDuckGoRedirect(t *testing.T) {
	raw := "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.org%2Fa%3Fb%3D1"
	got := UnwrapRedirect(raw)
	if got != "https://example.org/a?b=1" {
		t.Fatalf("got %q", got)
	}
}
