#!/usr/bin/env python3
"""
Super Fetch Core - 共享工具模块
统一存放跨文件使用的常量、工具函数和配置
"""
import sys
import os
import pathlib
import asyncio


# 技能根目录（代码跟着技能走，不写死绝对路径）
SKILL_DIR = pathlib.Path(__file__).resolve().parent


def get_data_dir():
    """数据目录：默认在技能目录下的 .data，可用 SUPER_FETCH_HOME 覆盖。"""
    override = os.environ.get("SUPER_FETCH_HOME")
    if override:
        return pathlib.Path(os.path.abspath(os.path.expanduser(override)))
    return SKILL_DIR / ".data"


# 统一数据根目录
DATA_DIR = get_data_dir()
DATA_DIR.mkdir(parents=True, exist_ok=True)
try:
    os.chmod(DATA_DIR, 0o700)   # 已有目录也收紧权限
except OSError:
    pass

# 数据库路径
DB_PATH = str(DATA_DIR / "links.db")


def secure_file(path):
    """把文件权限收紧到 0600（会话文件里有 Cookie）"""
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass


def resolve_session_path(session_arg):
    """解析会话文件路径。

    相对路径只允许落在数据目录内，防止 `-s ../../x` 逃逸出去。
    """
    if not session_arg:
        return None
    # 如果是绝对路径或当前路径已存在该文件，直接使用
    if os.path.isabs(session_arg) or os.path.exists(session_arg):
        return session_arg
    # 否则，强制定位到数据目录下（剥掉可能的上跳）
    return str(DATA_DIR / os.path.basename(session_arg))


def setup_asyncio():
    """跨平台设置 asyncio 事件循环"""
    if sys.platform == "win32":
        # Windows 上使用 ProactorEventLoop 以支持 subprocess
        try:
            loop = asyncio.ProactorEventLoop()
            asyncio.set_event_loop(loop)
        except (AttributeError, NotImplementedError):
            pass


# 真实浏览器请求头
REAL_HEADERS = {
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
    "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
    "Sec-Ch-Ua": '"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"',
    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
}


# User-Agent 池
USER_AGENTS = [
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Safari/605.1.15",
]


# 反检测：抹掉最常见的自动化特征（对所有浏览器上下文生效）
STEALTH_JS = """
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
Object.defineProperty(navigator, 'languages', { get: () => ['zh-CN', 'zh', 'en'] });
Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3, 4, 5] });
window.chrome = window.chrome || { runtime: {} };
const _q = window.navigator.permissions && window.navigator.permissions.query;
if (_q) {
  window.navigator.permissions.query = (p) =>
    p && p.name === 'notifications'
      ? Promise.resolve({ state: Notification.permission })
      : _q.call(window.navigator.permissions, p);
}
const _gp = WebGLRenderingContext.prototype.getParameter;
WebGLRenderingContext.prototype.getParameter = function (p) {
  if (p === 37445) return 'Intel Inc.';
  if (p === 37446) return 'Intel Iris OpenGL Engine';
  return _gp.call(this, p);
};
"""
