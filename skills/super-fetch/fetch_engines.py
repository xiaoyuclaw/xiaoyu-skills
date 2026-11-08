import sys
import os
import json
import asyncio
import tempfile
from urllib.parse import urlparse
from typing import Optional, Tuple
from curl_cffi.requests import AsyncSession
from playwright.async_api import async_playwright, Browser, BrowserContext, Page

from core import REAL_HEADERS, STEALTH_JS, secure_file


try:  # playwright-stealth 2.x 用类式 API，1.x 才是 stealth_async 函数
    import playwright_stealth
    from playwright_stealth import Stealth as _Stealth
except ImportError:  # 没装就别拦着抓取
    playwright_stealth = None
    _Stealth = None


async def apply_stealth(page):
    """兼容 playwright-stealth 1.x / 2.x 的注入入口。"""
    if _Stealth is not None:
        await _Stealth().apply_stealth_async(page)
        return
    if playwright_stealth is not None and hasattr(playwright_stealth, "stealth_async"):
        await playwright_stealth.stealth_async(page)


def atomic_write_json(data: dict, filepath: str):
    """原子化写入 JSON，防止高并发下文件损坏"""
    os.makedirs(os.path.dirname(filepath), exist_ok=True)
    fd, tmp_path = tempfile.mkstemp(dir=os.path.dirname(filepath), suffix='.tmp')
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as f:
            json.dump(data, f, indent=2, ensure_ascii=False)
        # 跨平台原子替换：Windows 上 os.replace 也可用，但需确保目标文件不存在或可覆盖
        try:
            os.replace(tmp_path, filepath)
        except PermissionError:
            # Windows 上可能需要先删除目标文件
            if os.path.exists(filepath):
                os.remove(filepath)
            os.replace(tmp_path, filepath)
    except Exception as e:
        if os.path.exists(tmp_path):
            try:
                os.remove(tmp_path)
            except:
                pass
        raise e


def format_session_data(session_file: str, target_url: str = "", persist: bool = False) -> dict:
    """自动识别并转换各种 Cookie 格式为 Playwright StorageState 格式。

    persist=False（默认）只做内存转换，**不改动用户文件**；
    只有显式传 persist=True（如 --convert-session）才写回。
    """
    default_state = {"cookies": [], "origins": []}
    if not session_file or not os.path.exists(session_file):
        return default_state

    try:
        secure_file(session_file)   # 会话里有 Cookie，顺手收紧权限
        with open(session_file, 'r', encoding='utf-8') as f:
            data = json.load(f)

        if isinstance(data, dict) and "cookies" in data:
            return data

        pw_state = {"cookies": [], "origins": []}
        if isinstance(data, list):
            # 转换 Cookie-Editor 等插件导出的列表格式
            for c in data:
                same_site = str(c.get("sameSite", "Lax")).capitalize()
                if same_site in ["No_restriction", "None"]:
                    same_site = "None"
                elif same_site not in ["Strict", "Lax", "None"]:
                    same_site = "Lax"

                pw_state["cookies"].append({
                    "name": c.get("name", ""), "value": c.get("value", ""),
                    "domain": c.get("domain", ""), "path": c.get("path", "/"),
                    "expires": c.get("expirationDate", -1), "httpOnly": c.get("httpOnly", False),
                    "secure": c.get("secure", False), "sameSite": same_site
                })
        elif isinstance(data, dict):
            # 转换简单的键值对格式
            target_domain = urlparse(target_url).netloc if target_url else ""
            for k, v in data.items():
                pw_state["cookies"].append({
                    "name": str(k), "value": str(v), "domain": target_domain,
                    "path": "/", "expires": -1, "httpOnly": False,
                    "secure": False, "sameSite": "Lax"
                })

        if persist:
            atomic_write_json(pw_state, session_file)
            secure_file(session_file)
        return pw_state
    except Exception as e:
        print(f"[*] ⚠️ Session 转换警告: {e}", file=sys.stderr)
        return default_state


