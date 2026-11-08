# 批量抓取

## 基本用法

```bash
# 直接传入多个 URL
python fetch.py https://url1.com https://url2.com https://url3.com

# 从文件读取（每行一个 URL）
cat > urls.txt << 'EOF'
https://example.com/page1
https://example.com/page2
https://example.org/article
EOF
python fetch.py -F urls.txt

# 导出结果到 JSON
python fetch.py -F urls.txt -o results.json
```

## 调优参数

| 参数 | 默认 | 说明 |
|------|------|------|
| `-c` | 5 | 并发数（防爬严就降，反之升） |
| `--domain-delay-min` | 2.0 | 同域名最小间隔（秒） |
| `--domain-delay-max` | 5.0 | 同域名最大间隔（秒） |

```bash
# 保守模式（防爬严格）
python fetch.py -F urls.txt -c 2 --domain-delay-min 3 --domain-delay-max 6

# 激进模式（内部网站）
python fetch.py -F urls.txt -c 20 --domain-delay-min 0.5 --domain-delay-max 1
```

## 引擎选择

| 引擎 | 并发方式 | 适用场景 |
|------|---------|----------|
| `playwright`（默认） | 单 Browser + 多 Context | JS 渲染、动态内容 |
| `cffi` | AsyncSession 原生并发 | 纯静态页面、API |

```bash
# playwright（默认）
python fetch.py -F urls.txt

# cffi
python fetch.py -F urls.txt -e cffi
```

## 输出格式

批量模式输出 JSON：

```json
{
  "total": 3,
  "success": 2,
  "failed": 1,
  "results": [
    {
      "url": "https://example.com",
      "success": true,
      "title": "页面标题",
      "content": "Markdown 内容",
      "namespace": "abc123",
      "error": ""
    }
  ]
}
```
