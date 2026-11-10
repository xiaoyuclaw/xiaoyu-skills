// Package xrender 把探测/内嵌数据的结构渲染成易读 Markdown。
package xrender

import (
	"encoding/json"
	"fmt"
	"strings"

	"superfetch/internal/xcore"
	"superfetch/internal/xfetch"
)

// EmbeddedView 一块内嵌数据的摘要。
type EmbeddedView struct {
	Kind      string
	Chars     int
	Truncated bool
	Preview   string
	Outline   [][2]string
}

// EmbeddedMarkdown 渲染 --embedded 的 Markdown。
func EmbeddedMarkdown(views []EmbeddedView) string {
	if len(views) == 0 {
		return "没有内嵌 JSON。数据可能在接口里，试 `--discover`。"
	}
	var out []string
	for _, v := range views {
		out = append(out, fmt.Sprintf("## %s（%d 字符）", v.Kind, v.Chars))
		if v.Truncated {
			out = append(out, "> 已截断，完整数据用 `-L full -o file.json`")
		}
		for _, row := range v.Outline {
			out = append(out, fmt.Sprintf("- `%s`: %s", row[0], row[1]))
		}
		if prev := strings.TrimSpace(v.Preview); prev != "" {
			out = append(out, fmt.Sprintf("\n预览: `%s`", xcore.TruncateRunes(prev, 160)))
		}
		out = append(out, "")
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// DiscoverTable 渲染 --discover 的候选接口表。
func DiscoverTable(captured []xfetch.Captured, count int) string {
	if len(captured) == 0 {
		return "没有捕获到 XHR/fetch 请求。"
	}
	out := []string{
		fmt.Sprintf("## 候选接口（%d 个）", count), "",
		"| # | 方法 | 路径 | 状态 | 大小 | JSON |", "|---|---|---|---|---|---|",
	}
	for i, c := range captured {
		p := c.URL
		if idx := strings.Index(p, "://"); idx >= 0 {
			p = p[idx+3:]
		}
		jsonMark := ""
		if c.LooksJSON {
			jsonMark = "✓"
		}
		out = append(out, fmt.Sprintf("| %d | %s | %.60s | %d | %d B | %s |",
			i, c.Method, p, c.Status, c.Bytes, jsonMark))
	}
	out = append(out, "")
	out = append(out, `挑一个用：`+"`--discover \"路径子串\" --verify`")
	return strings.Join(out, "\n")
}

// ReplayBlock 渲染回放结果。
func ReplayBlock(rep *xfetch.ReplayResult, method, url string) string {
	if rep == nil {
		return ""
	}
	if rep.Error != "" {
		return fmt.Sprintf("### 回放失败\n`%s`（%.2fs）", rep.Error, rep.Seconds)
	}
	head := strings.ReplaceAll(strings.TrimSpace(rep.BodyHead), "\n", " ")
	head = xcore.TruncateRunes(head, 180)
	return fmt.Sprintf("### 回放成功\n- `%s %s`\n- 状态 %d ｜ %d B ｜ %.2fs\n- 片段: `%s`",
		method, url, rep.Status, rep.Bytes, rep.Seconds, head)
}

// EvalBlock 渲染 --eval 的结果。
func EvalBlock(val any, limit int) string {
	s, err := json.Marshal(val)
	if err != nil {
		s = []byte(fmt.Sprintf("%v", val))
	}
	if xcore.RuneLen(string(s)) <= limit {
		return "```json\n" + string(s) + "\n```"
	}
	return "```json\n" + xcore.TruncateRunes(string(s), limit) + "\n…（已截断，完整用 -j）\n```"
}
