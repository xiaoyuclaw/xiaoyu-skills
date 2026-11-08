#!/usr/bin/env python3
"""把搜索结果整理成紧凑列表：标题 + 摘要片段 + 链接代号。"""
import re

HEAD = re.compile(r'^###\s+\[?(.+?)\]?(?:\((@[\w\-]+)\))?\s*$')


def _clean(s):
    s = re.sub(r'!\[[^\]]*\]\(@[\w\-]+\)', '', s)          # 去掉图片
    s = re.sub(r'\[([^\]]*)\]\(@[\w\-]+\)', r'\1', s)
    s = re.sub(r'[*_]{1,2}', '', s)
    return re.sub(r'\s+', ' ', s).strip()


def parse_items(markdown):
    items, cur = [], None
    for line in markdown.splitlines():
        m = HEAD.match(line.strip())
        if m:
            if cur:
                items.append(cur)
            title = _clean(m.group(1))
            if title:
                cur = {'title': title, 'code': m.group(2) or '', 'body': []}
        elif cur is not None and line.strip():
            cur['body'].append(line.strip())
    if cur:
        items.append(cur)
    return [i for i in items if i['title']][:20]


def md_results(result, level='normal'):
    n = {'brief': 5, 'normal': 10, 'full': 20}[level]
    snip = {'brief': 50, 'normal': 110, 'full': 400}[level]
    items = parse_items(result.get('content', ''))
    head = "## %s ｜ %s ｜ %s ｜ %ss" % (
        result.get('engine', '?'), result.get('query', ''),
        result.get('via', ''), result.get('elapsed', ''))
    if not items:
        return head + "\n\n（没解析出条目，用 -j 看原始内容）"
    out = [head, ""]
    for i, it in enumerate(items[:n], 1):
        body = _clean(' '.join(it['body']))[:snip]
        tail = (" [%s]" % it['code']) if it['code'] else ""
        out.append("%d. **%s**%s" % (i, it['title'][:80], tail))
        if body:
            out.append("   %s" % body)
    if len(items) > n:
        out.append("")
        out.append("…还有 %d 条，用 -L full 或 -j" % (len(items) - n))
    return "\n".join(out)


def items(result, limit=20, snip=200):
    """结构化条目，供 -j 输出。"""
    out = []
    for it in parse_items(result.get('content', ''))[:limit]:
        out.append({
            'title': it['title'][:120],
            'snippet': _clean(' '.join(it['body']))[:snip],
            'code': it['code'],
        })
    return out
