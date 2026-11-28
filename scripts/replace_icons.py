"""Generate ChannelTerm desktop icons from the repository master or a new SVG."""

import argparse
from io import BytesIO
from pathlib import Path
from shutil import rmtree
from time import monotonic, sleep

import resvg_py
from PIL import Image, ImageDraw


BRAND = Path(__file__).resolve().parents[1] / "assets" / "brand"
MASTER = BRAND / "channelterm-logo.svg"
GENERATED = BRAND / "generated"
SIZES = (16, 24, 32, 48, 64, 128, 256, 512, 1024)


def check_alpha(image):
    size = image.width
    alpha = image.getchannel("A")
    if alpha.getpixel((0, 0)) != 0 or alpha.getpixel((size // 2, size // 2)) != 255:
        raise ValueError(f"{size}px icon needs a transparent margin and an opaque tile")
    # All partially or fully transparent pixels must connect to the exterior.
    mask = alpha.point(lambda value: 255 if value == 255 else 0)
    ImageDraw.floodfill(mask, (0, 0), 255)
    if mask.getextrema() != (255, 255):
        raise ValueError(f"{size}px icon has a transparent hole inside the tile")


def render(source, size):
    png = resvg_py.svg_to_bytes(
        svg_string=source, width=size, height=size, skip_system_fonts=True
    )
    image = Image.open(BytesIO(png)).convert("RGBA")
    check_alpha(image)
    return png, image


def generate(svg_bytes):
    source = svg_bytes.decode("utf-8-sig")
    files = {MASTER: svg_bytes}
    images = []
    for size in SIZES:
        png, image = render(source, size)
        files[GENERATED / "png" / f"{size}x{size}.png"] = png
        images.append(image)

    for extension in ("ico", "icns"):
        stream = BytesIO()
        options = {"append_images": images[:-1]}
        if extension == "ico":
            options["sizes"] = [(size, size) for size in SIZES if size <= 256]
        images[-1].save(stream, extension, **options)
        files[GENERATED / f"channelterm.{extension}"] = stream.getvalue()

    # Electron packages these under fixed window-resource names.
    files[GENERATED / "channelterm-window.ico"] = files[GENERATED / "channelterm.ico"]
    files[GENERATED / "channelterm-window.png"] = files[GENERATED / "png" / "512x512.png"]
    return files


def update_icons(source, *, check=False):
    if check:
        for path, data in generate(source.read_bytes()).items():
            if not path.is_file() or path.read_bytes() != data:
                raise ValueError(f"Regenerate missing or stale icon: {path}")
    else:
        publish(source)
    print("ChannelTerm desktop icons are up to date.")


def publish(source):
    # Reserve the output set so concurrent imports cannot mix two designs.
    GENERATED.mkdir(parents=True, exist_ok=True)
    lock = GENERATED / ".icon-update"
    deadline = monotonic() + 10
    while True:
        try:
            lock.mkdir()
            break
        except FileExistsError:
            if monotonic() >= deadline:
                raise ValueError(f"Another icon update is using {lock}") from None
            sleep(0.1)
    changes = []
    replaced = []
    try:
        # Windows keeps a staged file's ACL when it is moved. Stage in the
        # project-inheriting lock directory so a separate build user can read it.
        for path, data in generate(source.read_bytes()).items():
            previous = path.read_bytes() if path.is_file() else None
            if previous == data:
                continue
            path.parent.mkdir(parents=True, exist_ok=True)
            (lock / path.name).write_bytes(data)
            if previous is not None:
                (lock / (path.name + ".old")).write_bytes(previous)
            changes.append(path)
        for path in changes:
            (lock / path.name).replace(path)
            replaced.append(path)
    except OSError:
        for path in reversed(replaced):
            backup = lock / (path.name + ".old")
            if backup.exists():
                backup.replace(path)
            else:
                path.unlink()
        raise
    finally:
        rmtree(lock)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("svg", type=Path, nargs="?", help="replace the master with this SVG")
    mode.add_argument("--check", action="store_true", help="report stale or missing icons without writing")
    args = parser.parse_args()
    try:
        update_icons(args.svg or MASTER, check=args.check)
    except (OSError, ValueError) as error:
        parser.exit(1, f"{error}\n")