async def fetch_with_curl_cffi(url: str, proxy: str = None, timeout: int = 15,
                              session_file: str = None, session=None) -> tuple:
    """抓取单个 URL。

    传入 session（AsyncSession）时复用它——批量抓取可以省掉每个 URL 一次 TLS 握手。
    """
    proxies = {"http": proxy, "https": proxy} if proxy else None
    cffi_cookies = {}

    # cffi 只读 session，不写入
    if session_file and os.path.exists(session_file):
        pw_state = format_session_data(session_file, url)
        for c in pw_state.get("cookies", []):
            cffi_cookies[c["name"]] = c["value"]

    async def _fetch(sess):
        from urllib.parse import urlparse as _up2
        _u = _up2(url)
        _h = {"Referer": f"{_u.scheme}://{_u.netloc}/",
              "Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "navigate",
              "Sec-Fetch-Site": "same-origin", "Upgrade-Insecure-Requests": "1"}
        response = await sess.get(url, headers=_h)
        response.raise_for_status()
        ctype = response.headers.get("content-type", "").lower()
        if any(t in ctype for t in ["text", "json", "xml", "javascript"]):
            return response.text, ctype
        return response.content, ctype

    if session is not None:
        return await _fetch(session)

    async with AsyncSession(impersonate="chrome120", proxies=proxies, timeout=timeout,
                            headers=REAL_HEADERS, cookies=cffi_cookies) as sess:
        return await _fetch(sess)


async def _harden(ctx):
    """统一给浏览器上下文加反检测脚本与时区。"""
    try:
        await ctx.add_init_script(STEALTH_JS)
    except Exception:
        pass


def _ensure_display():
    """有头模式需要 DISPLAY；没设就用 :0（Wayland 下走 XWayland）。"""
    if not os.environ.get('DISPLAY') and os.path.exists('/tmp/.X11-unix/X0'):
        os.environ['DISPLAY'] = ':0'


