#!/usr/bin/env python3
"""WP の範囲の検査(作業指示書 3.1 の共通の完了条件 3。監督の受け入れ用)。make の試験から呼ばない。

使い方:
  check_scope.py --root <worktree> --base <開始 commit> --allow <パスの先頭> [--allow ...]

判定: 開始 commit からの差分(未 commit と未追跡を含む)の全行が --allow のどれかで始まり、変更禁止のパスに触れていない。
終了コード: 0 合格 / 1 範囲の外か変更禁止に差分あり / 2 入力の誤り
"""
import argparse
import fnmatch
import subprocess
import sys

PROTECTED_PREFIXES = (
    "app/test/contract/",
    "internal/contract/",
    "adapters/acquire/recv/embed.go",
    "app/src/i18n/keys.json",
    "app/src/i18n/check.ts",
    "data/prices/src/",
    "app/src/kit/types.ts",
    "app/src/data/send-fields.json",
    "tools/lintsize/",
    "app/eslint.config.js",
)
PROTECTED_GLOBS = ("*/contract*_test.go", "internal/*/doc.go")


def git(root, *args):
    return subprocess.run(["git", "-C", root, *args], check=True, capture_output=True, text=True).stdout.splitlines()


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--root", required=True)
    parser.add_argument("--base", required=True)
    parser.add_argument("--allow", required=True, action="append")
    args = parser.parse_args()
    try:
        changed = set(git(args.root, "diff", "--name-only", args.base))
        changed |= set(git(args.root, "ls-files", "--others", "--exclude-standard"))
    except subprocess.CalledProcessError as e:
        print(f"error: {e.stderr.strip()}", file=sys.stderr)
        return 2
    outside = sorted(p for p in changed if not p.startswith(tuple(args.allow)))
    protected = sorted(
        p for p in changed
        if p.startswith(PROTECTED_PREFIXES) or any(fnmatch.fnmatch(p, g) for g in PROTECTED_GLOBS)
    )
    print(f"changed={len(changed)} outside={len(outside)} protected={len(protected)}")
    for p in outside:
        print(f"outside {p}")
    for p in protected:
        print(f"protected {p}")
    ok = not outside and not protected
    print("PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
