#!/usr/bin/env python3
"""Draw the SimpleToggle app icon (resources/icon.png).

The icon is the in-app logo: the dot-matrix prompt chevron (same dot pattern as
app/dots.go chevronDots) next to the glowing switch, on a graphite squircle that
follows Apple's 1024 / 824 macOS icon template.

    uv run --with pillow Scripts/make-icon.py [out.png]
"""

import sys

from PIL import Image, ImageChops, ImageDraw, ImageFilter

S = 4  # supersampling
N = 1024 * S
BOX = 824 * S  # squircle size inside the 1024 canvas
OFF = (N - BOX) // 2
RADIUS = int(BOX * 0.2237)

CHEVRON = ["#...", ".#..", "..#.", ".#..", "#..."]


def s(v):
    return int(round(v * S))


def squircle_mask():
    m = Image.new("L", (N, N), 0)
    ImageDraw.Draw(m).rounded_rectangle((OFF, OFF, OFF + BOX, OFF + BOX), RADIUS, fill=255)
    return m


def glow(size, draw_fn, blur, alpha):
    """A blurred white shape, used as additive light."""
    layer = Image.new("L", size, 0)
    draw_fn(ImageDraw.Draw(layer))
    layer = layer.filter(ImageFilter.GaussianBlur(blur))
    return layer.point(lambda v: int(v * alpha))


def main(out):
    mask = squircle_mask()

    # Surface: graphite with a soft light from the top and a faint dot grid.
    bg = Image.new("L", (N, N), 0)
    d = ImageDraw.Draw(bg)
    for y in range(OFF, OFF + BOX, S):
        t = (y - OFF) / BOX
        d.line((0, y, N, y), fill=int(25 - 13 * t))  # #191919 -> #0c0c0c
    grid = Image.new("L", (N, N), 0)
    g = ImageDraw.Draw(grid)
    pitch, r = s(32), s(2.2)
    for y in range(OFF + pitch // 2, OFF + BOX, pitch):
        for x in range(OFF + pitch // 2, OFF + BOX, pitch):
            g.ellipse((x - r, y - r, x + r, y + r), fill=14)
    light = Image.new("L", (N, N), 0)
    ImageDraw.Draw(light).ellipse((s(170), s(-260), s(854), s(380)), fill=26)
    light = light.filter(ImageFilter.GaussianBlur(s(120)))
    base = ImageChops.add(ImageChops.add(bg, grid), light)

    # Layout: chevron (3 columns) + gap + switch, centered.
    dot, step = 42, 50  # chevron dot diameter and pitch
    ch_w, ch_h = 2 * step + dot, 4 * step + dot
    sw_w, sw_h, stroke = 368, 192, 10
    gap = 64
    x0 = 512 - (ch_w + gap + sw_w) / 2
    cy = 512

    # Chevron dots
    def chevron(dr):
        for row, line in enumerate(CHEVRON):
            for col, ch in enumerate(line):
                if ch == "#":
                    x = x0 + col * step
                    y = cy - ch_h / 2 + row * step
                    dr.ellipse((s(x), s(y), s(x + dot), s(y + dot)), fill=255)

    chev_glow = glow((N, N), chevron, s(14), 0.24)
    chev = Image.new("L", (N, N), 0)
    chevron(ImageDraw.Draw(chev))
    chev = chev.point(lambda v: int(v * 0.93))

    # Switch: outlined pill, knob on and glowing
    sx = x0 + ch_w + gap
    pill = (s(sx), s(cy - sw_h / 2), s(sx + sw_w), s(cy + sw_h / 2))
    inset = 36
    kd = sw_h - 2 * inset
    kx = sx + sw_w - inset - kd
    knob = (s(kx), s(cy - kd / 2), s(kx + kd), s(cy + kd / 2))

    pill_fill = Image.new("L", (N, N), 0)
    ImageDraw.Draw(pill_fill).rounded_rectangle(pill, s(sw_h / 2), fill=22)
    pill_line = Image.new("L", (N, N), 0)
    ImageDraw.Draw(pill_line).rounded_rectangle(pill, s(sw_h / 2), outline=128, width=s(stroke))
    pill_glow = glow((N, N), lambda dr: dr.rounded_rectangle(pill, s(sw_h / 2), outline=255, width=s(stroke)), s(22), 0.16)
    knob_bloom = glow((N, N), lambda dr: dr.ellipse(knob, fill=255), s(48), 0.42)
    knob_halo = glow((N, N), lambda dr: dr.ellipse(knob, fill=255), s(10), 0.55)
    knob_l = Image.new("L", (N, N), 0)
    ImageDraw.Draw(knob_l).ellipse(knob, fill=255)

    art = base
    for layer in (pill_fill, pill_glow, pill_line, chev_glow, chev, knob_bloom, knob_halo):
        art = ImageChops.add(art, layer)
    art = ImageChops.lighter(art, knob_l)

    # Hairline rim, like the cards in the app
    rim = Image.new("L", (N, N), 0)
    ImageDraw.Draw(rim).rounded_rectangle((OFF, OFF, OFF + BOX, OFF + BOX), RADIUS, outline=34, width=s(2))
    art = ImageChops.add(art, rim)

    icon = Image.merge("RGBA", (art, art, art, mask))

    # Drop shadow from Apple's template
    shadow = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    sm = mask.filter(ImageFilter.GaussianBlur(s(14))).point(lambda v: int(v * 0.42))
    shadow.putalpha(ImageChops.offset(sm, 0, s(10)))
    out_img = Image.alpha_composite(shadow, icon)
    out_img.resize((1024, 1024), Image.LANCZOS).save(out)


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else "resources/icon.png")
