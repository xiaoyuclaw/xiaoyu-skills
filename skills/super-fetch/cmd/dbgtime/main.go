package main

import (
	"context"
	"fmt"
	"time"

	gomoufox "github.com/ehmo/gomoufox"
	"superfetch/internal/xparse"
)

func main() {
	t := time.Now()
	lap := func(name string) { fmt.Printf("  %-20s %5.2fs\n", name, time.Since(t).Seconds()); t = time.Now() }

	ctx := context.Background()
	b, err := gomoufox.New(ctx)
	if err != nil {
		panic(err)
	}
	lap("浏览器启动")

	page, err := b.NewPage(ctx,
		gomoufox.WithViewport(1280, 900),
		gomoufox.WithContextLocale("zh-CN"),
		gomoufox.WithTimezoneID("Asia/Shanghai"),
	)
	if err != nil {
		panic(err)
	}
	lap("new_page")

	if _, err := page.Goto(ctx, "https://example.com",
		gomoufox.WaitUntil("domcontentloaded"), gomoufox.WithTimeout(30*time.Second)); err != nil {
		panic(err)
	}
	lap("goto")

	time.Sleep(600 * time.Millisecond)
	lap("稳定等待 0.6s")

	html, err := page.Content(ctx)
	if err != nil {
		panic(err)
	}
	lap("content")
	_ = page.Close()
	b.Close()
	lap("关闭浏览器")

	t2 := time.Now()
	xparse.ExtractPageContent(html, "https://example.com", false)
	fmt.Printf("  %-20s %5.2fs\n", "解析→Markdown", time.Since(t2).Seconds())
}
