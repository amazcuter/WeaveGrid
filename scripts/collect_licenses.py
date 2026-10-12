#!/usr/bin/env python3
"""Collect original notices for the binary's Go modules and frontend runtime deps."""

import argparse
import json
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parents[1]
LICENSE_NAME = re.compile(r"^(?:licen[cs]e|copying|notice|patents)(?:[.-].*)?$", re.I)


def command(*args):
    return subprocess.run(args, cwd=ROOT, check=True, text=True, capture_output=True).stdout


def json_stream(text):
    decoder = json.JSONDecoder()
    position = 0
    while position < len(text):
        if text[position].isspace():
            position += 1
            continue
        value, position = decoder.raw_decode(text, position)
        yield value


def collect(go_license_dir=None):
    parts = ["WeaveGrid third-party license texts\nGenerated from installed, locked dependencies.\n"]
    count = 0

    def include(label, directory):
        nonlocal count
        directory = Path(directory)
        files = sorted(p for p in directory.iterdir() if p.is_file() and LICENSE_NAME.match(p.name))
        if not files:
            raise ValueError(f"缺少原始许可文件：{label}")
        parts.append("\n" + "=" * 72 + "\n" + label + "\n")
        for path in files:
            parts.append("\n--- " + path.name + " ---\n" + path.read_text(encoding="utf-8"))
        count += 1

    lock = json.loads((ROOT / "web/package-lock.json").read_text(encoding="utf-8"))
    for name, item in sorted(lock["packages"].items()):
        if not name or item.get("dev"):
            continue
        directory = ROOT / "web" / name
        if not directory.is_dir():
            raise ValueError(f"依赖尚未安装：{name}；请先 npm ci")
        metadata = json.loads((directory / "package.json").read_text(encoding="utf-8"))
        if metadata["version"] != item["version"]:
            raise ValueError(f"依赖与锁文件不一致：{name}")
        include(f"npm: {metadata['name']}@{metadata['version']} ({item.get('license', 'see text')})", directory)

    modules = {}
    for package in json_stream(command("go", "list", "-deps", "-json", "./cmd/weavegrid")):
        module = package.get("Module")
        if module and not module.get("Main"):
            if module.get("Replace"):
                raise ValueError("发布前请核实 Go replace 模块的来源与许可")
            modules[module["Path"]] = module
    for name, module in sorted(modules.items()):
        include(f"Go: {name}@{module['Version']}", module["Dir"])
    go_root = Path(command("go", "env", "GOROOT").strip())
    # Official toolchains keep LICENSE in GOROOT; some Linux distributions
    # install the same notice separately under their package license directory.
    candidates = [Path(go_license_dir)] if go_license_dir else [
        go_root, go_root.parent.parent / "share/licenses" / go_root.name,
    ]
    license_dir = next((directory for directory in candidates if directory.is_dir()
                        and any(p.is_file() and LICENSE_NAME.match(p.name)
                                for p in directory.iterdir())), None)
    if license_dir is None:
        raise ValueError("缺少 Go 工具链原始许可；请用 --go-license-dir 指定安装包的许可目录")
    include("Go toolchain / standard library: " + command("go", "version").strip(), license_dir)
    return "\n".join(parts), count


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", default="bin/THIRD_PARTY_LICENSES.txt")
    parser.add_argument("--go-license-dir", help="Go 安装包原始许可所在目录（默认自动查找）")
    args = parser.parse_args()
    text, count = collect(args.go_license_dir)
    output = ROOT / args.output
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(text, encoding="utf-8")
    print(f"已收集 {count} 组原始许可：{output.name}")


if __name__ == "__main__":
    main()
