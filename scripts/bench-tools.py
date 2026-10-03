#!/usr/bin/env python3
"""Compare two ReadyRig builds on the same agent tasks.

For each build it starts an isolated instance (own data dir, own ports, no
Chrome, no tunnel) and runs the tasks below over MCP, the way an agent would,
using the best strategy the build offers (batching, long waits and list_tasks
wait are used only when the build has them). It reports, per task, the number
of requests, the bytes returned (a proxy for tokens: about 3.5 bytes/token), the
wall time, and whether the job completed.

Usage: scripts/bench-tools.py OLD_BINARY NEW_BINARY [--quick]
"""
import argparse, json, os, re, shutil, subprocess, sys, tempfile, threading, time, urllib.request

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BYTES_PER_TOKEN = 3.5


class Server:
    def __init__(self, label, binary, port, workspace, root):
        self.label, self.data = label, os.path.join(root, 'data-' + label)
        self.log = open(os.path.join(root, label + '.log'), 'w')
        self.proc = subprocess.Popen([binary, '--data-dir', self.data, 'serve', '--foreground', '--workspace', workspace, '--allow-shell', '--no-chrome', '--no-update', '--gateway', '127.0.0.1:%d' % (port + 1), '--ui', '127.0.0.1:%d' % port], stdout=self.log, stderr=self.log, start_new_session=True)
        self.base = None
        for _ in range(100):
            time.sleep(0.2)
            m = re.search(r'Agent API: (\S+)', open(self.log.name).read())
            if m:
                self.base = m.group(1)
                break
        if not self.base:
            raise SystemExit('%s did not start' % label)

    def stop(self):
        try:
            os.killpg(self.proc.pid, 15)
        except ProcessLookupError:
            pass


class Client:
    """One MCP session; counts requests and bytes of everything the agent receives."""

    def __init__(self, base):
        self.base, self.sid, self.n, self.calls, self.bytes = base, None, 0, 0, 0
        init = self.rpc('initialize', {'protocolVersion': '2025-06-18', 'clientInfo': {'name': 'bench'}})
        self.instructions = len(init['instructions'])
        tools = self.rpc('tools/list')['tools']
        self.tools = {t['name'] for t in tools}
        self.tools_list_bytes = len(json.dumps(tools, separators=(',', ':')))

    def rpc(self, method, params=None):
        self.n += 1
        body = {'jsonrpc': '2.0', 'id': self.n, 'method': method}
        if params is not None:
            body['params'] = params
        req = urllib.request.Request(self.base + '/mcp', json.dumps(body).encode(), {'Content-Type': 'application/json'})
        if self.sid:
            req.add_header('Mcp-Session-Id', self.sid)
        with urllib.request.urlopen(req, timeout=180) as r:
            self.sid = r.headers.get('Mcp-Session-Id') or self.sid
            return json.loads(r.read())['result']

    def call(self, name, args):
        """Returns (text, is_error). Counts one request and the text bytes received."""
        res = self.rpc('tools/call', {'name': name, 'arguments': args})
        text = ''.join(c['text'] for c in res['content'] if c['type'] == 'text')
        self.calls += 1
        self.bytes += len(text)
        return text, res.get('isError', False)

    def has(self, tool):
        return tool in self.tools


def finished(text):
    return '[running' not in text and ('[exit code' in text or '[terminated' in text or '[timed out]' in text or '"exit_code"' in text)


def session_of(text):
    m = re.search(r'session_id[=":\s]+([0-9a-f]{24})', text)
    return m.group(1) if m else None


def wait_for_completion(c, sid):
    """Poll with the longest wait the build allows until the command ends."""
    wait = 55000
    while True:
        text, err = c.call('write_stdin', {'session_id': sid, 'yield_time_ms': wait})
        if err and 'yield_time_ms' in text and wait > 20000:
            wait = 20000  # an older build caps the wait at 20 s
            continue
        if finished(text) or (err and 'session' in text):
            return text


def task_survey(c):
    files = ['README.md', 'internal/server/server.go', 'internal/harness/registry.go', 'internal/computer/computer.go']
    reads = [('read_file', {'path': p}) for p in files]
    extra = [('search_files', {'query': 'func ', 'path': 'internal/harness'}), ('list_directory', {'path': 'internal'})]
    if c.has('batch'):
        c.call('batch', {'calls': [{'tool': t, 'arguments': a} for t, a in reads + extra]})
    else:
        for t, a in reads + extra:
            c.call(t, a)
    return {}


def task_big_output(c):
    c.call('exec_command', {'command': 'i=0; while [ $i -lt 20000 ]; do echo "padding line number $i"; i=$((i+1)); done', 'yield_time_ms': 8000})
    return {}


def task_small_ops(c):
    c.call('write_file', {'path': 'bench/a.txt', 'content': 'one\ntwo\nthree\n'})
    c.call('edit_file', {'path': 'bench/a.txt', 'old_string': 'two', 'new_string': '2'})
    c.call('read_file', {'path': 'bench/a.txt'})
    c.call('exec_command', {'command': 'echo hello'})
    c.call('exec_command', {'command': 'ls /nonexistent-bench-dir'})
    return {}


