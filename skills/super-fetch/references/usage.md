# 常见用法

## 单 URL 抓取

```bash
# 基础抓取（输出 Markdown，链接用代号表示）
python fetch.py https://example.com

# 等待 JS 渲染
python fetch.py https://example.com -w 5

# 全量提取（不过滤广告等噪音）
python fetch.py https://example.com --full

# 使用代理
python fetch.py https://example.com -p http://127.0.0.1:7890
```

## 文件下载

```bash
# 下载文件（二进制必须用 cffi 引擎）
python fetch.py https://example.com/file.pdf -e cffi -o ./file.pdf
python fetch.py https://example.com/image.png -e cffi -o ./image.png
```

## 引擎选择

| 引擎 | 速度 | 适用场景 |
|------|------|----------|
| `playwright`（默认） | 中 | JS 渲染、动态内容、需要登录 |
| `cffi` | 快 | 纯静态页面、API、文件下载 |
