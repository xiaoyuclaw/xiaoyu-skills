#!/usr/bin/env bash
# 在技能目录内创建 .venv 并装依赖（不污染系统 Python）
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

if command -v uv >/dev/null 2>&1; then
  uv venv .venv
  uv pip install --python .venv/bin/python -r requirements.txt
else
  python3 -m venv .venv
  .venv/bin/pip install -U pip
  .venv/bin/pip install -r requirements.txt
fi

# Playwright 浏览器（已存在则跳过下载）
.venv/bin/python -m playwright install chromium chromium-headless-shell || true
echo "[*] 完成：$DIR/.venv"
