#!/usr/bin/env python3
"""Exercise real versioned binaries, authenticated APIs, shutdown and restart.

All processes, ports, logs and installation files are isolated in a temp folder.
Set READYRIG_TEST_PAUSE=1 to inspect the ready UI; create the printed continue file
when done. No production installation or user data is touched.
"""
import hashlib
import http.server
import http.cookiejar
import json
import os
import pathlib
import re
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]


def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


def eventually(fn, timeout=30):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            value = fn()
            if value:
                return value
        except (OSError, ValueError):
            pass
        time.sleep(.1)
    raise AssertionError('timed out waiting for expected update state')


with tempfile.TemporaryDirectory(prefix='readyrig-update-e2e-') as tmp:
    root = pathlib.Path(tmp)
    arch = subprocess.check_output(['go', 'env', 'GOARCH'], text=True).strip()
    goos = subprocess.check_output(['go', 'env', 'GOOS'], text=True).strip()
    installed = root / 'readyrig'
    desktop = os.environ.get('READYRIG_TEST_DESKTOP', os.environ.get('RELAY_TEST_DESKTOP')) == '1'
    flavor = 'readyrig' if desktop else 'readyrig-web'
    new = root / f'{flavor}-{goos}-{arch}'
    for version, target in [('0.4.0', installed), ('0.5.0', new)]:
        subprocess.run(['go', 'build', *([] if desktop else ['-tags', 'nogui']), '-ldflags',
                        f'-X computer-use-server/internal/buildinfo.Version={version}',
                        '-o', str(target), './cmd/adapter'], cwd=ROOT, check=True)

    class Feed(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            body = new.read_bytes() if self.path == '/asset' else json.dumps({
                'version': '0.5.0', 'notes': '后台更新与重启验证',
                'assets': {new.name: {'url': f'http://127.0.0.1:{self.server.server_port}/asset',
                                      'size': new.stat().st_size,
                                      'sha256': hashlib.sha256(new.read_bytes()).hexdigest()}}
            }).encode()
            self.send_response(200)
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *args):
            pass

    feed = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Feed)
    threading.Thread(target=feed.serve_forever, daemon=True).start()
    ui, gateway = port(), port()
    base = f'http://127.0.0.1:{ui}'
    logpath = root / 'process.log'
    log = logpath.open('w')
    proc = subprocess.Popen([str(installed), 'desktop' if desktop else 'web', *([] if desktop else ['--foreground']),
                             '--allow-shell', '--no-chrome', '--workspace', str(root / 'workspace'),
                             '--data-dir', str(root / 'data'), '--ui', f'127.0.0.1:{ui}',
                             '--gateway', f'127.0.0.1:{gateway}', '--update-feed',
                             f'http://127.0.0.1:{feed.server_port}/latest'], stdout=log, stderr=log)
    try:
        def keys():
            return re.findall(r'Dashboard: .*#key=([^\s]+)', logpath.read_text())

        if desktop:
            def staged():
                return any(p.stat().st_mode & 0o111 and p.read_bytes() == new.read_bytes()
                           for p in root.glob('.readyrig-update-*/download'))
            eventually(staged)
            # chmod is the final staging operation, just before publishing ready.
            time.sleep(.2)
            proc.terminate()
            proc.wait(timeout=20)
            assert installed.read_bytes() == new.read_bytes(), logpath.read_text()
            print('PASS: native Wails quit hook installed 0.5.0', flush=True)
            raise SystemExit(0)
        key = eventually(lambda: keys() and keys()[0])

        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

        def api(path, data=None):
            req = urllib.request.Request(base + path, data=json.dumps(data).encode() if data is not None else None,
                                         headers={'Content-Type': 'application/json'})
            with opener.open(req, timeout=5) as r:
                return json.load(r)

        api('/api/login', {'key': key})
        api('/api/update/check', {})
        eventually(lambda: api('/api/update')['state'] == 'ready')
        assert api('/api/state')['version'] == '0.4.0'
        assert api('/api/state')['enabled']['terminal'] is True
        assert subprocess.check_output([str(installed), 'version'], text=True).strip() == 'ReadyRig 0.4.0'
        # Ensure normal service data survives the restart.
        api('/api/tools/write_file', {'path': 'update-test.txt', 'content': 'preserved'})
        total = api('/api/state')['summary']['total']
        # The initial shell launch flag must not override a later saved choice.
        for category, enabled in [('terminal', False), ('computer', True), ('files', False)]:
            api('/api/capability', {'category': category, 'enabled': enabled})
        if os.environ.get('READYRIG_TEST_PAUSE', os.environ.get('RELAY_TEST_PAUSE')) == '1':
            print(f'Dashboard: {base}/#key={key}', flush=True)
            print(f'Continue file: {root / "continue"}', flush=True)
            eventually(lambda: (root / 'continue').exists(), timeout=600)
        api('/api/update/restart', {})
        eventually(lambda: len(keys()) >= 2)
        key = keys()[-1]
        api('/api/login', {'key': key})
        assert api('/api/state')['version'] == '0.5.0'
        assert api('/api/state')['summary']['total'] == total
        assert api('/api/state')['enabled']['terminal'] is False
        assert api('/api/state')['enabled']['computer'] is True
        assert api('/api/state')['enabled']['files'] is False
        assert api('/api/state')['enabled']['browser'] is False
        saved = json.loads((root / 'data' / 'cli.json').read_text())
        assert saved == {'allow-shell': False, 'allow-computer': True, 'no-files': True}
        assert (root / 'workspace' / 'update-test.txt').read_text() == 'preserved'
        assert installed.read_bytes() == new.read_bytes()
        print('PASS: background stage → authenticated restart → 0.5.0; data, launch options and capability choices preserved', flush=True)
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        feed.shutdown()
        log.close()
