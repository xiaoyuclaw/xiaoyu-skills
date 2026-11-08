#!/usr/bin/env python3
"""小鱼出图工具箱 —— matplotlib + seaborn 模板，配行情抓取与本地缓存。

设计目标：一张常规图只用 3~5 行调用；样式、字体、边距、末值标签全部封装，
避免每次重写版式代码（那是 token 消耗的大头）。

依赖（已装在用户级 site-packages）：
    numpy scipy pandas matplotlib seaborn
"""
from __future__ import annotations

import json
import os
import urllib.request

import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import matplotlib.dates as mdates
from matplotlib.ticker import MultipleLocator
import pandas as pd
import seaborn as sns

CACHE = os.path.expanduser('~/.cache/xiaoyu-charts')
FONT = 'Noto Sans CJK SC'
PALETTE = ['#c0392b', '#2c6e9b', '#1e8449', '#8e44ad', '#d68910', '#16a085', '#7f8c8d']
UA = {'User-Agent': 'Mozilla/5.0'}


# ---------------------------------------------------------------- 数据
def _download(url: str, name: str) -> str:
    os.makedirs(CACHE, exist_ok=True)
    path = os.path.join(CACHE, name)
    if not os.path.exists(path):
        req = urllib.request.Request(url, headers=UA)
        with urllib.request.urlopen(req, timeout=30) as r, open(path, 'wb') as f:
            f.write(r.read())
    return path


def _kline(secid: str, tag: str, refresh: bool = False) -> dict[str, float]:
    """东方财富日 K，返回 {日期: 收盘}。secid 形如 1.600938 / 116.00883 / 106.COP。"""
    path = os.path.join(CACHE, f'{tag}.json')
    if refresh and os.path.exists(path):
        os.remove(path)
    url = ('https://push2his.eastmoney.com/api/qt/stock/kline/get?secid=%s&klt=101&fqt=0'
           '&beg=20200101&end=20500101&fields1=f1,f2,f3&fields2=f51,f53&lmt=3000' % secid)
    raw = json.load(open(_download(url, f'{tag}.json')))
    d = raw['data']
    if not d:
        raise RuntimeError('取数失败：%s' % secid)
    return {r.split(',')[0]: float(r.split(',')[1]) for r in d['klines']}


def fetch_ashare(code: str, **kw) -> dict[str, float]:
    """A 股，如 '600938'、'601857'、'000001'。"""
    secid = ('1.' if code[0] == '6' else '0.') + code
    return _kline(secid, 'A' + code, **kw)


def fetch_hk(code: str, **kw) -> dict[str, float]:
    """港股，如 '00883'。"""
    return _kline('116.' + code, 'HK' + code, **kw)


def fetch_us(ticker: str, **kw) -> dict[str, float]:
    """美股，如 'COP'、'XOM'。"""
    return _kline('106.' + ticker, 'US' + ticker, **kw)


def fetch_brent(**kw) -> dict[str, float]:
    """布伦特原油连续合约（新浪财经）。"""
    path = os.path.join(CACHE, 'BRENT.json')
    if kw.get('refresh') and os.path.exists(path):
        os.remove(path)
    url = ('https://stock2.finance.sina.com.cn/futures/api/jsonp.php/var%20t=/'
           'GlobalFuturesService.getGlobalFuturesDailyKLine?symbol=OIL')
    raw = open(_download(url, 'BRENT.json'), encoding='utf-8-sig').read()
    arr = json.loads(raw[raw.index('([') + 1: raw.rindex('])') + 1])
    return {x['date']: float(x['close']) for x in arr}


# ---------------------------------------------------------------- 加工
def frame(series: dict[str, dict[str, float]], start: str | None = None,
          base: str | None = None) -> pd.DataFrame:
    """把多条价格序列对齐成「指数化」长表：日期 / 指数 / 标的。"""
    dates = sorted(set().union(*[set(v) for v in series.values()]))
    if start:
        dates = [d for d in dates if d >= start]
    base = base or dates[0]

    out = []
    for name, s in series.items():
        last, rows = None, []
        for d in dates:
            if d in s:
                last = s[d]
            rows.append((d, last))
        anchor = next(v for d, v in rows if v is not None and d >= base)
        for d, v in rows:
            out.append({'日期': pd.to_datetime(d), '指数': v / anchor * 100, '标的': name})
    return pd.DataFrame(out)


def returns(series: dict[str, dict[str, float]], start: str, end: str | None = None) -> pd.DataFrame:
    """宽表 + 日收益率，用于相关性/回测。"""
    dates = sorted(set().union(*[set(v) for v in series.values()]))
    dates = [d for d in dates if d >= start and (end is None or d <= end)]
    df = pd.DataFrame({k: [v.get(d) for d in dates] for k, v in series.items()},
                      index=pd.to_datetime(dates)).ffill()
    return df.pct_change().dropna()


# ---------------------------------------------------------------- 主题
def theme(dpi: int = 130, fontsize: int = 11):
    sns.set_theme(style='whitegrid', context='notebook', rc={
        'font.sans-serif': [FONT], 'axes.unicode_minus': False,
        'figure.facecolor': 'white', 'axes.facecolor': 'white',
        'grid.color': '#ececec', 'font.size': fontsize,
    })
    plt.rcParams['figure.dpi'] = dpi


