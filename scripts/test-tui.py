#!/usr/bin/env python3
"""Verify the CLI in a real terminal, with isolated settings and no cloud access."""
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]


class TUITests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory(prefix='readyrig-tui-build-')
        cls.binary = Path(cls.build.name) / 'readyrig'
        subprocess.run(['go', 'build', '-tags', 'nogui', '-o', str(cls.binary), './cmd/adapter'],
                       cwd=ROOT, check=True, capture_output=True, timeout=120)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='readyrig-tui-')
        self.home = Path(self.temp.name).resolve()
        self.data = self.home / 'private data'
        self.env = dict(os.environ, HOME=str(self.home), TERM='xterm-256color',
                        READYRIG_NO_UPDATE='1', READYRIG_DAEMON_CHILD='')
        self.pid = self.terminal = None
        self.addCleanup(self.cleanup)

    def cleanup(self):
        if self.pid is not None:
            try:
                os.kill(self.pid, signal.SIGKILL)
                os.waitpid(self.pid, 0)
            except ProcessLookupError:
                pass
        if self.terminal is not None:
            os.close(self.terminal)
        self.cli('stop', check=False)
        self.temp.cleanup()

    def cli(self, *args, check=True):
        return subprocess.run([str(self.binary), *args, '--data-dir', str(self.data)],
                              env=self.env, check=check, capture_output=True, text=True, timeout=20)

    def open_terminal(self, *args):
        self.pid, self.terminal = pty.fork()
        if self.pid == 0:
            os.execve(str(self.binary), [str(self.binary), *args, '--data-dir', str(self.data)], self.env)
        fcntl.ioctl(self.terminal, termios.TIOCSWINSZ, struct.pack('HHHH', 28, 120, 0, 0))

    def read_until(self, marker):
        output = bytearray()
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            readable, _, _ = select.select([self.terminal], [], [], 0.1)
            if readable:
                try:
                    chunk = os.read(self.terminal, 65536)
                except OSError:
                    break
                if not chunk:
                    break
                output.extend(chunk)
                if marker.encode() in output:
                    return output.decode(errors='replace')
        self.fail(f'terminal did not show {marker!r}: {output.decode(errors="replace")}')

    def send(self, text):
        os.write(self.terminal, text.encode())

    def drain_terminal(self):
        # Terminal applications need their output consumed while we poll the API;
        # otherwise the PTY's output buffer can block the next keyboard event.
        output = bytearray()
        while select.select([self.terminal], [], [], 0)[0]:
            try:
                chunk = os.read(self.terminal, 65536)
            except OSError:
                break
            if not chunk:
                break
            output.extend(chunk)
        return output

    def wait_state(self, predicate):
        deadline = time.monotonic() + 6
        output = bytearray()
        while time.monotonic() < deadline:
            output.extend(self.drain_terminal())
            state = json.loads(self.cli('status').stdout)
            if predicate(state):
                return state
            time.sleep(0.05)
        self.fail('service state did not change as requested: ' + output.decode(errors='replace'))

    def quit(self, key='q'):
        self.send(key)
        deadline = time.monotonic() + 6
        while time.monotonic() < deadline:
            self.drain_terminal()
            waited, status = os.waitpid(self.pid, os.WNOHANG)
            if waited:
                self.pid = None
                self.assertEqual(os.waitstatus_to_exitcode(status), 0)
                flags = termios.tcgetattr(self.terminal)[3]
                self.assertTrue(flags & termios.ICANON, 'terminal canonical mode was not restored')
                self.assertTrue(flags & termios.ECHO, 'terminal echo was not restored')
                return
            time.sleep(0.05)
        self.fail('TUI did not quit')

    def test_default_launch_guide_dashboard_actions_and_quit(self):
        # Configure ephemeral ports so this test never touches a running user's instance.
        self.data.mkdir(mode=0o700)
        (self.data / 'cli.json').write_text(json.dumps({
            'gateway': '127.0.0.1:0', 'ui': '127.0.0.1:0', 'cloud-url': '', 'no-update': True}))
        # Use setup to exercise the actual terminal prompts and background start.
        self.open_terminal('setup')
        self.read_until('Workspace folder')
        self.send(str(self.home / 'work space') + '\n')
        self.read_until('Enable terminal tools?')
        self.send('yes\n')
        self.read_until('Enable Chrome browser tools?')
        self.send('no\n')
        self.read_until('Start ReadyRig in the background now?')
        self.send('\n')
        self.read_until('ReadyRig started in the background.')
        _, status = os.waitpid(self.pid, 0)
        self.pid = None
        self.assertEqual(os.waitstatus_to_exitcode(status), 0)
        os.close(self.terminal)
        self.terminal = None
        self.open_terminal()  # No command must enter the TUI.
        screen = self.read_until('Quitting this dashboard keeps the service running.')
        self.assertIn('1 Overview', screen)
        self.assertIn('Agent API', screen)
        self.assertIn('MCP', screen)

        daemon_pid = int(re.search(r'running  PID (\d+)', screen).group(1))
        self.assertEqual(os.getsid(daemon_pid), daemon_pid, 'daemon still belongs to the terminal session')
        self.send('p')
        self.wait_state(lambda s: s['paused'])
        self.send('p')
        self.wait_state(lambda s: not s['paused'])
        self.send('t')
        self.wait_state(lambda s: not s['enabled']['terminal'])
        project = self.home / '新项目 with spaces'
        project.mkdir()
        self.send('2')
        self.read_until('a add folder')
        self.send('a')
        self.read_until('Project folder')
        self.send(str(project) + '\r')
        self.wait_state(lambda s: len(s['project_access']['projects']) == 2)
        self.send('\x1b[B\r')  # Select the second project with an arrow and Enter.
        self.wait_state(lambda s: s['workspace'] == str(project))
        self.cli('call', 'help')
        self.send('4')
        self.read_until('help  success')
        self.quit()
        self.assertEqual(self.cli('status').returncode, 0, 'quitting TUI stopped the daemon')
        self.assertIn('already configured', self.cli('setup', '--if-needed').stdout)
        self.cli('restart')
        state = json.loads(self.cli('status').stdout)
        self.assertFalse(state['enabled']['terminal'], 'TUI capability choice was lost on restart')
        self.assertFalse(state['enabled']['browser'])
        self.assertEqual(state['workspace'], str(project))

    def test_first_launch_opens_guide_and_ctrl_c_keeps_daemon(self):
        # Default initial launch must guide the user before entering the dashboard.
        self.open_terminal()
        self.read_until('ReadyRig setup')
        self.send(str(self.home / 'workspace') + '\nno\nno\nno\n')
        self.read_until('Service stopped. Press s to start')
        self.quit()
        self.assertTrue((self.data / 'cli.json').is_file())
        self.cli('config', 'set', 'gateway', '127.0.0.1:0')
        self.cli('config', 'set', 'ui', '127.0.0.1:0')
        self.cli('config', 'set', 'cloud-url', '')
        os.close(self.terminal)
        self.terminal = None
        self.open_terminal()
        self.read_until('Quitting this dashboard keeps the service running.')
        self.send('s')
        self.read_until('Service stopped. Press s to start')
        self.assertNotEqual(self.cli('status', check=False).returncode, 0)
        self.send('s')
        self.read_until('running  PID')
        self.quit('\x03')
        self.assertEqual(self.cli('status').returncode, 0)


if __name__ == '__main__':
    unittest.main()