async def fetch_with_playwright(url: str, proxy: str = None, timeout: int = 30, session_file: str = None, is_interactive: bool = False, wait: int = 3) -> tuple:
    async with async_playwright() as p:
        launch_args = ['--disable-blink-features=AutomationControlled',
                       '--window-position=-2400,-2400', '--window-size=1280,900']
        if sys.platform != 'win32':
            launch_args.append('--no-sandbox')

        _ensure_display()
        browser = await p.chromium.launch(
            headless=False,          # 永不 headless：无头指纹会被站点识别
            proxy={"server": proxy} if proxy else None,
            args=launch_args
        )

        # 不覆盖 UA：使用浏览器真实版本，避免 UA 与实际内核/指纹不一致
        context_args = {"viewport": {"width": 1280, "height": 800}, "locale": "zh-CN"}
        if session_file and os.path.exists(session_file):
            context_args["storage_state"] = session_file

        context = await browser.new_context(timezone_id="Asia/Shanghai", **context_args)
        await _harden(context)

        # 优化后的注入 JS
        inject_js = """
        (() => {
            function createButton() {
                if (document.getElementById('_claw_btn')) return;
                const btn = document.createElement('div');
                btn.id = '_claw_btn';
                btn.innerHTML = '✅ 完成操作，点击抓取内容';
                Object.assign(btn.style, {
                    position: 'fixed', zIndex: '2147483647', padding: '12px 24px',
                    background: '#00c853', color: 'white', borderRadius: '30px',
                    cursor: 'pointer', boxShadow: '0 4px 15px rgba(0,0,0,0.5)',
                    top: '20px', right: '20px', fontWeight: 'bold',
                    userSelect: 'none', fontFamily: 'sans-serif',
                    display: 'block', visibility: 'visible', opacity: '1'
                });

                let isDragging = false, hasMoved = false, startX, startY, initX, initY;
                btn.addEventListener('mousedown', (e) => {
                    isDragging = true; hasMoved = false;
                    startX = e.clientX; startY = e.clientY;
                    initX = btn.offsetLeft; initY = btn.offsetTop;
                });
                window.addEventListener('mousemove', (e) => {
                    if (!isDragging) return;
                    const dx = e.clientX - startX, dy = e.clientY - startY;
                    if (Math.abs(dx) > 5 || Math.abs(dy) > 5) hasMoved = true;
                    btn.style.left = (initX + dx) + 'px';
                    btn.style.top = (initY + dy) + 'px';
                    btn.style.right = 'auto';
                });
                window.addEventListener('mouseup', () => { isDragging = false; });

                btn.addEventListener('click', (e) => {
                    if (hasMoved) return;
                    window._fetch_interactive_done = true;
                    btn.style.background = '#ff9100';
                    btn.innerHTML = '⏳ 正在同步并解析...';
                });
                (document.body || document.documentElement).appendChild(btn);
            }

            // 初始尝试创建
            createButton();
            // 针对 SPA 页面，使用观察器确保按钮不被删掉
            const observer = new MutationObserver(() => {
                if (!document.getElementById('_claw_btn')) createButton();
            });
            observer.observe(document.documentElement, { childList: true, subtree: true });

            // 每秒强制检查一次（防止某些极端情况）
            setInterval(createButton, 1000);
        })();
        """

        # 1. 预注入
        if is_interactive:
            await context.add_init_script(inject_js)

        page = await context.new_page()
        if playwright_stealth or _Stealth:
            try:
                # 不再手动改 UA（会与真实内核版本不符）
                await apply_stealth(page)
            except Exception as e:
                print(f"[*] ⚠️ stealth 注入失败（继续抓取）: {e}", file=sys.stderr)

        try:
            # 使用 try-except 包装 goto，防止因某些资源加载失败导致程序崩溃
            try:
                # 先等网络空闲（SPA 的 XHR 更可能已经回来）；等不到也不致命
                await page.goto(url, wait_until="networkidle", timeout=timeout*1000)
            except Exception:
                try:
                    await page.goto(url, wait_until="domcontentloaded", timeout=timeout*1000)
                except Exception as e:
                    print(f"[*] 页面加载超时或部分失败（继续）: {str(e)[:80]}", file=sys.stderr)

            if is_interactive:
                # 2. 页面加载后再次手动注入，确保按钮存在
                try:
                    await page.evaluate(inject_js)
                except:
                    pass

                print("[*] 浏览器已打开，请在页面完成操作（如登录、过验证码）后，点击页面右上角的绿色按钮。")

                # 等待用户点击按钮
                while True:
                    if page.is_closed():
                        break
                    try:
                        # 检查标志位
                        done = await page.evaluate("window._fetch_interactive_done === true")
                        if done:
                            break
                    except:
                        break  # 页面可能已关闭
                    await asyncio.sleep(0.5)
            else:
                await asyncio.sleep(wait)

            # 检查页面是否被关闭（用户在交互模式关窗口，或其他异常）
            if page.is_closed():
                print("[*] 浏览器已被关闭，尝试保存会话...")
                if session_file:
                    try:
                        await context.storage_state(path=session_file)
                        print(f"[*] ✅ 会话已保存: {session_file}")
                    except Exception:
                        pass
                return "", "text/html"

            # 只有交互模式（用户亲自登录）才把会话落盘；
            # 批量并发时多个 context 写同一个文件会互相覆盖。
            if session_file and is_interactive:
                try:
                    await context.storage_state(path=session_file)
                    secure_file(session_file)
                except Exception as e:
                    print(f"[*] ⚠️ 会话保存失败: {e}", file=sys.stderr)

            content = await page.content()
            try:
                ctype = await page.evaluate("document.contentType || 'text/html'")
            except:
                ctype = 'text/html'
            return content, ctype
        finally:
            try:
                await browser.close()
            except Exception:
                pass


