#!/usr/bin/env python3
"""Compare idle and single-key terminal traffic from two bmux builds.

Usage: python3 scripts/measure-redraw.py /path/to/baseline /path/to/candidate
Both binaries are copied into a temporary directory and run with isolated
sockets, homes and configuration; existing bmux servers are never restarted.
"""
import argparse
import importlib.util
import json
import os
import shutil
import sys
import tempfile
import time

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location(
    "compare", os.path.join(os.path.dirname(__file__), "compare-tmux.py")
)
compare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compare)


def capture(client, seconds):
    data = bytearray()
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        data.extend(client.read(max(0, deadline - time.monotonic())))
    return data


def measure(label, source):
    with tempfile.TemporaryDirectory(prefix="bmux-redraw-") as work:
        binary = os.path.join(work, "bmux")
        shutil.copy2(source, binary)
        mux = compare.Bmux(work, binary, 100)
        client = None
        try:
            mux.new("measure")
            client = mux.attach("measure", 120, 40)
            time.sleep(1)
            command = (
                "python3 -c 'import os; "
                'os.write(1, b"\\x1b[2J\\x1b[H" + '
                '(b"x" * 110 + b"\\r\\n") * 30 + b"READY"); '
                'os.system("stty raw -echo"); '
                '[os.write(1, os.read(0, 1)) for _ in iter(int, 1)]\''
            )
            mux.send("measure", command)
            if client.wait_for(b"READY", 10) is None:
                raise RuntimeError("test pane not ready")
            client.drain()
            idle = capture(client, 2.5)
            os.write(client.fd, b"z")
            changed = capture(client, 0.25)
            if b"z" not in changed:
                raise RuntimeError("single-key echo was not observed")
            print(json.dumps({
                "build": label,
                "idle_bytes_per_second": round(len(idle) / 2.5),
                "idle_repeated_x": idle.count(b"x"),
                "single_key_window_bytes": len(changed),
                "single_key_repeated_x": changed.count(b"x"),
                "key_echo_seen": True,
            }), flush=True)
        finally:
            if client is not None:
                client.close()
            mux.stop()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("baseline")
    parser.add_argument("candidate")
    args = parser.parse_args()
    measure("baseline", args.baseline)
    measure("candidate", args.candidate)


if __name__ == "__main__":
    main()
