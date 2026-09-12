#!/usr/bin/env python3
"""Generate LOD1 boxes with four side captures and transparent top/bottom.

This is a dedicated entry point based on glb_lod1_six_view_boxes_v3. It
renders only the left, right, front and back views. The top and bottom box
faces remain in the GLB geometry but use fully transparent materials.

Run with Blender:

    blender --background --python glb_lod1_six_view_boxes_v4.py -- input \
        --output output --emission-strength 0.35 --overwrite
"""

from __future__ import annotations

import sys
from pathlib import Path


SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

import glb_lod1_six_view_boxes_v3 as lod1  # noqa: E402


def main() -> int:
    if "--" not in sys.argv:
        sys.argv.extend(("--", "--transparent-top-bottom"))
    elif "--transparent-top-bottom" not in sys.argv:
        sys.argv.append("--transparent-top-bottom")
    return lod1.main()


if __name__ == "__main__":
    raise SystemExit(main())
