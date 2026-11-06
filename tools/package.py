#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把 fpk/ 目录封装为可分发的 .fpk 安装包（不依赖 fnpack）。

fnOS 的 .fpk 本质是一个 tar.gz，内部结构：
    manifest            # 应用元信息
    app.tgz             # 应用本体（bin/ 与 ui/），同样为 tar.gz
    cmd/                # 生命周期脚本
    config/privilege    # 运行身份
    config/resource     # 端口与共享目录声明
    ICON.PNG / ICON_256.PNG

用法：python3 tools/package.py [amd64|arm64]
"""
import io
import os
import sys
import tarfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FPK = os.path.join(ROOT, "fpk")
DIST = os.path.join(ROOT, "dist")

ARCH = sys.argv[1] if len(sys.argv) > 1 else "amd64"
PLATFORM = "arm" if ARCH == "arm64" else "x86"


def read_manifest():
    meta = {}
    with open(os.path.join(FPK, "manifest"), "r", encoding="utf-8") as f:
        for line in f:
            if "=" not in line:
                continue
            k, v = line.split("=", 1)
            meta[k.strip()] = v.strip()
    return meta


def tar_add(tf, arcname, data, mode, is_dir=False):
    info = tarfile.TarInfo(arcname)
    info.mode = mode
    info.uid = info.gid = 0
    info.uname = info.gname = "root"
    if is_dir:
        info.type = tarfile.DIRTYPE
        tf.addfile(info)
    else:
        info.size = len(data)
        tf.addfile(info, io.BytesIO(data))


def build_app_tgz(bin_path):
    """把 fpk/app 下的 bin/ 与 ui/ 打成 app.tgz"""
    out = os.path.join(DIST, "app.tgz")
    os.makedirs(DIST, exist_ok=True)
    appdir = os.path.join(FPK, "app")
    with tarfile.open(out, "w:gz") as tf:
        tar_add(tf, "./", b"", 0o755, True)
        for root, dirs, files in os.walk(appdir):
            rel = os.path.relpath(root, appdir).replace("\\", "/")
            prefix = "./" if rel == "." else "./%s/" % rel
            for d in sorted(dirs):
                tar_add(tf, "%s%s/" % (prefix, d), b"", 0o755, True)
            for fn in sorted(files):
                p = os.path.join(root, fn)
                if os.path.abspath(p) == os.path.abspath(bin_path):
                    mode = 0o755
                else:
                    mode = 0o644
                with open(p, "rb") as f:
                    tar_add(tf, "%s%s" % (prefix, fn), f.read(), mode)
    return out


def build_fpk(meta, app_tgz):
    name = "%s_%s_%s.fpk" % (meta["appname"], meta["version"], PLATFORM)
    out = os.path.join(DIST, name)
    with tarfile.open(out, "w:gz") as tf:
        tar_add(tf, "./", b"", 0o755, True)
        with open(os.path.join(FPK, "manifest"), "rb") as f:
            tar_add(tf, "./manifest", f.read(), 0o644)
        with open(app_tgz, "rb") as f:
            tar_add(tf, "./app.tgz", f.read(), 0o644)
        for d in ("cmd", "config", "ui"):
            src = os.path.join(FPK, d)
            if not os.path.isdir(src):
                continue
            tar_add(tf, "./%s/" % d, b"", 0o755, True)
            for root, dirs, files in os.walk(src):
                rel = os.path.relpath(root, FPK).replace("\\", "/")
                for dd in sorted(dirs):
                    tar_add(tf, "./%s/%s/" % (rel, dd), b"", 0o755, True)
                for fn in sorted(files):
                    p = os.path.join(root, fn)
                    mode = 0o755 if d == "cmd" else 0o644
                    with open(p, "rb") as f:
                        tar_add(tf, "./%s/%s" % (rel, fn), f.read(), mode)
        for fn in ("ICON.PNG", "ICON_256.PNG"):
            p = os.path.join(FPK, fn)
            if os.path.exists(p):
                with open(p, "rb") as f:
                    tar_add(tf, "./" + fn, f.read(), 0o644)
    return out


def main():
    meta = read_manifest()
    if meta.get("platform") != PLATFORM:
        print("提示：manifest 的 platform=%s，本次按 %s 打包；如需一致请先修改 manifest。"
              % (meta.get("platform"), PLATFORM))

    bin_path = os.path.join(FPK, "app", "bin", "media-reporter")
    if not os.path.exists(bin_path):
        print("缺少二进制：%s\n请先执行 ./build.sh（会在该路径生成 linux 二进制）" % bin_path)
        return 1

    app_tgz = build_app_tgz(bin_path)
    out = build_fpk(meta, app_tgz)
    print("BUILT %s (%d bytes)" % (out, os.path.getsize(out)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
