---
name: super-search
description: 基于 super-fetch 的聚合搜索工具，提供通用搜索、学术、技术、社媒、金融等多种领域的专业搜索能力。
user-invocable: true
dependencies:
  - super-fetch
---

# Super Search

基于 super-fetch 的多领域搜索工具集。

> **本环境没有可用的内置 `web_search`**（它走 DeepSeek 官方搜索 API，需要有效 API key 或账号路由；
> 当前聊天路由是 opencode-go，工具被隐去）。所以搜索就这里：多引擎回退，免费。
>
> 只有 Go 版（2026-11-10 统一）：`./sv`、`svg`、`~/agents/xiaoyu/.agents/skills/super-fetch/bin/sv` 是同一个二进制；
> 数据（Cookie/链接库）和 super-fetch 共用 `../super-fetch/.data`。

## 结果抽取（2026-11-09 重构）

- **条目从 DOM 抽，不从 Markdown 正则猜**：每个引擎配一份块选择器
  （bing `li.b_algo`、baidu `.c-container`、sogou `.vrwrap`、360 `.res-list`、ddg `article[data-testid=result]`），
  块内取「标题链接 + 摘要」，URL 用真实 href。
- **通用兜底**：选择器没命中时，找「重复出现 ≥3 次、且内含外链标题」的兄弟块。
- **最后兜底**：一条都抽不出来时，直接把清洗后的 Markdown 输出给模型自己读（不再输出「没解析出条目」）。
- `--md` 强制输出 Markdown（附 `@代号 → 真实地址` 对照表），适合让模型直接读整页结果。
- `-j` 的 `items[]` 现在带 `url` 字段，可直接引用。

```bash
./sv "关键词" --md          # 直接给 Markdown + 链接对照
./sv "关键词" -j            # 结构化：title / snippet / code / url
```

验证状态与实测（2026-11-09）：

| 引擎 | 成功率（4 查询实测） | 状态 |
|---|---|---|
| **toutiao**（头条搜索） | 可用，1.3–2.5s | 中文默认第一：热点/价格类更新快；已过滤热榜卡（26-hotlist）、视频卡（26-video）、站内聚合页、无来源卡片；少于 2 条自动回退 bing |
| **bing** | 4/4，1.0–1.8s | 英文默认第一，中文第二 |
| baidu | 4/4，1.6–2.0s | 中文时效最好；URL 已从 `baidu.com/link` 解成真实地址（读 `mu` 属性） |
| ddg | 可用 | 改用 `html.duckduckgo.com/html/` 无 JS 接口，**不要预热**，URL 自动解 `uddg` |
| **shenma**（神马） | 可用但会软限流 | SSR 页面，结构 `.sc` + `.qk-title-text`；连续请求会返回「没有找到相关结果」，间隔几秒恢复 |
| sogou | 0/4 | 本机 IP 被判定（浏览器通道也是验证码），基本不可用 |
| 360 | 1/4 | 本机 IP 被判定（返回「访问异常页面」），不稳定 |

- **默认顺序**：中文 `bing → baidu → toutiao → 360 → sogou → shenma → ddg`；英文 `bing → baidu → ddg → 360 → sogou`。
- 神马的前端是 `qk-*` 类名，标题是 div、链接在同级 `a.qk-link-wrapper`；抽取器已支持
  「标题不是链接时往上/找同级链接」。夸克同一套前端但被阿里风控（punish + secaptcha），已移除。
  头条用于中文新鲜资讯，bing 是可靠性兜底。
- **广告/推广过滤**：按 class（`b_ad`/`result--ad`/`ec-tuiguang`/`e-ads`）、`data-tuiguang` 属性、
  以及「广告/推广/赞助」开头的文案剔除；「大家还在搜」「相关搜索」「进一步探索」这类卡片也过滤。
- 教训：**不要给 curl_cffi 手写 Accept/Accept-Language**——它会顶掉 impersonate 的默认头，
  ddg 会直接判人机。
- 头条链接是 `/search/jump?url=<zlink?h5_url=真实地址>`，纯字符串解码（`_unwrap_toutiao`），
  不用多发请求；解不出时保留跳转链接也能打开。

