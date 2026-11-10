---
name: super-fetch
description: 高性能网页抓取工具，将网页转换为干净的 Markdown。支持 JavaScript 渲染、会话保持、登录态保存、批量并发抓取。跨平台 Windows/macOS/Linux。
---

# Super Fetch

高性能网页抓取工具，支持 JS 渲染、登录态保存、批量并发。

## 安装（自包含，Go 版）

2026-11-10 起只有 Go 版（Python 版已删，.venv 不再需要）。二进制已编好，直接跑：

```bash
cd ~/agents/xiaoyu/.agents/skills/super-fetch
./sf "<url>"              # 本目录包装器 → bin/sf
./sfl @abcd-1             # 链接代号反查
```

PATH 里也有同名包装器：`sf` / `sfg`（同一个二进制）、`sv` / `svg`（搜索）、`sflg`（反查）。

目录结构：

```
super-fetch/
├── bin/          sf、sv、sfl（Go 二进制，gitignore）
├── cmd/          Go 入口（cmd/sf、cmd/sv、cmd/sfl）
├── internal/     实现
├── .data/        数据：links.db、会话/Cookie（gitignore）
├── sf sv sfl     目录内包装器
└── README.md     CLI 全量参数
```

数据目录默认就是本目录的 `.data/`（可用 `SUPER_FETCH_HOME` 覆盖）。
重新编译：`go build -o bin/sf ./cmd/sf && go build -o bin/sv ./cmd/sv && go build -o bin/sfl ./cmd/sfl`
（与 Python 版对比的旧数据在 `BENCHMARK.md`，仅供存档。）

## 抓取语义（2026-11-09 修复）

- **非 UTF-8 页面自动解码**：响应头没写 charset 时，依次看 `<meta charset>` → UTF-8 → GB18030 →
  charset_normalizer。GBK/GB2312 站点不再乱码（此前 Python 版会乱码、Go 版不会）。
- **首页/列表页选块修正**：URL 是首页（路径为 `/` 或 `index.*`）时，优先取「最大的干净链接容器」
  （≥800 字、≥10 链接、不含 header/nav/footer），而不是评分最高的那篇小文章；
  容器若等于整页则退回常规策略。普通文章页不受影响。
- **HTTP 错误码语义**：`400/401/402/404/405/410/451` 视为**确定性失败**，直接返回
  `{"error": "HTTP 404"}`、退出码 1，不再开浏览器抓回错误页；
  `403/429/5xx` 仍会尝试浏览器兜底（JS 挑战/限流有时能过）。

```bash
./sf "https://httpbin.org/status/404"; echo $?   # {"error": "HTTP 404"} / 1
```

## 浏览器后端与常驻 daemon（2026-11-09）

- **headless 默认 `virtual`**（虚拟显示器，Camoufox 的第三种模式）：`SF_HEADLESS=virtual|true|false` 可改。
- **可选 nodriver**：`SF_BROWSER=nodriver`，直接驱动系统 Chrome（`/usr/bin/google-chrome`），
  无 webdriver 痕迹（`navigator.webdriver=False`、无 `cdc_` 变量），单次约 7s。
- **默认 Camoufox / gomoufox**（Firefox 内核）。Python 侧用 `browser_backend.py`，
  Go 侧用 `gomoufox`；`SF_BROWSER=chromium` 可切回 Patchright / chromedp。
- **常驻 daemon**：第一次浏览器抓取时自动拉起一个常驻进程，之后的抓取复用它。
  - Python：`browser_daemon.py`，socket `/tmp/sf-camoufox-<uid>.sock`
  - Go：`bin/sfd`，socket `/run/user/<uid>/sf-gomoufox-<uid>.sock`
  - 单实例：daemon 启动时对 `<sock>.lock` 加 flock，已有实例时直接退出（不会重复启动）
  - 生命周期：**随 DSH 宿主进程退出**（记录 `--dsh-pid`，每 10s 检查），空闲 30 分钟也退出
  - 禁用：`SF_NO_DAEMON=1`
- **实测**（example.com，`--via browser`）：

  | | 冷启动（含 daemon 拉起） | 之后每次 |
  |---|---|---|
  | Python Camoufox | ~15s | 4.5–7s（`-w 1` 可再快 2s） |
  | Go gomoufox | ~15s | **3.0–3.3s** |

- **注意**：Camoufox 的浏览器二进制必须存在（`~/.cache/camoufox/browser-*/camoufox-bin`）。
  代码里已加保险：找不到就直接报错，**不会**让 camoufox 自己去下载/清理缓存
  （它的 pkgman 清理会把手动安装的目录整个删掉，2026-11-09 踩过一次）。

## 引擎怎么选

| 引擎 | 速度 | 适用 |
|---|---|---|
| `auto`（默认） | 快 | 先用 cffi，内容少且页面明显靠 JS 渲染时才换 playwright |
| `cffi` | 最快 | 静态页、文档、搜索结果页 |
| `playwright` | 慢 3-6 倍 | SPA、需要滚动/登录、动态加载 |

