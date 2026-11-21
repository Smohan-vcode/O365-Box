#!/usr/bin/env python3
"""Render ANSI frames (from the TUI's TestShots) into PNG screenshots.

Usage: tui-shots.py <frames-dir> <out-dir>
Writes <name>.html next to each PNG and screenshots them with Node Playwright
(scripts/tui-shots.js). Engine tooling: no company data.
"""
import html, os, re, sys

PALETTE16 = ["#000000", "#cd3131", "#0dbc79", "#e5e510", "#2472c8", "#bc3fbc", "#11a8cd", "#e5e5e5",
             "#666666", "#f14c4c", "#23d18b", "#f5f543", "#3b8eea", "#d670d6", "#29b8db", "#ffffff"]


def xterm(n):
    if n < 16:
        return PALETTE16[n]
    if n < 232:
        n -= 16
        steps = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (steps[n // 36], steps[(n // 6) % 6], steps[n % 6])
    v = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (v, v, v)


SGR = re.compile(r"\x1b\[([0-9;]*)m")


def to_html(text):
    out, fg, bg, bold, italic, under, faint = [], None, None, False, False, False, False
    pos = 0
    for m in list(SGR.finditer(text)) + [None]:
        end = m.start() if m else len(text)
        chunk = text[pos:end]
        if chunk:
            style = []
            if fg: style.append("color:" + fg)
            if bg: style.append("background:" + bg)
            if bold: style.append("font-weight:700")
            if italic: style.append("font-style:italic")
            if under: style.append("text-decoration:underline")
            if faint: style.append("opacity:.6")
            out.append('<span style="%s">%s</span>' % (";".join(style), html.escape(chunk)) if style else html.escape(chunk))
        if not m:
            break
        pos = m.end()
        codes = [int(c) if c else 0 for c in m.group(1).split(";")]
        i = 0
        while i < len(codes):
            c = codes[i]
            if c == 0: fg = bg = None; bold = italic = under = faint = False
            elif c == 1: bold = True
            elif c == 2: faint = True
            elif c == 3: italic = True
            elif c == 4: under = True
            elif c == 22: bold = faint = False
            elif c == 23: italic = False
            elif c == 24: under = False
            elif c == 39: fg = None
            elif c == 49: bg = None
            elif 30 <= c <= 37: fg = PALETTE16[c - 30]
            elif 90 <= c <= 97: fg = PALETTE16[c - 90 + 8]
            elif 40 <= c <= 47: bg = PALETTE16[c - 40]
            elif 100 <= c <= 107: bg = PALETTE16[c - 100 + 8]
            elif c in (38, 48) and i + 2 < len(codes) and codes[i + 1] == 5:
                col = xterm(codes[i + 2]); i += 2
                if c == 38: fg = col
                else: bg = col
            elif c in (38, 48) and i + 4 < len(codes) and codes[i + 1] == 2:
                col = "#%02x%02x%02x" % tuple(codes[i + 2:i + 5]); i += 4
                if c == 38: fg = col
                else: bg = col
            i += 1
    return "".join(out)


PAGE = """<!doctype html><html><head><meta charset="utf-8"><style>
body{margin:0;background:#1b1d23;padding:28px}
.win{display:inline-block;background:#0f1117;border-radius:12px;box-shadow:0 18px 50px rgba(0,0,0,.55);overflow:hidden}
.bar{height:30px;background:#23262f;display:flex;align-items:center;padding:0 12px;gap:8px}
.dot{width:12px;height:12px;border-radius:50%%}
.t{color:#9aa0ab;font:12px -apple-system,Segoe UI,sans-serif;margin-left:10px}
pre{margin:0;padding:16px 18px;color:#d0d0d0;font:15px/1.32 'DejaVu Sans Mono','Menlo',monospace;white-space:pre}
</style></head><body><div class="win"><div class="bar"><div class="dot" style="background:#ff5f57"></div>
<div class="dot" style="background:#febc2e"></div><div class="dot" style="background:#28c840"></div>
<div class="t">box · Company in a Box</div></div><pre>%s</pre></div></body></html>"""


def main():
    src, dst = sys.argv[1], sys.argv[2]
    os.makedirs(dst, exist_ok=True)
    for name in sorted(f for f in os.listdir(src) if f.endswith(".ansi")):
        body = to_html(open(os.path.join(src, name), encoding="utf-8").read())
        with open(os.path.join(dst, name[:-5] + ".html"), "w", encoding="utf-8") as f:
            f.write(PAGE % body)
    import subprocess
    subprocess.run(["node", os.path.join(os.path.dirname(os.path.abspath(__file__)), "tui-shots.js"), dst], check=True)


if __name__ == "__main__":
    main()
