#!/usr/bin/env python3
"""Public-source allowlist, limited credential checks, and history-free export.

No network access. Findings never print matched values. This is not a full
security audit and cannot recognize all private prose or arbitrary secrets.
"""

import argparse
import gzip
import hashlib
import io
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tarfile


ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / "scripts/public-files.txt"
MAX_BYTES = 2 * 1024 * 1024
PRIVATE_PARTS = {
    ".git", ".codex", ".idea", ".vscode", "data", "artifacts", "bin",
    "dist", "node_modules", "exports", "backups", "__pycache__",
}
PATTERNS = {
    "private-key": re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----"),
    "provider-key": re.compile(r"\bsk-[A-Za-z0-9_-]{24,}"),
    "github-token": re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})"),
    "aws-access-id": re.compile(r"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b"),
    "literal-bearer": re.compile(r"\bBearer\s+[A-Za-z0-9._~+/-]{24,}=*"),
    "literal-credential": re.compile(
        r'''\b(?:[A-Z_]*(?:API_KEY|ACCESS_TOKEN|SECRET_KEY)|apiKey|api_key|access_token|_authToken)'''
        r'''["']?\s*[:=]\s*["']([A-Za-z0-9+/_=.:-]{16,})["']'''
    ),
    "credential-in-url": re.compile(r"https?://[^\s/@:]+:[^\s/@]+@"),
    "personal-absolute-path": re.compile(
        r"/(?:home|Users)/[^/\s\"<>]+/|/run/" r"media/[^/\s\"<>]+/"
    ),
}


def private_reason(name):
    path = PurePosixPath(name)
    if path.is_absolute() or ".." in path.parts or "\\" in name:
        return "unsafe-path"
    if any(part in PRIVATE_PARTS for part in path.parts):
        return "private-or-generated-directory"
    if path.name == "TEST_REPORT.md":
        return "private-acceptance-report"
    if path.name.startswith(".env") and path.name != ".env.example":
        return "environment-file"
    if path.name in {"auth.json", "credentials.json"}:
        return "credential-file"
    if re.search(r"\.(?:db|sqlite|sqlite3)(?:-.*)?$|\.(?:key|pem|log|tsbuildinfo)$", name):
        return "private-or-generated-file"
    return None


def read_manifest():
    names = []
    for line in MANIFEST.read_text(encoding="utf-8").splitlines():
        name = line.strip()
        if not name or name.startswith("#"):
            continue
        if private_reason(name):
            raise ValueError(f"拒绝清单路径：{name} ({private_reason(name)})")
        if name in names or str(PurePosixPath(name)) != name:
            raise ValueError(f"重复或非规范路径：{name}")
        names.append(name)
    required = {"LICENSE", "NOTICE", "COMMERCIAL.md", "README.md", "go.mod", "web/package-lock.json"}
    if not required.issubset(names):
        raise ValueError("公开清单缺少许可、说明或构建锁文件")
    return sorted(names)


def findings(label, data):
    if len(data) > MAX_BYTES:
        return [f"{label}: oversized-file（请单独人工检查）"]
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError:
        return [f"{label}: non-text-file（需明确的分发规则）"]
    issues = []
    for rule, pattern in PATTERNS.items():
        for match in pattern.finditer(text):
            line = text.count("\n", 0, match.start()) + 1
            issues.append(f"{label}:{line}: {rule}")
    return issues


def public_files(names):
    files, issues = {}, []
    for name in names:
        path = ROOT
        for part in PurePosixPath(name).parts:
            path = path / part
            if path.is_symlink():
                raise ValueError(f"拒绝符号链接：{name}")
        if not path.is_file():
            raise ValueError(f"公开文件缺失：{name}")
        if path.stat().st_size > MAX_BYTES:
            raise ValueError(f"公开文件过大：{name}")
        data = path.read_bytes()
        issues.extend(findings(name, data))
        files[name] = data
    return files, issues


