#!/usr/bin/env python3
"""super-search —— 带 Cookie 预热的搜索引擎抓取。

两个关键结论（本机 2026-11 实测）：

1. 直接 GET 结果页会被风控（百度降级成广告页、搜狗要验证码）。必须先访问首页拿身份 cookie。
2. **headless 浏览器会被识别**（百度/搜狗/DDG 都弹验证）；改用真实显示器上的 headful 浏览器
   （DISPLAY=:0，窗口挪到屏幕外）就能通过。playwright 因此是默认通道，cffi 作为无显示器时的回退。

用法：
    ./sv "MiniMax H3 价格"
    ./sv "关键词" -e baidu,sogou
    ./sv "关键词" --via cffi        # 无显示器环境用
    ./sv "关键词" --json
"""
import argparse
import asyncio
import glob
import json
import os
import pathlib
import re
import sys
import time
import urllib.parse

HERE = pathlib.Path(__file__).resolve().parent
FETCH_DIR = HERE.parent / "super-fetch"
sys.path.insert(0, str(FETCH_DIR))

from fetch_parser import make_soup, clean_noise_elements, to_markdown, process_links_to_ids  # noqa: E402
from core import REAL_HEADERS, DATA_DIR  # noqa: E402

ENGINES = {
    "bing": {
        "home": "https://cn.bing.com/",
        "search": "https://cn.bing.com/search?q={q}",
        "domain": ".bing.com",
        "sel": "#b_results",
        "start": r"\n\d+\. \[",
    },
    "baidu": {
        "home": "https://www.baidu.com/",
        "search": "https://www.baidu.com/s?wd={q}",
        "domain": ".baidu.com",
        "sel": "#content_left",
        "start": r"### \[",
    },
    "sogou": {
        "home": "https://www.sogou.com/",
        "search": "https://www.sogou.com/web?query={q}",
        "domain": ".sogou.com",
        "sel": "#main",
        "start": r"### ",
    },
    "ddg": {
        "home": "https://duckduckgo.com/",
        "search": "https://duckduckgo.com/?q={q}",
        "domain": ".duckduckgo.com",
        "sel": "ol.react-results--main",
        "start": r"### ",
    },
    "360": {
        "home": "https://www.so.com/",
        "search": "https://www.so.com/s?q={q}",
        "domain": ".so.com",
        "sel": "#main",
        "start": r"### \[",
    },
}

CAPTCHA_MARKS = ("验证码", "安全验证", "滑块验证", "人机验证", "拖动左侧滑块",
                 "请完成下方验证", "unusual traffic")


def is_blocked(html: str) -> bool:
    """只看标题与正文文本——脚本里常有 challenge/captcha 字样，看原始 HTML 会误判。"""
    m = re.search(r"<title[^>]*>(.*?)</title>", html, re.I | re.S)
    title = m.group(1) if m else ""
    if any(k in title for k in ("验证", "安全", "人机")):
        return True
    text = re.sub(r"<script.*?</script>|<style.*?</style>", " ", html, flags=re.I | re.S)
    text = re.sub(r"<[^>]+>", " ", text)
    return any(k in text for k in CAPTCHA_MARKS)


def state_path(engine: str) -> pathlib.Path:
    return DATA_DIR / f"pwstate-{engine}.json"


def cookie_jar(engine: str) -> pathlib.Path:
    return DATA_DIR / f"cookies-{engine}.json"


# ---------------- 显示器准备 ----------------
def display_env() -> dict:
    """headful 需要 X/Wayland。没设 DISPLAY 时尝试用 :0 + 现成的 Xauthority。"""
    env = dict(os.environ)
    if not env.get("DISPLAY") and pathlib.Path("/tmp/.X11-unix/X0").exists():
        env["DISPLAY"] = ":0"
    if not env.get("XAUTHORITY"):
        uid = os.getuid()
        cands = glob.glob(f"/run/user/{uid}/.mutter-Xwaylandauth.*") + \
                [os.path.expanduser("~/.Xauthority")]
        for c in cands:
            if os.path.exists(c):
                env["XAUTHORITY"] = c
                break
    return env


def has_display() -> bool:
    env = display_env()
    return bool(env.get("DISPLAY")) and os.path.exists("/tmp/.X11-unix/X0")


# ---------------- playwright（默认） ----------------
# 省流：默认拦掉图片/字体/媒体，纯文本页面用不上，还能少暴露请求
BLOCK_TYPES = ("image", "media", "font")

STEALTH_JS = """
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
Object.defineProperty(navigator, 'languages', { get: () => ['zh-CN', 'zh', 'en'] });
Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3, 4, 5] });
window.chrome = window.chrome || { runtime: {} };
"""



