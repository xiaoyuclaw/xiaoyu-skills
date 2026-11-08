# 新闻搜索

## 通用新闻搜索

### 百度新闻（推荐）

```bash
python fetch.py "https://news.baidu.com/ns?word=关键词" -e cffi
```

### 新浪新闻

```bash
python fetch.py "https://search.sina.com.cn/?q=关键词" -e cffi
```

### 网易新闻

```bash
python fetch.py "https://news.163.com/search/" -e cffi
```

### 搜狐新闻

```bash
python fetch.py "https://search.sohu.com/?keyword=关键词" -e cffi
```

### 澎湃新闻

```bash
python fetch.py "https://www.thepaper.cn/search_result?query=关键词" -e cffi
```

## AI/科技垂直媒体

### 机器之心（推荐 AI 新闻）

```bash
python fetch.py "https://www.jiqizhixin.com/search?keyWord=AI" -e playwright -w 3
```

### 36氪

```bash
python fetch.py "https://www.36kr.com/" -e playwright -w 3
python fetch.py "https://www.36kr.com/search/articles/AI" -e playwright -w 3
```

### 品玩

```bash
python fetch.py "https://www.pingwest.com/search?keyword=AI" -e playwright -w 3
```

### 量子位

```bash
python fetch.py "https://www.qbitai.com/search?keyword=AI" -e playwright -w 3
```

## 国际科技媒体

### TechCrunch

```bash
python fetch.py "https://techcrunch.com/search/AI/" -e playwright -w 3
```

### The Verge

```bash
python fetch.py "https://www.theverge.com/search?q=AI" -e playwright -w 3
```

### Ars Technica

```bash
python fetch.py "https://arstechnica.com/?s=AI" -e cffi
```

## 英文 AI 新闻

### Bing News AI

```bash
python fetch.py "https://www.bing.com/news/search?q=AI+news" -e playwright -w 3
```

### Google News AI

```bash
python fetch.py "https://news.google.com/search?q=AI&hl=en" -e playwright -w 3
```

## 搜索技巧

1. **时间筛选**：在搜索词后加 `2026` 或 `today` 限定时间
2. **多关键词**：用 `+` 连接，如 `AI+大模型+发布`
3. **热点追踪**：同时搜索 `AI+爆火`、`AI+viral`、`AI+trending`
4. **组合搜索**：`site:zhihu.com AI` 可限定网站

## 输出格式建议

```
## 新闻速递（YYYY-MM-DD）

1. [新闻标题]
   > 一句话摘要（不超过50字）
   > 🔗 来源名称 | 发布时间
```