## 翻页与条数（2026-11-09 修正）

- **默认只抓首页**（0.4–0.8s），`-P/--pages N` 才拼接后续页，按 URL 去重，不做重排。
- `-n/--count` 控制最终条数（默认 10）。

各引擎翻页的实测情况：

| 引擎 | 参数 | 实测（2 页） |
|---|---|---|
| **bing** | `&first=11&FORM=PERE` | ✅ 19 条 |
| **baidu** | `&pn=10` | ✅ 19 条 |
| **toutiao** | `&offset=10` | ✅ 15 条 |
| **shenma** | `&page=N` | ⚠️ 能翻，但软限流时首页都可能空 |
| **ddg** | POST `/html/` 表单（`vqd` 令牌） | ⚠️ 经常 202 限流，时好时坏 |
| 360 | `&pn=N` | 未验证（IP 被判定） |
| sogou | `&page=N` | 未验证（IP 被判定） |

- bing 翻页必须：**同一会话**（先取首页拿 cookie）+ **Referer** + `www.bing.com`（`cn.bing.com` 会忽略 `first`）。
- bing 结果链接是 `bing.com/ck/a?u=a1<base64url>` 跳转，已实现解码，否则翻页结果会被站内链接过滤成 0 条。

```bash
sv "关键词"                    # 首页 + 默认 10 条
sv "关键词" -n 20              # 首页，最多 20 条
sv "关键词" -e bing -P 3       # bing 三页（~8s）
sv "关键词" -e baidu -P 2      # baidu 两页（~7s）
```

## 快速开始（推荐用 sv）

```bash
./sv "MiniMax H3 视频 API 价格"              # 默认按 bing → baidu → 360 顺序回退
./sv "关键词" -e baidu                        # 指定引擎
./sv "关键词" -e baidu,bing --json            # 多引擎顺序 + JSON 输出
./sv "关键词" -m 4000                         # 限制输出字符数
```

`sv` 会自动：

1. 读 `.data/cookies-<引擎>.json` 里的历史 cookie；
2. 没有 cookie 就先访问首页「预热」，让风控发身份 cookie（**百度不加这步会直接给广告页或安全验证**）；
3. 命中验证码就清 cookie 重试一次，仍失败则换下一个引擎；
4. 只抽取结果列表容器（`#b_results` / `#content_left` / `#main`），并砍掉「大家还在搜」「为您推荐」这类噪声。

### 两个必须知道的坑（本机实测）

**坑一：直接查结果页会被风控**
百度没有 `BAIDUID` 时会降级成广告页/安全验证。必须先访问首页预热拿 cookie，`sv` 已自动处理。

**坑二：headless 浏览器会被识别**
百度/搜狗/DuckDuckGo 对无头浏览器直接弹验证，`playwright-stealth`、真 Chrome 通道、注入 cookie 都没用。
解法是跑 **headful**：用真实显示器（`DISPLAY=:0`，窗口挪到屏幕外），验证立刻消失。

`sv` 会自动探测显示器（`DISPLAY` + `/tmp/.X11-unix/X0`），没有显示器时回退 cffi。

### 引擎可用性（2026-11，本机）

| 引擎 | playwright(headful) | cffi | 耗时 | 质量 |
|---|---|---|---|---|
| Bing | ✅ | ✅ | 0.4s / 7s | 干净，中文相关性一般 |
| 百度 | ✅ | ✅（需预热） | 1.1s / 8.6s | 中文最好 |
| 搜狗 | ✅ | ⚠️ 时好时坏 | 2.5s / 9.3s | 结果不错，反爬最凶 |
| 360 | ✅ | ✅ | 0.8s / 7s | SEO 噪声多 |
| DuckDuckGo | ✅ | ❌ | 7s | 能用，广告块多 |
| Google | ❌ | ❌ | - | 本机拿不到结果 |

### 为什么有些页面「没渲染就关了」

三个原因，现在都已处理：