实测同一页面：cffi 1.9s vs playwright 6.4s。

## 快速开始

```bash
# 抓取单个网页（使用默认会话，链接用代号表示）
sf https://example.com -s

# 批量抓取多个网页（2 个及以上 URL 才进批量模式）
sf https://example.com/page1 https://example.com/page2 -s
# 注意：只传 1 个 URL 会走单 URL 模式；批量请传 ≥2 个或用 -F

# 从文件读取 URL 列表并保存结果到results.json（多URL时使用-o保存查询结果到json）
sf -F urls.txt -o results.json

# 需要用户手动交互时弹出浏览器手动操作（如登录/验证码），登陆状态默认保存到默认会话中
sf https://example.com -i

# 通过链接代号查询真实URL
sfl @abcd-123

# 按照命名空间清理数据库
sfl --clear abcd

# 单 URL 输出 JSON / 写入文件 / 附链接对照
sf https://example.com --json
sf https://example.com -o out.md
sf https://example.com --links

# 下载文件（单URL时使用-o下载文件）
sf "https://avatars.githubusercontent.com/u/252820863?s=48&v=4" -o "img.jpg" 
```

## 核心概念

### 两种模式

| 触发方式 | 模式 | 说明 |
|---------|------|------|
| 单个 URL | 单 URL 模式 | 输出 Markdown 到终端 |
| 多个 URL 或 `-F` | 批量并发模式 | 输出 JSON 结果 |

### 会话（Session）

会话用于保存登录状态，避免重复登录。

```bash
sf https://example.com         # 独立会话
sf https://example.com -s      # 使用默认会话
sf https://example.com -i      # 创建会话（弹出浏览器）
```

## 别改回 headless（2026-11-08 主人要求）

playwright 一律 `headless=False`，窗口开在 `-2400,-2400`（屏幕外）。原因：无头指纹会被站点识别——知网检索页对 headless 直接 403，有头就正常 200。

批量模式 `PlaywrightPool` 的默认值也已改成 `headless=False`。

## 兜底原则

任何路径最终都能落到 playwright：`auto` 下 cffi 失败、正文过薄、命中验证页（不论长短），都会自动升级到有头浏览器。只有显式 `--via http` 才是"不许升级"。

## 五种情况怎么选（2026-11-08 定稿）

| 情况 | 做法 | 耗时 |
|---|---|---|
| 普通网页 | `./sf URL`（默认 auto：cffi 优先） | 1-3s |
| SSR 注水数据 | `--embedded` | 0.8s |
| 数据在接口里 | `--discover` 列候选 → 判断 → `--discover "子串" --verify` | 10s（一次性） |
| 要登录 | `--login` 存会话；之后按域名**自动匹配** | — |
| 真需要 JS | `--via browser` | 8-10s |

## 输出暴露度（省 token）

```bash
./sf URL -L brief     # ~800 字，不附链接表
./sf URL -L normal    # ~5000 字（默认）
./sf URL -L full      # 不截断
./sf URL -m 20000     # 自定义
```

`--json` 输出里带 `chars` 和 `truncated`，方便判断要不要放宽。

## 接口库（只作参考）

```bash
./sf URL --via browser --discover "brief/grid" --verify --remember   # 记一条
./sf --sites            # 查看
./sf --sites cnki       # 按域名过滤
```

抓到已知域名的页面时会提示「接口库中有记录」，**但不自动发请求**——过期配方当真理比没有更糟。连续失败 3 次自动标 `stale`。

## Cookie 规则（主人 2026-11-08 定）

**能用完整 cookie 就用完整的，不要挑单个名。**

- `storage_state`（`-s xxx.json`）里的 cookie **全部带上**，不要只挑 `Ecp_ClientId` 这类"看起来关键的"
- 跨域同名去重，取第一个
- 中文值自动 URL 编码（urllib 只接受 latin-1）
- 理由：挑单个省不了多少流量，但一旦漏掉会话 cookie，就会出现"明明登录了还是拿不到数据"这种最难查的问题

## 输出形态：默认 markdown，-j 才给原始 JSON

所有探测类结果都先清洗成 AI 易读的紧凑 markdown：

| 模式 | 默认输出 | `-j` |
|---|---|---|
| `--embedded` | 结构大纲（key-path + 类型 + 大小）+ 预览 | 原始 JSON |
| `--discover` | 候选接口表格（方法/路径/状态/大小/JSON） | 原始 JSON |
| `--verify` | 回放状态 + 耗时 + 片段 | 原始 JSON |
| `--eval` | JSON 代码块（≤800 字） | 原始 JSON |
| 正文抓取 | markdown + 链接代号（`@ns-1`） | 结构化 JSON |

实测体积：`--discover` 6612 → **336 字节**，`--embedded` 7709 → **807 字节**。

需要全量数据时用 `-o file.json` 落盘——**不经过上下文**，再写脚本读文件处理。