def sweep_stale_profiles(max_age_hours: int = 1):
    """清理被杀进程遗留的临时 profile（只动 playwright 自己的命名）"""
    import shutil
    now = time.time()
    for pattern in ("/tmp/playwright_chromiumdev_profile-*", "/tmp/playwright-artifacts-*"):
        for d in glob.glob(pattern):
            try:
                if now - os.path.getmtime(d) > max_age_hours * 3600:
                    shutil.rmtree(d, ignore_errors=True)
            except Exception:
                pass


async def cffi_warmup(engine: str, timeout: int = 20) -> dict:
    """用 cffi 访问首页拿 cookie —— 百度只认这种方式发的身份 cookie。

    实测：playwright 自己访问首页再搜索仍会弹滑块；把 cffi 拿到的 cookie 注入 headful
    浏览器则直接通过。所以预热交给 cffi，渲染交给 playwright。
    """
    from curl_cffi.requests import AsyncSession

    cfg = ENGINES[engine]
    try:
        async with AsyncSession(impersonate="chrome120", headers=REAL_HEADERS,
                                timeout=timeout) as s:
            await s.get(cfg["home"])
            await asyncio.sleep(0.8)
            cookies = s.cookies.get_dict()
            if cookies:
                jar = cookie_jar(engine)
                jar.write_text(json.dumps(cookies, ensure_ascii=False))
                os.chmod(jar, 0o600)
            return cookies
    except Exception:
        return {}


async def wait_stable(page, min_wait: float = 1.0, max_wait: float = 5.0) -> None:
    """等正文长度稳定下来，避免页面还没渲染就抓走。"""
    start = time.time()
    await asyncio.sleep(min_wait)
    last = -1
    stable = 0
    while time.time() - start < max_wait:
        try:
            n = await page.evaluate("document.body ? document.body.innerText.length : 0")
        except Exception:
            return
        if n == last and n > 0:
            stable += 1
            if stable >= 2:
                return
        else:
            stable = 0
            last = n
        await asyncio.sleep(0.35)


class PlaywrightSearcher:
    """一次运行内复用浏览器与上下文：多引擎回退时省掉重复启动（约 1.9s/次）"""

    def __init__(self, timeout: int = 30, headless: bool = False, show: bool = False):
        self.timeout = timeout
        self.headless = headless
        self._pw = None
        self._browser = None
        self._ctx = None
        self._routes_set = False
        self.show = show

    async def start(self):
        from playwright.async_api import async_playwright

        for k, v in display_env().items():
            if k in ("DISPLAY", "XAUTHORITY"):
                os.environ[k] = v

        pos = "120,80" if self.show else "-2400,-2400"
        self._pw = await async_playwright().start()
        self._browser = await self._pw.chromium.launch(
            headless=self.headless,
            args=["--no-sandbox", "--disable-blink-features=AutomationControlled",
                  "--disable-dev-shm-usage", "--no-first-run", "--disable-background-networking",
                  f"--window-position={pos}", "--window-size=1280,900"],
        )
        self._ctx = await self._browser.new_context(locale="zh-CN",
                                                    timezone_id="Asia/Shanghai",
                                                    viewport={"width": 1366, "height": 900})
        try:
            await self._ctx.add_init_script(STEALTH_JS)
        except Exception:
            pass

        async def route(r):
            if r.request.resource_type in BLOCK_TYPES:
                await r.abort()
            else:
                await r.continue_()

        await self._ctx.route("**/*", route)

    async def fetch(self, engine: str, query: str) -> str:
        cfg = ENGINES[engine]
        url = cfg["search"].format(q=urllib.parse.quote(query))
        state = state_path(engine)
        page = await self._ctx.new_page()
        try:
            domain = cfg.get("domain", "")
            cookies = []
            if state.exists():
                try:
                    cookies = json.loads(state.read_text()).get("cookies", [])
                except Exception:
                    cookies = []
            if not cookies:
                # 用 cffi 预热拿身份 cookie，再注入浏览器（playwright 自己预热过不了百度）
                jar = await cffi_warmup(engine, self.timeout)
                cookies = [{"name": k, "value": v, "domain": domain, "path": "/"}
                           for k, v in jar.items()]
            if cookies:
                try:
                    await self._ctx.add_cookies(cookies)
                except Exception:
                    pass

            await page.goto(url, wait_until="domcontentloaded", timeout=self.timeout * 1000)
            sel = cfg.get("sel")
            if sel:
                try:
                    await page.wait_for_selector(sel, timeout=4000)
                except Exception:
                    pass
            await wait_stable(page)
            html = await page.content()
            if not is_blocked(html):
                try:
                    st = await self._ctx.storage_state()
                    state.write_text(json.dumps(st, ensure_ascii=False))
                    os.chmod(state, 0o600)
                except Exception:
                    pass
            return html
        finally:
            await page.close()

    async def close(self):
        try:
            if self._browser:
                await self._browser.close()
        finally:
            if self._pw:
                await self._pw.stop()
            self._browser = self._ctx = self._pw = None


