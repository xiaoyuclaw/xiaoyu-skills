package xparse

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"superfetch/internal/xcore"
)

// EmbeddedBlock 页面内嵌的一段结构化数据。
type EmbeddedBlock struct {
	Kind string `json:"kind"`
	Raw  string `json:"-"`
	Data any    `json:"data"`
}

var embeddedPatterns = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`(?is)<script[^>]+id="__NEXT_DATA__"[^>]*>(.*?)</script>`), "NEXT_DATA"},
	{regexp.MustCompile(`(?s)window\.__NUXT__\s*=\s*(\{.*?\})\s*;?\s*</script>`), "NUXT"},
	{regexp.MustCompile(`(?s)window\.__INITIAL_STATE__\s*=\s*(\{.*?\})\s*;?\s*</script>`), "INITIAL_STATE"},
	{regexp.MustCompile(`(?s)window\.__APOLLO_STATE__\s*=\s*(\{.*?\})\s*;?\s*</script>`), "APOLLO_STATE"},
	{regexp.MustCompile(`(?is)<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`), "JSON-LD"},
}

// ExtractEmbeddedData 从 HTML 里挖出服务端注水的 JSON，按体积降序。
func ExtractEmbeddedData(input string, minBytes, limit int) []EmbeddedBlock {
	if minBytes <= 0 {
		minBytes = 200
	}
	if limit <= 0 {
		limit = 3
	}
	var out []EmbeddedBlock
	for _, pat := range embeddedPatterns {
		for _, m := range pat.re.FindAllStringSubmatch(input, -1) {
			if len(m) < 2 {
				continue
			}
			raw := strings.TrimSpace(m[1])
			if len(raw) < minBytes {
				continue
			}
			var data any
			if err := json.Unmarshal([]byte(raw), &data); err != nil {
				continue
			}
			out = append(out, EmbeddedBlock{Kind: pat.kind, Raw: raw, Data: data})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return xcore.RuneLen(out[i].Raw) > xcore.RuneLen(out[j].Raw)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
