#!/usr/bin/env python3
"""Generate every project asset from one palette and one mark.

Hand-editing several SVGs let them drift: three different background gradients
and two different accent colours shipped side by side. Everything here derives
from PALETTE and mark(), so the assets cannot disagree again. Re-run after any
visual change:

    python3 assets/generate.py && bash assets/render.sh
"""
import math
import pathlib

OUT = pathlib.Path(__file__).parent

PALETTE = {
    "ink_top": "#16293F",   # plate highlight
    "ink_mid": "#0F1B2D",   # plate body
    "ink_bot": "#080F1A",   # plate shadow
    "accent_a": "#00ADD8",  # Go blue; the kept layers
    "accent_b": "#22D3F5",
    "foam": "#7DE3FF",      # the layer being reclaimed
    "text": "#E8F4FA",
    "muted": "#8FB3C7",
    "amber": "#E3B341",     # dry-run notice
}

# The mark in unit coordinates (0..1 box). Two solid layers are kept; the third
# is intact on the left and breaks into fragments that lift away.
BARS = [
    (0.000, 0.716, 1.000, 0.122, "solid", 1.00),
    (0.000, 0.556, 1.000, 0.122, "solid", 0.80),
    (0.000, 0.396, 0.449, 0.122, "foam", 0.94),
]
FRAGMENTS = [
    (0.518, 0.400, 0.159, 0.70, -12),
    (0.728, 0.322, 0.127, 0.48, 18),
    (0.902, 0.222, 0.094, 0.32, -22),
    (1.032, 0.136, 0.069, 0.19, 12),
]


def mark(x0, y0, size, filter_id=None):
    """Emit the mark as SVG, scaled into a size-by-size box at (x0, y0)."""
    p = []
    for bx, by, bw, bh, kind, op in BARS:
        fill = "url(#solid)" if kind == "solid" else PALETTE["foam"]
        p.append(
            f'<rect x="{x0 + bx * size:.1f}" y="{y0 + by * size:.1f}" '
            f'width="{bw * size:.1f}" height="{bh * size:.1f}" '
            f'rx="{bh * size / 2:.1f}" fill="{fill}" opacity="{op}"/>'
        )
    for fx, fy, fs, op, rot in FRAGMENTS:
        w = fs * size
        cx, cy = x0 + fx * size + w / 2, y0 + fy * size + w / 2
        p.append(
            f'<rect x="{x0 + fx * size:.1f}" y="{y0 + fy * size:.1f}" '
            f'width="{w:.1f}" height="{w:.1f}" rx="{w * 0.3:.1f}" '
            f'fill="{PALETTE["foam"]}" opacity="{op}" '
            f'transform="rotate({rot} {cx:.1f} {cy:.1f})"/>'
        )
    body = "\n    ".join(p)
    if filter_id:
        return f'<g filter="url(#{filter_id})">\n    {body}\n  </g>'
    return body


def defs(extra=""):
    return f'''<defs>
    <linearGradient id="plate" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="{PALETTE['ink_top']}"/>
      <stop offset="0.5" stop-color="{PALETTE['ink_mid']}"/>
      <stop offset="1" stop-color="{PALETTE['ink_bot']}"/>
    </linearGradient>
    <linearGradient id="solid" x1="0" y1="0" x2="1" y2="0">
      <stop offset="0" stop-color="{PALETTE['accent_a']}"/>
      <stop offset="1" stop-color="{PALETTE['accent_b']}"/>
    </linearGradient>{extra}
  </defs>'''


def squircle_path(cx, cy, half, n=5.0, steps=720):
    """Apple-style superellipse. A rounded rect's circular corner meets the
    straight edge with a curvature jump; a squircle's curvature is continuous,
    which is what makes macOS icons read as smooth rather than merely round."""
    pts = []
    for i in range(steps):
        t = 2 * math.pi * i / steps
        c, s = math.cos(t), math.sin(t)
        x = math.copysign(abs(c) ** (2.0 / n), c)
        y = math.copysign(abs(s) ** (2.0 / n), s)
        pts.append((cx + x * half, cy + y * half))
    return "M %.2f %.2f " % pts[0] + " ".join("L %.2f %.2f" % p for p in pts[1:]) + " Z"