# ---------------- cffi（无显示器回退） ----------------
async def cffi_fetch(engine: str, query: str, timeout: int = 20) -> str:
    from curl_cffi.requests import AsyncSession

    cfg = ENGINES[engine]
    url = cfg["search"].format(q=urllib.parse.quote(query))
    jar = cookie_jar(engine)
    cookies = {}
    if jar.exists():
        try:
            cookies = json.loads(jar.read_text())
        except Exception:
            pass

    async with AsyncSession(impersonate="chrome120", headers=REAL_HEADERS,
                            timeout=timeout, cookies=cookies or None) as s:
        if not cookies:
            try:
                await s.get(cfg["home"])
                await asyncio.sleep(1)
            except Exception:
                pass
        r = await s.get(url)
        try:
            jar.write_text(json.dumps(s.cookies.get_dict(), ensure_ascii=False))
            os.chmod(jar, 0o600)
        except Exception:
            pass
        return r.text


def extract(engine: str, html: str, limit: int):
    soup = make_soup(html)
    title_node = soup.find("title")
    title = title_node.get_text(strip=True) if title_node else ""
    clean_noise_elements(soup)
    node = soup.select_one(ENGINES[engine].get("sel") or "body") or soup.body or soup
    ns = process_links_to_ids(node, ENGINES[engine]["home"])
    markdown = to_markdown(node)
    cut = ENGINES[engine].get("start")
    if cut:
        m = re.search(cut, markdown)
        if m:
            markdown = markdown[m.start():]
    if limit:
        markdown = markdown[:limit]
    return title, markdown, ns


async def fetch_one(engine, query, via, timeout, searcher, headless):
    """按通道抓一次，返回 (html, 实际用的通道)。

    auto = 先 cffi（1-2s）；被风控或抛错才升级 headful playwright（5-10s）。
    """
    if via == "cffi":
        return await cffi_fetch(engine, query, timeout), "cffi"
    if via == "auto":
        try:
            html = await cffi_fetch(engine, query, timeout)
            if not is_blocked(html):
                return html, "cffi"
        except Exception:
            pass
    if searcher is None:
        searcher = PlaywrightSearcher(timeout, headless)
        await searcher.start()
    return await searcher.fetch(engine, query), "playwright"


async def run(engines, query, via, limit, timeout, headless, show=False):
    searcher = None
    try:
        if via in ("playwright", "auto") and has_display():
            searcher = PlaywrightSearcher(timeout, headless, show)
            try:
                await searcher.start()
            except Exception as e:
                print(f"[*] ⚠️ playwright 启动失败，回退 cffi: {str(e)[:100]}", file=sys.stderr)
                searcher = None
                if via == "playwright":
                    via = "cffi"

        for engine in engines:
            if engine not in ENGINES:
                print(f"[*] ⚠️ 未知引擎 {engine}，跳过", file=sys.stderr)
                continue
            t0 = time.time()
            try:
                html, used = await fetch_one(engine, query, via, timeout, searcher, headless)
            except Exception as e:
                print(f"[*] ❌ {engine} ({via}): {str(e)[:120]}", file=sys.stderr)
                continue

            if is_blocked(html):
                # 状态可能被污染：清掉重试一次
                state_path(engine).unlink(missing_ok=True)
                try:
                    html, used = await fetch_one(engine, query, via, timeout, searcher, headless)
                except Exception:
                    pass

            # auto 模式：cffi 撞风控时，明确改用 playwright 再试一次
            if is_blocked(html) and via == "auto" and searcher is not None:
                print(f"[*] ↻ {engine}: cffi 撞风控，改用 playwright", file=sys.stderr)
                try:
                    html = await searcher.fetch(engine, query)
                    used = "playwright"
                except Exception as e:
                    print(f"[*] ❌ {engine} playwright: {str(e)[:80]}", file=sys.stderr)

            if is_blocked(html):
                print(f"[*] ⚠️ {engine}: 仍被风控，换下一个", file=sys.stderr)
                continue

            # 先不截断，质量判断要看全文
            title, markdown, ns = extract(engine, html, None)

            # 内容太少 = 没拿到真结果（常见于被降级成空白页/验证页）
            if len(markdown.strip()) < 200:
                if via == "auto" and used == "cffi" and searcher is not None:
                    print(f"[*] ↻ {engine}: cffi 只拿到 {len(markdown.strip())} 字符，改用 playwright",
                          file=sys.stderr)
                    try:
                        html = await searcher.fetch(engine, query)
                        used = "playwright"
                        title, markdown, ns = extract(engine, html, None)
                    except Exception as e:
                        print(f"[*] ❌ {engine} playwright: {str(e)[:80]}", file=sys.stderr)
                if len(markdown.strip()) < 200:
                    print(f"[*] ⚠️ {engine}: 内容过少（{len(markdown.strip())} 字符），换下一个",
                          file=sys.stderr)
                    continue
            elapsed = time.time() - t0
            channel = used if via != "auto" else f"auto→{used}"
            print(f"[*] ✅ {engine} ({channel}): {elapsed:.2f}s / {len(markdown)} 字符", file=sys.stderr)
            return {"engine": engine, "via": channel, "query": query, "title": title,
                    "namespace": ns, "content": markdown[:limit] if limit else markdown,
                    "elapsed": round(elapsed, 2)}
        return None
    finally:
        if searcher:
            await searcher.close()


