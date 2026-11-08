# 会话与登录

## 会话规则

| 命令 | 说明 |
|------|------|
| `python fetch.py <url>` | 独立会话，无状态 |
| `python fetch.py <url> -s` | 使用默认会话 `session.json` |
| `python fetch.py <url> -s my.json` | 使用指定会话 |
| `python fetch.py <url> -i` | 交互模式，弹出浏览器 |
| `python fetch.py <url> -i -s my.json` | 交互模式，保存到指定会话 |

## 交互模式（推荐）

适用于需要登录或验证码的场景。

```bash
# 1. 弹出浏览器，手动完成登录
python fetch.py https://example.com -i

# 2. 右上角点击"✅ 完成操作"保存会话

# 3. 后续自动使用会话
python fetch.py https://example.com/private -s
```

## 导入已有 Cookie

适用于无法本地打开浏览器、或使用他人登录态的场景。

1. 安装浏览器插件 [Cookie-Editor](https://cookie-editor.com/)
2. 登录目标网站，导出 JSON
3. 保存到 `~/.agents/super-fetch/my_cookies.json`
4. 使用：`python fetch.py https://example.com -s my_cookies.json`

### 支持的 Cookie 格式

- Playwright 原生格式（含 `cookies` 和 `origins`）
- Cookie-Editor 导出的列表格式
- 简单键值对格式 `{"key": "value"}`

## 使用规范

| 场景 | 推荐做法 |
|------|----------|
| 日常抓取 | 不使用 `-s`，独立会话 |
| 单网站长期使用 | 用 `-i` 创建会话后用 `-s` |
| 多网站/多账号 | 用 `-i -s site_a.json`、`-i -s site_b.json` |
| 共享登录态 | 用 Cookie-Editor 导出 JSON 分享 |
| 批量带会话 | `fetch.py -F urls.txt -s session.json` |

## 注意事项

- 会话文件包含敏感 Cookie，请勿提交到 Git
- 定期重新登录，Cookie 可能过期
- 不同网站使用不同的会话文件，避免 Cookie 污染
- 批量抓取时，所有 URL 共享同一个会话
