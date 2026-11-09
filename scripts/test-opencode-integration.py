#!/usr/bin/env python3
"""Validate an installed bmux plugin on a private, credential-free OpenCode server.

Usage: python3 scripts/test-opencode-integration.py --bmux ./bmux --opencode opencode
Pass --v1 for OpenCode 1.18.29+. No shared service or global config is changed.
"""

import argparse
import base64
import http.client
import json
from pathlib import Path
import shutil
import secrets
import socket
import subprocess
import tempfile
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bmux", required=True)
    parser.add_argument("--opencode", default="opencode")
    parser.add_argument("--v1", action="store_true")
    args = parser.parse_args()
    bmux = str(Path(args.bmux).resolve())
    opencode = str(Path(shutil.which(args.opencode) or args.opencode).resolve())

    # All validation files stay in this checkout, including OpenCode's database.
    with tempfile.TemporaryDirectory(prefix=".opencode-validation-", dir=".") as tmp:
        home = Path(tmp).resolve()
        config = home / "config"
        project = home / "project"
        project.mkdir()
        bin_dir = home / "bin"
        bin_dir.mkdir()
        reports = home / "reports"
        fake_bmux = bin_dir / "bmux"
        fake_bmux.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$BMUX_TEST_REPORTS"\n')
        fake_bmux.chmod(0o755)
        # Do not inherit credentials, config overrides, shared-server connection
        # settings, or real bmux targeting. Our bmux executable is a recording fake.
        env = {
            "PATH": f"{bin_dir}:/usr/bin:/bin",
            "HOME": str(home),
            "XDG_CONFIG_HOME": str(config),
            "XDG_DATA_HOME": str(home / "data"),
            "XDG_CACHE_HOME": str(home / "cache"),
            "XDG_STATE_HOME": str(home / "state"),
            "OPENCODE_DB": str(home / "opencode.db"),
            "OPENCODE_PASSWORD": secrets.token_urlsafe(32),
            "OPENCODE_SERVER_PASSWORD": "",  # V1 uses the legacy variable below
            "BMUX_SESSION": "validation",
            "BMUX_SESSION_ID": "999999",
            "BMUX_PANE": "1",
            "BMUX_OPENCODE_SESSION": "ses_bmux_validation",
            "BMUX_TEST_REPORTS": str(reports),
        }
        env["OPENCODE_SERVER_PASSWORD"] = env["OPENCODE_PASSWORD"]
        if args.v1:
            # The isolated XDG data directory already isolates V1's database.
            env.pop("OPENCODE_DB")
            # V1's bootstrap otherwise fetches catalogs/default package plugins
            # into each fresh home. They are not needed for this loader test.
            env.update({"OPENCODE_DISABLE_MODELS_FETCH": "true",
                        "OPENCODE_DISABLE_DEFAULT_PLUGINS": "true",
                        "OPENCODE_DISABLE_AUTOUPDATE": "true"})
        subprocess.run([bmux, "integration", "install", "opencode"], env=env, check=True,
                       stdout=subprocess.DEVNULL)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        log = home / "server.log"
        with log.open("w") as output:
            server = subprocess.Popen(
                [opencode, "serve", "--hostname", "127.0.0.1", "--port", str(port), "--print-logs"],
                cwd=project, env=env, stdout=output, stderr=output,
            )
            try:
                def request(method, path, body=None):
                    data = json.dumps(body).encode() if body is not None else None
                    headers = {"Authorization": "Basic " + base64.b64encode(
                        ("opencode:" + env["OPENCODE_PASSWORD"]).encode()).decode()}
                    if data is not None:
                        headers["Content-Type"] = "application/json"
                    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
                    try:
                        conn.request(method, path, body=data, headers=headers)
                        response = conn.getresponse()
                        result = response.read()
                        if response.status >= 400:
                            raise RuntimeError(f"private API returned HTTP {response.status}")
                        return json.loads(result) if result else None
                    finally:
                        conn.close()

                def await_result(fn):
                    deadline = time.monotonic() + 30
                    last_error = None
                    while time.monotonic() < deadline:
                        try:
                            result = fn()
                            if result:
                                return result
                        except RuntimeError as error:
                            last_error = str(error)
                        except OSError as error:
                            last_error = type(error).__name__
                        if server.poll() is not None:
                            raise AssertionError("private server exited")
                        time.sleep(0.1)
                    raise AssertionError(f"timed out waiting for private server ({last_error})")

                prefix = "" if args.v1 else "/api"
                if args.v1:
                    # Let the legacy server finish bootstrap before probing it.
                    time.sleep(2)
                await_result(lambda: request("GET", "/global/health" if args.v1 else "/api/info"))
                if args.v1:
                    session = request("POST", "/session", {})["id"]
                    # An intentionally missing provider exercises lifecycle events
                    # without contacting a provider or spending model tokens.
                    request("POST", f"/session/{session}/prompt_async",
                            {"model": {"providerID": "bmux-validation-missing", "modelID": "missing"},
                             "parts": [{"type": "text", "text": "plugin runtime validation"}]})
                    await_result(lambda: reports.exists() and "agent idle opencode" in reports.read_text())
                    text = reports.read_text()
                    assert "agent working opencode" in text, "V1 did not report a busy session"
                    assert f"-- opencode --session {session}" in text, "V1 did not retain resume argv"
                    print("PASS: OpenCode V1 installed plugin, working/idle events and conversation resume")
                    return

                def installed_plugin():
                    plugins = request("GET", "/api/plugin")["data"]
                    return next((p for p in plugins if p.get("id") == "bmux.agent-state"), None)

                plugin = await_result(installed_plugin)
                assert plugin["state"]["status"] == "active", f"plugin not active: {plugin['state']['status']}"
                root = env["BMUX_OPENCODE_SESSION"]
                request("POST", prefix + "/session", {"id": root, "location": {"directory": str(project)},
                                                      "permissions": [{"action": "bmux-validation", "resource": "*", "effect": "ask"}]})
                child = request("POST", prefix + "/session", {"parentID": root})["data"]["id"]
                other = request("POST", prefix + "/session", {"location": {"directory": str(project)}})["data"]["id"]

                def form(session):
                    return request("POST", f"/api/session/{session}/form",
                                   {"title": "Validation", "fields": [{"key": "ok", "type": "boolean"}]})["data"]["id"]

                def lines():
                    return reports.read_text().splitlines() if reports.exists() else []

                form(other)
                time.sleep(0.5)
                assert not lines(), "unrelated root attributed to pane"
                form_id = form(child)
                await_result(lambda: lines() and lines()[-1].startswith("agent blocked opencode"))
                request("POST", f"/api/session/{child}/form/{form_id}/reply", {"answer": {"ok": True}})
                await_result(lambda: lines() and lines()[-1].startswith("agent idle opencode"))
                form_id = form(root)
                await_result(lambda: lines()[-1].startswith("agent blocked opencode"))
                request("DELETE", f"/api/session/{root}/form/{form_id}")
                await_result(lambda: lines()[-1].startswith("agent idle opencode"))
                permission = request("POST", f"/api/session/{child}/permission",
                                     {"action": "bmux-validation", "resources": ["isolated"]})["data"]
                assert permission["effect"] == "ask", "validation permission did not prompt"
                await_result(lambda: lines()[-1].startswith("agent blocked opencode"))
                request("POST", f"/api/session/{child}/permission/{permission['id']}/reply", {"decision": "once"})
                await_result(lambda: lines()[-1].startswith("agent idle opencode"))
                assert all(f"-- env BMUX_OPENCODE_SESSION={root} opencode --standalone --session {root}" in line
                           for line in lines()), "resume argv did not retain standalone root binding"
                subprocess.run([bmux, "integration", "uninstall", "opencode"], env=env, check=True,
                               stdout=subprocess.DEVNULL)
                await_result(lambda: not any(p["id"] == "bmux.agent-state"
                                             for p in request("GET", "/api/plugin")["data"]))
                count = len(lines())
                form(root)
                time.sleep(0.5)
                assert len(lines()) == count, "unloaded subscription still reports"
                print("PASS: OpenCode V2 installed plugin, child forms/permissions, isolation, resume, unload cleanup")
            except Exception:
                # Preserve isolated diagnostics locally without printing passwords
                # or server logs into a shared test/CI transcript.
                diagnostic = Path(f".opencode-validation-error-{'v1' if args.v1 else 'v2'}.log")
                shutil.copyfile(log, diagnostic)
                print(f"Private server diagnostics saved locally to {diagnostic}")
                raise
            finally:
                server.terminate()
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait()


if __name__ == "__main__":
    main()
