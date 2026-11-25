#!/usr/bin/env python3
"""Render bmux VT frame streams at 2x Retina into crisp PNG screenshots or MP4/WebM videos."""
import argparse
import os
import struct
import subprocess
from PIL import Image, ImageDraw, ImageFont

THEMES = {
    "dark": {
        "DEFAULT_FG": (0xAB, 0xB2, 0xBF),
        "DEFAULT_BG": (0x28, 0x2C, 0x34),
        "HEADER_BG": (0x21, 0x25, 0x2B),
        "HEADER_BORDER": (0x3B, 0x3F, 0x4C),
        "HEADER_TEXT": (0x84, 0x8B, 0x98),
        "ANSI_16": [
            (0x3E, 0x44, 0x52),  # 0 black
            (0xE0, 0x6C, 0x75),  # 1 red
            (0x98, 0xC3, 0x79),  # 2 green
            (0xE5, 0xC0, 0x7B),  # 3 yellow
            (0x61, 0xAF, 0xEF),  # 4 blue
            (0xC6, 0x78, 0xDD),  # 5 magenta
            (0x56, 0xB6, 0xC2),  # 6 cyan
            (0xAB, 0xB2, 0xBF),  # 7 white
            (0x5C, 0x63, 0x70),  # 8 bright black
            (0xE8, 0x66, 0x71),  # 9 bright red
            (0x98, 0xC3, 0x79),  # 10 bright green
            (0xE5, 0xC0, 0x7B),  # 11 bright yellow
            (0x61, 0xAF, 0xEF),  # 12 bright blue
            (0xC6, 0x78, 0xDD),  # 13 bright magenta
            (0x56, 0xB6, 0xC2),  # 14 bright cyan
            (0xDC, 0xDF, 0xE4),  # 15 bright white
        ],
    },
    "light": {
        "DEFAULT_FG": (0x4C, 0x4F, 0x69),
        "DEFAULT_BG": (0xEF, 0xF1, 0xF5),
        "HEADER_BG": (0xE6, 0xE9, 0xEF),
        "HEADER_BORDER": (0xBC, 0xC0, 0xCC),
        "HEADER_TEXT": (0x7C, 0x7F, 0x93),
        "ANSI_16": [
            (0xCC, 0xD0, 0xDA),  # 0 black
            (0xD2, 0x0F, 0x39),  # 1 red
            (0x40, 0xA0, 0x2B),  # 2 green
            (0xDF, 0x8E, 0x1D),  # 3 yellow
            (0x1E, 0x66, 0xF5),  # 4 blue
            (0xEA, 0x76, 0xCB),  # 5 magenta/pink
            (0x17, 0x92, 0x99),  # 6 cyan/teal
            (0x5C, 0x5F, 0x77),  # 7 white
            (0xAC, 0xB0, 0xBE),  # 8 bright black
            (0xD2, 0x0F, 0x39),  # 9 bright red
            (0x40, 0xA0, 0x2B),  # 10 bright green
            (0xDF, 0x8E, 0x1D),  # 11 bright yellow
            (0x1E, 0x66, 0xF5),  # 12 bright blue
            (0xEA, 0x76, 0xCB),  # 13 bright magenta
            (0x17, 0x92, 0x99),  # 14 bright cyan
            (0x4C, 0x4F, 0x69),  # 15 bright white
        ],
    },
}

FONT_CANDIDATES_REG = [
    "/usr/share/fonts/adwaita-mono-fonts/AdwaitaMono-Regular.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
    "/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono.ttf",
    "/System/Library/Fonts/Menlo.ttc",
]

FONT_CANDIDATES_BOLD = [
    "/usr/share/fonts/adwaita-mono-fonts/AdwaitaMono-Bold.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
    "/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono-Bold.ttf",
    "/System/Library/Fonts/Menlo.ttc",
]

FONT_CANDIDATES_NERD = [
    "/usr/share/fonts/nerd-fonts/NerdFontsSymbolsOnly/SymbolsNerdFontMono-Regular.ttf",
    "/usr/local/share/fonts/SymbolsNerdFontMono-Regular.ttf",
]


