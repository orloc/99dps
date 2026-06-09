#!/usr/bin/env python3
"""Generate the 99dps app icon: a DPS-meter bar chart on a dark tile in an
EverQuest-flavored palette — Ruins-of-Kunark metallic gold framing (an ornate
double frame, like a classic EQ window) over near-black, with the bars a
gilded gold gradient. Outputs a multi-resolution icon.ico for the Windows build.
Run two ways: default writes icon.ico; pass a name to preview a variant."""

from PIL import Image, ImageDraw

S = 256
img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
d = ImageDraw.Draw(img)

RADIUS = 46
BG = (12, 11, 9, 255)        # warm near-black (Kunark stone/dark)
GOLD = (201, 162, 39, 255)   # metallic EQ gold (frame)
GOLD_DIM = (120, 98, 30, 255)
BASE = (150, 122, 52, 255)   # dim gold baseline

# ornate double gold frame, like a classic EQ window
d.rounded_rectangle([2, 2, S - 3, S - 3], radius=RADIUS, fill=BG)
d.rounded_rectangle([2, 2, S - 3, S - 3], radius=RADIUS, outline=GOLD, width=6)
d.rounded_rectangle([14, 14, S - 15, S - 15], radius=RADIUS - 12, outline=GOLD_DIM, width=2)

# bars: a gilded gold gradient, descending (a "leaderboard" of damage)
bars = [(240, 208, 96, 255),   # bright gold (top dealer)
        (212, 175, 55, 255),   # gold
        (184, 144, 44, 255),   # deep gold
        (140, 109, 31, 255)]   # bronze
heights = [0.88, 0.66, 0.48, 0.32]

margin_x, gap = 44, 14
baseline, top = 206, 58
n = len(bars)
bar_w = (S - 2 * margin_x - (n - 1) * gap) / n
for i, (c, h) in enumerate(zip(bars, heights)):
    x0 = margin_x + i * (bar_w + gap)
    y0 = baseline - (baseline - top) * h
    d.rounded_rectangle([x0, y0, x0 + bar_w, baseline], radius=8, fill=c)

# baseline rule under the bars
d.line([margin_x - 6, baseline + 3, S - margin_x + 6, baseline + 3], fill=BASE, width=5)

img.save("icon.ico", sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
img.save("/tmp/icon_preview.png")
print("wrote icon.ico + /tmp/icon_preview.png")