async def fetch_target(url, engine, proxy, retries, session_file, is_interactive, wait, timeout=30):
    # retries=0 表示只尝试 1 次，不重试；retries=N 表示尝试 N+1 次
    for i in range(retries + 1):
        try:
            if engine == 'playwright':
                return await fetch_with_playwright(url, proxy, timeout, session_file, is_interactive, wait)
            return await fetch_with_curl_cffi(url, proxy, timeout, session_file)
        except Exception as e:
            if i == retries:  # 最后一次尝试失败才抛出异常
                raise e
            await asyncio.sleep(1)


class PlaywrightPool:
    """
    Playwright 并发池 - 复用单个 Browser，创建多个 Context/Page 实现并发

    优势：
    - 只启动一个浏览器进程，资源占用低
    - 每个请求有独立的 Context，Cookie 隔离
    - 支持真正的并发抓取
    """

    def __init__(
        self,
        proxy: Optional[str] = None,
        pool_size: int = 3,
        headless: bool = False,
        user_agent: Optional[str] = None,
    ):
        self.proxy = proxy
        self.pool_size = pool_size
        self.headless = headless
        self.user_agent = user_agent  # None = 用浏览器自带 UA

        self._playwright = None
        self._browser: Optional[Browser] = None
        self._semaphore: Optional[asyncio.Semaphore] = None
        self._lock = asyncio.Lock()

    async def _init_browser(self):
        """初始化浏览器（仅调用一次）"""
        if self._browser is not None:
            return

        async with self._lock:
            if self._browser is not None:
                return

            self._playwright = await async_playwright().start()

            launch_args = ['--disable-blink-features=AutomationControlled',
                           '--window-position=-2400,-2400', '--window-size=1280,900']
            if sys.platform != 'win32':
                launch_args.append('--no-sandbox')

            _ensure_display()
            self._browser = await self._playwright.chromium.launch(
                headless=self.headless,
                proxy={"server": self.proxy} if self.proxy else None,
                args=launch_args
            )

            self._semaphore = asyncio.Semaphore(self.pool_size)

    async def fetch_page(
        self,
        url: str,
        timeout: int = 30,
        session_file: Optional[str] = None,
        wait: int = 3,
    ) -> Tuple[str, str]:
        """从池中获取一个 Page 来抓取"""
        await self._init_browser()

        async with self._semaphore:
            # 创建独立的 Context（Cookie 隔离）
            context_args = {
                "viewport": {"width": 1280, "height": 800},
                "locale": "zh-CN",
            }
            if self.user_agent:
                context_args["user_agent"] = self.user_agent

            if session_file and os.path.exists(session_file):
                context_args["storage_state"] = session_file

            context = await self._browser.new_context(**context_args)

            # 注入 stealth（如果可用）
            page = await context.new_page()
            if playwright_stealth or _Stealth:
                try:
                    # 不再手动改 UA（会与真实内核版本不符）
                    await apply_stealth(page)
                except Exception as e:
                    print(f"[*] ⚠️ stealth 注入失败（继续抓取）: {e}", file=sys.stderr)

            try:
                await page.goto(url, wait_until="domcontentloaded", timeout=timeout * 1000)
                await asyncio.sleep(wait)

                content = await page.content()
                try:
                    ctype = await page.evaluate("document.contentType || 'text/html'")
                except:
                    ctype = 'text/html'
                return content, ctype
            finally:
                await context.close()

    async def close(self):
        """关闭浏览器"""
        if self._browser:
            await self._browser.close()
            self._browser = None
        if self._playwright:
            await self._playwright.stop()
            self._playwright = None

    async def __aenter__(self):
        await self._init_browser()
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb):
        await self.close()


