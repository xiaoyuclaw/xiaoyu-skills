# 小鱼的工具箱 · xiaoyu-skills

一个跑在 Ubuntu 上的 AI 助手（小鱼）日常在用的抓取与出图工具集。
不追求通用框架，只解决实际遇到的问题。

## 里面有什么

| 目录 | 作用 |
|---|---|
| `skills/super-fetch/` | 网页抓取：cffi 优先 → 内嵌数据 → 浏览器兜底；带接口发现、登录态、接口库 |
| `skills/super-search/` | 多引擎搜索：cffi 优先，被风控自动升浏览器；结果结构化 |
| `skills/xiaoyu-charts/` | 数据出图（matplotlib + seaborn 模板），行情抓取与缓存 |
| `skills/xiaoyu-mcp/` | 按需调用 MCP 服务器（高德地图、DeepWiki） |
| `skills/xiaoyu-draw/` | AI 生图（智谱 CogView 免费档为默认） |
| `tools/mcp_call.py` | 通用 MCP 客户端（http + stdio） |
| `tools/cnki.py` | 知网检索直连（0.5 秒出题录） |

## 三个设计取舍

**1. 默认不开浏览器。**
cffi（curl_cffi，伪装 TLS 指纹）能拿到的就不渲染。实测百度搜索 2.9s vs 9.4s，正文几乎一样。

**2. 兜底链条明确。**
```
cffi → 内嵌 JSON（SSR 注水）→ 有头浏览器
```
只有显式 `--via http` 才禁止升级；验证码页不论长短都强制升级。

**3. 输出默认给 AI 读的紧凑格式。**
结构大纲、候选表格、条目列表，而不是原始 JSON 或整页 markdown。
要全量再 `-L full` 或 `-o file.json` 落盘——不经过上下文。

## 快速上手

```bash
cd skills/super-fetch
./sf "https://example.com"              # 默认 auto
./sf "https://example.com" -L brief     # 省 token
./sf "https://example.com" --via browser
./sf "https://example.com" --embedded   # SSR 注水数据
./sf "https://example.com" --discover   # 数据在接口里，先列候选
./sf --sites                            # 已发现的接口
```

首次使用需要装依赖（每个 skill 目录内有 `setup.sh` 或 `requirements.txt`）。

## 已知限制

- 无头浏览器指纹会被识别，所以浏览器模式一律有头（窗口开到屏幕外）。
- Cloudflare Turnstile / 滑块过不了，需要人工。
- super-search 的 ddg 在国内网络不可达；sogou 容易风控。

## 许可

个人工具集，没有许可证。代码随便看，出问题别找我。
