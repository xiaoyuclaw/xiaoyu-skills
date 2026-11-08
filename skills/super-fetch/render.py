#!/usr/bin/env python3
"""把探测结果渲染成 AI 易读的紧凑 markdown（默认），-j 时仍可拿原始 JSON。"""


def _shape(v, depth=0):
    if isinstance(v, dict):
        return "object{%s}" % ", ".join(list(v)[:6])
    if isinstance(v, list):
        inner = _shape(v[0], depth + 1) if v else ""
        return "array[%d]%s" % (len(v), (" " + inner) if inner else "")
    if isinstance(v, str):
        return "str[%d]" % len(v)
    return type(v).__name__


def outline(data, prefix="", depth=0, max_rows=12):
    """把嵌套结构压成若干行 key-path，只到第二层。"""
    rows = []

    def walk(node, path, d):
        if len(rows) >= max_rows or d > 1:
            return
        if isinstance(node, dict):
            for k, v in node.items():
                p = f"{path}.{k}" if path else k
                rows.append((p, _shape(v)))
                if isinstance(v, (dict, list)) and len(rows) < max_rows:
                    walk(v[0] if isinstance(v, list) and v else v, p, d + 1)
        elif isinstance(node, list) and node:
            walk(node[0], path + "[0]", d + 1)

    walk(data, prefix, depth)
    return rows


def md_embedded(blocks, limit_rows=12):
    if not blocks:
        return "没有内嵌 JSON。数据可能在接口里，试 `--discover`。"
    out = []
    for b in blocks:
        out.append(f"## {b['kind']}（{b['chars']} 字符）")
        if b.get("truncated"):
            out.append(f"> 已截断，完整数据用 `-L full -o file.json`")
            data = None
        else:
            data = b.get("data")
        rows = b.get("outline")
        if rows is None and data is not None:
            rows = outline(data, max_rows=limit_rows)
        for path, shape in (rows or []):
            out.append(f"- `{path}`: {shape}")
        prev = (b.get("preview") or "").strip()
        if prev:
            out.append(f"\n预览: `{prev[:160]}`")
        out.append("")
    return "\n".join(out).strip()


def md_discover(captured, count, url=""):
    if not captured:
        return "没有捕获到 XHR/fetch 请求。"
    out = [f"## 候选接口（{count} 个）", "",
           "| # | 方法 | 路径 | 状态 | 大小 | JSON |", "|---|---|---|---|---|---|"]
    for i, c in enumerate(captured):
        p = c["url"].split("://", 1)[-1]
        out.append("| %d | %s | %s | %s | %d B | %s |" % (
            i, c["method"], p[:60], c["status"], c["bytes"], "✓" if c["looks_json"] else ""))
    out.append("")
    out.append("挑一个用：`--discover \"路径子串\" --verify`")
    return "\n".join(out)


def md_replay(rep, method="", url=""):
    if not rep:
        return ""
    if rep.get("error"):
        return f"### 回放失败\n`{rep['error']}`（{rep.get('seconds')}s）"
    head = (rep.get("body_head") or "").strip().replace("\n", " ")[:180]
    return ("### 回放成功\n"
            f"- `{method} {url}`\n"
            f"- 状态 {rep['status']} ｜ {rep['bytes']} B ｜ {rep['seconds']}s\n"
            f"- 片段: `{head}`")


def md_eval(val, limit=800):
    import json as _json
    s = _json.dumps(val, ensure_ascii=False)
    if len(s) <= limit:
        return "```json\n" + s + "\n```"
    return "```json\n" + s[:limit] + "\n…（已截断，完整用 -j）\n```"
