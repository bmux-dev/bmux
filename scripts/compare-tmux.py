#!/usr/bin/env python3
"""Compares bmux with tmux: keystroke latency, output throughput and server
memory. Each multiplexer runs with its own throwaway socket and config, with
an attached client in a pty, so the measurements cover the whole path a user
sees: client, server, pane and back.

Usage: scripts/compare-tmux.py [--quick]   (needs go, tmux and python3)
Prints Markdown tables. --quick runs fewer samples, for checking the script.
"""
import fcntl
import os
import pty
import select
import shutil
import signal
import statistics
import struct
import subprocess
import sys
import tempfile
import termios
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
QUICK = "--quick" in sys.argv[1:]
MARKS = ("\u00a7".encode(), "\u00b6".encode())  # § and ¶: no bar draws these
END = b"\xc2\xa7END\xc2\xa7"


class Client:
    """An attached client in a pty of the given size."""

    def __init__(self, argv, env, cols, rows):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execvpe(argv[0], argv, env)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        os.kill(self.pid, signal.SIGWINCH)

    def read(self, timeout):
        r, _, _ = select.select([self.fd], [], [], timeout)
        if not r:
            return b""
        try:
            return os.read(self.fd, 1 << 16)
        except OSError:
            return b""

    def drain(self, quiet=0.2, limit=1.0):
        """Reads until output pauses for quiet seconds, or for limit seconds."""
        deadline = time.monotonic() + limit
        while time.monotonic() < deadline and self.read(quiet):
            pass

    def wait_for(self, needle, timeout):
        """Reads until needle appears; returns the time it did, or None."""
        tail, deadline = b"", time.monotonic() + timeout
        while time.monotonic() < deadline:
            chunk = self.read(deadline - time.monotonic())
            if needle in tail + chunk:
                return time.perf_counter()
            tail = (tail + chunk)[-len(needle):]
        return None

    def close(self):
        try:
            os.kill(self.pid, signal.SIGTERM)
            os.waitpid(self.pid, 0)
        except (ProcessLookupError, ChildProcessError):
            pass
        os.close(self.fd)


def rss_kib(pid):
    out = subprocess.run(["ps", "-o", "rss=", "-p", str(pid)], capture_output=True, text=True).stdout
    return int(out.strip() or 0)


class Bmux:
    name = "bmux"

    def __init__(self, work, binary, history):
        self.work, self.bin = work, binary
        os.makedirs(f"{work}/home", mode=0o700)
        os.makedirs(f"{work}/run", mode=0o700)
        with open(f"{work}/config.toml", "w") as f:
            f.write(f"onboarding = false\n[sidebar]\nshow = false\n[scrollback]\nlines = {history}\n")
        self.env = env(work, BMUX_RUNTIME_DIR=f"{work}/run", BMUX_CONFIG=f"{work}/config.toml")

    def new(self, session, command=None):
        subprocess.run([self.bin, "new", "--detach", "--dir", self.work, session], env=self.env, check=True, capture_output=True)
        if command:
            self.send(session, command)

    def send(self, session, text):
        subprocess.run([self.bin, "send", f"{session}:1", "--text", text, "--key", "Enter"], env=self.env, check=True)

    def attach(self, session, cols, rows):
        return Client([self.bin, "attach", session], self.env, cols, rows)

    def server_pid(self):
        out = subprocess.run(["pgrep", "-f", f"{self.bin} --serve"], capture_output=True, text=True).stdout.split()
        return int(out[0])

    def stop(self):
        subprocess.run(["pkill", "-f", f"{self.bin} --serve"])
        for _ in range(50):
            if subprocess.run(["pgrep", "-f", f"{self.bin} --serve"], capture_output=True).returncode:
                break
            time.sleep(0.1)


