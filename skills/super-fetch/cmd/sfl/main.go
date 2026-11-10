// Command sfl 根据链接代号反查原始 URL（对齐 Python 版 get_link.py）。
package main

import (
	"fmt"
	"os"
	"strings"

	"superfetch/internal/xcore"
	"superfetch/internal/xdb"
)

func main() {
	var ids []string
	clear := false
	yes := false
	for _, a := range os.Args[1:] {
		switch a {
		case "-c", "--clear":
			clear = true
		case "-y", "--yes":
			yes = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "用法: sfl @abcd-1 [@abcd-2 ...] | sfl -c [-y] | sfl -c @abcd")
			return
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "未知参数: %s\n", a)
				os.Exit(2)
			}
			ids = append(ids, a)
		}
	}

	if clear {
		if len(ids) == 0 && !yes {
			fmt.Printf("拒绝执行：将清空全部 %d 条链接记录。确认请加 --yes。\n", xdb.CountLinks())
			return
		}
		n, err := xdb.ClearLinks(ids)
		if err != nil {
			fmt.Printf("数据库操作失败: %v\n", err)
			os.Exit(1)
		}
		if len(ids) == 0 {
			fmt.Println("[*] 🧹 已清空数据库中所有链接记录。")
		} else {
			fmt.Printf("[*] 🧹 已清理 %d 条记录。\n", n)
		}
		return
	}

	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "用法: sfl @abcd-1 [@abcd-2 ...] | sfl -c [-y]")
		return
	}
	var clean []string
	for _, raw := range ids {
		clean = append(clean, strings.TrimPrefix(raw, "@"))
	}
	found := xdb.LookupLinks(clean)
	for _, raw := range ids {
		id := strings.TrimPrefix(raw, "@")
		if u, ok := found[id]; ok {
			fmt.Printf("@%s -> %s\n", id, u)
		} else {
			fmt.Printf("@%s -> [未找到或已清理]\n", id)
		}
	}
	_ = xcore.DBPath()
}