def task_wait_silent_job(c, seconds):
    t, _ = c.call('exec_command', {'command': 'sleep %d; echo silent-done' % seconds, 'yield_time_ms': 500, 'background': True})
    sid = session_of(t)
    text = wait_for_completion(c, sid)
    return {'completed': 'silent-done' in text}


def task_two_jobs(c, a, b):
    ids = []
    for s in (a, b):
        t, _ = c.call('exec_command', {'command': 'sleep %d; echo job-%d-done' % (s, s), 'yield_time_ms': 300, 'background': True})
        ids.append(session_of(t))
    if c.has('list_tasks'):
        text, err = c.call('list_tasks', {'wait': 'all', 'yield_time_ms': 55000})
        if err:  # this build has list_tasks without waiting
            text = ''
        else:
            while 'running' in text.replace('running ', 'running ') and re.search(r'running \d', text):
                text, _ = c.call('list_tasks', {'wait': 'all', 'yield_time_ms': 55000})
            return {'completed': True}
    done = 0
    for sid in ids:
        text = wait_for_completion(c, sid)
        done += 'done' in text
    return {'completed': done == 2}


def task_long_default_timeout(c, seconds):
    t, _ = c.call('exec_command', {'command': 'sleep %d; echo finished-ok' % seconds, 'yield_time_ms': 1000})
    sid = session_of(t)
    text = wait_for_completion(c, sid)
    return {'completed': 'finished-ok' in text}


def timed(fn, c, *args):
    c0, b0, t0 = c.calls, c.bytes, time.time()
    extra = fn(c, *args)
    return dict({'calls': c.calls - c0, 'bytes': c.bytes - b0, 'seconds': round(time.time() - t0, 1)}, **extra)


def run_build(label, binary, port, workspace, root, quick):
    srv = Server(label, binary, port, workspace, root)
    out = {}
    try:
        main = Client(srv.base)
        out['session setup'] = {'calls': 2, 'bytes': main.instructions + main.tools_list_bytes, 'tools': len(main.tools)}
        long_results, threads = {}, []
        silent, pair, longsleep = (25, (10, 14), 40) if quick else (50, (20, 25), 70)
        jobs = [('wait for a silent %ds job' % silent, task_wait_silent_job, (silent,)), ('wait for two jobs (%ds, %ds)' % pair, task_two_jobs, pair), ('%ds command, default timeout' % longsleep, task_long_default_timeout, (longsleep,))]
        for name, fn, args in jobs:
            def work(name=name, fn=fn, args=args):
                try:
                    long_results[name] = timed(fn, Client(srv.base), *args)
                except Exception as e:  # report, do not hide
                    long_results[name] = {'calls': 0, 'bytes': 0, 'seconds': 0, 'completed': False, 'error': repr(e)[:80]}
            t = threading.Thread(target=work)
            t.start()
            threads.append(t)
        for name, fn in [('survey: 4 files + search + list', task_survey), ('508 KB command output', task_big_output), ('small ops (write, edit, read, 2 commands)', task_small_ops)]:
            out[name] = timed(fn, Client(srv.base))
        for t in threads:
            t.join()
        out.update(long_results)
    finally:
        srv.stop()
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('old')
    ap.add_argument('new')
    ap.add_argument('--quick', action='store_true', help='shorter jobs (about 25 s instead of 70 s)')
    ap.add_argument('--json', help='also write raw results to this file')
    a = ap.parse_args()
    root = tempfile.mkdtemp(prefix='readyrig-bench-')
    ws = os.path.join(root, 'ws')
    os.makedirs(ws)
    shutil.copy(os.path.join(REPO, 'README.md'), ws)
    shutil.copytree(os.path.join(REPO, 'internal'), os.path.join(ws, 'internal'), ignore=shutil.ignore_patterns('node_modules', 'assets', '*.png', '*.jpg'))
    results = {}
    threads = []
    for label, binary, port in (('old', a.old, 29331), ('new', a.new, 29441)):
        def go(label=label, binary=binary, port=port):
            results[label] = run_build(label, binary, port, ws, root, a.quick)
        t = threading.Thread(target=go)
        t.start()
        threads.append(t)
    for t in threads:
        t.join()
    shutil.rmtree(root, ignore_errors=True)
    if a.json:
        json.dump(results, open(a.json, 'w'), indent=1)
    print('%-44s %-26s %-26s %s' % ('task', 'old: calls / ~tokens / sec', 'new: calls / ~tokens / sec', 'completed old -> new'))
    for task in results['old']:
        o, n = results['old'][task], results['new'][task]
        fmt = lambda r: '%d / %d / %s' % (r['calls'], round(r['bytes'] / BYTES_PER_TOKEN), r['seconds']) if 'calls' in r else ''
        done = ''
        if 'completed' in o or 'completed' in n:
            done = '%s -> %s' % (o.get('completed'), n.get('completed'))
        if task == 'session setup':
            print('%-44s %-26s %-26s tools %d -> %d' % (task, '- / %d / -' % round(o['bytes'] / BYTES_PER_TOKEN), '- / %d / -' % round(n['bytes'] / BYTES_PER_TOKEN), o['tools'], n['tools']))
        else:
            print('%-44s %-26s %-26s %s' % (task, fmt(o), fmt(n), done))


if __name__ == '__main__':
    main()