async def playwright_probe(url: str, session_file: str = None, eval_js: str = None,
                           xhr_pattern: str = None, wait: int = 6, timeout: int = 45,
                           show: bool = False, replay: bool = False,
                           replaces: list = None) -> str:
    """返回 JS（--xhr）或在页面里执行 JS（--eval）。

    走有头浏览器（无头会被站点识别）。返回一个 JSON 字符串。
    """
    import json as _json
    pos = "120,80" if show else "-2400,-2400"
    _ensure_display()
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=False, args=[
            '--no-sandbox', '--disable-blink-features=AutomationControlled',
            f'--window-position={pos}', '--window-size=1280,900'])
        ctx_args = {"locale": "zh-CN", "viewport": {"width": 1366, "height": 900}}
        if session_file and os.path.exists(session_file):
            ctx_args["storage_state"] = session_file
        ctx = await browser.new_context(timezone_id="Asia/Shanghai", **ctx_args)
        await _harden(ctx)
        page = await ctx.new_page()

        keep_body = bool(replay)          # 只列候选时不回大 body，省 token
        captured = []
        if xhr_pattern is not None:
            async def on_resp(resp):
                try:
                    req = resp.request
                    if req.resource_type not in ('xhr', 'fetch'):
                        return
                    if xhr_pattern and xhr_pattern not in resp.url:
                        return
                    body = await resp.text()
                    ct = resp.headers.get('content-type', '')
                    item = {
                        'url': resp.url.split('?')[0],
                        'url_full': resp.url,
                        'status': resp.status,
                        'method': req.method,
                        'request_body': (req.post_data or '')[:4000],
                        'content_type': ct,
                        'bytes': len(body),
                        'looks_json': body.lstrip()[:1] in ('{', '['),
                        'preview': body[:180].replace('\n', ' '),
                    }
                    if keep_body:
                        item['body'] = body[:20000]
                    captured.append(item)
                except Exception:
                    pass
            page.on('response', lambda r: asyncio.create_task(on_resp(r)))

        result = {'url': url}
        try:
            await page.goto(url, timeout=timeout * 1000, wait_until='domcontentloaded')
            await page.wait_for_timeout(max(wait, 2) * 1000)
        except Exception as e:
            result['goto_error'] = str(e)

        if eval_js:
            try:
                result['result'] = await page.evaluate(eval_js)
            except Exception as e:
                result['eval_error'] = str(e)
        if xhr_pattern is not None:
            result['count'] = len(captured)
            result['captured'] = captured
            if replay and captured:
                entry = dict(captured[0])
                if replaces:                      # 实验：替换请求里的参数再回放
                    body = entry.get('request_body') or ''
                    url = entry.get('url_full') or ''
                    from urllib.parse import quote
                    for pair in replaces:
                        if '=' not in pair:
                            continue
                        old, new = pair.split('=', 1)
                        for o, n in ((old, new), (quote(old, safe=''), quote(new, safe=''))):
                            body = body.replace(o, n)
                            url = url.replace(o, n)
                    entry['request_body'] = body
                    entry['url_full'] = url
                    result['replaced'] = replaces
                result['replay'] = await _replay_captured(entry, await ctx.cookies())

        await browser.close()
        return _json.dumps(result, ensure_ascii=False)


async def _replay_captured(entry: dict, cookies: list) -> dict:
    """用 curl_cffi 回放浏览器抓到的请求，验证“不开浏览器也能拿到”。"""
    import time
    from curl_cffi import requests as creq

    jar = {c['name']: c['value'] for c in cookies if c.get('name')}
    headers = {
        'User-Agent': ('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 '
                       '(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'),
        'Referer': entry.get('url', ''),
        'X-Requested-With': 'XMLHttpRequest',
        'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8',
    }
    body = entry.get('request_body') or None
    t0 = time.time()
    try:
        r = creq.request(entry.get('method', 'POST'), entry.get('url_full') or entry['url'],
                         data=body, headers=headers, cookies=jar,
                         impersonate='chrome', timeout=30)
        cost = time.time() - t0
        return {'status': r.status_code, 'seconds': round(cost, 3),
                'bytes': len(r.content), 'body_head': r.text[:3000]}
    except Exception as e:
        return {'error': str(e), 'seconds': round(time.time() - t0, 3)}