def write_icon():
    """Square app icon for the README and the repo."""
    svg = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" width="256" height="256" role="img" aria-label="reclaim icon">
  {defs()}
  <rect width="256" height="256" rx="58" fill="url(#plate)"/>
  {mark(48, 30, 160)}
</svg>
'''
    (OUT / "icon.svg").write_text(svg)


def write_icon_macos():
    """Big Sur icon: squircle plate, top gloss, lifted art, hairline rim."""
    path = squircle_path(512, 512, 412)
    extra = f'''
    <linearGradient id="gloss" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#FFFFFF" stop-opacity="0.16"/>
      <stop offset="0.42" stop-color="#FFFFFF" stop-opacity="0.03"/>
      <stop offset="0.43" stop-color="#FFFFFF" stop-opacity="0"/>
    </linearGradient>
    <filter id="shadow" x="-20%" y="-20%" width="140%" height="140%">
      <feDropShadow dx="0" dy="18" stdDeviation="22" flood-color="#000000" flood-opacity="0.42"/>
    </filter>
    <filter id="lift" x="-40%" y="-40%" width="180%" height="180%">
      <feDropShadow dx="0" dy="8" stdDeviation="14" flood-color="#00131F" flood-opacity="0.55"/>
    </filter>
    <clipPath id="clip"><path d="{path}"/></clipPath>'''
    svg = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024" role="img" aria-label="reclaim macOS icon">
  {defs(extra)}
  <g filter="url(#shadow)"><path d="{path}" fill="url(#plate)"/></g>
  <g clip-path="url(#clip)">
    <rect width="1024" height="1024" fill="url(#gloss)"/>
    {mark(236, 176, 552, filter_id="lift")}
  </g>
  <path d="{path}" fill="none" stroke="#FFFFFF" stroke-opacity="0.10" stroke-width="2"/>
</svg>
'''
    (OUT / "icon-macos.svg").write_text(svg)


def write_banner():
    mono = "ui-monospace,'SF Mono','JetBrains Mono',Menlo,monospace"
    sans = "ui-sans-serif,system-ui,'Segoe UI',Roboto,sans-serif"
    svg = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 400" width="1280" height="400" role="img" aria-label="reclaim banner">
  {defs()}
  <rect width="1280" height="400" fill="url(#plate)"/>
  {mark(96, 118, 196)}
  <text x="392" y="176" font-family="{mono}" font-size="82" font-weight="700" fill="{PALETTE['text']}" letter-spacing="-1">reclaim</text>
  <text x="396" y="228" font-family="{sans}" font-size="27" fill="{PALETTE['muted']}">Process-aware storage reclamation for Ubuntu and Debian.</text>
  <text x="396" y="272" font-family="{sans}" font-size="27" fill="{PALETTE['muted']}">Tiered. Dry-run by default. Never deletes what it cannot regenerate.</text>
  <rect x="396" y="300" width="185" height="42" rx="10" fill="{PALETTE['accent_a']}" opacity="0.10"/>
  <text x="416" y="328" font-family="{mono}" font-size="21" fill="{PALETTE['accent_a']}">$ reclaim clean</text>
