import bpy
import csv
import argparse
import sys
from collections import defaultdict


def parse_args():
    """
    Blender 自己也有命令行参数。
    我们自己的参数放在 -- 后面，例如：

    blender --background --python extract_max_handle.py -- \
        input.fbx output.csv
    """
    argv = sys.argv

    if "--" in argv:
        argv = argv[argv.index("--") + 1:]
    else:
        argv = []

    parser = argparse.ArgumentParser(
        description="Extract MaxHandle and object name from FBX"
    )

    parser.add_argument(
        "fbx_file",
        help="Input FBX file"
    )

    parser.add_argument(
        "output_csv",
        help="Output CSV file"
    )

    return parser.parse_args(argv)


def main():
    args = parse_args()

    fbx_file = args.fbx_file
    output_csv = args.output_csv

    print(f"FBX: {fbx_file}")
    print(f"Output: {output_csv}")

    # -------------------------------------------------------
    # 清空 Blender 场景
    # -------------------------------------------------------

    bpy.ops.object.select_all(action="SELECT")
    bpy.ops.object.delete(use_global=False)

    # -------------------------------------------------------
    # 导入 FBX
    # -------------------------------------------------------

    print("Importing FBX...")

    bpy.ops.import_scene.fbx(
        filepath=fbx_file
    )

    # -------------------------------------------------------
    # 提取 MaxHandle
    # -------------------------------------------------------

    rows = []

    for obj in bpy.data.objects:

        if "MaxHandle" not in obj:
            continue

        max_handle = obj["MaxHandle"]

        rows.append({
            "max_handle": str(max_handle),
            "name": obj.name,
            "type": obj.type,
        })

    # MaxHandle 通常是整数
    def sort_key(row):
        try:
            return int(row["max_handle"])
        except ValueError:
            return row["max_handle"]

    rows.sort(key=sort_key)

    # -------------------------------------------------------
    # 输出 CSV
    # -------------------------------------------------------

    with open(
        output_csv,
        "w",
        newline="",
        encoding="utf-8-sig"
    ) as f:

        writer = csv.DictWriter(
            f,
            fieldnames=[
                "max_handle",
                "name",
                "type"
            ]
        )

        writer.writeheader()
        writer.writerows(rows)

    print()
    print(f"Found {len(rows)} nodes with MaxHandle")
    print(f"CSV: {output_csv}")

    # -------------------------------------------------------
    # 检查 MaxHandle <-> Name
    # -------------------------------------------------------

    handle_to_names = defaultdict(set)
    name_to_handles = defaultdict(set)

    for row in rows:

        handle_to_names[
            row["max_handle"]
        ].add(row["name"])

        name_to_handles[
            row["name"]
        ].add(row["max_handle"])

    duplicate_handles = {
        handle: names
        for handle, names in handle_to_names.items()
        if len(names) > 1
    }

    duplicate_names = {
        name: handles
        for name, handles in name_to_handles.items()
        if len(handles) > 1
    }

    print()
    print("=" * 80)
    print("Check Result")
    print("=" * 80)

    print(f"Node count:              {len(rows)}")
    print(f"Unique MaxHandle count:  {len(handle_to_names)}")
    print(f"Unique Name count:       {len(name_to_handles)}")

    print()

    if not duplicate_handles:
        print("[OK] No MaxHandle maps to multiple names")
    else:
        print("[ERROR] Duplicate MaxHandle:")

        for handle, names in duplicate_handles.items():
            print(f"  MaxHandle={handle}")

            for name in names:
                print(f"      {name}")

    print()

    if not duplicate_names:
        print("[OK] No Name maps to multiple MaxHandles")
    else:
        print("[ERROR] Duplicate Name:")

        for name, handles in duplicate_names.items():
            print(f"  Name={name}")

            for handle in handles:
                print(f"      MaxHandle={handle}")

    print()

    if (
        len(rows) == len(handle_to_names)
        and
        len(rows) == len(name_to_handles)
    ):
        print("[OK] MaxHandle and Name have a one-to-one mapping")
    else:
        print("[WARNING] MaxHandle and Name do NOT have a strict one-to-one mapping")


if __name__ == "__main__":
    main()