# Python 版 vs Go 版 实测对比

测试环境：本机（VMware/Ubuntu，出口机房 IP），2026-11-09。
命令一律走同一个 `.data` 目录，同一批 URL、同一台机器、顺序执行。

## 1. 启动耗时

`--help` 连续 10 次取中位数：

| | 耗时 |
|---|---|
| Python `sf` | **728 ms** |
| Go `sf` | **17 ms** |

Go 版是静态单文件（`sf` 27MB / `sv` 26MB / `sfl` 12MB），没有解释器与依赖导入。

## 2. 单 URL 抓取（`-L full`，纯 HTTP 通道优先）

| URL | Python | Go | Python 字符 | Go 字符 | 文本相似度 |
|---|---|---|---|---|---|
| example.com | 1.29s | 0.38s | 203 | 203 | 0.985 |
| www.gov.cn | 1.26s | 0.18s | 6,196 | 6,382 | 0.980 |
| en.wikipedia.org/wiki/Web_scraping | 1.79s | 1.41s | 42,327 | 42,446 | 0.995 |
| joelonsoftware.com（长文） | 1.38s | 0.40s | 20,796 | 20,807 | 0.999 |
| github.com/deepseek-ai/DeepSeek-V3 | 2.07s | 0.54s | 17,934 | 17,457 | 1.000 |
| sspai.com | 1.64s | 0.95s | 2,432 | 5,551 | 0.821 |
| 36kr.com（风控，走浏览器） | 11.67s | 5.37s | 9,906 | 10,793 | 0.989 |
| news.ycombinator.com（503，两边都失败） | 9.20s | 5.92s | 276 | 218 | 0.965 |

- 合计：Python **30.3s**，Go **15.1s**（约 2×）
- 平均文本相似度 **0.967**；除 sspai 外都在 0.98 以上
- sspai 差异是 Go 的正文容器选得更大（多拿到正文，少了 0.82 的相似度）

## 3. 批量抓取（6 URL，`-c 3`）

| | 耗时 | 成功 |
|---|---|---|
| Python | 6.28s | 6/6 |
| Go | **4.40s** | 6/6 |

（首轮测试时 Python 批量是 0/6，暴露出 `fetch.py` 里 `_parse_html` 引用全局 `args`
的 `NameError`；已修复，修完两边都是 6/6。）

## 4. 搜索（`sv -e bing`，`-L brief`）

| 查询 | Python | Go | 引擎 |
|---|---|---|---|
| rust async runtime | 3.80s | 2.14s | bing / bing |
| MiniMax speech 2.8 价格 | 3.13s | 1.76s | bing / bing |
| arxiv transformer 2026 | 2.87s | 1.76s | bing / bing |

## 5. 功能对齐

| 功能 | Python | Go | 备注 |
|---|---|---|---|
| `-e auto/cffi/playwright` | ✅ | ✅ | 自动升级逻辑一致 |
| 元刷新跟随、拦截页检测、骨架屏重抓 | ✅ | ✅ | |
| `-L / -m / -o / -j / -l` | ✅ | ✅ | 截断按 Unicode 字符数 |
| 链接代号 `@ns-id` + SQLite 链接库 | ✅ | ✅ | 同一个 `links.db`，可互查 |
| `sfl` 反查/清理 | ✅ | ✅ | |
| 接口库 `--sites` / `--discover` / `--verify` / `--remember` | ✅ | ✅ | 同一张 `sites` 表 |
| `--embedded`（NEXT_DATA / NUXT / JSON-LD…） | ✅ | ✅ | Go 版 outline 按 key 排序 |
| `--eval` | ✅ | ✅ | |
| 登录态 `-s` / 按域名自动匹配 / `-i` 交互登录 | ✅ | ✅ | Go 用 chromedp，storageState 格式互通 |
| 批量并发 + 域名限流 + 抖动 | ✅ | ✅ | |
| 搜索 `sv`（5 引擎、预热、会话复用） | ✅ | ✅ | |

## 6. 需要注意的差异

1. **Markdown 细节**：链接里的括号会被转义成 `%28`/`%29`；列表项之间的空行比 markdownify 少。
   正文内容一致，只是排版略有出入。
2. **内嵌 JSON 的字段顺序**：Go 的 map 无序，outline 按字母序；Python 保插入顺序。
3. **百度的 HTTP 通道**：测试期间连续请求把本机 IP 打到风控，两边都会退到浏览器；
   对照测试确认这不是 Go 的问题（同一时刻两边都被拦）。
4. **浏览器二进制**：Go 版复用 Playwright 缓存里的 Chromium，也可用 `CHROME_PATH` 指定。

## 7. 结论

- **启动**：17ms vs 728ms，差 40 倍，适合被脚本高频调用。
- **单页**：快 2 倍左右（省掉解释器 + 依赖导入）。
- **内容质量**：平均 0.967 相似度，长文/文档类几乎一致。
- **代价**：Markdown 排版细节与内嵌 JSON 顺序有微小差异；需要 Chrome 才能走浏览器通道。
