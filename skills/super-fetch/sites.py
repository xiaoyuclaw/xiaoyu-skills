#!/usr/bin/env python3
"""接口库：记录 discover 出来的数据接口，供后续参考。

刻意只做“参考”——存接口地址、方法、请求体样本、需要的 cookie 键名，
运行时不自动替你发请求，避免把过期配方当真理。
"""
import json
import sqlite3
from urllib.parse import urlparse

from core import DB_PATH

DDL = """
CREATE TABLE IF NOT EXISTS sites (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  domain        TEXT NOT NULL,
  pattern       TEXT NOT NULL,
  method        TEXT,
  endpoint      TEXT,
  body_template TEXT,
  cookie_keys   TEXT,
  extract       TEXT,
  note          TEXT,
  status        TEXT DEFAULT 'active',
  ok_count      INTEGER DEFAULT 0,
  fail_count    INTEGER DEFAULT 0,
  created_at    TEXT DEFAULT CURRENT_TIMESTAMP,
  last_ok       TEXT,
  UNIQUE(domain, pattern)
)
"""


def _conn():
    c = sqlite3.connect(DB_PATH)
    c.row_factory = sqlite3.Row
    c.execute(DDL)
    return c


def save_site(domain, pattern, method, endpoint, body_template="", cookie_keys=None,
              extract="", note=""):
    cookie_keys = json.dumps(cookie_keys or [], ensure_ascii=False)
    with _conn() as c:
        c.execute("""
            INSERT INTO sites (domain, pattern, method, endpoint, body_template, cookie_keys, extract, note)
            VALUES (?,?,?,?,?,?,?,?)
            ON CONFLICT(domain, pattern) DO UPDATE SET
                method=excluded.method, endpoint=excluded.endpoint,
                body_template=excluded.body_template, cookie_keys=excluded.cookie_keys,
                extract=excluded.extract, note=excluded.note, status='active'
        """, (domain, pattern, method, endpoint, body_template[:4000], cookie_keys, extract, note))


def find_site(url):
    u = urlparse(url)
    domain, path = u.netloc, u.path or "/"
    with _conn() as c:
        rows = c.execute("SELECT * FROM sites WHERE domain=? AND status='active'", (domain,)).fetchall()
    best = None
    for r in rows:
        if r["pattern"] and r["pattern"] in url:
            if best is None or len(r["pattern"]) > len(best["pattern"]):
                best = r
    return dict(best) if best else None


def list_sites(domain=None):
    with _conn() as c:
        if domain:
            rows = c.execute("SELECT * FROM sites WHERE domain LIKE ? ORDER BY status, domain",
                             (f"%{domain}%",)).fetchall()
        else:
            rows = c.execute("SELECT * FROM sites ORDER BY domain, pattern").fetchall()
    return [dict(r) for r in rows]


def touch(url, ok=True):
    s = find_site(url)
    if not s:
        return
    with _conn() as c:
        if ok:
            c.execute("UPDATE sites SET ok_count=ok_count+1, last_ok=CURRENT_TIMESTAMP WHERE id=?", (s["id"],))
        else:
            c.execute("""UPDATE sites SET fail_count=fail_count+1,
                         status=CASE WHEN fail_count+1>=3 THEN 'stale' ELSE status END
                         WHERE id=?""", (s["id"],))