1. **百度缺 cookie 时会返回一张几乎空白的「安全验证」页**（正文只有 59 字符），看起来就像没加载。
2. **playwright 自己访问首页预热没用**——实测先开百度首页再搜索仍会弹滑块；必须用 **cffi 预热拿 cookie，再注入浏览器**。这条已内置：无状态时自动 cffi 预热 → 注入 headful 上下文。
3. **固定等 1.2s 会抓早**。改成**稳定等待**：轮询 `document.body.innerText.length`，连续两次不变才收（最短 1s，最长 5s）。

因此耗时变成：冷启动（要预热）10-15s，热启动（有 cookie）6-7s。

### 速度优化（已内置）

| 手段 | 效果 |
|---|---|
| 拦掉 image/media/font | 少下几百 KB，快约 12% |
| 自适应等待（1.2s + 选择器 3s 兜底） | 替代固定 2s，快时更早返回 |
| 一次运行复用同一个浏览器 | 多引擎回退时省约 1.9s/个 |
| 复用 storage_state（有 cookie 就不再预热） | 省一次首页往返 |

实测：百度单次从 **8.6s → 5.8s**，多引擎回退 7.5s 出结果。

### 残留与隐私

- **临时 profile**：每次启动用 `/tmp/playwright_chromiumdev_profile-*`，正常关闭即删；异常被杀只留 4 KB 空壳，`sv` 启动时会清扫超过 1 小时的遗留。
- **不碰用户自己的 Chrome 配置**（`~/.config/google-chrome` 全程只读不写，我们用的是独立临时 profile）。
- **会话状态**：`super-fetch/.data/pwstate-<引擎>.json`，权限 600，里面是搜索站的匿名 cookie（`BAIDUID` 之类），不含账号密码。
- **一键清除**：`./sv --clear-state`
- **不截图、不录屏、不存 trace**。
- headful 窗口定位在屏幕外（-2400,-2400），但任务栏/Alt-Tab 里可能短暂出现。

### 通道怎么选

```bash
./sv "关键词"                  # 默认 auto：先 cffi（0.6-2s），被风控自动升级 playwright
                              # 引擎按语言自选：中文→百度，英文→Bing
./sv "关键词" --via playwright # 强制 headful 浏览器，最稳但 5-10s
./sv "关键词" --via cffi       # 最快，无显示器环境
./sv "关键词" --show           # 调试：把浏览器窗口显示出来（默认挪到屏幕外，不可见）
```

### 手工抓搜索页（老办法，仍可用）

```bash
cd ../super-fetch
./sf "https://cn.bing.com/search?q=关键词" -e cffi
```

## 搜索引擎速查表

| 类型 | 引擎 | URL 模板 | 推荐引擎 |
|------|------|----------|----------|
| 通用 | Bing | `https://cn.bing.com/search?q={q}` | cffi |
| 通用 | 百度 | `https://www.baidu.com/s?wd={q}` | cffi |
| 通用 | 搜狗 | `https://sogou.com/web?query={q}` | cffi |
| 通用 | 360 | `https://www.so.com/s?q={q}` | cffi |
| 新闻 | 百度新闻 | `https://news.baidu.com/ns?word={q}` | cffi |
| 图片 | Bing 图片 | `https://cn.bing.com/images/search?q={q}` | playwright |
| 视频 | Bing 视频 | `https://www.bing.com/videos/search?q={q}` | playwright |

## 使用规范

### 引擎选择

| 引擎 | 适用场景 |
|------|----------|
| `cffi`（默认）| 静态搜索结果页、速度优先 |
| `playwright` | 动态加载内容、需登录、有验证码 |

### 会话使用

```bash
# 无会话（默认）
sf "https://cn.bing.com/search?q=关键词"

# 使用默认会话
sf "https://cn.bing.com/search?q=关键词" -s

# 使用指定会话
sf "https://cn.bing.com/search?q=关键词" -s my_search.json
```

### 交互模式（处理验证码/登录）

```bash
sf "https://cn.bing.com/search?q=关键词" -i
```

## 领域搜索

### 学术搜索