def load_first_font(candidates, size):
    for path in candidates:
        if os.path.exists(path):
            return ImageFont.truetype(path, size)
    return None


def build_palette_256(ansi_16):
    pal = list(ansi_16)
    steps = [0, 95, 135, 175, 215, 255]
    for r in steps:
        for g in steps:
            for b in steps:
                pal.append((r, g, b))
    for i in range(24):
        v = 8 + i * 10
        pal.append((v, v, v))
    return pal


def unpack_color(c, default, palette_256):
    kind = (c >> 24) & 0xFF
    if kind == 0:
        return default
    if kind in (1, 2):
        return palette_256[c & 0xFF]
    if kind == 3:
        return ((c >> 16) & 0xFF, (c >> 8) & 0xFF, c & 0xFF)
    return default


def blend(c1, c2, alpha):
    return (
        int(c1[0] * alpha + c2[0] * (1 - alpha)),
        int(c1[1] * alpha + c2[1] * (1 - alpha)),
        int(c1[2] * alpha + c2[2] * (1 - alpha)),
    )


BOX_CHARS = set("─│┌┐└┘├┤┬┴┼╭╮╯╰═║━┃")
POWERLINE_CHARS = {0xE0B0, 0xE0B1, 0xE0B2, 0xE0B3}


def draw_box_char(draw, ch, x, y, cw, ch_h, fg, attrs):
    mx = x + cw // 2
    my = y + ch_h // 2
    x2 = x + cw - 1
    y2 = y + ch_h - 1
    lw = 4 if (attrs & 1) or ch in ("━", "┃") else 2

    if ch in ("─", "━", "═"):
        draw.line([(x, my), (x2, my)], fill=fg, width=lw)
    elif ch in ("│", "┃", "║"):
        draw.line([(mx, y), (mx, y2)], fill=fg, width=lw)
    elif ch in ("┌", "╭"):
        draw.line([(mx, my), (x2, my)], fill=fg, width=lw)
        draw.line([(mx, my), (mx, y2)], fill=fg, width=lw)
    elif ch in ("┐", "╮"):
        draw.line([(x, my), (mx, my)], fill=fg, width=lw)
        draw.line([(mx, my), (mx, y2)], fill=fg, width=lw)
    elif ch in ("└", "╰"):
        draw.line([(mx, y), (mx, my)], fill=fg, width=lw)
        draw.line([(mx, my), (x2, my)], fill=fg, width=lw)
    elif ch in ("┘", "╯"):
        draw.line([(mx, y), (mx, my)], fill=fg, width=lw)
        draw.line([(x, my), (mx, my)], fill=fg, width=lw)
    elif ch == "├":
        draw.line([(mx, y), (mx, y2)], fill=fg, width=lw)
        draw.line([(mx, my), (x2, my)], fill=fg, width=lw)
    elif ch == "┤":
        draw.line([(mx, y), (mx, y2)], fill=fg, width=lw)
        draw.line([(x, my), (mx, my)], fill=fg, width=lw)
    elif ch == "┬":
        draw.line([(x, my), (x2, my)], fill=fg, width=lw)
        draw.line([(mx, my), (mx, y2)], fill=fg, width=lw)
    elif ch == "┴":
        draw.line([(x, my), (x2, my)], fill=fg, width=lw)
        draw.line([(mx, y), (mx, my)], fill=fg, width=lw)
    elif ch == "┼":
        draw.line([(x, my), (x2, my)], fill=fg, width=lw)
        draw.line([(mx, y), (mx, y2)], fill=fg, width=lw)


