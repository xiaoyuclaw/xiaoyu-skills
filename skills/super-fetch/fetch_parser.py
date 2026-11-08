import os
import re
import sqlite3
import random
import string
from urllib.parse import urlparse, urljoin, unquote
from bs4 import BeautifulSoup, Comment
import markdownify
from markdownify import MarkdownConverter

from core import DATA_DIR, DB_PATH, secure_file


def init_db():
    """初始化链接数据库，并执行概率性清理"""
    try:
        with sqlite3.connect(DB_PATH, timeout=15) as conn:
            conn.execute('''CREATE TABLE IF NOT EXISTS links
                            (id TEXT PRIMARY KEY, url TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)''')
            if random.random() < 0.05:
                conn.execute("DELETE FROM links WHERE created_at <= datetime('now', '-7 days')")
        secure_file(DB_PATH)
    except Exception as e:
        import sys
        print(f"[*] ⚠️ 数据库初始化异常: {e}", file=sys.stderr)


def remove_base64_images(soup):
    """移除 base64 图片，保留其他图片"""
    for img in soup.find_all('img'):
        src = img.get('src', '')
        if src.startswith('data:'):
            img.decompose()


def process_links_to_ids(soup: BeautifulSoup, base_url: str):
    """将 A 标签和 IMG 标签的链接转化为代号，并存入数据库"""
    init_db()
    ns = ''.join(random.choices(string.ascii_lowercase + string.digits, k=4))
    links_data = []
    counter = 1

    # 同时处理链接和图片
    for tag in soup.find_all(['a', 'img']):
        attr = 'href' if tag.name == 'a' else 'src'
        link = tag.get(attr)

        if not link or link.startswith(('javascript:', 'mailto:', '#', 'data:')):
            continue

        abs_url = urljoin(base_url, link)
        link_id = f"{ns}-{counter}"
        tag[attr] = f"@{link_id}"
        links_data.append((link_id, abs_url))
        counter += 1

    if links_data:
        try:
            with sqlite3.connect(DB_PATH, timeout=15) as conn:
                conn.executemany("INSERT OR REPLACE INTO links (id, url) VALUES (?, ?)", links_data)
        except:
            pass
    return ns


# 噪音关键词（用于 class/id 过滤）- 更保守的列表
NOISE_KEYWORDS = [
    'nav', 'navigation', 'navbar', 'footer', 'sidebar',
    'ad-', '-ad-', 'ads-', '-ads-',
    'cookie', 'consent'
]

# 正文关键词（加分）
CONTENT_KEYWORDS = [
    'article', 'content', 'main', 'post', 'entry'
]


def calculate_content_score(node):
    """
    保守的正文提取算法 - 宁滥勿缺
    """
    # 只读评分：不修改 DOM（清理交给 clean_noise_elements）
    text = node.get_text(strip=True)
    text_len = len(text)

    # 降低文本长度阈值
    if text_len < 20:
        return 0

    # 1. 计算段落数
    p_count = len(node.find_all('p'))
    li_count = len(node.find_all(['li', 'dd']))
    heading_count = len(node.find_all(['h1', 'h2', 'h3', 'h4', 'h5', 'h6']))
    span_count = len(node.find_all(['span', 'div']))
    block_count = p_count + li_count + heading_count

    # 2. 计算链接密度 - 放宽限制
    links = node.find_all('a')
    link_text_len = sum(len(a.get_text(strip=True)) for a in links)
    link_density = link_text_len / text_len if text_len > 0 else 1

    # 放宽链接密度限制
    if link_density > 0.8:
        return 0

    # 3. 检查 class/id 关键词
    identity = ''
    if node.get('class'):
        identity += ' ' + ' '.join(node.get('class', []))
    if node.get('id'):
        identity += ' ' + node.get('id', '')
    identity = identity.lower()

    # 噪音惩罚 - 更保守
    noise_penalty = 1.0
    for keyword in NOISE_KEYWORDS:
        if keyword in identity:
            noise_penalty *= 0.3
            break

    # 正文加分
    content_bonus = 1.0
    for keyword in CONTENT_KEYWORDS:
        if keyword in identity:
            content_bonus = 1.5
            break

    # 综合评分 - 更简单直接
    base_score = text_len * (1 - link_density * 0.5)

    # 段落加分
    if p_count >= 1:
        base_score *= 1.1
    if p_count >= 3:
        base_score *= 1.2

    score = base_score * content_bonus * noise_penalty
    return score


