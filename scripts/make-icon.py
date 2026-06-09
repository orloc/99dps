#!/usr/bin/env python3
"""Generate the 99dps app icon: a DPS-meter bar chart in the terminal palette
(cyan/green/yellow/red bars, ranked tallest-first like a damage leaderboard) on a
dark rounded tile. Outputs a multi-resolution icon.ico for the Windows build."""

from PIL import Image, ImageDraw

S = 256
img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
d = ImageDraw.Draw(img)

RADIUS = 46
BG = (13, 17, 23, 255)       # dark slate (GitHub-dark)
BORDER = (44, 52, 62, 255)
BASE = (110, 122, 134, 255)  # baseline rule

# dark rounded tile + subtle border
d.rounded_rectangle([2, 2, S - 3, S - 3], radius=RADIUS, fill=BG)
d.rounded_rectangle([2, 2, S - 3, S - 3], radius=RADIUS, outline=BORDER, width=4)

# bars: the barColors palette, descending heights (sorted DPS rows)
colors = [(56, 209, 209, 255),   # cyan  (top dealer)
          (76, 175, 80, 255),    # green
          (255, 209, 64, 255),   # yellow
          (231, 76, 70, 255)]    # red
heights = [0.88, 0.66, 0.48, 0.32]

margin_x, gap = 40, 14
baseline, top = 210, 54
n = len(colors)
bar_w = (S - 2 * margin_x - (n - 1) * gap) / n
for i, (c, h) in enumerate(zip(colors, heights)):
    x0 = margin_x + i * (bar_w + gap)
    y0 = baseline - (baseline - top) * h
    d.rounded_rectangle([x0, y0, x0 + bar_w, baseline], radius=9, fill=c)

# baseline rule under the bars
d.line([margin_x - 8, baseline + 3, S - margin_x + 8, baseline + 3], fill=BASE, width=5)

img.save("icon.ico", sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
img.save("/tmp/icon_preview.png")
print("wrote icon.ico + /tmp/icon_preview.png")
