---
name: xiaoyu-charts
description: 小鱼出图——用 matplotlib + seaborn 模板（scripts/viz.py）画行情走势、多标的对比、相关性热力图，行情数据自动抓取并缓存，输出 PNG 发给 QQ。当用户要画股价/油价/指数走势图、多标的对比、相关性图、分布图等数据图表，或说「画个图看看」「做个图」时使用。注意：AI 生图、插画、配图走 xiaoyu-draw，不是这个。
---

# 小鱼出图（数据图）

## 何时用

- 股价、油价、指数、基金的走势图 / 对比图 / 归一化曲线
- 相关性热力图、收益率分布、分组统计
- 用户说「画一下走势」「对比一下」「做个图」

**不要用**于 AI 生图、插画、封面（那是 `xiaoyu-draw`），也不要临时手写绘图代码——模板就是为省 token 存在的。

## 环境

库已经装在用户级 site-packages（numpy / pandas / matplotlib / seaborn / altair / plotly / mplfinance），**直接用系统 `python3`**，不要新建 venv，不要重装。

```bash
cd ~/agents/xiaoyu/.agents/skills/xiaoyu-charts
python3 scripts/viz.py          # 自检 + 出示例图
```

## 30 秒上手

```python
import sys
sys.path.insert(0, '/home/xunbu/agents/xiaoyu/.agents/skills/xiaoyu-charts/scripts')
import viz

s = {
    '中国海油 A': viz.fetch_ashare('600938'),
    '中国石油 A': viz.fetch_ashare('601857'),
    '康菲石油':   viz.fetch_us('COP'),
    '布伦特原油': viz.fetch_brent(),
}
df = viz.frame(s, start='2026-07-10')          # 对齐 + 指数化（起点=100）
viz.line_chart(
    df, '7 月以来：同一波油价，三种反应',
    '2026-07-10 = 100 ｜ 中海油、中石油为 A 股，康菲为美股',
    events=[('2026-08-26', '中报：派息 0.94 港元'), ('2026-11-04', '溢价出清 −16%', 2)],
    benchmark='布伦特原油',
    out='/tmp/chart.png',
)
print(viz.thumbnail('/tmp/chart.png'))         # 先看缩略图
```

## 函数一览

| 函数 | 说明 |
|---|---|
| `fetch_ashare('600938')` | A 股日线收盘（东方财富） |
| `fetch_hk('00883')` | 港股 |
| `fetch_us('COP')` | 美股 |
| `fetch_brent()` | 布伦特原油连续 |
| `frame(series, start=, base=)` | 对齐 + 指数化，返回 日期/指数/标的 长表 |
| `returns(series, start, end)` | 宽表日收益率，喂给相关性和统计 |
| `line_chart(df, ...)` | 多线图：自动配色、末值标签、事件竖线、基准虚线 |
| `heatmap(corr, title)` | 相关性热力图 |
| `thumbnail(path, 600)` | 缩略图，调试用 |

所有取数结果缓存在 `~/.cache/xiaoyu-charts/`；要最新数据传 `refresh=True`。

## 省 token 的规矩（重要）

1. **先缩略图，后正式图**。600px 的缩略图约 300 tokens，1800px 要 1.5k+。版式没确认前只看缩略图。
2. **不重写版式**。改参数（`events`、`benchmark`、`ylim`、`colors`），不要复制整段代码再改。
3. **数据走缓存**。别每次重写抓取和解析逻辑。
4. **一次交代清楚**。给 `line_chart` 的标题、副标题、事件、基准一次给全，避免改一轮看一轮。

## 已知的坑

- `sns.lineplot` 默认会算置信区间并画阴影带，多序列时容易误导；模板里用 `ax.plot`，不用它。
- 事件标签**必须放绘图区外上方**（模板已处理，行号传 2 表示第二行）。塞在图内一定压线。
- 中文字体统一 `Noto Sans CJK SC`，换机器/新环境先 `fc-list :lang=zh` 确认。
- 东方财富 secid：A 股 `1.` / `0.`，港股 `116.`，美股 `106.`；`fqt=0` 是不复权，跨除权日算涨幅要说明。
- 长假、停牌会造成平段，图上看起来像"没动"，先确认再解释。
- 指数化只消除价格差异，不消除币种；跨币种比较要在副标题里注明。

## 什么时候别用它

填充区域、双向箭头、极复杂的自定义标注——这些用 PIL 手画更快（参考 `assets/oil-trend-*.png` 的版式）。但先试一次模板，多数情况够用。

## 交付

图存 `~/agents/xiaoyu/assets/`，用 `qqbot_send_file` 发；文件名带主题和日期，如 `cnooc-ah-20261106.png`。
