#!/usr/bin/env python3
"""知网检索（直连 brief/grid 接口，不用浏览器）。

用法: ./cnki.py "油价预测" [--size 20] [--json]
"""
import json, os, sys, urllib.parse, urllib.request
from urllib.parse import quote
from bs4 import BeautifulSoup

STATE = os.path.expanduser('~/.agents/super-fetch/cnki.json')
UA = ('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 '
      '(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36')
KUA = 'YSTT4HG0,LSTPFY1C,EMRPGLPA,JUP3MUPD,MPMFIG1A,WQ0UVIAA,BLZOG7CK,PWFIRAGL,NN3FJMUV,NLBO1Z6R'


def cookies():
    """带完整 cookie（主人要求：不挑单个，整包带上更省心）。"""
    d = json.load(open(STATE))
    parts, seen = [], set()
    for c in d['cookies']:
        if 'cnki' not in c.get('domain', ''):
            continue
        k, v = c['name'], str(c['value'])
        if k in seen:            # 同名跨域去重，取第一个
            continue
        seen.add(k)
        if any(ord(ch) > 127 for ch in v):
            v = quote(v, safe='')
        parts.append('%s=%s' % (k, v))
    return '; '.join(parts)
    out = []
    for c in d['cookies']:
        if 'cnki' not in c['domain']:
            continue
        v = str(c['value'])
        if any(ord(ch) > 127 for ch in v):      # urllib 只能发 latin-1，中文值转码
            v = quote(v, safe='')
        out.append('%s=%s' % (c['name'], v))
    return '; '.join(out)


def search(kw, size=20, page=1):
    qj = {"Platform": "", "Resource": "CROSSDB", "Classid": "WD0FTY92", "Products": "",
          "QNode": {"QGroup": [{"Key": "Subject", "Title": "", "Logic": 0, "Items": [
              {"Field": "SU", "Value": kw, "Operator": "TOPRANK", "Logic": 0,
               "Vector": "", "Title": "主题"}], "ChildItems": []}]},
          "ExScope": 1, "SimpTrad": "0", "SearchType": 2, "Rlang": "CHINESE",
          "KuaKuCode": KUA, "Expands": {}, "View": "changeDBCh", "SearchFrom": 1}
    body = urllib.parse.urlencode({
        'boolSearch': 'true', 'QueryJson': json.dumps(qj, ensure_ascii=False),
        'pageNum': page, 'pageSize': size, 'dstyle': 'listmode',
        'aside': '(主题：%s)' % kw, 'searchFrom': '资源范围：总库', 'CurPage': page})
    req = urllib.request.Request('https://kns.cnki.net/kns8s/brief/grid', data=body.encode(),
        headers={'User-Agent': UA, 'Cookie': cookies(),
                 'Referer': 'https://kns.cnki.net/kns8s/defaultresult/index',
                 'X-Requested-With': 'XMLHttpRequest',
                 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8'})
    with urllib.request.urlopen(req, timeout=25) as r:
        return r.read().decode('utf-8', 'replace')


def parse(html):
    soup = BeautifulSoup(html, 'lxml')
    m = soup.select_one('#countPageDiv em')
    total = m.get_text(strip=True) if m else '?'
    out = []
    for tr in soup.select('table.result-table-list tbody tr'):
        tds = tr.find_all('td')
        if len(tds) < 6:
            continue
        a = tr.select_one('td.name a')
        if not a:
            continue
        out.append({
            'title': a.get_text(strip=True),
            'authors': tds[2].get_text(' ', strip=True) if len(tds) > 2 else '',
            'source': tds[3].get_text(strip=True) if len(tds) > 3 else '',
            'date': tds[4].get_text(strip=True) if len(tds) > 4 else '',
            'db': tds[5].get_text(strip=True) if len(tds) > 5 else '',
            'href': a.get('href', ''),
        })
    return total, out


if __name__ == '__main__':
    kw = sys.argv[1]
    size = 20
    if '--size' in sys.argv:
        size = int(sys.argv[sys.argv.index('--size') + 1])
    total, rows = parse(search(kw, size))
    if '--json' in sys.argv:
        print(json.dumps({'total': total, 'rows': rows}, ensure_ascii=False, indent=2))
    else:
        print('主题「%s」共 %s 条\n' % (kw, total))
        for i, r in enumerate(rows[:12], 1):
            print('%2d. %s' % (i, r['title'][:60]))
            print('    %s | %s | %s | %s' % (r['authors'][:20], r['source'][:18], r['date'], r['db']))
