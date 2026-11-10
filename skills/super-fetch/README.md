# super-fetch（Go 版）

`sf` / `sv` / `sfl` 的唯一实现。2026-11-10 起 Python 版已删除，本目录就是技能本体，
数据在 `.data/`（`links.db`、登录态、搜索 Cookie）。

## 文档

- `BENCHMARK-TASK.md` — 抓取性能评测任务（sf vs 内置 web_fetch 的测法与口径，含内容质量 8 项指标）
- `FETCH-HARD-TASK.md` — 困难抓取任务评测（多页/反爬/登录/接口发现），对比 auto、cffi、全浏览器、discover 等策略
- `STRATEGIES.md` — 策略判断手册（HTTP / 浏览器 / 三步走接口发现 / 内嵌数据 / 登录态）

## 浏览器后端（三种，SF_BROWSER 切换，默认 nodriver）

| 后端 | 内核 | 冷启动 | 热启动 | 说明 |
|---|---|---|---|---|
| **nodriver**（默认） | 系统真实 Chrome | 3.8s | **1.4s** | CDP 远程调试端口 + 无自动化参数，`navigator.webdriver=false` |
| **chromedp** | Playwright Chromium | 8.0s | 3.7s | 兼容性最好 |
| **gomoufox** | Camoufox / Firefox | 35s | 2.6s | 反检测最强，冷启动最慢 |

```bash
SF_BROWSER=nodriver sf "<url>" --via browser   # 默认
SF_BROWSER=chromedp sf "<url>" --via browser
SF_BROWSER=gomoufox sf "<url>" --via browser
SF_HEADLESS=virtual|true|false                 # gomoufox 默认 virtual
```

**三种后端都支持按需预热（daemon）**：首次抓取自动拉起 `bin/sfd --backend X`，
之后复用；单实例 flock、随 DSH 退出、空闲 15 分钟退出。

**功能一致性**（三种后端都已验证）：普通抓取、会话保存/恢复（`-s` + `--remember`）、
`--eval`、`--discover`、`--embedded`、批量 `-F`、交互登录 `-i`、`sv` 浏览器通道。
其中 `--eval` / `--discover` / `-i` 固定走 chromedp（需要 CDP），其余按 `SF_BROWSER` 选择。

**Cookie / 登录态怎么算「通用」**：

- 浏览器 profile **不共享**：三个后端各用各的临时 profile（nodriver/chromedp 在 `/tmp/sf-*`，
  gomoufox 用它自己的），daemon 退出就清掉。
- **session 文件（Playwright storageState）跨后端通用**：`-s 名字.json` 读入注入，
  `--remember` 抓完写回；三种后端都已用 httpbin cookie 往返验证。
- **带 `-s` 也走常驻 daemon（热启动）**：`sf "<url>" --via browser -s x.json` 热起来 1.7s 左右；
  `<!-- -->` 想强制新开浏览器（不碰 daemon）：加 `--fresh`。

> 内存约束：这台机器只有 6G，**同一时间只保留一个 daemon**——
> 切换后端时客户端会先杀掉其它后端的 daemon（按 `<sock>.pid`）。
> 禁用 daemon：`SF_NO_DAEMON=1`。

## 构建

```bash
cd ~/agents/xiaoyu/.agents/skills/super-fetch
go build -o bin/sf  ./cmd/sf
go build -o bin/sv  ./cmd/sv
go build -o bin/sfl ./cmd/sfl
```

二进制是静态链接的单文件（约 27MB），拷到别的 Linux 机器直接能跑；
浏览器通道需要一个 Chrome/Chromium（自动查找 `CHROME_PATH` → Playwright 缓存 → 系统 chrome）。

## 用法

与 Python 版完全一致：

```bash
sf "https://example.com"                 # 默认 auto：先 HTTP，内容太薄再升浏览器
sf "https://36kr.com/" -e playwright     # 强制浏览器
sf "https://x.com/a" -j -L full          # JSON 输出、不截断
sf "https://x.com" --discover -j         # 列 XHR 候选接口
sf "https://x.com" --discover "api" --verify   # 回放接口
sf "https://x.com" --embedded            # 取 SSR 注水 JSON
sf "https://x.com" -i                    # 人工登录（有头浏览器 + 绿色按钮）
sf URL1 URL2 -c 3                        # 批量
sf -F urls.txt --silent                  # 从文件批量
sf --sites cnki                          # 接口库
sfl @abcd-1                              # 链接代号反查
sv "关键词" -L brief                      # 搜索
sv "关键词" -j                            # 结构化结果
sv --clear-state                         # 清搜索 Cookie
```

## 实现要点

| 层 | Python 版 | Go 版 |
|---|---|---|
| HTTP/TLS 指纹 | curl_cffi `impersonate=chrome` | `bogdanfinn/tls-client` `profiles.Chrome_152` + 手工按 Chrome 顺序排头 |
| 浏览器 | Playwright / Patchright | `chromedp`（CDP 直连，无 Node 依赖） |
| HTML 解析 | BeautifulSoup + lxml | `goquery` + `x/net/html` |
| Markdown | markdownify | `JohannesKaufmann/html-to-markdown/v2` |
| 存储 | sqlite3 | `modernc.org/sqlite`（纯 Go，无 cgo） |
| 反检测 | stealth 注入 + 真实窗口 | 同样的 init script + `--disable-blink-features=AutomationControlled` |

## 与 Python 版的差异

1. **内嵌 JSON 的字段顺序**：Go 的 `map` 无序，`--embedded` 的 outline 按 key 排序；
   Python 保插入顺序。内容一致，只是展示顺序不同。
2. **字符计数的截断点**：两边都按 Unicode 字符数截断，但 Markdown 生成的空白/列表缩进
   略有差异（同一页正文相似度 0.98–1.00）。
3. **`javascript:` 链接**：html-to-markdown 会把括号转义成 `%28`/`%29`，
   markdownify 保持原样；不影响正文。
4. **交互模式**：按钮点击、多标签页跟随、Cookie/localStorage 落盘都实现了，
   但窗口关闭时只能保存当前会话（与 Python 行为一致）。
5. 浏览器二进制默认复用 Playwright 缓存里的 Chromium；不想用它就设 `CHROME_PATH`。

## 数据目录

默认本目录的 `.data/`（二进制在别处时按同级 super-fetch/.data 找）。找不到时
`super-fetch` 时，回退到 `~/.agents/super-fetch`；也可以显式指定：

```bash
SUPER_FETCH_HOME=/path/to/data ./bin/sf "..."
```