</svg>
'''
    (OUT / "banner.svg").write_text(svg)


# Real output, captured from a dry run on a developer machine.
DEMO = [
    ("prompt", "$ reclaim clean --discover --gradle"),
    ("blank", ""),
    ("amber", "DRY-RUN — nothing was deleted. Re-run with --apply to clean."),
    ("blank", ""),
    ("head", "== Reclaimable =="),
    ("unit", ("go build cache", "824.8MiB")),
    ("unit", ("Claude/Cache", "553.9MiB")),
    ("unit", ("yarn berry cache", "497.0MiB")),
    ("unit", ("figma-linux/Code Cache", "125.9MiB")),
    ("unit", ("Antigravity/CachedData", "122.1MiB")),
    ("unit", ("deno cache", "101.4MiB")),
    ("unit", (".cache/bruno-updater", "99.5MiB")),
    ("unit", (".cache/node", "95.9MiB")),
    ("blank", ""),
    ("head", "== Locked by running apps =="),
    ("lock", ("JetBrains caches", "6.2GiB", "jetbrains-ide (pid 1155531)")),
    ("lock", ("chrome cache", "1.9GiB", "chrome (pid 2733868)")),
    ("blank", ""),
    ("head", "== Withheld (may destroy information) =="),
    ("held", ("Claude session transcripts", "1.4GiB", "--claude-history --allow-lossy")),
    ("blank", ""),
    ("total", "would free 7.3GiB"),
]


def write_demo():
    mono = "ui-monospace,'SF Mono','JetBrains Mono',Menlo,monospace"
    W, LH, TOP, PAD = 980, 25, 78, 34
    H = TOP + len(DEMO) * LH + 34

    rows, y = [], TOP
    for kind, val in DEMO:
        if kind == "blank":
            y += LH
            continue
        if kind == "prompt":
            rows.append(f'<text x="{PAD}" y="{y}" fill="{PALETTE["accent_a"]}">{val}</text>')
        elif kind == "amber":
            rows.append(f'<text x="{PAD}" y="{y}" fill="{PALETTE["amber"]}">{val}</text>')
        elif kind == "head":
            rows.append(f'<text x="{PAD}" y="{y}" fill="{PALETTE["accent_b"]}" font-weight="700">{val}</text>')
        elif kind == "unit":
            label, size = val
            rows.append(f'<text x="{PAD + 18}" y="{y}" fill="{PALETTE["muted"]}">•</text>'
                        f'<text x="{PAD + 42}" y="{y}" fill="{PALETTE["text"]}">{label}</text>'
                        f'<text x="{PAD + 470}" y="{y}" fill="{PALETTE["accent_b"]}" text-anchor="end">{size}</text>')
        elif kind == "lock":
            label, size, who = val
            rows.append(f'<text x="{PAD + 18}" y="{y}" fill="{PALETTE["muted"]}">•</text>'
                        f'<text x="{PAD + 42}" y="{y}" fill="{PALETTE["text"]}">{label}</text>'
                        f'<text x="{PAD + 470}" y="{y}" fill="{PALETTE["accent_b"]}" text-anchor="end">{size}</text>'
                        f'<text x="{PAD + 500}" y="{y}" fill="{PALETTE["muted"]}">held by {who}</text>')
        elif kind == "held":
            label, size, flag = val
            rows.append(f'<text x="{PAD + 18}" y="{y}" fill="{PALETTE["muted"]}">•</text>'
                        f'<text x="{PAD + 42}" y="{y}" fill="{PALETTE["text"]}">{label}</text>'
                        f'<text x="{PAD + 470}" y="{y}" fill="{PALETTE["accent_b"]}" text-anchor="end">{size}</text>'
                        f'<text x="{PAD + 500}" y="{y}" fill="{PALETTE["muted"]}">include with: {flag}</text>')
        elif kind == "total":
            rows.append(f'<text x="{PAD}" y="{y}" fill="{PALETTE["text"]}" font-weight="700">{val}</text>')
        y += LH

    body = "\n    ".join(rows)
    svg = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W} {H}" width="{W}" height="{H}" role="img" aria-label="reclaim terminal output">
  {defs()}
  <rect width="{W}" height="{H}" rx="12" fill="{PALETTE['ink_bot']}"/>
  <path d="M0 12a12 12 0 0 1 12-12h{W - 24}a12 12 0 0 1 12 12v30H0z" fill="{PALETTE['ink_mid']}"/>
  <circle cx="26" cy="21" r="6.5" fill="#FF5F57"/>
  <circle cx="48" cy="21" r="6.5" fill="#FEBC2E"/>
  <circle cx="70" cy="21" r="6.5" fill="#28C840"/>
  <text x="{W / 2}" y="26" font-family="{mono}" font-size="13" fill="{PALETTE['muted']}" text-anchor="middle">reclaim — dry run</text>
  <g font-family="{mono}" font-size="15">
    {body}
  </g>
</svg>
'''
    (OUT / "demo.svg").write_text(svg)


if __name__ == "__main__":
    write_icon()
    write_icon_macos()
    write_banner()
    write_demo()
    print("wrote icon.svg icon-macos.svg banner.svg demo.svg")
