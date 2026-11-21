"""Generate macOS and Windows app icons from the brand mascot (same look as the
Android adaptive icon: mascot on a warm #FFF1E4 background).

macOS (Big Sur+) icons are a rounded square inset in the canvas with a soft
shadow; Windows wants a multi-size .ico. Run from the repo root:

    python3 scripts/make_desktop_icons.py
"""
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter

ROOT = Path(__file__).resolve().parent.parent / 'app'
MASCOT = ROOT / 'assets/brand/mascot.png'
BACKGROUND = (0xFF, 0xF1, 0xE4, 255)
MAC = ROOT / 'macos/Runner/Assets.xcassets/AppIcon.appiconset'
WIN = ROOT / 'windows/runner/resources/app_icon.ico'


def square_art(size: int = 1024, mascot_ratio: float = 0.78) -> Image.Image:
    """Full-bleed brand artwork: background colour with the mascot centred."""
    art = Image.new('RGBA', (size, size), BACKGROUND)
    m = Image.open(MASCOT).convert('RGBA')
    m = m.crop(m.getbbox())
    scale = size * mascot_ratio / max(m.size)
    m = m.resize((round(m.width * scale), round(m.height * scale)), Image.LANCZOS)
    art.alpha_composite(m, ((size - m.width) // 2, (size - m.height) // 2 + round(size * 0.02)))
    return art


def mac_icon(src: Image.Image) -> Image.Image:
    canvas, body, radius = 1024, 824, 185  # Apple's macOS icon grid
    art = src.convert('RGBA').resize((body, body), Image.LANCZOS)
    mask = Image.new('L', (body, body), 0)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, body - 1, body - 1), radius=radius, fill=255)
    art.putalpha(mask)
    out = Image.new('RGBA', (canvas, canvas), (0, 0, 0, 0))
    shadow = Image.new('RGBA', (canvas, canvas), (0, 0, 0, 0))
    off = (canvas - body) // 2
    ImageDraw.Draw(shadow).rounded_rectangle((off, off + 12, off + body, off + body + 12), radius=radius, fill=(0, 0, 0, 70))
    out = Image.alpha_composite(out, shadow.filter(ImageFilter.GaussianBlur(14)))
    out.alpha_composite(art, (off, off))
    return out


def main() -> None:
    src = square_art()
    icon = mac_icon(src)
    for size in (16, 32, 64, 128, 256, 512, 1024):
        icon.resize((size, size), Image.LANCZOS).save(MAC / f'app_icon_{size}.png')
    rounded = mac_icon(square_art())
    rounded.crop((100, 100, 924, 924)).save(WIN, sizes=[(s, s) for s in (16, 24, 32, 48, 64, 128, 256)])
    print('icons written')


if __name__ == '__main__':
    main()
