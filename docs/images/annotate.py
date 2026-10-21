"""Draws the numbered callouts of docs/images/overview.png on 03-explanation.png.

Run from the repository root after regenerating the screenshots (see README.md
in this folder):  python3 docs/images/annotate.py
Needs Pillow. The positions are for a window of 1100x800 pixels.
"""
from PIL import Image, ImageDraw, ImageFont

# number -> (x, y) of the centre of the marker
CALLOUTS = {
    1: (940, 30),    # toolbar, row 1: LLM, Check, Auto-check, Statistics, Preferences, web server
    2: (665, 76),    # toolbar, row 2: files
    3: (420, 114),   # legend
    4: (700, 245),   # the text, with underlines
    5: (1060, 114),  # issues list
    6: (1062, 470),  # header with the count, Regenerate, Not an error
    7: (1062, 620),  # explanation
    8: (400, 788),   # status bar
}
COLOR = (200, 40, 40)

img = Image.open("docs/images/03-explanation.png").convert("RGB")
draw = ImageDraw.Draw(img)
try:
    font = ImageFont.truetype("DejaVuSans-Bold.ttf", 17)
except OSError:
    font = ImageFont.load_default()
for n, (x, y) in CALLOUTS.items():
    r = 13
    draw.ellipse((x - r, y - r, x + r, y + r), fill=COLOR, outline="white", width=2)
    draw.text((x, y), str(n), fill="white", font=font, anchor="mm")
img.save("docs/images/overview.png")
