#!/usr/bin/env python3
import argparse
import sys
import json
import asyncio
import os
import random
import time
from urllib.parse import urlparse
from dataclasses import dataclass
from typing import List, Optional, Dict

from core import (
    get_data_dir,
    DATA_DIR,
    resolve_session_path,
    setup_asyncio,
    USER_AGENTS,
)
from fetch_engines import (
    fetch_target,
    fetch_with_curl_cffi,
    PlaywrightPool,
)
from fetch_parser import extract_page_content


@dataclass
class FetchResult:
    """单个抓取结果"""
    url: str
    success: bool
    title: str = ""
    markdown: str = ""
    namespace: str = ""
    error: str = ""
    html_len: int = 0
    script_count: int = 0


class DomainRateLimiter:
    """域名级别的速率限制器 - 防止对同一域名请求过快"""

    def __init__(self, min_delay: float = 1.0, max_delay: float = 3.0):
        self.min_delay = min_delay
        self.max_delay = max_delay
        self.last_fetch_time: Dict[str, float] = {}
        self._lock = asyncio.Lock()

    async def wait(self, domain: str):
        """等待直到可以安全请求该域名"""
        async with self._lock:
            now = time.time()
            last_time = self.last_fetch_time.get(domain, 0)
            delay = random.uniform(self.min_delay, self.max_delay)
            elapsed = now - last_time

            if elapsed < delay:
                wait_time = delay - elapsed
                self.last_fetch_time[domain] = now + wait_time
                return wait_time
            else:
                self.last_fetch_time[domain] = now
                return 0