def main():
    ap = argparse.ArgumentParser(description="super-search：带预热的搜索")
    ap.add_argument("query", nargs="?")
    ap.add_argument("--engine", "-e", default=None,
                    help="逗号分隔，按顺序尝试；不填则按查询语言选（中文→百度优先，英文→Bing 优先）")
    ap.add_argument("--via", choices=["auto", "playwright", "cffi", "http", "browser"],
                    default="auto",
                    help="auto（默认：先 http 不行再 browser）/ http(=cffi) / browser(=playwright)")
    ap.add_argument("--headless", action="store_true", help="强制无头（大概率被风控）")
    ap.add_argument("--json", "-j", action="store_true", help="输出结构化 JSON（items 列表）")
    ap.add_argument("--raw", action="store_true", help="配合 -j：额外带上原始正文 content")
    ap.add_argument("--level", "-L", choices=["brief", "normal", "full"], default="normal",
                    help="输出暴露程度：brief 800 字 / normal 5000 字 / full 不截断")
    ap.add_argument("--limit", "-m", type=int, default=None, help="自定义字符上限，覆盖 --level")
    ap.add_argument("--timeout", "-t", type=int, default=30)
    ap.add_argument("--show", action="store_true",
                    help="把浏览器窗口显示在屏幕上（调试用，默认挪到屏幕外）")
    ap.add_argument("--clear-state", action="store_true",
                    help="清掉所有引擎的 cookie/会话状态后退出")
    args = ap.parse_args()
    args.via = {"http": "cffi", "browser": "playwright"}.get(args.via, args.via)
    if args.limit is None:
        args.limit = {"brief": 800, "normal": 5000, "full": 0}[args.level]

    if args.clear_state:
        n = 0
        for f in list(DATA_DIR.glob("pwstate-*.json")) + list(DATA_DIR.glob("cookies-*.json")):
            f.unlink(missing_ok=True); n += 1
        print(f"已清理 {n} 个状态文件")
        return 0

    if not args.query:
        ap.error("缺少查询词（--clear-state 除外）")

    sweep_stale_profiles()

    if args.engine:
        engines = [e.strip() for e in args.engine.split(",") if e.strip()]
    elif re.search(r"[\u4e00-\u9fff]", args.query or ""):
        engines = ["baidu", "sogou", "bing", "360", "ddg"]      # 中文：百度/搜狗更强
    else:
        engines = ["bing", "baidu", "ddg", "360", "sogou"]      # 英文：Bing 更准
    via = args.via
    if via in ("playwright", "auto") and not has_display():
        print("[*] 没有可用显示器，回退到 cffi", file=sys.stderr)
        via = "cffi"

    result = asyncio.run(run(engines, args.query, via, args.limit,
                             args.timeout, args.headless, args.show))
    if result and not args.json:
        from sr_render import md_results
        print(md_results(result, args.level))
        return

    if result and args.json:
        from sr_render import items as _items
        struct = {k: result[k] for k in ('engine', 'query', 'via', 'elapsed') if k in result}
        struct['items'] = _items(result, limit={"brief": 5, "normal": 10, "full": 20}[args.level])
        struct['count'] = len(struct['items'])
        if getattr(args, 'raw', False):
            struct['content'] = result.get('content', '')
        print(json.dumps(struct, ensure_ascii=False))
        return

    if not result:
        msg = ("所有引擎都不可用。常见原因：① 该引擎在当前网络不可达（如 ddg 需代理）；"
               "② 被风控，试 --via browser；③ 换引擎，国内推荐 -e baidu / -e bing / -e sogou")
        print(json.dumps({"error": "所有引擎都不可用", "hint": msg, "query": args.query},
                         ensure_ascii=False) if args.json else msg)
        return 1

    if args.json:
        print(json.dumps(result, ensure_ascii=False))
    else:
        print(f"# {result['title']}\n> 引擎: {result['engine']} | 通道: {result['via']} | 耗时: {result['elapsed']}s\n")
        print(result["content"])
    return 0


if __name__ == "__main__":
    sys.exit(main())
