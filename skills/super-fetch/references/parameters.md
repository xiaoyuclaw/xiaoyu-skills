# 参数参考

## 通用参数

| 参数 | 简写 | 默认 | 说明 |
|------|------|------|------|
| `--engine` | `-e` | playwright | 引擎：playwright / cffi |
| `--full` | `-f` | false | 全量提取，不过滤 |
| `--session` | `-s` | - | 会话文件 |
| `--wait` | `-w` | 3 | 渲染等待秒数 |
| `--proxy` | `-p` | - | 代理地址 |
| `--retries` | `-r` | 2 | 重试次数 |
| `--output` | `-o` | - | 单 URL：下载文件 / 批量：JSON |
| `--max-chars` | `-m` | 50000 | 最大字符数（仅单 URL） |

## 仅单 URL

| 参数 | 简写 | 默认 | 说明 |
|------|------|------|------|
| `--interactive` | `-i` | false | 交互模式，弹出浏览器 |

## 仅批量

| 参数 | 简写 | 默认 | 说明 |
|------|------|------|------|
| `--file` | `-F` | - | URL 文件路径 |
| `--concurrency` | `-c` | 5 | 并发数 |
| `--domain-delay-min` | - | 2.0 | 同域名最小间隔（秒） |
| `--domain-delay-max` | - | 5.0 | 同域名最大间隔（秒） |
| `--jitter` | - | 0.5 | 全局随机抖动上限（秒） |
| `--silent` | - | false | 静默模式，只输出 JSON |

## 模式判断

```bash
# 单 URL 模式
python fetch.py https://example.com
python fetch.py https://example.com -i

# 批量模式（多个 URL 或 -F 触发）
python fetch.py https://url1.com https://url2.com
python fetch.py -F urls.txt
```

## 数据存储

- 目录：`~/.agents/super-fetch/`
- `links.db` - 链接代号映射
- `*.json` - 会话文件
