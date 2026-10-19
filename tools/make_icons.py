"""LocalDrop 아이콘 PNG를 만듭니다. (Pillow 필요: pip install pillow)

python tools/make_icons.py  →  web/static/*.png, winres/icon*.png
"""
import os
from PIL import Image, ImageDraw

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
GREEN = (11, 122, 87, 255)
WHITE = (255, 255, 255, 255)


def draw_icon(size, rounded=True, stroke=9.0, inset=0.0):
    """100x100 좌표계로 그린 뒤 줄여서 부드러운 가장자리를 만듭니다."""
    ss = 8
    S = size * ss
    img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    u = S / 100.0
    if rounded:
        d.rounded_rectangle([0, 0, S - 1, S - 1], radius=24 * u, fill=GREEN)
    else:
        d.rectangle([0, 0, S, S], fill=GREEN)

    def P(x, y):
        # inset > 0 이면 화살표를 가운데로 조금 모읍니다 (작은 크기용)
        cx = 50 + (x - 50) * (1 - inset)
        cy = 50 + (y - 50) * (1 - inset)
        return (cx * u, cy * u)

    w = stroke * u
    r = w / 2

    def line(a, b):
        d.line([P(*a), P(*b)], fill=WHITE, width=int(round(w)))
        for pt in (a, b):
            x, y = P(*pt)
            d.ellipse([x - r, y - r, x + r, y + r], fill=WHITE)

    # 위: 오른쪽 화살표, 아래: 왼쪽 화살표 (폰 ↔ PC)
    line((26, 38), (70, 38))
    line((58, 26), (70, 38))
    line((58, 50), (70, 38))
    line((74, 62), (30, 62))
    line((42, 50), (30, 62))
    line((42, 74), (30, 62))
    return img.resize((size, size), Image.LANCZOS)


def save(img, *parts):
    path = os.path.join(ROOT, *parts)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    img.save(path, optimize=True)
    print("만듦:", os.path.relpath(path, ROOT))


if __name__ == "__main__":
    save(draw_icon(64), "web", "static", "favicon.png")
    save(draw_icon(192), "web", "static", "icon-192.png")
    save(draw_icon(512), "web", "static", "icon-512.png")
    save(draw_icon(180, rounded=False, inset=0.08), "web", "static", "apple-touch-icon.png")
    save(draw_icon(256), "winres", "icon256.png")
    save(draw_icon(48, stroke=10), "winres", "icon48.png")
    save(draw_icon(32, stroke=11, inset=-0.04), "winres", "icon32.png")
    save(draw_icon(24, stroke=12, inset=-0.06), "winres", "icon24.png")
    save(draw_icon(16, stroke=13, inset=-0.1), "winres", "icon16.png")
