#!/usr/bin/env python3
import sqlite3
import sys
import os
import argparse

from core import DB_PATH


def main():
    parser = argparse.ArgumentParser(description="根据代号反查原始链接工具")
    parser.add_argument(
        "ids", nargs="*", help="链接代号(如 abcd-1) 或 命名空间(如 abcd)"
    )
    parser.add_argument(
        "--clear",
        "-c",
        action="store_true",
        help="清理模式。若带参数则清理指定项，不带参数则清空全部（需配 --yes）。",
    )
    parser.add_argument("--yes", "-y", action="store_true", help="确认清空全部记录")
    args = parser.parse_args()

    if not os.path.exists(DB_PATH) and not args.clear:
        print(f"错误: 未找到链接数据库 {DB_PATH}")
        sys.exit(1)

    try:
        # 增加 timeout 防止在 fetch.py 写入时产生数据库锁冲突
        with sqlite3.connect(DB_PATH, timeout=20) as conn:
            if args.clear:
                if not args.ids:
                    # 情况 1: 完全清空 —— 必须显式确认，防手滑
                    if not args.yes:
                        total = conn.execute("SELECT COUNT(*) FROM links").fetchone()[0]
                        print(f"拒绝执行：将清空全部 {total} 条链接记录。"
                              f"确认请加 --yes。")
                        return
                    conn.execute("DELETE FROM links")
                    print("[*] 🧹 已清空数据库中所有链接记录。")
                else:
                    # 情况 2: 精准清理
                    for item in args.ids:
                        clean_val = item.lstrip("@")
                        if "-" in clean_val:
                            # 如果提供了具体编号，只删那一个
                            cursor = conn.execute(
                                "DELETE FROM links WHERE id = ?", (clean_val,)
                            )
                            print(f"[*] 🧹 已清理特定链接: @{clean_val}")
                        else:
                            # 如果只提供命名空间，删除该组下所有链接
                            cursor = conn.execute(
                                "DELETE FROM links WHERE id LIKE ?", (f"{clean_val}-%",)
                            )
                            print(
                                f"[*] 🧹 已清理命名空间: {clean_val} (影响 {cursor.rowcount} 条记录)"
                            )

                try:
                    conn.execute("VACUUM")  # 压缩数据库文件体积
                except:
                    pass
                return

            # 查询模式
            if args.ids:
                cursor = conn.cursor()
                for lid in args.ids:
                    clean_id = lid.lstrip("@")
                    cursor.execute("SELECT url FROM links WHERE id = ?", (clean_id,))
                    row = cursor.fetchone()
                    if row:
                        print(f"@{clean_id} -> {row[0]}")
                    else:
                        print(f"@{clean_id} -> [未找到或已清理]")
            else:
                parser.print_help()

    except Exception as e:
        print(f"数据库操作失败: {str(e)}")
        sys.exit(1)


if __name__ == "__main__":
    main()
