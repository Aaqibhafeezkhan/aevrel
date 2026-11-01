"""Record a real training run as a trace.

This is the experiment instrument, not part of the product. It trains a small model on the local
GPU, saves real checkpoints to a real directory on a timer, and writes the five trace events as
they happen. `ckptctl simulate` can then replay that file.

What is real: step times, checkpoint write times, file sizes, free space, and the effect of a
competing writer on the same disk.

What is not real: the "kill" events. The job does not die; it only records the moments at which a
platform would have taken it away, because scoring needs those moments and nothing else.

Usage:
  python record.py --size small --minutes 20 --save-every 5m --dir /tmp/ckpt-small \
      --trace /tmp/ckpt-small.jsonl [--competing-writer 200] [--kill-at 7m,15m]
"""

import argparse
import json
import os
import shutil
import threading
import time

import torch


def parse_duration(text):
    """'5m' -> 300 seconds; '90s' -> 90; '1h' -> 3600."""
    units = {"s": 1, "m": 60, "h": 3600}
    if text and text[-1] in units:
        return float(text[:-1]) * units[text[-1]]
    return float(text)


def build_model(size, device):
    """Two sizes, roughly 8x apart in parameters: that is the fleet claim in miniature."""
    hidden = 512 if size == "small" else 1536
    model = torch.nn.Sequential(
        torch.nn.Linear(1024, hidden),
        torch.nn.GELU(),
        torch.nn.Linear(hidden, hidden),
        torch.nn.GELU(),
        torch.nn.Linear(hidden, hidden),
        torch.nn.GELU(),
        torch.nn.Linear(hidden, 1024),
    ).to(device)
    return model


TRACE = []


def emit(kind, **fields):
    event = {"t": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "event": kind}
    event.update(fields)
    TRACE.append(event)


def free_bytes(path):
    return shutil.disk_usage(path).free


def competing_writer(path, megabytes_per_second, stop):
    """Keeps the disk busy so a checkpoint write is slow and jittery, like shared storage."""
    chunk = bytearray(8 * 1024 * 1024)
    period = len(chunk) / (megabytes_per_second * 1024 * 1024)
    while not stop.is_set():
        begin = time.time()
        with open(os.path.join(path, ".competing"), "wb") as f:
            for _ in range(16):
                f.write(chunk)
                time.sleep(period)
        os.remove(os.path.join(path, ".competing"))
        time.sleep(0.2)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--size", choices=["small", "big"], default="small")
    ap.add_argument("--minutes", type=float, default=20, help="wall-clock length of the run")
    ap.add_argument("--save-every", default="5m", help="timer between seals, e.g. 5m")
    ap.add_argument("--dir", required=True, help="checkpoint directory")
    ap.add_argument("--trace", required=True, help="trace file to write")
    ap.add_argument("--competing-writer", type=float, default=0,
                    help="megabytes per second a competing writer sustains, 0 to disable")
    ap.add_argument("--kill-at", default="", help="comma separated moments, e.g. 7m,15m")
    ap.add_argument("--seed", type=int, default=1)
    args = ap.parse_args()

    torch.manual_seed(args.seed)
    device = "cuda" if torch.cuda.is_available() else "cpu"
    print("device:", device, torch.cuda.get_device_name(0) if device == "cuda" else "cpu")
    model = build_model(args.size, device)
    opt = torch.optim.AdamW(model.parameters(), lr=1e-4)
    batch = torch.randn(64, 1024, device=device)
    target = torch.randn(64, 1024, device=device)

    os.makedirs(args.dir, exist_ok=True)
    save_every = parse_duration(args.save_every)
    kills = [parse_duration(x) for x in args.kill_at.split(",") if x]

    stop = threading.Event()
    if args.competing_writer > 0:
        threading.Thread(target=competing_writer, args=(args.dir, args.competing_writer, stop),
                         daemon=True).start()

    begin = time.time()
    step = 0
    next_save = 0.0
    kill_index = 0
    emit("free", bytes=free_bytes(args.dir))

    while True:
        elapsed = time.time() - begin
        if elapsed >= args.minutes * 60:
            break
        if kill_index < len(kills) and elapsed >= kills[kill_index]:
            emit("kill")
            kill_index += 1
        if elapsed >= next_save:
            path = os.path.join(args.dir, f"step-{step}.pt")
            emit("save_start", step=step)
            save_begin = time.time()
            torch.save({"model": model.state_dict(), "opt": opt.state_dict(), "step": step}, path)
            save_end = time.time()
            emit("save_end", step=step, bytes=os.path.getsize(path), sealed=True)
            emit("free", bytes=free_bytes(args.dir))
            print(f"sealed step {step} at {elapsed/60:.1f} min, write {save_end-save_begin:.1f}s, "
                  f"{os.path.getsize(path)/1e6:.0f} MB")
            next_save = save_end - begin + save_every
            emit("step", step=step)
            continue

        loss = ((model(batch) - target) ** 2).mean()
        opt.zero_grad()
        loss.backward()
        opt.step()
        step += 1
        if step % 20 == 0:
            emit("step", step=step)

    stop.set()
    with open(args.trace, "w") as f:
        for event in TRACE:
            f.write(json.dumps(event, separators=(",", ":")) + "\n")
    print("trace:", args.trace, len(TRACE), "events")


if __name__ == "__main__":
    main()