class Tmux:
    name = "tmux"

    def __init__(self, work, history):
        self.work, self.sock = work, f"compare-{os.getpid()}-{os.path.basename(work)}"
        os.makedirs(f"{work}/home", mode=0o700)
        with open(f"{work}/tmux.conf", "w") as f:
            f.write(f"set -g status off\nset -g history-limit {history}\nset -g default-terminal tmux-256color\n")
        self.env = env(work)

    def tmux(self, *args, **kw):
        return subprocess.run(["tmux", "-L", self.sock, "-f", f"{self.work}/tmux.conf", *args], env=self.env, capture_output=True, text=True, **kw)

    def new(self, session, command=None):
        self.tmux("new-session", "-d", "-s", session, "-c", self.work, check=True)
        if command:
            self.send(session, command)

    def send(self, session, text):
        self.tmux("send-keys", "-t", session, "-l", text, check=True)
        self.tmux("send-keys", "-t", session, "Enter", check=True)

    def attach(self, session, cols, rows):
        return Client(["tmux", "-L", self.sock, "attach", "-t", session], self.env, cols, rows)

    def server_pid(self):
        return int(self.tmux("display-message", "-p", "#{pid}", check=True).stdout)

    def stop(self):
        self.tmux("kill-server")


def env(work, **extra):
    e = {"PATH": os.environ["PATH"], "HOME": f"{work}/home", "TERM": "xterm-256color",
         "SHELL": "/bin/bash", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
    e.update(extra)
    return e


def fresh(kind, binary, history=10000):
    work = tempfile.mkdtemp(prefix=f"compare-{kind}-")
    mux = Bmux(work, binary, history) if kind == "bmux" else Tmux(work, history)
    return mux, work


# Echoes each two-byte mark over the previous one, so only the newest mark is
# ever on screen and a repaint of old output can't look like the echo.
ECHO = "stty raw -echo; exec python3 -c 'import os; [os.write(1, b\"\\r\" + os.read(0, 2)) for _ in iter(int, 1)]'"


def latency(kind, binary, samples):
    """Milliseconds from a key reaching the client until its echo comes back."""
    mux, work = fresh(kind, binary)
    try:
        mux.new("lat", ECHO)
        c = mux.attach("lat", 120, 40)
        time.sleep(1.5)
        c.drain()
        times = []
        for i in range(samples + 20):
            mark = MARKS[i % 2]
            c.drain(0.01)
            t0 = time.perf_counter()
            os.write(c.fd, mark)
            t1 = c.wait_for(mark, 2)
            if t1 is None:
                raise RuntimeError(f"{kind}: no echo for keystroke {i}")
            if i >= 20:  # the first keystrokes warm up both programs
                times.append((t1 - t0) * 1000)
            time.sleep(0.02)
        c.close()
        return times
    finally:
        mux.stop()
        shutil.rmtree(work, ignore_errors=True)


def throughput(kind, binary, lines, runs):
    """Seconds from Enter until the line after `seq 1 LINES` reaches the client."""
    mux, work = fresh(kind, binary)
    try:
        mux.new("tp", "PS1='$ '")
        c = mux.attach("tp", 120, 40)
        time.sleep(1.5)
        c.drain()
        results = []
        for _ in range(runs):
            os.write(c.fd, b"clear\r")  # so the last run's marker isn't on screen
            c.drain(0.3)
            os.write(c.fd, f"seq 1 {lines}; printf '\\302\\247END\\302\\247\\n'".encode())
            c.drain(0.3)
            t0 = time.perf_counter()
            os.write(c.fd, b"\r")
            t1 = c.wait_for(END, 120)
            if t1 is None:
                raise RuntimeError(f"{kind}: seq never finished")
            results.append(t1 - t0)
            c.drain(0.5)
        c.close()
        return results
    finally:
        mux.stop()
        shutil.rmtree(work, ignore_errors=True)


# Fills the pane's history with N full-width lines, plain or with a color change
# every 8 cells (like compiler or ls output), then prints the end marker and
# writes the pane's width to a file.
FILL = ("python3 -c 'import shutil, sys; w = shutil.get_terminal_size().columns; "
        "n, c = int(sys.argv[1]), sys.argv[2] == \"colored\"; "
        "line = \"\".join((\"\\x1b[3%dm\" % (i // 8 % 7 + 1) if c and i % 8 == 0 else \"\") + \"x\" for i in range(w)) + (\"\\x1b[m\" if c else \"\"); "
        "[sys.stdout.write(line + \"\\n\") for _ in range(n)]; print(\"\\u00a7END\\u00a7\"); "
        "open(sys.argv[3], \"w\").write(str(w))'")


def memory(kind, binary, lines, cols, style):
    """Server RSS in KiB after LINES full-width lines of scrollback, and the
    pane's width."""
    mux, work = fresh(kind, binary, history=max(lines, 100))
    try:
        mux.new("mem", "PS1='$ '")
        c = mux.attach("mem", cols, 40)
        time.sleep(1.5)
        c.drain()
        mux.send("mem", f"{FILL} {lines} {style} {work}/width")
        if c.wait_for(END, 300) is None:
            raise RuntimeError(f"{kind}: fill never finished")
        time.sleep(2)
        c.drain()
        full = rss_kib(mux.server_pid())
        with open(f"{work}/width") as f:
            width = int(f.read())
        c.close()
        return full, width
    finally:
        mux.stop()
        shutil.rmtree(work, ignore_errors=True)


def panes(kind, binary, count):
    """Server RSS in KiB with COUNT sessions, each one shell."""
    mux, work = fresh(kind, binary)
    try:
        for i in range(count):
            mux.new(f"p{i}")
        time.sleep(2)
        return rss_kib(mux.server_pid())
    finally:
        mux.stop()
        shutil.rmtree(work, ignore_errors=True)


def pct(xs, p):
    xs = sorted(xs)
    return xs[min(len(xs) - 1, int(p / 100 * len(xs)))]


def mib(kib):
    return f"{kib / 1024:.1f} MiB"


def main():
    build = tempfile.mkdtemp(prefix="compare-bin-")
    binary = f"{build}/bmux"
    subprocess.run(["go", "build", "-o", binary, "./cmd/bmux"], cwd=ROOT, check=True)
    tmux_version = subprocess.run(["tmux", "-V"], capture_output=True, text=True).stdout.strip()
    bmux_version = subprocess.run([binary, "--version"], capture_output=True, text=True).stdout.strip()
    print(f"{bmux_version} (this checkout) vs {tmux_version}, {os.uname().sysname} {os.uname().machine}\n")
    kinds = ("bmux", "tmux")
    samples, runs, seq_lines = (60, 1, 200000) if QUICK else (300, 5, 1000000)
    try:
        print(f"Keystroke echo, {samples} keys echoed by a program in the pane, client 120x40 (ms):\n")
        print("| | median | p95 | p99 | max |\n| --- | --- | --- | --- | --- |")
        for k in kinds:
            t = latency(k, binary, samples)
            print(f"| {k} | {statistics.median(t):.2f} | {pct(t, 95):.2f} | {pct(t, 99):.2f} | {max(t):.2f} |")

        print(f"\n`seq 1 {seq_lines}` until the next line reaches the client, median of {runs} (s):\n")
        print("| | time |\n| --- | --- |")
        for k in kinds:
            print(f"| {k} | {statistics.median(throughput(k, binary, seq_lines, runs)):.2f} |")

        print("\nServer memory (RSS) with one pane, history limit = lines kept:\n")
        print("| Scrollback | bmux | tmux |\n| --- | --- | --- |")
        cases = [(0, 80, "plain")] + [(n, w, s) for n in ((10000,) if QUICK else (10000, 50000))
                                         for w in (80, 200) for s in ("plain", "colored")]
        for n, w, s in cases:
            row = [memory(k, binary, n, w, s) for k in kinds]
            widths = "/".join(str(width) for _, width in row) if row[0][1] != row[1][1] else str(row[0][1])
            label = "idle" if n == 0 else f"{n:,} lines, {widths} cols, {s}"
            print(f"| {label} | " + " | ".join(mib(full) for full, _ in row) + " |")

        count = 4 if QUICK else 20
        print(f"\nServer memory (RSS) with {count} sessions, one shell each:\n")
        print("| | RSS |\n| --- | --- |")
        for k in kinds:
            print(f"| {k} | {mib(panes(k, binary, count))} |")
    finally:
        shutil.rmtree(build, ignore_errors=True)


if __name__ == "__main__":
    main()
