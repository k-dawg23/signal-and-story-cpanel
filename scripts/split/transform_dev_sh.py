#!/usr/bin/env python3
"""Rewrite scripts/dev.sh for Go-only or Node-only GitHub exports. See REPOSITORIES.md."""
from __future__ import annotations

import sys
from pathlib import Path


def strip_export_lines(lines: list[str]) -> list[str]:
    return [ln for ln in lines if "### EXPORT_OMIT_" not in ln]


def slice_out_range(lines: list[str], start: str, end: str) -> list[str]:
    out: list[str] = []
    i = 0
    while i < len(lines):
        if start in lines[i]:
            i += 1
            while i < len(lines) and end not in lines[i]:
                i += 1
            if i < len(lines):
                i += 1
            continue
        out.append(lines[i])
        i += 1
    return out


def go_line(path: Path) -> None:
    raw = path.read_text(encoding="utf-8").splitlines(keepends=True)
    cut = slice_out_range(raw, "EXPORT_OMIT_GO_LINE_START", "EXPORT_OMIT_GO_LINE_END")
    fixed: list[str] = []
    for ln in cut:
        if ln.lstrip().startswith("elif GO_CMD"):
            ln = ln.replace("elif GO_CMD", "if GO_CMD", 1)
        fixed.append(ln)
    out = strip_export_lines(fixed)
    path.write_text("".join(out), encoding="utf-8")


def node_line(path: Path) -> None:
    raw = path.read_text(encoding="utf-8").splitlines(keepends=True)
    cut = slice_out_range(raw, "EXPORT_OMIT_NODE_LINE_START", "EXPORT_OMIT_NODE_LINE_END")
    out = strip_export_lines(cut)
    text = "".join(out)
    text = text.replace('SAS_API:-go" == "node"', 'SAS_API:-node" == "node"')
    path.write_text(text, encoding="utf-8")


def main() -> None:
    if len(sys.argv) != 3:
        print("usage: transform_dev_sh.py go|node path/to/dev.sh", file=sys.stderr)
        sys.exit(2)
    mode, p = sys.argv[1], Path(sys.argv[2])
    if mode == "go":
        go_line(p)
    elif mode == "node":
        node_line(p)
    else:
        print("mode must be go or node", file=sys.stderr)
        sys.exit(2)


if __name__ == "__main__":
    main()
