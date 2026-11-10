package main

import (
	"fmt"
	"os"
	"strings"

	"superfetch/internal/xparse"
)

func main() {
	data, _ := os.ReadFile("/tmp/sspai.html")
	doc, err := xparse.MakeSoup(string(data))
	if err != nil {
		panic(err)
	}
	xparse.CleanNoiseElements(doc.Selection)
	best := xparse.BestContentNode(doc)
	tag := "?"
	if len(best.Nodes) > 0 {
		tag = best.Nodes[0].Data
	}
	cls, _ := best.Attr("class")
	fmt.Printf("Go 选中: <%s class=%q> 文本=%d\n", tag, cls, len(strings.TrimSpace(best.Text())))
	for _, sel := range []string{"article", "div.community__text", "body"} {
		n := doc.Find(sel)
		fmt.Printf("  %s: %d 个, 首个文本=%d\n", sel, n.Length(), len(strings.TrimSpace(n.First().Text())))
	}
}
