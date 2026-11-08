# 社媒搜索

## 知乎（专业问答）

```bash
python fetch.py "https://www.zhihu.com/search?type=content&q=关键词" -e playwright -w 5
```

## 小红书（生活百科）

```bash
python fetch.py "https://www.xiaohongshu.com/search_result?keyword=关键词" -e playwright -w 5
```

## 微博（热点话题）

```bash
python fetch.py "https://s.weibo.com/weibo?q=关键词" -e cffi
```

## YouTube（视频）

```bash
python fetch.py "https://www.youtube.com/results?search_query=关键词" -e playwright -w 5
```

## Bilibili（视频）

```bash
python fetch.py "https://search.bilibili.com/all?keyword=关键词" -e playwright -w 5
```

## 微信搜狗（公众号）

```bash
python fetch.py "https://weixin.sogou.com/weixin?type=2&query=关键词" -e cffi
```
