# 安装

## 环境要求

- Python 3.12+
- uv 或 pip

## 安装依赖

### 使用 uv（推荐）

```bash
cd super-fetch
uv pip install playwright playwright_stealth curl_cffi beautifulsoup4 markdownify
```

### 使用 pip

```bash
cd super-fetch
pip install playwright playwright_stealth curl_cffi beautifulsoup4 markdownify
```

## 安装 Playwright 浏览器

```bash
# playwright 需要安装浏览器驱动
playwright install chromium
```

## 数据目录

首次运行时会自动创建数据目录：

- **Linux/macOS**: `~/.agents/super-fetch/`
- **Windows**: `%USERPROFILE%\.openclaw\super-fetch\`

存储内容：
- `links.db` - 链接代号映射数据库
- `session.json` - 默认会话文件
- `*.json` - 自定义会话文件