| 平台 | 说明 | URL 模板 | 引擎 |
|------|------|----------|------|
| Arxiv | AI/物理/数学论文 | `https://arxiv.org/search/?query={q}` | cffi |
| PapersWithCode | 论文+代码 | `https://paperswithcode.com/search?q={q}` | cffi |
| Semantic Scholar | 语义搜索 | `https://www.semanticscholar.org/search?q={q}` | playwright |
| PubMed | 医学文献 | `https://pubmed.ncbi.nlm.nih.gov/?term={q}` | cffi |
| 知网 | 中文学术 | `https://kns.cnki.net/kns8s/defaultresult/index?kw={q}` | playwright |
| Google Scholar | 综合学术 | `https://scholar.google.com/scholar?q={q}` | playwright |

### 技术搜索

| 平台 | 说明 | URL 模板 | 引擎 |
|------|------|----------|------|
| GitHub | 代码仓库 | `https://github.com/search?q={q}&type=repositories` | playwright |
| StackOverflow | 技术问答 | `https://stackoverflow.com/search?q={q}` | cffi |
| V2EX | 中文社区 | `https://www.v2ex.com/?q={q}` | cffi |
| Hugging Face | AI 模型 | `https://huggingface.co/models?search={q}` | playwright |
| GitHub Issues | 问题搜索 | `https://github.com/search?q={q}&type=issues` | playwright |
| Read the Docs | 文档 | `https://readthedocs.org/search/?q={q}` | cffi |

### 社媒搜索

| 平台 | 说明 | URL 模板 | 引擎 |
|------|------|----------|------|
| 知乎 | 专业问答 | `https://www.zhihu.com/search?type=content&q={q}` | playwright |
| 小红书 | 生活百科 | `https://www.xiaohongshu.com/search_result?keyword={q}` | playwright |
| 微博 | 热点话题 | `https://s.weibo.com/weibo?q={q}` | cffi |
| YouTube | 视频 | `https://www.youtube.com/results?search_query={q}` | playwright |
| Bilibili | 视频 | `https://search.bilibili.com/all?keyword={q}` | playwright |
| 微信搜狗 | 公众号 | `https://weixin.sogou.com/weixin?type=2&query={q}` | cffi |

### 金融搜索

| 平台 | 说明 | URL 模板 | 引擎 |
|------|------|----------|------|
| 雪球 | A股/美股讨论 | `https://xueqiu.com/k?q={q}` | cffi |
| 东方财富 | 公告/财报 | `https://so.eastmoney.com/Search/Index?keyword={q}` | cffi |
| Yahoo Finance | 全球金融 | `https://finance.yahoo.com/lookup?s={q}` | playwright |
| 同花顺 | 行情 | `http://www.10jqka.com.cn/search?query={q}` | cffi |
|  TradingView | 图表 | `https://www.tradingview.com/search/?q={q}` | playwright |

### 购物/电商搜索

| 平台 | 说明 | URL 模板 | 引擎 |
|------|------|----------|------|
| 淘宝 | 综合电商 | `https://s.taobao.com/search?q={q}` | playwright |
| 京东 | 3C/家电 | `https://search.jd.com/Search?keyword={q}` | playwright |
| 拼多多 | 性价比 | `https://mobile.yangkeduo.com/search_result.html?search_key={q}` | playwright |
| 亚马逊 | 全球购 | `https://www.amazon.cn/s?k={q}` | cffi |

### 生活服务搜索

| 平台 | 说明 | URL 模板 | 引擎 |
|------|------|----------|------|
| 美团 | 本地生活 | `https://i.meituan.com/` | playwright |
| 大众点评 | 商家评价 | `https://www.dianping.com/search/keyword/0/0_{q}` | playwright |
| 携程 | 旅游/酒店 | `https://so.ctrip.com/hotels/?keyword={q}` | playwright |
| 12306 | 火车票 | `https://kyfw.12306.cn/otn/leftTicket/init` | playwright |
| 高德地图 | POI | `https://www.amap.com/search?query={q}` | playwright |

## 使用示例

### 示例 1: 快速搜索（静态页）

```bash
sf "https://cn.bing.com/search?q=python+教程" -e cffi
```

### 示例 2: 动态搜索（Playwright）

```bash
sf "https://github.com/search?q=python&type=repositories" -e playwright -w 5
```

### 示例 3: 带登录搜索

