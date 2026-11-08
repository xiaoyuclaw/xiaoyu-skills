---
name: super-fetch
description: 高性能网页抓取工具，将网页转换为干净的 Markdown。支持 JavaScript 渲染、会话保持、登录态保存、批量并发抓取。跨平台 Windows/macOS/Linux。
---

# Super Fetch

高性能网页抓取工具，支持 JS 渲染、登录态保存、批量并发。

## 安装（自包含）

所有依赖和数据都在技能目录内，不写死任何绝对路径：

```bash
cd <技能目录>
./setup.sh            # 建 .venv 并装 requirements.txt + Playwright 浏览器
./sf <url>            # 用技能自带 .venv 运行（没有则退回系统 python3）
./sf-link @abcd-1     # 链接代号反查
```

目录结构：

```
<技能目录>/
├── .venv/       依赖（gitignore）
├── .data/       数据：links.db、会话文件（gitignore）
└── *.py
```

数据目录可用 `SUPER_FETCH_HOME` 覆盖。

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
python fetch.py https://example.com -s

# 批量抓取多个网页（2 个及以上 URL 才进批量模式）
python fetch.py https://example.com/page1 https://example.com/page2 -s
# 注意：只传 1 个 URL 会走单 URL 模式；批量请传 ≥2 个或用 -F

# 从文件读取 URL 列表并保存结果到results.json（多URL时使用-o保存查询结果到json）
python fetch.py -F urls.txt -o results.json

# 需要用户手动交互时弹出浏览器手动操作（如登录/验证码），登陆状态默认保存到默认会话中
python fetch.py https://example.com -i

# 通过链接代号查询真实URL
python get_link.py @abcd-123

# 按照命名空间清理数据库
python get_link.py --clear abcd

# 单 URL 输出 JSON / 写入文件 / 附链接对照
python fetch.py https://example.com --json
python fetch.py https://example.com -o out.md
python fetch.py https://example.com --links

# 下载文件（单URL时使用-o下载文件）
python fetch.py "https://avatars.githubusercontent.com/u/252820863?s=48&v=4" -o "img.jpg" 
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
python fetch.py https://example.com         # 独立会话
python fetch.py https://example.com -s      # 使用默认会话
python fetch.py https://example.com -i      # 创建会话（弹出浏览器）
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

- [安装指南](references/installation.md) - uv/pip 安装、Playwright 浏览器
- [常见用法](references/usage.md) - 单 URL 抓取、文件下载、引擎选择
- [批量抓取](references/batch.md) - 并发配置、调优参数、输出格式
- [会话与登录](references/session.md) - 交互模式、Cookie 导入
- [链接反查](references/link-lookup.md) - 代号反查、数据库清理
- [参数参考](references/parameters.md) - 完整参数说明

## 项目结构

```
super-fetch/
├── fetch.py           # 统一入口
├── fetch_engines.py    # 抓取引擎
├── fetch_parser.py     # HTML 解析
├── core.py             # 共享工具
├── get_link.py        # 链接反查
└── references/         # 详细文档
    ├── installation.md
    ├── usage.md
    ├── batch.md
    ├── session.md
    ├── link-lookup.md
    └── parameters.md
```

## 数据存储

- 目录：`~/.agents/super-fetch/`（可用环境变量 `SUPER_FETCH_HOME` 覆盖，权限 0700）
- 从旧版迁移：原目录 `~/.openclaw/super-fetch/` 不再使用，历史链接记录不会自动搬过来
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
