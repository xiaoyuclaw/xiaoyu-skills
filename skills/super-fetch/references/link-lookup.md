# 链接反查

## 为什么需要反查

抓取微博/知乎等平台搜索结果时，链接会变成代号（如 `@abc-12`），需反查获取真实 URL。

## 基本用法

```bash
# 反查单个
python get_link.py @abc-12

# 反查多个
python get_link.py @abc-12 @abc-15 @abc-20
```

## 完整流程

```bash
# 1. 抓取搜索结果
python fetch.py "https://s.weibo.com/weibo?q=关键词" -s session.json -w 5

# 2. 输出中链接是 @abc-12 这样的代号

# 3. 反查获取真实 URL
python get_link.py @abc-12
# 输出：@abc-12 -> https://weibo.com/1234567890

# 4. 用真实 URL 访问
python fetch.py "https://weibo.com/1234567890" -s session.json -w 5
```

## 数据库清理

代号 `@{namespace}-{number}` 中的 namespace 是每次抓取随机生成的，旧的代号在新的抓取中可能失效。

```bash
# 清理全部（谨慎使用）
python get_link.py --clear

# 删除单个
python get_link.py --clear @abcd-1

# 删除某命名空间下所有链接
python get_link.py --clear abcd
```

> **注意**：请在查询完链接后及时清理数据库。`links.db` 会自动清理 7 天前的旧数据（5% 概率触发），但主动清理更安全。
