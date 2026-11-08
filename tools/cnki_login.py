#!/usr/bin/env python3
"""开一个有头浏览器让主人登录知网，期间每 10 秒保存一次登录态。"""
import asyncio, os, sys
from playwright.async_api import async_playwright

OUT = os.path.expanduser('~/.agents/super-fetch/cnki.json')
URL = sys.argv[1] if len(sys.argv) > 1 else 'https://fsso.cnki.net/'

async def main():
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    async with async_playwright() as p:
        b = await p.chromium.launch(headless=False, args=['--no-sandbox', '--start-maximized'])
        print('launched', flush=True)
        ctx = await b.new_context(viewport=None, locale='zh-CN')
        page = await ctx.new_page()
        print('page created', flush=True)
        try:
            await page.goto(URL, timeout=45000, wait_until='domcontentloaded')
            print('goto ok:', await page.title(), flush=True)
        except Exception as e:
            print('goto 失败（窗口仍开着）:', e, flush=True)
        for i in range(180):                     # 30 分钟
            await asyncio.sleep(10)
            try:
                await ctx.storage_state(path=OUT)
                print('saved', i, flush=True)
            except Exception as e:
                print('save err', e, flush=True)

asyncio.run(main())
