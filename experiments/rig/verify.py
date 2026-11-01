#!/usr/bin/env python3
"""Verify a checkpoint the way a reader would: manifest first, then every shard's size and hash.

Both arms of an experiment are measured with this one tool. A copy that exists but does not verify is
not a surviving checkpoint — it is a torn one, and counting it as surviving is how a rig lies.
"""
import hashlib
import json
import os
import sys


def steps(root):
    out = []
    for name in os.listdir(root):
        path = os.path.join(root, name)
        if name.startswith("step-") and os.path.isdir(path):
            try:
                out.append((int(name.split("-", 1)[1]), path))
            except ValueError:
                pass
    return sorted(out)


def verify(path):
    try:
        with open(os.path.join(path, "manifest.json"), "rb") as f:
            manifest = json.load(f)
    except (OSError, ValueError) as err:
        return False, "manifest unreadable: %s" % err
    for shard in manifest.get("shards", []):
        target = os.path.join(path, "rank-%d.bin" % shard["rank"])
        try:
            with open(target, "rb") as f:
                data = f.read()
        except OSError as err:
            return False, "rank-%d missing: %s" % (shard["rank"], err)
        if len(data) != shard["bytes"]:
            return False, "rank-%d is %d bytes, manifest says %d" % (shard["rank"], len(data), shard["bytes"])
        if hashlib.sha256(data).hexdigest() != shard["sha256"]:
            return False, "rank-%d hash mismatch" % shard["rank"]
    return True, "ok"


def main():
    if len(sys.argv) < 2:
        print("usage: verify.py <checkpoint-dir>", file=sys.stderr)
        return 2
    root = sys.argv[1]
    found = steps(root) if os.path.isdir(root) else []
    if not found:
        print(json.dumps({"step": -1, "ok": False, "why": "no step directory"}))
        return 1
    for step, path in reversed(found):
        ok, why = verify(path)
        if ok:
            print(json.dumps({"step": step, "ok": True, "why": "ok", "dir": path}))
            return 0
    step, path = found[-1]
    ok, why = verify(path)
    print(json.dumps({"step": -1, "ok": False, "why": why, "newest": step, "dir": path}))
    return 1


if __name__ == "__main__":
    sys.exit(main())