def git(*args):
    return subprocess.run(
        ["git", "-c", "core.quotepath=false", *args], cwd=ROOT,
        check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    ).stdout


def audit_git(names):
    issues = []
    tracked = git("ls-files", "-z").decode("utf-8").split("\0")
    for name in filter(None, tracked):
        reason = private_reason(name)
        if reason or name not in names:
            issues.append(f"tracked:{name}: {reason or 'not-in-public-manifest'}")
        path = ROOT / name
        if path.is_symlink():
            issues.append(f"tracked:{name}: symlink")
        elif path.is_file() and not any(p.is_symlink() for p in path.parents):
            if path.stat().st_size <= MAX_BYTES:
                issues.extend(findings(f"tracked:{name}", path.read_bytes()))
            else:
                issues.append(f"tracked:{name}: oversized-file")
    objects = git("rev-list", "--objects", "--all").decode("utf-8").splitlines()
    for item in objects:
        oid, sep, name = item.partition(" ")
        if not sep or git("cat-file", "-t", oid).strip() != b"blob":
            continue
        label = f"history:{oid[:12]}:{name}"
        reason = private_reason(name)
        if reason:
            issues.append(f"{label}: {reason}")
        if int(git("cat-file", "-s", oid)) > MAX_BYTES:
            issues.append(f"{label}: oversized-file")
        else:
            issues.extend(findings(label, git("cat-file", "blob", oid)))
    return issues


def export(files, output):
    destination = Path(output)
    if not destination.is_absolute():
        destination = ROOT / destination
    # 发布产物仅写入自己的 dist 目录，且不覆盖已有包。
    if (ROOT / "dist").is_symlink() or destination.parent.resolve() != (ROOT / "dist").resolve():
        raise ValueError("输出必须是项目 dist/ 目录中的新文件")
    if not destination.name.endswith(".tar.gz") or destination.exists() or destination.is_symlink():
        raise ValueError("输出须以 .tar.gz 结尾且不能已存在")
    checksums = "".join(f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, data in files.items())
    entries = {**files, "SOURCE_MANIFEST.sha256": checksums.encode("utf-8")}
    buffer = io.BytesIO()
    with gzip.GzipFile(fileobj=buffer, mode="wb", filename="", mtime=0) as zipped:
        with tarfile.open(fileobj=zipped, mode="w", format=tarfile.PAX_FORMAT) as archive:
            for name, data in sorted(entries.items()):
                info = tarfile.TarInfo("weavegrid/" + name)
                info.size = len(data)
                info.mode = 0o755 if name.endswith(".sh") else 0o644
                info.mtime = 0
                info.uid = info.gid = 0
                info.uname = info.gname = ""
                archive.addfile(info, io.BytesIO(data))
    destination.parent.mkdir(parents=True, exist_ok=True)
    with destination.open("xb") as out:
        out.write(buffer.getvalue())
    print(f"已生成 {destination.name}：{len(files)} 个公开文件，无 Git 历史或本地数据")
    print(f"SHA-256: {hashlib.sha256(buffer.getvalue()).hexdigest()}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["check", "audit-git", "export"])
    parser.add_argument("--output", default="dist/weavegrid-source.tar.gz")
    args = parser.parse_args()
    try:
        names = read_manifest()
        files, issues = public_files(names)
        if args.command == "audit-git":
            issues.extend(audit_git(names))
        if issues:
            print("需处理的发布检查结果（不显示匹配内容）：", file=sys.stderr)
            for issue in sorted(set(issues)):
                print("- " + issue, file=sys.stderr)
            return 1
        if args.command == "export":
            export(files, args.output)
        else:
            print(f"已检查 {len(files)} 个公开文件；未命中当前规则。仍需人工检查隐私与授权。")
        return 0
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        if isinstance(error, subprocess.CalledProcessError):
            print("Git 检查失败；请在仓库内运行并确认历史可读。", file=sys.stderr)
        else:
            print(str(error), file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
