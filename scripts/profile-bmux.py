#!/usr/bin/env python3
"""Capture client/server Go CPU and allocation profiles over private sockets.

This only connects to explicitly enabled profiling listeners. It never starts,
restarts, or stops bmux, and uses no TCP connections or third-party packages.
"""

import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import http.client
import os
from pathlib import Path
import re
import socket
import stat
import sys


class UnixHTTPConnection(http.client.HTTPConnection):
    def __init__(self, path, timeout):
        super().__init__("localhost", timeout=timeout)
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(str(self.path))


def private_directory(path, create=False):
    if create:
        path.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid():
        raise ValueError(f"{path} must be a directory owned by your user, not a symlink")
    if stat.S_IMODE(info.st_mode) != 0o700:
        raise ValueError(f"{path} must have permissions 0700")


def capture(endpoint, profile, output, seconds):
    kind = "cpu" if profile == "profile" else "allocs"
    target = output / f"{endpoint.stem}.{kind}.pprof"
    # Never overwrite an earlier capture. Use a new output directory per run.
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    connection = UnixHTTPConnection(endpoint, seconds + 15)
    try:
        with os.fdopen(fd, "wb") as file:
            connection.request("GET", f"/debug/pprof/{profile}?seconds={seconds}")
            response = connection.getresponse()
            if response.status != 200:
                detail = response.read(4096).decode(errors="replace").strip()
                raise RuntimeError(f"{endpoint.name}: HTTP {response.status}: {detail}")
            while chunk := response.read(65536):
                file.write(chunk)
        return target
    except BaseException:
        target.unlink(missing_ok=True)
        raise
    finally:
        connection.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dir", required=True, type=Path, help="BMUX_PPROF_DIR of the test processes")
    parser.add_argument("--output", required=True, type=Path, help="new private directory for this capture")
    parser.add_argument("--seconds", type=int, default=30, help="capture duration, 1–300 seconds (default: 30)")
    parser.add_argument("--role", choices=["all", "client", "server"], default="all")
    args = parser.parse_args()
    if not 1 <= args.seconds <= 300:
        parser.error("--seconds must be between 1 and 300")
    private_directory(args.dir)
    endpoints = []
    for path in sorted(args.dir.glob("*.sock")):
        match = re.fullmatch(r"(client|server)-[0-9]+\.sock", path.name)
        if match and (args.role == "all" or args.role == match[1]):
            if stat.S_ISSOCK(path.lstat().st_mode):
                endpoints.append(path)
    if not endpoints:
        raise ValueError("no profiling sockets found; start a profiling-enabled test build with BMUX_PPROF_DIR")
    private_directory(args.output, create=True)
    print(f"Capturing CPU and allocations from {len(endpoints)} process(es) for {args.seconds}s.", file=sys.stderr)
    failed = False
    with ThreadPoolExecutor(max_workers=2 * len(endpoints)) as pool:
        futures = [pool.submit(capture, path, profile, args.output, args.seconds)
                   for path in endpoints for profile in ("profile", "allocs")]
        for future in as_completed(futures):
            try:
                print(future.result())
            except Exception as error:
                print(f"capture failed: {error}", file=sys.stderr)
                failed = True
    return 1 if failed else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        print(f"profile-bmux: {error}", file=sys.stderr)
        sys.exit(1)