def get_best_content_node(soup):
    """
    保守策略：优先考虑 body，只移除明显噪音
    """
    # 策略 1: 语义化标签 - 降低门槛
    semantic_tags = ['article', 'main', '[role="main"]']
    for tag in semantic_tags:
        try:
            elem = soup.select_one(tag)
            if elem:
                text = elem.get_text(strip=True)
                if len(text) > 50:
                    return elem
        except:
            pass

    # 策略 2: 查找常见的正文容器
    common_selectors = [
        '.post-content', '.entry-content', '.article-content',
        '#post-content', '#entry-content', '#article-content',
        '.content-body', '.post-body', '.article-body',
        '.content', '#content'
    ]
    for selector in common_selectors:
        try:
            elem = soup.select_one(selector)
            if elem:
                text = elem.get_text(strip=True)
                if len(text) > 50:
                    return elem
        except:
            pass

    # 策略 3: 评分算法 - 更宽松
    candidates = []
    for tag in soup.find_all(['div', 'article', 'section', 'main', 'body']):
        score = calculate_content_score(tag)
        if score > 0:
            candidates.append((score, tag))

    if candidates:
        candidates.sort(key=lambda x: x[0], reverse=True)
        # 取最高分
        for score, tag in candidates:
            text = tag.get_text(strip=True)
            if len(text) > 30:
                return tag

    # 策略 4: 直接用 body，只移除最明显的噪音
    if soup.body:
        return soup.body
    return soup


def clean_noise_elements(soup):
    """只移除最明显的噪音元素，保留尽可能多的内容"""
    # 移除注释
    for comment in soup.find_all(string=lambda text: isinstance(text, Comment)):
        comment.extract()

    # 只移除最确定的噪音标签
    noise_selectors = [
        'script', 'style', 'noscript', 'svg', 'canvas'
    ]

    for selector in noise_selectors:
        try:
            for elem in soup.select(selector):
                elem.decompose()
        except:
            pass


def make_soup(html):
    """优先用 lxml（快 20-30%），没装则退回标准库解析器"""
    try:
        return BeautifulSoup(html, 'lxml')
    except Exception:
        return BeautifulSoup(html, 'html.parser')


def to_markdown(root) -> str:
    """直接把已有的 soup 节点转 Markdown，避免每次重新解析 HTML（快 2-3 倍）"""
    try:
        return MarkdownConverter(heading_style="ATX").convert_soup(root)
    except Exception:
        return markdownify.markdownify(str(root), heading_style="ATX")