# ---------------------------------------------------------------- 图
def line_chart(df: pd.DataFrame, title: str, subtitle: str = '', out: str = 'chart.png',
               events: list | None = None, benchmark: str | None = None,
               ylabel: str = '走势指数（起点 = 100）', ylim: tuple | None = None,
               width: float = 15, height: float = 8, dpi: int = 130,
               colors: dict | None = None, legend_loc: str = 'upper left'):
    """多线走势图：自动配色、末值标签、事件竖线（标签放绘图区外上方，不压线）。"""
    theme(dpi)
    names = list(df['标的'].unique())
    last = df.sort_values('日期').groupby('标的').tail(1).set_index('标的')['指数']
    order = last.sort_values(ascending=False).index.tolist()          # 强的在上面
    if benchmark in order:                                            # 基准置顶
        order.remove(benchmark); order.insert(0, benchmark)
    seen = list(dict.fromkeys(df['标的']))
    cmap = colors or {n: PALETTE[i % len(PALETTE)] for i, n in enumerate(seen)}
    if benchmark and not (colors or {}).get(benchmark):
        cmap[benchmark] = '#3a3a3a'

    fig, ax = plt.subplots(figsize=(width, height), dpi=dpi)
    fig.subplots_adjust(left=0.08, right=0.885, top=0.775, bottom=0.105)

    for n in order:
        sub = df[df['标的'] == n]
        is_b = (n == benchmark)
        ax.plot(sub['日期'], sub['指数'], color=cmap[n], label=n,
                lw=2.8 if is_b else 2.4, ls=(0, (7, 4)) if is_b else '-',
                solid_capstyle='round', zorder=5 if is_b else 3)

    ax.set_xlim(df['日期'].min(), df['日期'].max())
    if ylim:
        ax.set_ylim(*ylim)
    ax.yaxis.set_major_locator(MultipleLocator(10))
    ax.xaxis.set_major_locator(mdates.MonthLocator())
    ax.xaxis.set_major_formatter(mdates.DateFormatter('%m月'))
    ax.set_xlabel('')
    ax.set_ylabel(ylabel, color='#666', fontsize=11, labelpad=8)
    ax.tick_params(colors='#666', labelsize=11, length=0)
    sns.despine(ax=ax, top=True, right=True)

    for d, lab, *rest in (events or []):
        row = rest[0] if rest else 1
        dy = 10 if row == 1 else 32
        ax.axvline(pd.Timestamp(d), color='#b5b5b5', ls=(0, (4, 4)), lw=0.9, zorder=1)
        ax.annotate(lab, xy=(pd.Timestamp(d), 1.0), xycoords=('data', 'axes fraction'),
                    xytext=(0, dy), textcoords='offset points', va='bottom',
                    ha='left' if row == 1 else 'right', fontsize=10.5,
                    color='#555', weight='bold')

    for n in order:
        v = last[n]
        ax.annotate('%s %.0f' % (n, v), xy=(df['日期'].max(), v), xytext=(9, 0),
                    textcoords='offset points', va='center', fontsize=11.5,
                    color=cmap[n], weight='bold')

    h, l = ax.get_legend_handles_labels()
    idx = [l.index(n) for n in order if n in l]
    ax.legend([h[i] for i in idx], [l[i] for i in idx], loc=legend_loc, frameon=False,
              fontsize=11.5, handlelength=2.4, labelcolor='#333')

    fig.text(0.08, 0.935, title, fontsize=23, weight='bold', color='#1e1e1e')
    if subtitle:
        fig.text(0.08, 0.895, subtitle, fontsize=11.5, color='#8a8a8a')
    fig.savefig(out, facecolor='white')
    plt.close(fig)
    return out


def heatmap(corr: pd.DataFrame, title: str, out: str = 'heatmap.png', dpi: int = 140):
    """相关性热力图（seaborn 的原生场景）。"""
    theme(dpi, fontsize=12)
    fig, ax = plt.subplots(figsize=(8.6, 6.6), dpi=dpi)
    sns.heatmap(corr, annot=True, fmt='.2f', cmap='RdYlBu_r', vmin=-0.2, vmax=1.0,
                square=True, linewidths=2, linecolor='white',
                cbar_kws={'shrink': 0.8}, annot_kws={'size': 14, 'weight': 'bold'}, ax=ax)
    ax.set_title(title, fontsize=16, weight='bold', pad=14, loc='left')
    ax.tick_params(axis='x', rotation=0, labelsize=12)
    ax.tick_params(axis='y', rotation=0, labelsize=12)
    fig.tight_layout()
    fig.savefig(out, facecolor='white')
    plt.close(fig)
    return out


def thumbnail(path: str, width: int = 600) -> str:
    """调试预览用：出一张缩略图，自己看效果花更少 token。"""
    from PIL import Image
    im = Image.open(path)
    h = int(im.height * width / im.width)
    out = path.rsplit('.', 1)[0] + '_thumb.png'
    im.resize((width, h), Image.LANCZOS).save(out)
    return out


if __name__ == '__main__':
    s = {
        '中国海油 A': fetch_ashare('600938'),
        '中国石油 A': fetch_ashare('601857'),
        '康菲石油': fetch_us('COP'),
        '布伦特原油': fetch_brent(),
    }
    df = frame(s, start='2026-07-10')
    line_chart(df, '7 月以来：同一波油价，三种反应',
               '2026-07-10 = 100 ｜ 中海油、中石油为 A 股，康菲为美股',
               events=[('2026-08-26', '中报：派息 0.94 港元'), ('2026-11-04', '溢价出清 −16%', 2)],
               benchmark='布伦特原油', out='/tmp/viz_demo.png')
    print(thumbnail('/tmp/viz_demo.png'))