## 详细文档

- `README.md` — CLI 全量参数与示例
- `BENCHMARK.md` / `FETCH-BENCHMARK.md` — 与旧 Python 版的历史对比（存档）
- 参数速查：`sf --help`、`sv --help`、`sfl -h`

## 项目结构

```
super-fetch/
├── cmd/          sf / sv / sfl 入口
├── internal/     抓取引擎、解析、会话、链接库
├── bin/          编译产物
├── .data/        links.db + 会话/Cookie
└── bench-results/ 2026-11-09 压测截图与结论
```

## 数据存储

- 目录：本技能目录下 `.data/`（`SUPER_FETCH_HOME` 可覆盖，权限 0700）
- `links.db` - 链接代号映射
- `*.json` - 会话文件（权限 0600；**只有 `-i` 交互模式才会写回**，普通抓取只读）

## 抓 AJAX 接口（--xhr / --eval）

页面内容是 JS 异步加载时，别只抓 HTML。两个开关直接拿数据：

```bash
# 1) 捕获 URL 含 "brief/grid" 的 XHR 响应，附带请求体（可直接回放）
./sf "https://kns.cnki.net/kns8s/defaultresult/index?kw=碳中和" -s cnki.json -e playwright --xhr "brief/grid"

# 2) 在页面里执行 JS，返回结果（写爬虫的单步版）
./sf "https://example.com" -e playwright --eval "document.title"
```

输出都是 JSON：

- `--xhr` → `{"captured": [{"url","status","method","request_body","body"}], "count": n}`
- `--eval` → `{"result": <JS 返回值>}`

**两段式（推荐）**：先 `--xhr` 不带值 → 只返回候选清单（method/url/status/bytes/preview，不回大 body，省 token）；模型看完 URL 判断哪个是数据接口，再用 `--xhr "子串" --replay` 抓取并回放。

⚠️ 别用序号选（网络顺序每次不同，序号会漂）。用 URL 子串。

加 `--replay` 可以**一步验证**：抓到的请求立刻用 curl_cffi 回放，输出里带 `{"replay":{"status","seconds","bytes"}}`，直接看出"离了浏览器还能不能用、快多少"。

**标准流程**：先用 `--xhr` 找到接口和请求体 → 再用 curl/urllib 直连回放，比每次开浏览器快 15–25 倍。只有接口逆向不出来时才继续用 playwright。

两者都强制走有头浏览器（~10 秒），属于探索工具，别放批量里跑。

## 知网（CNKI）取原文（2026-11-08 实测）

登录态只需存一次：`./sf "https://kns.cnki.net/kns8s/" --login -s cnki.json`（弹窗登录后写回；之后 `*.cnki.net` **按域名自动匹配**，命令里不用再写 `-s`）。

拿一篇论文的 PDF 全文，三步：

```bash
# 1) 检索：直连 brief/grid 接口，0.5 秒，不开浏览器
~/agents/xiaoyu/tools/cnki.py "原油价格预测" --size 20

# 2) 详情页：自动带 cnki.json，cffi 约 6 秒（拿标题、作者、下载链）
./sf "<检索结果里的详情页 URL>" -L brief

# 3) 原文：详情页里 `https://bar.cnki.net/bar/download/order?id=...` 那条链
./sf "https://bar.cnki.net/bar/download/order?id=..." -o 论文.pdf     # 8 秒，766KB
```

一条命令版（检索 + 下第 N 条）：`~/agents/xiaoyu/tools/cnki.py "原油价格预测" --full 2 --out ~/papers`
—— 实测硕士论文 75 页 / 5.2MB，博士 170 页 / 23MB 都能落盘。

坑（都踩过）：

- **`pageSize` 只认 10 / 20 / 50**：传 5 会返回「字段【pageSize】校验失败」，看起来像查无数据。`cnki.py` 已自动吸附到最近的合法值。
- **未登录时下载链跳 `login.cnki.net`**，`-o x.pdf` 存下来的是 markdown 文本而不是 PDF。下完 `file x.pdf` 验一下。
- 详情页 URL 的 `v=...` 参数必须从检索结果原样带，拼不出来。
- 学位论文 PDF/CAJ 都有；**2000 年前后的老论文是扫描版**，`pdftotext` 出来是乱码，需要 OCR。
- 检索页本身对 headless 403（见上一节），所以检索走接口、别开浏览器；详情页和下载链用 cffi 就够。

## 接口型站点：发现策略（参考）

页面正文是骨架、数据在 XHR 里时，不要硬上浏览器：

1. `./sf "<url>" --discover` 列出候选接口
2. **自己判断**：哪个是数据接口（看 body 是条目还是埋点/配置）、分页参数是什么、要不要 Referer/登录
3. `./sf "<api-url>" --via http` 直接取数，需要 POST/特殊头时用 `--discover --verify` 回放

可能要多轮：列表接口 → 详情页再发现一次详情接口。

完整判断手册见 `STRATEGIES.md`。