```bash
# 首次交互登录
sf "https://www.zhihu.com/search?q=AI" -i

# 后续使用会话
sf "https://www.zhihu.com/search?q=AI" -s
```

## 参数速查

| 参数 | 说明 | 搜索场景 |
|------|------|----------|
| `-e cffi` | 快速引擎 | 通用/新闻/静态页面 |
| `-e playwright` | JS 渲染引擎 | 动态内容/社媒/学术 |
| `-w 5` | 等待 5 秒 | 复杂动态页面 |
| `-i` | 交互模式 | 需要登录/验证码 |
| `-s` | 使用默认会话 | 复用之前的登录态 |
| `-s my.json` | 使用指定会话 | 多账号切换 |
| `-f` | 全量模式 | 需要完整内容时 |

## 数据存储

- **目录**: super-fetch 技能目录下的 `.data/`（跟随技能，不写死绝对路径）
- 与 super-fetch 共享会话和链接数据库

## AI 新闻搜索（重点）

AI 新闻需要多维度分层搜索，**重点关注：新模型、新产品、新应用动向**。

### AI 新闻搜索维度

#### A. 新模型发布（最高优先级 ⭐⭐⭐⭐⭐）

```bash
# OpenAI 模型动态
sf "https://www.bing.com/news/search?q=OpenAI+model+release+site:openai.com" -e playwright -w 3

# Anthropic 模型动态
sf "https://www.bing.com/news/search?q=Anthropic+Claude+model" -e playwright -w 3

# Google AI 模型动态
sf "https://www.bing.com/news/search?q=Google+DeepMind+AI+model+Gemini" -e playwright -w 3

# Meta AI 模型动态
sf "https://www.bing.com/news/search?q=Meta+AI+Llama+model+release" -e playwright -w 3

# Mistral AI 模型动态
sf "https://www.bing.com/news/search?q=Mistral+AI+model+release" -e playwright -w 3

# 国内大模型（百度/阿里/字节/DeepSeek）- 用 Bing 国际版搜索中文
sf "https://cn.bing.com/search?q=文心一言+OR+通义千问+OR+豆包+OR+DeepSeek+发布+2026" -e playwright -w 3
```

#### B. 新产品/新应用发布（最高优先级 ⭐⭐⭐⭐⭐）

```bash
# Product Hunt 新品
sf "https://www.producthunt.com/" -e playwright -w 5
sf "https://www.producthunt.com/search?q=AI" -e playwright -w 3

# AI 产品发布
sf "https://www.bing.com/news/search?q=AI+product+launch+2026" -e playwright -w 3

# AI 应用爆款
sf "https://www.reddit.com/r/MachineLearning/" -e playwright -w 3
```

#### C. 社区热度/病毒传播（关键维度）

```bash
# Reddit AI 热点
sf "https://www.reddit.com/search/?q=AI+viral+OR+AI+trending" -e playwright -w 3

# Hacker News AI
sf "https://news.ycombinator.com/" -e playwright -w 3

# GitHub Trending AI
sf "https://github.com/trending" -e playwright -w 5
```

#### D. 大厂动态/产品发布

```bash
# Bing AI 新闻
sf "https://www.bing.com/news/search?q=AI+product+launch+2026" -e playwright -w 3

# 搜狗 AI 新闻
sf "https://www.sogou.com/web?query=AI+大模型+发布+site:news.sina.com.cn" -e cffi
```

#### E. 垂直媒体/专业来源

```bash
# 36氪 AI
sf "https://www.36kr.com/search/articles/AI" -e playwright -w 3

# 机器之心
sf "https://www.jiqizhixin.com/" -e playwright -w 3

# Bing AI 专题
sf "https://www.bing.com/news/search?q=AI+model+release+China" -e playwright -w 3
```

### AI 新闻输出格式

```
## AI 新闻速递（YYYY-MM-DD）

### 🆕 新模型 / ⭐⭐⭐⭐⭐
1. [模型名称] - [一句话描述]
   > 🔗 来源 | 发布时间

### 🚀 新产品 / ⭐⭐⭐⭐
2. [产品名称] - [一句话描述]
   > 🔗 来源 | 发布时间

### 💡 新应用 / ⭐⭐⭐
3. [应用名称] - [一句话描述]
   > 🔗 来源 | 发布时间

### 🔥 社区热点 / ⭐⭐⭐⭐
4. [热点内容] - [一句话描述]
   > 🔗 来源 | 热度指标
```

