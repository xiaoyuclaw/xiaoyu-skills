// Command sv 是 super-search 的 Go 实现（完整对齐 Python 版 CLI）。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"superfetch/internal/xcore"
	"superfetch/internal/xsearch"
)

type options struct {
	Query      string
	Engines    string
	Via        string
	Headless   bool
	JSON       bool
	Raw        bool
	Level      string
	Limit      *int
	Timeout    int
	Show       bool
	ClearState bool
	Markdown   bool
	Pages      int
	Count      int
}

func parseArgs(args []string) (*options, error) {
	o := &options{Via: "auto", Level: "normal", Timeout: 30, Pages: 1, Count: 10}
	i := 0
	need := func(name string) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s 需要一个值", name)
		}
		i++
		return args[i], nil
	}
	for ; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			if o.Query == "" {
				o.Query = a
			}
			continue
		}
		name, inline, hasInline := strings.Cut(a, "=")
		value := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			return need(name)
		}
		switch name {
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "super-search（Go 版）：sv \"关键词\" [-e baidu,bing] [--via auto|playwright|cffi] [-j] [-L brief|normal|full] [-m N]")
			os.Exit(0)
		case "--engine", "-e":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Engines = v
		case "--via":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Via = v
		case "--headless":
			o.Headless = true
		case "--json", "-j":
			o.JSON = true
		case "--raw":
			o.Raw = true
		case "--level", "-L":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Level = v
		case "--limit", "-m":
			v, err := value()
			if err != nil {
				return nil, err
			}
			n, _ := strconv.Atoi(v)
			o.Limit = &n
		case "--timeout", "-t":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Timeout, _ = strconv.Atoi(v)
		case "--show":
			o.Show = true
		case "--md", "--markdown":
			o.Markdown = true
		case "--pages", "-P":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Pages, _ = strconv.Atoi(v)
		case "--count", "-n":
			v, err := value()
			if err != nil {
				return nil, err
			}
			o.Count, _ = strconv.Atoi(v)
		case "--clear-state":
			o.ClearState = true
		default:
			return nil, fmt.Errorf("未知参数: %s", name)
		}
	}
	switch o.Via {
	case "http":
		o.Via = "cffi"
	case "browser":
		o.Via = "playwright"
	}
	return o, nil
}

var reCJK = regexp.MustCompile(`[\p{Han}]`)

func main() {
	o, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if o.Limit == nil {
		n := map[string]int{"brief": 800, "normal": 5000, "full": 0}[o.Level]
		o.Limit = &n
	}

	if o.ClearState {
		n := 0
		for _, pat := range []string{"pwstate-*.json", "cookies-*.json"} {
			matches, _ := filepath.Glob(filepath.Join(xcore.DataDir(), pat))
			for _, m := range matches {
				if os.Remove(m) == nil {
					n++
				}
			}
		}
		fmt.Printf("已清理 %d 个状态文件\n", n)
		return
	}

	if o.Query == "" {
		fmt.Fprintln(os.Stderr, "缺少查询词（--clear-state 除外）")
		os.Exit(2)
	}

	var engines []string
	if o.Engines != "" {
		for _, e := range strings.Split(o.Engines, ",") {
			if e = strings.TrimSpace(e); e != "" {
				engines = append(engines, e)
			}
		}
	} else if reCJK.MatchString(o.Query) {
		engines = []string{"bing", "baidu", "toutiao", "360", "sogou", "shenma", "ddg"}
	} else {
		engines = []string{"bing", "baidu", "ddg", "360", "sogou"}
	}

	via := o.Via
	if (via == "playwright" || via == "auto") && !xsearch.HasDisplay() {
		fmt.Fprintln(os.Stderr, "[*] 没有可用显示器，回退到 cffi")
		via = "cffi"
	}

	result, err := xsearch.Run(engines, o.Query, via, *o.Limit, o.Timeout, o.Headless, o.Show,
		maxInt(o.Pages, 1), maxInt(o.Count, 1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[*] ❌ %v\n", err)
	}

	if result == nil {
		msg := "所有引擎都不可用。常见原因：① 该引擎在当前网络不可达（如 ddg 需代理）；" +
			"② 被风控，试 --via browser；③ 换引擎，国内推荐 -e baidu / -e bing / -e sogou"
		if o.JSON {
			printJSON(map[string]any{"error": "所有引擎都不可用", "hint": msg, "query": o.Query})
		} else {
			fmt.Println(msg)
		}
		os.Exit(1)
	}

	if o.JSON {
		structOut := map[string]any{
			"engine": result.Engine, "query": result.Query,
			"via": result.Via, "elapsed": result.Elapsed,
		}
		items := xsearch.StructuredItems(result, o.Count, 200)
		structOut["items"] = items
		structOut["count"] = len(items)
		if o.Raw {
			structOut["content"] = result.Content
		}
		printJSON(structOut)
		return
	}

	if o.Markdown {
		fmt.Println(result.Content + xsearch.LinkTable(result.Namespace, 40))
		return
	}
	fmt.Println(xsearch.MDResults(result, o.Level))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func printJSON(v any) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}