def extract_page_content(html: str, url: str, full_mode: bool = False):
    """解析 HTML 并转化为结构化 Markdown"""
    soup = make_soup(html)
    if soup.title and soup.title.string:
        title = soup.title.string.strip()
    else:
        title = "Untitled Page"

    if full_mode:
        root = soup.body if soup.body else soup
    else:
        # 轻微清理噪音
        clean_noise_elements(soup)
        # 获取最佳内容节点
        root = get_best_content_node(soup)

    # 关键 1/3：清理大部分噪音标签，但保留 textarea（里面有有意思的 JSON 数据）
    noise_tags = ['script', 'style', 'noscript', 'svg', 'canvas', 'link']
    for elem in root.find_all(noise_tags):
        try:
            elem.decompose()
        except:
            pass

    # 移除 base64 图片
    remove_base64_images(root)

    ns = process_links_to_ids(root, url)

    # 关键 2/3：处理 textarea 里的 JSON 数据，把里面的链接也转成代号
    # （init_db 已在 process_links_to_ids 里调用过）
    counter = 1
    textarea_link_maps = []  # 保存每个 textarea 的替换映射

    for ta in root.find_all('textarea'):
        if ta.string:
            content = ta.string
            # 先找 linkurl 字段
            linkurl_pattern = r'"linkurl"\s*:\s*"([^"]+)"'
            matches = list(re.finditer(linkurl_pattern, content))
            link_map = {}

            for m in matches:
                encoded_url = m.group(1)
                try:
                    # URL decode
                    decoded_url = unquote(encoded_url)
                    # 再处理一次，因为可能有双重编码
                    if '%' in decoded_url:
                        decoded_url = unquote(decoded_url)
                except:
                    decoded_url = encoded_url

                link_id = f"{ns}-{counter}"
                link_map[encoded_url] = (link_id, decoded_url)

                # 存入数据库
                try:
                    with sqlite3.connect(DB_PATH, timeout=15) as conn:
                        conn.execute("INSERT OR REPLACE INTO links (id, url) VALUES (?, ?)", (link_id, decoded_url))
                except:
                    pass
                counter += 1

            textarea_link_maps.append((ta, link_map))

    # 替换 textarea 内容里的链接
    for ta, link_map in textarea_link_maps:
        if ta.string:
            content = ta.string
            for encoded_url, (link_id, decoded_url) in link_map.items():
                # 替换 "linkurl": "xxx" 为 "linkurl": "@ns-id"
                content = content.replace(f'"linkurl":"{encoded_url}"', f'"linkurl":"@{link_id}"')
                content = content.replace(f'"linkurl": "{encoded_url}"', f'"linkurl": "@{link_id}"')
            ta.string = content

    try:
        markdown = to_markdown(root)
    except Exception:
        markdown = root.get_text(separator='\n\n', strip=True)

    # 关键 3/3：双重保险 - 清理剩余的 script/style/link
    for tag in ['style', 'script', 'link']:
        markdown = re.sub(rf'<{tag}[^>]*>.*?</{tag}>', '', markdown, flags=re.I | re.S)

    # 清洗多余换行
    markdown = re.sub(r'\n{3,}', '\n\n', markdown).strip()
    return title, markdown, ns


# ---------------------------------------------------------------- 内嵌数据
_EMBED = [
    (r'<script[^>]+id="__NEXT_DATA__"[^>]*>(.*?)</script>', 'NEXT_DATA'),
    (r'window\.__NUXT__\s*=\s*(\{.*?\})\s*;?\s*</script>', 'NUXT'),
    (r'window\.__INITIAL_STATE__\s*=\s*(\{.*?\})\s*;?\s*</script>', 'INITIAL_STATE'),
    (r'window\.__APOLLO_STATE__\s*=\s*(\{.*?\})\s*;?\s*</script>', 'APOLLO_STATE'),
    (r'<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>', 'JSON-LD'),
]


def extract_embedded_data(html: str, min_bytes: int = 200, limit: int = 3):
    """从 HTML 里挖出服务端注水的 JSON（很多“JS 才能显示”的页面其实数据在源码里）。

    返回 [{'kind','bytes','data'}]，按体积降序，最多 limit 条。
    """
    import json as _json
    import re as _re
    out = []
    for pat, kind in _EMBED:
        for m in _re.finditer(pat, html, _re.S | _re.I):
            raw = m.group(1).strip()
            if len(raw) < min_bytes:
                continue
            try:
                data = _json.loads(raw)
            except Exception:
                continue
            out.append({'kind': kind, 'bytes': len(raw), 'data': data})
    out.sort(key=lambda x: x['bytes'], reverse=True)
    return out[:limit]
