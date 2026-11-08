#!/usr/bin/env python3
"""通用 MCP 客户端 —— 按需调用，不挂进工具列表。

用法：
    ./mcp_call.py list                       # 列出已登记的服务器
    ./mcp_call.py tools amap                 # 列出某服务器的工具
    ./mcp_call.py call amap maps_geo address=杭州西湖
    ./mcp_call.py call amap maps_geo --json '{"address":"杭州西湖"}'

服务器登记在 ~/agents/xiaoyu/credentials/mcp_servers.json（600）：
    {"amap": {"transport": "http", "url": "https://mcp.amap.com/mcp?key=..."},
     "local": {"transport": "stdio", "command": "uv",
               "args": ["run", "server.py"], "env": {}}}

两种传输：http（streamable-http/SSE）与 stdio（本地进程，换行分隔 JSON-RPC）。
"""
import json
import os
import subprocess
import sys
import urllib.request

REG = os.path.expanduser('~/agents/xiaoyu/credentials/mcp_servers.json')
PROTO = '2025-03-26'


# ---------------------------------------------------------------- http
def _post(url, payload, headers, sid):
    h = {'Content-Type': 'application/json',
         'Accept': 'application/json, text/event-stream'}
    h.update(headers or {})
    if sid:
        h['mcp-session-id'] = sid
    req = urllib.request.Request(url, data=json.dumps(payload).encode(), headers=h, method='POST')
    with urllib.request.urlopen(req, timeout=60) as r:
        sid = r.headers.get('mcp-session-id') or sid
        ctype = r.headers.get('content-type', '')
        body = r.read().decode('utf-8', 'replace')
    if 'text/event-stream' in ctype:
        for line in body.splitlines():
            if line.startswith('data:'):
                return json.loads(line[5:].strip()), sid
        return {}, sid
    return (json.loads(body) if body.strip() else {}), sid


def http_session(srv):
    url, headers = srv['url'], srv.get('headers')
    _, sid = _post(url, {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize',
                         'params': {'protocolVersion': PROTO, 'capabilities': {},
                                    'clientInfo': {'name': 'xiaoyu', 'version': '0.1'}}}, headers, None)
    try:
        _post(url, {'jsonrpc': '2.0', 'method': 'notifications/initialized'}, headers, sid)
    except Exception:
        pass
    return lambda method, params=None: _post(
        url, {'jsonrpc': '2.0', 'id': 2, 'method': method, 'params': params or {}}, headers, sid)[0]


# ---------------------------------------------------------------- stdio
class StdioSession:
    def __init__(self, srv):
        env = dict(os.environ, **{k: str(v) for k, v in (srv.get('env') or {}).items()})
        self.p = subprocess.Popen([srv['command']] + [str(a) for a in srv.get('args', [])],
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=subprocess.DEVNULL, env=env,
                                  cwd=srv.get('cwd'), text=True, bufsize=1)
        self._req('initialize', {'protocolVersion': PROTO, 'capabilities': {},
                                 'clientInfo': {'name': 'xiaoyu', 'version': '0.1'}})
        self._send({'jsonrpc': '2.0', 'method': 'notifications/initialized'})

    def _send(self, obj):
        self.p.stdin.write(json.dumps(obj) + '\n')
        self.p.stdin.flush()

    def _req(self, method, params=None):
        self._send({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params or {}})
        while True:
            line = self.p.stdout.readline()
            if not line:
                raise RuntimeError('服务器进程已退出')
            line = line.strip()
            if not line:
                continue
            try:
                msg = json.loads(line)
            except json.JSONDecodeError:
                continue          # 服务器可能往 stdout 打日志，跳过
            if msg.get('id') == 1:
                return msg

    def __call__(self, method, params=None):
        return self._req(method, params)

    def close(self):
        try:
            self.p.stdin.close()
            self.p.terminate()
        except Exception:
            pass


# ---------------------------------------------------------------- cli
def registry():
    if not os.path.exists(REG):
        raise SystemExit('还没有登记服务器：%s' % REG)
    return json.load(open(REG))


def content_text(res):
    out = []
    for block in (res.get('result', {}).get('content') or []):
        if block.get('type') == 'text':
            out.append(block.get('text', ''))
    return '\n'.join(out) if out else json.dumps(res, ensure_ascii=False)


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        return 1
    cmd, reg = sys.argv[1], registry()

    if cmd == 'list':
        for name, s in reg.items():
            print('%-10s %-6s %s' % (name, s.get('transport', 'http'),
                                     s.get('url') or s.get('command', '')))
        return 0

    if len(sys.argv) < 3:
        raise SystemExit('缺少服务器名')
    name = sys.argv[2]
    if name not in reg:
        raise SystemExit('未登记：%s（可用：%s）' % (name, ', '.join(reg)))
    srv = reg[name]
    sess = StdioSession(srv) if srv.get('transport') == 'stdio' else http_session(srv)
    try:
        if cmd == 'tools':
            res = sess('tools/list')
            for t in res['result']['tools']:
                print('-', t['name'], '|', ','.join(t['inputSchema'].get('required', [])))
            return 0
        if cmd == 'call':
            tool = sys.argv[3]
            if '--json' in sys.argv:
                args = json.loads(sys.argv[sys.argv.index('--json') + 1])
            else:
                args = dict(a.split('=', 1) for a in sys.argv[4:])
            res = sess('tools/call', {'name': tool, 'arguments': args})
            if res.get('result', {}).get('isError'):
                print('工具报错：', content_text(res))
                return 2
            print(content_text(res))
            return 0
    finally:
        if isinstance(sess, StdioSession):
            sess.close()
    raise SystemExit('未知命令：%s' % cmd)


if __name__ == '__main__':
    sys.exit(main())