class BatchFetcher:
    """批量抓取器 - 支持 cffi 和 playwright 两种引擎的并发"""

    def __init__(
        self,
        concurrency: int = 5,
        domain_min_delay: float = 2.0,
        domain_max_delay: float = 5.0,
        global_jitter: float = 0.5,
    ):
        self.concurrency = concurrency
        self.semaphore = asyncio.Semaphore(concurrency)
        self.rate_limiter = DomainRateLimiter(domain_min_delay, domain_max_delay)
        self.global_jitter = global_jitter

    def get_random_user_agent(self) -> str:
        return random.choice(USER_AGENTS)

    async def _rate_limit_wait(self, url: str, silent: bool = False):
        """统一的速率限制等待"""
        domain = urlparse(url).netloc

        wait_time = await self.rate_limiter.wait(domain)
        if wait_time > 0:
            # 静默只是不打印，等待必须照做，否则限流形同虚设
            if not silent:
                print(f"[*] ⏳ 对 {domain} 限流，等待 {wait_time:.2f}s: {url}", file=sys.stderr)
            await asyncio.sleep(wait_time)

        if self.global_jitter > 0:
            await asyncio.sleep(random.uniform(0, self.global_jitter))

    async def _parse_html(self, html, content_type, url, full_mode) -> FetchResult:
        """解析 HTML 为 Markdown"""
        if content_type and any(
            content_type.startswith(bt)
            for bt in ["application/pdf", "image/", "audio/", "video/", "application/zip"]
        ):
            return FetchResult(
                url=url,
                success=False,
                error=f"不支持的二进制内容: {content_type}",
            )

        if isinstance(html, bytes):
            html = html.decode("utf-8", errors="ignore")

        if args.engine in ("auto", "cffi") and isinstance(html, str):
            _tgt = meta_refresh_target(html)
            if _tgt:
                if not args.json:
                    print(f"[*] ↪️ 跟随跳转: {_tgt}", file=sys.stderr)
                try:
                    html, content_type = await _fetch_once(args, session_path, engine, args.timeout, url=_tgt)
                except Exception:
                    pass

        if isinstance(html, str) and html.startswith('{"error"'):
            err_data = json.loads(html)
            return FetchResult(
                url=url,
                success=False,
                error=err_data.get("error", "未知错误"),
            )

        html_len = len(html)
        script_count = html.lower().count("<script")
        title, markdown, ns = extract_page_content(html, url, full_mode=full_mode)
        return FetchResult(
            url=url,
            success=True,
            title=title,
            markdown=markdown,
            namespace=ns,
            html_len=html_len,
            script_count=script_count,
        )

    async def fetch_cffi(
        self,
        url: str,
        proxy: Optional[str],
        timeout: int,
        session_path: Optional[str],
        wait: int,
        full_mode: bool,
        silent: bool = False,
        session=None,
    ) -> FetchResult:
        """使用 cffi 引擎抓取单个 URL"""
        await self._rate_limit_wait(url, silent)

        async with self.semaphore:
            if not silent:
                print(f"[*] 🚀 [cffi] 开始抓取: {url}", file=sys.stderr)

            try:
                html, content_type = await fetch_with_curl_cffi(
                    url, proxy, timeout, session_path, session=session
                )

                result = await self._parse_html(html, content_type, url, full_mode)

                if result.success and not silent:
                    print(f"[*] ✅ [cffi] 抓取完成: {url}", file=sys.stderr)
                elif not silent:
                    print(f"[*] ❌ [cffi] 抓取失败: {url} - {result.error}", file=sys.stderr)

                return result

            except Exception as e:
                if not silent:
                    print(f"[*] ❌ [cffi] 抓取异常: {url} - {e}", file=sys.stderr)
                return FetchResult(url=url, success=False, error=str(e))

    async def fetch_all_cffi(
        self,
        urls: List[str],
        proxy: Optional[str],
        retries: int,
        session_path: Optional[str],
        wait: int,
        full_mode: bool,
        silent: bool = False,
        timeout: int = 30,
    ) -> List[FetchResult]:
        """cffi 引擎批量抓取 - 整批复用一个 AsyncSession，省掉每个 URL 的 TLS 握手"""

        async def fetch_with_retry(url: str) -> FetchResult:
            last_error = None
            for i in range(max(1, retries)):   # retries=0 表示只试 1 次，不是不试
                result = await self.fetch_cffi(
                    url, proxy, timeout, session_path, wait, full_mode, silent, session=sess
                )
                if result.success:
                    return result
                last_error = result.error
                if i < max(1, retries) - 1:
                    await asyncio.sleep(1)
            return FetchResult(url=url, success=False, error=last_error or "未知错误")

        from curl_cffi.requests import AsyncSession
        from core import REAL_HEADERS

        proxies = {"http": proxy, "https": proxy} if proxy else None
        async with AsyncSession(impersonate="chrome120", proxies=proxies,
                                timeout=timeout, headers=REAL_HEADERS) as sess:
            tasks = [fetch_with_retry(url) for url in urls]
            results = await asyncio.gather(*tasks, return_exceptions=True)
        # 处理异常：把抛出的异常转为 FetchResult
        processed = []
        for r in results:
            if isinstance(r, Exception):
                processed.append(FetchResult(url="", success=False, error=str(r)))
            else:
                processed.append(r)
        return processed

    async def fetch_all_playwright(
        self,
        urls: List[str],
        proxy: Optional[str],
        retries: int,
        session_path: Optional[str],
        wait: int,
        full_mode: bool,
        silent: bool = False,
    ) -> List[FetchResult]:
        """playwright 引擎批量抓取 - 使用 PlaywrightPool 复用 Browser"""

        results: List[FetchResult] = []

        async with PlaywrightPool(
            proxy=proxy,
            pool_size=self.concurrency,
            headless=False,
        ) as pool:

            async def fetch_with_retry(url: str) -> FetchResult:
                await self._rate_limit_wait(url, silent)

                last_error = None
                for i in range(max(1, retries)):   # retries=0 表示只试 1 次
                    if not silent:
                        print(f"[*] 🚀 [playwright] 开始抓取: {url}", file=sys.stderr)

                    try:
                        html, content_type = await pool.fetch_page(
                            url, 30, session_path, wait
                        )
                        result = await self._parse_html(html, content_type, url, full_mode)

                        if result.success:
                            if not silent:
                                print(f"[*] ✅ [playwright] 抓取完成: {url}", file=sys.stderr)
                            return result
                        last_error = result.error

                    except Exception as e:
                        last_error = str(e)
                        if not silent:
                            print(f"[*] ❌ [playwright] 异常: {url} - {e}", file=sys.stderr)

                    if i < max(1, retries) - 1:
                        await asyncio.sleep(1)

                return FetchResult(url=url, success=False, error=last_error or "未知错误")

            results = await asyncio.gather(*[fetch_with_retry(url) for url in urls], return_exceptions=True)
            # 处理异常：把抛出的异常转为 FetchResult
            processed = []
            for r in results:
                if isinstance(r, Exception):
                    processed.append(FetchResult(url="", success=False, error=str(r)))
                else:
                    processed.append(r)
            return processed

    async def fetch_all(
        self,
        urls: List[str],
        engine: str = "playwright",
        proxy: Optional[str] = None,
        retries: int = 0,
        session_path: Optional[str] = None,
        wait: int = 2,
        full_mode: bool = False,
        silent: bool = False,
        timeout: int = 30,
    ) -> List[FetchResult]:
        """统一入口 - 根据引擎选择并发策略"""
        if engine == "playwright":
            return await self.fetch_all_playwright(
                urls, proxy, retries, session_path, wait, full_mode, silent
            )
        else:
            return await self.fetch_all_cffi(
                urls, proxy, retries, session_path, wait, full_mode, silent, timeout
            )


