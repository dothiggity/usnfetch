#!/usr/bin/env python3
"""Build usnfetch's icon files from the two source images in assets/icons/.

  source-idle.png   cyan / green: nothing downloading
  source-busy.png   red / pink:   downloading

Writes
  cmd/usnfetch/icons/{idle,busy,paused}.ico   tray icons (16-64 px as 32-bit bitmaps, 256 px as PNG)
  web/icon-{idle,busy,paused}.png             browser tab icon and sidebar logo (96 px)

"paused" is a dimmed grey copy of the idle icon. The black outside the rounded frame becomes
transparent. Run from anywhere:  python assets/make_icons.py   (needs Pillow)
"""
import io
import os
import struct

from PIL import Image, ImageDraw

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
TRAY_SIZES = [16, 20, 24, 32, 40, 48, 64, 256]
WEB_SIZE = 96


def load(name):
    """The source on a transparent background, cropped square to its frame."""
    im = Image.open(os.path.join(HERE, "icons", name)).convert("RGB")
    lum = im.convert("L")
    im.paste((0, 0, 0), mask=lum.point(lambda v: 255 if v < 28 else 0))   # near-black noise -> black
    dark = lum.point(lambda v: 255 if v < 48 else 0)
    w, h = im.size
    for corner in ((0, 0), (w - 1, 0), (0, h - 1), (w - 1, h - 1)):       # the black around the frame
        if dark.getpixel(corner) == 255:
            ImageDraw.floodfill(dark, corner, 128)
    alpha = dark.point(lambda v: 0 if v == 128 else 255)
    out = im.convert("RGBA")
    out.putalpha(alpha)
    out = out.crop(alpha.getbbox())
    side = max(out.size)
    square = Image.new("RGBA", (side, side), (0, 0, 0, 0))
    square.paste(out, ((side - out.width) // 2, (side - out.height) // 2))
    return square


def scaled(im, n):
    # area-average in premultiplied alpha, so the transparent edge does not darken the frame
    return im.convert("RGBa").resize((n, n), Image.BOX).convert("RGBA")


def greyed(im):
    lum = im.convert("L").point(lambda v: int(v * 0.72))
    out = Image.merge("RGBA", (lum, lum, lum, im.getchannel("A")))
    return out


def ico(images):
    """A .ico: 32-bit BGRA bitmaps (straight alpha, empty AND mask), PNG for 256 px."""
    blobs = []
    for im in images:
        n = im.width
        if n >= 256:
            buf = io.BytesIO()
            im.save(buf, "PNG")
            blobs.append((n, buf.getvalue()))
            continue
        header = struct.pack("<IiiHHIIiiII", 40, n, n * 2, 1, 32, 0, 0, 0, 0, 0, 0)
        pixels = im.transpose(Image.FLIP_TOP_BOTTOM).tobytes("raw", "BGRA")
        blobs.append((n, header + pixels + bytes(((n + 31) // 32) * 4 * n)))
    out = struct.pack("<HHH", 0, 1, len(blobs))
    offset = 6 + 16 * len(blobs)
    for n, blob in blobs:
        out += struct.pack("<BBBBHHII", n % 256, n % 256, 0, 0, 1, 32, len(blob), offset)
        offset += len(blob)
    return out + b"".join(b for _, b in blobs)


def main():
    icons = {"idle": load("source-idle.png"), "busy": load("source-busy.png")}
    icons["paused"] = greyed(icons["idle"])
    tray_dir = os.path.join(ROOT, "cmd", "usnfetch", "icons")
    os.makedirs(tray_dir, exist_ok=True)
    for state, im in icons.items():
        with open(os.path.join(tray_dir, state + ".ico"), "wb") as f:
            f.write(ico([scaled(im, n) for n in TRAY_SIZES]))
        scaled(im, WEB_SIZE).save(os.path.join(ROOT, "web", f"icon-{state}.png"), optimize=True)
        print(state, "ok")


if __name__ == "__main__":
    main()