def draw_powerline(draw, cp, x, y, cw, ch_h, fg):
    x2 = x + cw - 1
    y2 = y + ch_h - 1
    my = y + ch_h // 2
    if cp == 0xE0B0:
        draw.polygon([(x, y), (x2, my), (x, y2)], fill=fg)
    elif cp == 0xE0B2:
        draw.polygon([(x2, y), (x, my), (x2, y2)], fill=fg)
    elif cp == 0xE0B1:
        draw.line([(x, y), (x2, my), (x, y2)], fill=fg, width=2)
    elif cp == 0xE0B3:
        draw.line([(x2, y), (x, my), (x2, y2)], fill=fg, width=2)


def make_base_image(width, height, top_bar, palette, font_reg):
    base_img = Image.new("RGB", (width, height), palette["DEFAULT_BG"])
    bdraw = ImageDraw.Draw(base_img)
    bdraw.rectangle([0, 0, width, top_bar - 1], fill=palette["HEADER_BG"])
    bdraw.line([(0, top_bar - 2), (width, top_bar - 2)], fill=palette["HEADER_BORDER"], width=2)
    for i, col in enumerate([(0xE0, 0x6C, 0x75), (0xE5, 0xC0, 0x7B), (0x98, 0xC3, 0x79)]):
        cx = 36 + i * 36
        cy = top_bar // 2
        bdraw.ellipse([cx - 10, cy - 10, cx + 10, cy + 10], fill=col)
    bdraw.text((width // 2 - 36, 18), "bmux", font=font_reg, fill=palette["HEADER_TEXT"])
    return base_img


def render_frame(raw, base_img, cols, rows, cw, ch, pad_x, top_bar, palette, palette_256, cell_struct, font_reg, font_bold, font_nerd):
    img = base_img.copy()
    draw = ImageDraw.Draw(img)
    default_fg = palette["DEFAULT_FG"]
    default_bg = palette["DEFAULT_BG"]
    off = 4
    for y in range(rows):
        py = top_bar + y * ch
        for x in range(cols):
            cp, fg_u, bg_u, attrs, cw_cells = cell_struct.unpack_from(raw, off)
            off += cell_struct.size
            px = pad_x + x * cw

            fg = unpack_color(fg_u, default_fg, palette_256)
            bg = unpack_color(bg_u, default_bg, palette_256)
            if attrs & (1 << 4):  # reverse
                fg, bg = bg, fg
            if attrs & (1 << 1):  # faint
                fg = blend(fg, bg, 0.55)

            if cw_cells == 0:
                continue
            cell_w_px = cw * max(1, cw_cells)

            if bg != default_bg:
                draw.rectangle([px, py, px + cell_w_px - 1, py + ch - 1], fill=bg)

            if cp > 32:
                ch_str = chr(cp)
                if ch_str in BOX_CHARS:
                    draw_box_char(draw, ch_str, px, py, cw, ch, fg, attrs)
                elif cp in POWERLINE_CHARS:
                    draw_powerline(draw, cp, px, py, cw, ch, fg)
                else:
                    if cp >= 0xE000 and font_nerd is not None:
                        font = font_nerd
                    elif attrs & 1:
                        font = font_bold
                    else:
                        font = font_reg
                    draw.text((px, py + 2), ch_str, font=font, fill=fg)

            if attrs & (1 << 3):  # underline
                draw.line([(px, py + ch - 4), (px + cell_w_px - 1, py + ch - 4)], fill=fg, width=2)
    return img


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--frames", required=True)
    ap.add_argument("--mode", choices=["png", "video"], default="png")
    ap.add_argument("--theme", choices=["dark", "light"], default="dark")
    ap.add_argument("--out", help="Output PNG path when --mode png")
    ap.add_argument("--out-dir", help="Output directory when --mode video")
    ap.add_argument("--frame-index", type=int, default=-1, help="Frame index for --mode png (-1 = last frame)")
    args = ap.parse_args()

    if args.mode == "png" and not args.out:
        ap.error("--out is required when --mode png")
    if args.mode == "video" and not args.out_dir:
        ap.error("--out-dir is required when --mode video")

    palette = THEMES[args.theme]
    palette_256 = build_palette_256(palette["ANSI_16"])

    font_reg = load_first_font(FONT_CANDIDATES_REG, 30) or ImageFont.load_default()
    font_bold = load_first_font(FONT_CANDIDATES_BOLD, 30) or font_reg
    font_nerd = load_first_font(FONT_CANDIDATES_NERD, 30)

    cw, ch = 20, 40
    pad_x, top_bar, pad_bottom = 48, 72, 36

    with open(args.frames, "rb") as f:
        hdr = f.read(6)
        if len(hdr) < 6:
            raise RuntimeError(f"invalid frames header in {args.frames}")
        cols, rows, fps = struct.unpack("<HHH", hdr)
        width = cols * cw + pad_x * 2
        height = top_bar + rows * ch + pad_bottom
        width += width % 2
        height += height % 2

        cell_struct = struct.Struct("<IIIBB")
        frame_bytes = 4 + cols * rows * cell_struct.size
        base_img = make_base_image(width, height, top_bar, palette, font_reg)

        if args.mode == "png":
            frames = []
            while True:
                raw = f.read(frame_bytes)
                if len(raw) < frame_bytes:
                    break
                frames.append(raw)
            if not frames:
                raise RuntimeError(f"no frames in {args.frames}")
            raw = frames[args.frame_index]
            img = render_frame(
                raw, base_img, cols, rows, cw, ch, pad_x, top_bar,
                palette, palette_256, cell_struct, font_reg, font_bold, font_nerd,
            )
            os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
            img.save(args.out, optimize=True)
            print(f"Rendered {width}x{height} PNG -> {args.out}")
            return

        os.makedirs(args.out_dir, exist_ok=True)
        mp4_path = os.path.join(args.out_dir, "demo.mp4")
        webm_path = os.path.join(args.out_dir, "demo.webm")
        poster_path = os.path.join(args.out_dir, "demo-poster.png")

        ff_mp4 = subprocess.Popen(
            [
                "ffmpeg", "-y", "-f", "rawvideo", "-vcodec", "rawvideo",
                "-pix_fmt", "rgb24", "-s", f"{width}x{height}", "-r", str(fps), "-i", "-",
                "-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "14", "-preset", "slow",
                "-tune", "stillimage", "-movflags", "+faststart", mp4_path,
            ],
            stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
        )
        ff_webm = subprocess.Popen(
            [
                "ffmpeg", "-y", "-f", "rawvideo", "-vcodec", "rawvideo",
                "-pix_fmt", "rgb24", "-s", f"{width}x{height}", "-r", str(fps), "-i", "-",
                "-c:v", "libvpx-vp9", "-crf", "18", "-b:v", "0", "-pix_fmt", "yuv420p",
                "-row-mt", "1", webm_path,
            ],
            stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
        )

        prev_raw = None
        prev_rgb = None
        idx = 0
        while True:
            raw = f.read(frame_bytes)
            if len(raw) < frame_bytes:
                break
            if raw == prev_raw and prev_rgb is not None:
                rgb = prev_rgb
            else:
                img = render_frame(
                    raw, base_img, cols, rows, cw, ch, pad_x, top_bar,
                    palette, palette_256, cell_struct, font_reg, font_bold, font_nerd,
                )
                if idx == 0:
                    img.save(poster_path, optimize=True)
                rgb = img.tobytes()
                prev_raw = raw
                prev_rgb = rgb

            ff_mp4.stdin.write(rgb)
            ff_webm.stdin.write(rgb)
            idx += 1

        ff_mp4.stdin.close()
        ff_webm.stdin.close()
        rc1 = ff_mp4.wait()
        rc2 = ff_webm.wait()
        if rc1 != 0:
            raise RuntimeError(f"ffmpeg mp4 failed ({rc1}): {ff_mp4.stderr.read().decode()}")
        if rc2 != 0:
            raise RuntimeError(f"ffmpeg webm failed ({rc2}): {ff_webm.stderr.read().decode()}")

        print(f"Rendered {idx} frames ({width}x{height} @ {fps}fps) -> {mp4_path}, {webm_path}, {poster_path}")


if __name__ == "__main__":
    main()