def read_urls_from_file(filepath: str) -> List[str]:
    """从文件读取 URL 列表（每行一个 URL）"""
    urls = []
    if not os.path.exists(filepath):
        return urls
    with open(filepath, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith("#"):
                urls.append(line)
    return urls


async def _fetch_once(args, session_path, engine, timeout, url=None):
    """执行一次抓取，返回 (html_or_bytes, content_type)"""
    result = await fetch_target(
        url or args.url, engine, args.proxy, args.retries,
        session_path, args.interactive, args.wait, timeout,
    )
    return result if isinstance(result, tuple) else (result, "text/html")


BINARY_TYPES = ("application/pdf", "image/", "audio/", "video/", "application/zip")


_BLOCK_MARKS = ("安全验证", "访问过于频繁", "Just a moment", "cf-challenge", "请输入验证码", "验证码")


def looks_blocked(html: str) -> bool:
    """页面是不是验证/拦截页。短页面 + 关键词才算，避免误伤正文。"""
    if not isinstance(html, str):
        return False
    return len(html) < 30000 and any(k in html for k in _BLOCK_MARKS)


def meta_refresh_target(html: str):
    """识别 <meta http-equiv="Refresh" content="0;url=...">，返回目标 URL。"""
    import re as _re
    m = _re.search(r'http-equiv=["\']?refresh["\']?[^>]*content=["\'][^"\']*url=([^"\'>\s]+)',
                   html, _re.I)
    return m.group(1) if m else None


def needs_playwright(html: str, markdown: str, threshold: int) -> bool:
    """内容薄 + 页面明显靠 JS 渲染，才值得用 playwright 重试。

    短小但静态的页面（比如 example.com）重试也拿不到更多东西，跳过。
    """
    if len(markdown.strip()) >= threshold:
        return False
    if len(html) > 200_000:
        return True
    low = html.lower()
    if low.count("<script") >= 3:
        return True
    # SPA 挂载点空壳
    if ('id="app"' in low or "id='app'" in low or 'id="root"' in low) and len(markdown.strip()) < 300:
        return True
    return False


def _dump_links(ns: str, limit: int = 50) -> str:
    """把本次抓取的链接代号对照表打出来"""
    import sqlite3
    from core import DB_PATH
    if not os.path.exists(DB_PATH):
        return ""
    rows = []
    try:
        with sqlite3.connect(DB_PATH, timeout=10) as conn:
            rows = conn.execute(
                "SELECT id, url FROM links WHERE id LIKE ? ORDER BY id LIMIT ?",
                (f"{ns}-%", limit)).fetchall()
    except Exception:
        return ""
    if not rows:
        return ""
    lines = ["", "---", "## 链接对照", ""]
    lines += [f"- @{i} → {u}" for i, u in rows]
    if len(rows) == limit:
        lines.append(f"- ……（最多显示 {limit} 条，用 get_link.py 查全部）")
    return "\n".join(lines)


async def run_single_fetch(args, session_path):
    """单 URL 抓取模式（auto 引擎会先试 cffi，内容太少再上 playwright）"""
    if not args.json:
        print(f"[*] 🚀 任务启动: {args.url}", file=sys.stderr)
        if session_path:
            print(f"[*] 🎫 使用会话: {os.path.basename(session_path)}", file=sys.stderr)

    try:
        from sites import find_site
        _s = find_site(args.url)
        if _s and not args.json:
            print("[*] 📚 接口库中有记录: %s %s（参考，不自动发请求）" % (_s["method"], _s["endpoint"]),
                  file=sys.stderr)
    except Exception:
        pass

    if getattr(args, "eval_js", None) or getattr(args, "xhr", None) is not None:
        from fetch_engines import playwright_probe
        out = await playwright_probe(args.url, session_path, args.eval_js, args.xhr,
                                     wait=args.wait or 6, timeout=args.timeout,
                                     show=getattr(args, "show", False),
                                     replay=getattr(args, "replay", False),
                                     replaces=getattr(args, "replace", None))
        if getattr(args, "remember", False):
            try:
                import json as _json
                from urllib.parse import urlparse as _up
                from sites import save_site
                d = _json.loads(out)
                c = (d.get("captured") or [None])[0]
                if c:
                    ep = c.get("url_full") or c["url"]
                    page = _up(args.url)
                    dome = page.netloc
                    pat = page.path        # 用「页面路径」当匹配键，接口地址只作参考
                    keys = []
                    if session_path and os.path.exists(session_path):
                        keys = sorted({k["name"] for k in _json.load(open(session_path)).get("cookies", [])})
                    save_site(dome, pat, c.get("method", "GET"), ep,
                              c.get("request_body", ""), keys, "html", args.url)
                    print("[*] 📚 已记入接口库: %s %s" % (c.get("method"), pat), file=sys.stderr)
            except Exception as e:
                print("[*] 记录失败: %s" % e, file=sys.stderr)

        if not args.json:
            import render as _r
            _d = json.loads(out)
            _parts = []
            if _d.get("captured") is not None:
                _parts.append(_r.md_discover(_d["captured"], _d.get("count", 0)))
            if "result" in _d:
                _parts.append(_r.md_eval(_d["result"]))
            if _d.get("replay"):
                _c = (_d.get("captured") or [{}])[0]
                _parts.append(_r.md_replay(_d["replay"], _c.get("method", ""), _c.get("url", "")))
            print("\n\n".join(p for p in _parts if p))
            return
        print(out)
        return

    engines = ["cffi", "playwright"] if args.engine == "auto" else [args.engine]
    title = markdown = ns = ""
    html = None
    last_error = None

    for idx, engine in enumerate(engines):
        try:
            html, content_type = await _fetch_once(args, session_path, engine, args.timeout)
        except Exception as e:
            last_error = e
            if not args.json:
                print(f"[*] ❌ [{engine}] 失败: {e}", file=sys.stderr)
            continue

        # 二进制内容（PDF/图片/音视频/压缩包）
        if content_type and any(content_type.startswith(bt) for bt in BINARY_TYPES):
            if args.output:
                mode = "wb" if isinstance(html, bytes) else "w"
                with open(args.output, mode) as f:
                    f.write(html)
                print(f"[*] ✅ 文件已保存: {args.output}", file=sys.stderr)
                return
            print(json.dumps({"error": f"二进制内容 ({content_type}) 需 -o 参数"},
                             ensure_ascii=False))
            sys.exit(1)

        if isinstance(html, bytes):
            html = html.decode("utf-8", errors="ignore")

        if args.engine in ("auto", "cffi") and isinstance(html, str):
            _tgt = meta_refresh_target(html)
            if _tgt:
                if not args.json:
                    print(f"[*] ↪️ 跟随跳转: {_tgt}", file=sys.stderr)
                try:
                    html, content_type = await _fetch_once(args, session_path, engine, args.timeout, url=_tgt)
                except Exception:
                    pass

        if isinstance(html, str) and html.startswith('{"error"'):
            print(html)
            sys.exit(1)

        if getattr(args, "data", False) and isinstance(html, str):
            from fetch_parser import extract_embedded_data
            _blocks = extract_embedded_data(html)
            _cap = {"brief": 600, "normal": 6000, "full": None}[args.level]
            if args.max_chars is not None:
                _cap = args.max_chars
            _trimmed = []
            from render import outline as _outline
            for b in _blocks:
                oc = _outline(b["data"])
                raw = json.dumps(b["data"], ensure_ascii=False)
                if _cap and len(raw) > _cap:
                    _trimmed.append({**b, "data": None, "truncated": True, "outline": oc,
                                     "chars": len(raw), "preview": raw[:_cap]})
                else:
                    _trimmed.append({**b, "chars": len(raw), "outline": oc})
            _out = {"url": args.url, "mode": "embedded", "blocks": _trimmed}
            if not _blocks:
                _out["hint"] = "HTML 里没有内嵌 JSON，试 --discover 找接口"
            if args.output:                     # 落盘不占 token
                with open(args.output, "w", encoding="utf-8") as _f:
                    json.dump(_out, _f, ensure_ascii=False, indent=2)
                print(f"[*] 💾 已保存: {args.output}", file=sys.stderr)
                return
            if args.json:
                print(json.dumps(_out, ensure_ascii=False))
            else:
                from render import md_embedded
                print(md_embedded(_trimmed))
            return

        if not args.json:
            print(f"[*] 📝 提取内容... [{engine}]", file=sys.stderr)
        title, markdown, ns = extract_page_content(html, args.url, full_mode=args.full)

        # 提示：内嵌数据比正文富得多时，告诉用户有更省的选择
        if not args.json and args.engine != "cffi":
            from fetch_parser import extract_embedded_data
            _b = extract_embedded_data(html) if isinstance(html, str) else []
            if _b and sum(x["bytes"] for x in _b) > max(3 * len(markdown), 2000):
                _kb = sum(x["bytes"] for x in _b) // 1024
                print(f"[*] 💡 页面内嵌 {_kb}KB 结构化数据（正文仅 {len(markdown)} 字），"
                      f"加 --embedded 可直接取", file=sys.stderr)

        # auto 模式：内容薄时，先看有没有 SSR 注水数据（比开浏览器便宜 10 倍）
        _blocked = looks_blocked(html)
        if _blocked and not args.json:
            print("[*] 🚫 疑似验证/拦截页，强制升级浏览器兜底", file=sys.stderr)

        if args.engine == "auto" and idx == 0 and (_blocked or needs_playwright(html, markdown, args.auto_threshold)):
            from fetch_parser import extract_embedded_data
            blocks = extract_embedded_data(html) if isinstance(html, str) else []
            if blocks:
                if not args.json:
                    print("[*] 💡 内容薄，但发现内嵌数据，免开浏览器", file=sys.stderr)
                print(json.dumps({"url": args.url, "mode": "embedded", "blocks": blocks},
                                 ensure_ascii=False))
                return

        _blocked = looks_blocked(html)
        if _blocked and not args.json:
            print("[*] 🚫 疑似验证/拦截页，强制升级浏览器兜底", file=sys.stderr)

        if args.engine == "auto" and idx == 0 and (_blocked or needs_playwright(html, markdown, args.auto_threshold)):
            if not args.json:
                print(f"[*] 🔄 cffi 只取到 {len(markdown.strip())} 字符，改用 playwright 重试",
                      file=sys.stderr)
            continue
        break

    if html is None:
        print(json.dumps({"error": f"运行异常: {last_error}"}, ensure_ascii=False))
        sys.exit(1)

    cap = {"brief": 800, "normal": 5000, "full": None}[args.level]
    if args.max_chars is not None:
        cap = args.max_chars
    if args.level == "brief":
        args.links = False          # 精简模式不附链接表

    if args.links and ns:
        markdown += _dump_links(ns)

    header = f"# {title}\n" if title else ""
    session_info = os.path.basename(session_path) if session_path else "无"
    meta = f"> [系统提示] 命名空间: {ns} | 会话: {session_info}\n\n"
    full_text = header + meta + markdown

    if args.json:
        print(json.dumps({
            "url": args.url, "success": True, "title": title,
            "namespace": ns, "content": (markdown[:cap] if cap else markdown),
            "chars": len(markdown), "truncated": bool(cap and len(markdown) > cap),
        }, ensure_ascii=False))
        return

    if args.output:
        with open(args.output, "w", encoding="utf-8") as f:
            f.write(full_text)
        print(f"[*] 💾 已保存: {args.output}", file=sys.stderr)
        return

    if cap and len(full_text) > cap:
        print(f"[*] ✂️ 输出已截断: {cap}/{len(full_text)} 字符（-L full 或 -m 放宽，-o 落盘）",
              file=sys.stderr)
        full_text = full_text[:cap] + "\n\n...（已截断）"
    print(full_text)


async def run_batch_fetch(args, session_path):
    """批量并发抓取模式"""
    # 收集 URL
    urls = list(args.urls) if args.urls else []
    if args.url and args.url not in urls:
        urls.insert(0, args.url)
    if args.file:
        urls.extend(read_urls_from_file(args.file))

    # 去重
    urls = list(dict.fromkeys(urls))

    if not urls:
        print(json.dumps({"error": "请提供 URL（参数或 -F 文件）"}, ensure_ascii=False))
        sys.exit(1)

    # 验证 URL
    valid_urls = []
    for url in urls:
        if urlparse(url).scheme:
            valid_urls.append(url)
        elif not args.silent:
            print(f"[*] ⚠️ 跳过无效 URL: {url}", file=sys.stderr)

    if not valid_urls:
        print(json.dumps({"error": "没有有效的 URL"}, ensure_ascii=False))
        sys.exit(1)

    if not args.silent:
        print(f"[*] 📋 批量抓取任务启动", file=sys.stderr)
        print(f"[*] 🔗 URL 数量: {len(valid_urls)}", file=sys.stderr)
        print(f"[*] 🚦 并发数: {args.concurrency}", file=sys.stderr)
        print(f"[*] 🔧 引擎: {args.engine}", file=sys.stderr)
        print(f"[*] ⏱️  域名间隔: {args.domain_delay_min}-{args.domain_delay_max}s", file=sys.stderr)
        if session_path:
            print(f"[*] 🎫 使用会话: {session_path}", file=sys.stderr)

    # 执行抓取
    fetcher = BatchFetcher(
        concurrency=args.concurrency,
        domain_min_delay=args.domain_delay_min,
        domain_max_delay=args.domain_delay_max,
        global_jitter=args.jitter,
    )

    results = await fetcher.fetch_all(
        valid_urls,
        engine=args.engine,
        proxy=args.proxy,
        retries=args.retries,
        session_path=session_path,
        wait=args.wait,
        full_mode=args.full,
        silent=args.silent,
        timeout=args.timeout,
    )

    # auto 引擎：对失败或内容过薄的 URL 用 playwright 补抓一次
    if args.engine == "auto":
        def _weak(r):
            if not r.success:
                return True
            return (len((r.markdown or "").strip()) < args.auto_threshold
                    and (r.html_len > 200_000 or r.script_count >= 3))

        weak = [r for r in results if _weak(r) and r.url]
        if weak:
            if not args.silent:
                print(f"[*] 🔄 {len(weak)} 个页面内容不足，改用 playwright 重试",
                      file=sys.stderr)
            retry = await fetcher.fetch_all(
                [r.url for r in weak], engine="playwright", proxy=args.proxy,
                retries=args.retries, session_path=session_path, wait=args.wait,
                full_mode=args.full, silent=True, timeout=args.timeout,
            )
            by_url = {r.url: r for r in retry}
            results = [by_url.get(r.url, r) if _weak(r) else r for r in results]

    # 格式化输出
    output = {
        "total": len(results),
        "success": sum(1 for r in results if r.success),
        "failed": sum(1 for r in results if not r.success),
        "results": [
            {
                "url": r.url,
                "success": r.success,
                "title": r.title,
                "content": r.markdown,
                "namespace": r.namespace,
                "error": r.error,
            }
            for r in results
        ],
    }

    if args.output:
        with open(args.output, "w", encoding="utf-8") as f:
            json.dump(output, f, ensure_ascii=False, indent=2)
        if not args.silent:
            print(f"[*] 💾 结果已保存到: {args.output}", file=sys.stderr)
    else:
        print(json.dumps(output, ensure_ascii=False))

    # 统计输出
    if not args.silent:
        print(f"\n[*] 📊 完成统计:", file=sys.stderr)
        print(f"[*]   总计: {output['total']}", file=sys.stderr)
        print(f"[*]   成功: {output['success']} ✅", file=sys.stderr)
        print(f"[*]   失败: {output['failed']} ❌", file=sys.stderr)


def main():
    setup_asyncio()

    parser = argparse.ArgumentParser(description="Super Fetch - 高性能抓取基座")

    # 通用参数
    parser.add_argument(
        "--via",
        "--engine",
        "-e",
        dest="engine",
        choices=["playwright", "auto", "cffi", "browser", "http"],
        default="auto",
        help="通道：browser（playwright）/ http（cffi）/ auto（先 http 按需升级）。"
             "--via browser 与 -e playwright 等价",
    )
    parser.add_argument("--sites", nargs="?", const="", metavar="DOMAIN",
                        help="列出已知接口库（可按域名过滤），不抓取")
    parser.add_argument("--remember", action="store_true",
                        help="配合 --discover --verify：把验证通过的接口记入接口库")
    parser.add_argument("--full", "-f", action="store_true", help="全量模式")
    parser.add_argument(
        "--session",
        "-s",
        nargs="?",
        const="session.json",
        help="Session 文件名：无参用 session.json，或指定文件名",
    )
    parser.add_argument("--wait", "-w", type=int, default=None, help="渲染等待秒数")
    parser.add_argument("--timeout", "-t", type=int, default=30, help="单次请求超时（秒，默认 30）")
    parser.add_argument("--auto-threshold", type=int, default=400,
                        help="auto 引擎判定「内容太薄」的字符数阈值（默认 400）")
    parser.add_argument("--proxy", "-p", help="代理设置")
    parser.add_argument("--retries", "-r", type=int, default=0, help="失败重试次数")
    parser.add_argument("--output", "-o", help="输出文件路径（批量模式为 JSON，单 URL 为二进制）")

    # 单 URL 模式参数
    single_group = parser.add_argument_group("单 URL 模式")
    single_group.add_argument("--login", "--interactive", "-i", dest="interactive", action="store_true", help="人工干预模式（仅单 URL）")
    single_group.add_argument("--level", "-L", choices=["brief", "normal", "full"], default="normal",
                              help="输出暴露程度：brief 约 800 字 / normal 约 5000 字 / full 不设限（仅单 URL）")
    single_group.add_argument("--max-chars", "-m", type=int, default=None,
                              help="自定义最大输出字符数，覆盖 --level（仅单 URL）")
    single_group.add_argument("--json", "-j", action="store_true", help="单 URL 以 JSON 输出（仅单 URL）")
    single_group.add_argument("--links", "-l", action="store_true", help="末尾附链接代号对照表（仅单 URL）")
    single_group.add_argument("--eval", dest="eval_js", metavar="JS",
                              help="在页面里执行 JS 并返回结果（仅单 URL，自带有头浏览器）")
    single_group.add_argument("--discover", "--xhr", dest="xhr", metavar="PATTERN", nargs="?", const="",
                              help="发现数据接口：不带值=列出候选（元数据），带子串=只抓匹配的（仅单 URL）")
    single_group.add_argument("--replace", dest="replace", action="append", metavar="OLD=NEW",
                              help="配合 --verify：把请求里的参数替换成新值再回放，可重复（仅单 URL）")
    single_group.add_argument("--verify", "--replay", dest="replay", action="store_true",
                              help="配合 --xhr：抓到的请求立刻用 curl_cffi 回放一遍（仅单 URL）")
    single_group.add_argument("--embedded", "--data", dest="data", action="store_true",
                              help="提取 HTML 里内嵌的 JSON（__NEXT_DATA__/JSON-LD 等，仅单 URL）")

    # 批量模式参数
    batch_group = parser.add_argument_group("批量并发模式（使用 -F 或传入多个 URL 触发）")
    batch_group.add_argument("urls", nargs="*", metavar="URL",
                             help="1 个 = 单 URL 模式；2 个以上 = 批量模式")
    batch_group.add_argument("--file", "-F", help="从文件读取 URL 列表（每行一个）")
    batch_group.add_argument("--concurrency", "-c", type=int, default=5, help="最大并发数（默认 5）")
    batch_group.add_argument("--domain-delay-min", type=float, default=2.0, help="同一域名最小请求间隔（秒）")
    batch_group.add_argument("--domain-delay-max", type=float, default=5.0, help="同一域名最大请求间隔（秒）")
    batch_group.add_argument("--jitter", type=float, default=0.5, help="全局随机抖动上限（秒）")
    batch_group.add_argument("--silent", action="store_true", help="静默模式，只输出最终 JSON")

    args = parser.parse_args()

    # 判断是否为批量模式：有 -F，或位置参数给了 2 个以上 URL
    is_batch_mode = args.file is not None or len(args.urls or []) > 1
    args.url = (args.urls or [None])[0]   # 单 URL 模式取第一个

    # 设置默认值
    if args.wait is None:
        args.wait = 3

    args.engine = {"http": "cffi", "browser": "playwright"}.get(args.engine, args.engine)

    # 没指定 -s 时，按域名自动找已保存的登录态
    _u = getattr(args, "url", None) or (getattr(args, "urls", None) or [None])[0]
    if not getattr(args, "session", None) and _u:
        try:
            import pathlib as _pl
            from core import DATA_DIR
            host = urlparse(_u).netloc
            dirs = [DATA_DIR, _pl.Path.home() / ".agents" / "super-fetch"]
            for d in dirs:
                for f in sorted(_pl.Path(d).glob("*.json")):
                    stem = f.stem.lower()
                    if len(stem) >= 3 and stem in host.lower():
                        args.session = str(f)
                        print(f"[*] 🔑 自动匹配登录态: {f.name}", file=sys.stderr)
                        raise StopIteration
        except StopIteration:
            pass
        except Exception:
            pass
    if getattr(args, "sites", None) is not None:
        from sites import list_sites
        rows = list_sites(args.sites or None)
        if not rows:
            print("接口库为空")
        for r in rows:
            print("%-26s %-7s %-46s %s %s" % (r["domain"], r["method"], r["endpoint"][:46],
                                              r["status"], "✓%d/✗%d" % (r["ok_count"], r["fail_count"])))
        return
    session_path = resolve_session_path(args.session) if args.session else None

    # 交互模式检查
    if args.interactive and (is_batch_mode or len(args.urls or []) != 1):
        print(json.dumps({"error": "批量模式不支持交互模式 (-i)"}, ensure_ascii=False))
        sys.exit(1)

    # 交互模式逻辑锚点：-i 自动强制开启 playwright，且默认使用 session.json（如果未指定）
    if args.interactive:
        args.engine = "playwright"
        if args.session is None:
            args.session = "session.json"
            session_path = resolve_session_path(args.session)

    if is_batch_mode:
        asyncio.run(run_batch_fetch(args, session_path))
    else:
        # 单 URL 模式：检查 url 参数
        if not args.url:
            parser.print_help()
            print("\n错误：请提供 URL（单 URL 模式）或使用 -F/传入多个 URL（批量模式）", file=sys.stderr)
            sys.exit(1)
        if not urlparse(args.url).scheme:
            print(json.dumps({"error": "无效的 URL"}, ensure_ascii=False))
            sys.exit(1)
        asyncio.run(run_single_fetch(args, session_path))


if __name__ == "__main__":
    main()