### 热度判断标准（新模型/产品/应用优先）

| 类型 | 热度 | 说明 |
|------|------|------|
| **新模型发布** | ⭐⭐⭐⭐⭐ | OpenAI/Anthropic/Google/Meta/国内大厂 |
| **新产品发布** | ⭐⭐⭐⭐⭐ | Product Hunt 热门、AI 应用爆款 |
| **新应用上线** | ⭐⭐⭐⭐ | AI 落地案例、行业应用 |
| **大厂官宣** | ⭐⭐⭐⭐ | 大厂 AI 动态 |
| **社区病毒传播** | ⭐⭐⭐⭐ | GitHub star暴涨、刷屏 |
| **论文/研究突破** | ⭐⭐⭐ | 学术进展 |

### AI 新闻源速查

| 来源 | 类型 | URL | 引擎 |
|------|------|-----|------|
| 机器之心 | 垂直媒体 | https://www.jiqizhixin.com/ | playwright |
| 36氪 | 科技媒体 | https://www.36kr.com/ | playwright |
| Reddit r/MachineLearning | 社区 | https://www.reddit.com/r/MachineLearning/ | playwright |
| Hacker News | 社区 | https://news.ycombinator.com/ | cffi |
| GitHub Trending | 开源趋势 | https://github.com/trending | playwright |
| Product Hunt | 产品发布 | https://www.producthunt.com/ | playwright |
| 百度 AI 新闻 | 搜索聚合 | https://www.baidu.com/s?wd=AI+新闻 | playwright |

## 详细领域文档

详见 references/ 目录：

- [学术搜索](./references/academic.md) - Arxiv、PapersWithCode、PubMed、知网等
- [技术搜索](./references/tech.md) - GitHub、StackOverflow、Hugging Face 等
- [社媒搜索](./references/social.md) - 知乎、小红书、微博、B站等
- [金融搜索](./references/finance.md) - 雪球、东方财富、Yahoo Finance 等
- [购物搜索](./references/shopping.md) - 淘宝、京东、拼多多、亚马逊等
- [生活服务](./references/lifestyle.md) - 美团、大众点评、携程、12306 等
- [新闻搜索](./references/news.md) - 百度新闻、新浪、网易、澎湃等

## 什么时候比内置 search 强（2026-11-08）

内置 search 按次计费；super-search 本地免费，代价是慢。所以：

| 场景 | 选 |
|---|---|
| 偶尔查、要质量 | 内置 search（付费但准） |
| 反复试查询词、批量 | **super-search（免费）** |
| 要指定引擎 / 要原始结果 | **super-search** |

```bash
./sv "查询词" --via http        # 纯 HTTP，最快
./sv "查询词" --via browser     # 需要 JS 时
./sv "查询词" -L brief          # 只要前 800 字，省 token
./sv "查询词" -j                # 结构化 JSON
```

`--via` 支持 `http`（=cffi）/ `browser`（=playwright）/ `auto`。

## 输出形态（2026-11-08 统一）

默认给**紧凑列表**，不是原始 markdown 大段文本：

```
## baidu ｜ 油价预测 ｜ auto→cffi ｜ 1.43s

1. **标题** [@ns-1]
   摘要前 110 字…
```

| 级别 | 条数 | 每条摘要 | 实测体积 |
|---|---|---|---|
| `-L brief` | 5 | 50 字 | ~0.6 KB |
| `-L normal`（默认） | 10 | 110 字 | ~2.4 KB |
| `-L full` | 20 | 400 字 | ~4.2 KB |
| `-j` | 同档 | 结构化 `items[]` | `{"engine","items":[{"title","snippet","code"}]}` |
| `-j --raw` | 同档 | 额外带 `content` 原文 | 调试用 |

链接代号（`@ns-1`）保留，需要细节时再用 sf 抓对应页面，不用一次把全文塞进上下文。
