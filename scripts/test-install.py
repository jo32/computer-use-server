#!/usr/bin/env python3
"""Exercise the curl installer with local download fixtures, never the network."""
import hashlib
import os
from pathlib import Path
import pty
import select
import shlex
import signal
import subprocess
import tempfile
import time
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "website/public/install.sh"
BINARY = b'''#!/bin/sh
case "$1" in
  help) printf 'ReadyRig fixture: config set\\n  setup [--if-needed]  Interactive guide\\n';;
  setup)
    [ -t 0 ] && [ -t 1 ] || exit 7
    printf '%s\\n' "$*" > "$MOCK_ROOT/setup-invocations"
    printf 'Workspace: '
    read -r answer
    printf '%s\\n' "$answer" > "$MOCK_ROOT/setup-answer"
    [ "${MOCK_SETUP_FAIL:-0}" != 1 ] || exit 1;;
esac
'''


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="readyrig-installer-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.mock = self.root / "mock"
        self.mock.mkdir()
        self.destination = self.root / "bin with spaces"
        self.destination.mkdir()
        self.installed = self.destination / "readyrig"
        self.installed.write_bytes(b"existing installation")
        self.env = dict(os.environ, PATH=str(self.mock) + os.pathsep + os.environ["PATH"],
                        HOME=str(self.root), MOCK_ROOT=str(self.root), MOCK_OS="Linux", MOCK_ARCH="x86_64")
        for name in ["READYRIG_VERSION", "READYRIG_INSTALL_DIR", "READYRIG_INSTALL_REPO", "READYRIG_NO_SETUP"]:
            self.env.pop(name, None)
        (self.root / "binary").write_bytes(BINARY)
        self.write_executable("uname", "#!/bin/sh\ncase $1 in -s) printf '%s\\n' \"$MOCK_OS\";; -m) printf '%s\\n' \"$MOCK_ARCH\";; esac\n")
        self.write_executable("curl", """#!/usr/bin/env python3
import os,sys,pathlib
args=sys.argv[1:]; root=pathlib.Path(os.environ['MOCK_ROOT'])
url=next(a for a in args if a.startswith('https://'))
with (root/'requests').open('a') as f: f.write(url+'\\n')
if url.endswith('/releases/latest'):
    print(os.environ.get('MOCK_LATEST','https://github.com/jo32/readyrig/releases/tag/v1.2.3'),end=''); sys.exit(0)
if os.environ.get('MOCK_FAIL') == '1': sys.exit(22)
if '/releases/download/v1.2.3/' not in url: sys.exit(22)
output=pathlib.Path(args[args.index('-o')+1])
source=root/('checksums' if url.endswith('/SHA256SUMS') else 'binary')
output.write_bytes(source.read_bytes())
""")

    def write_executable(self, name, content):
        path = self.mock / name
        path.write_text(content)
        path.chmod(0o755)

    def checksums(self, platform="linux", arch="amd64", content=None):
        if content is None:
            content = hashlib.sha256(BINARY).hexdigest() + f"  readyrig-web-{platform}-{arch}\n"
        (self.root / "checksums").write_text(content)

    def install(self, *arguments, pipe=False, auto=False):
        options = list(arguments) if auto else ["--install-dir", str(self.destination), *arguments]
        source = SCRIPT.read_text()
        if auto:
            # Map the shared bin into the fixture so root/default-path tests can
            # never modify the host's /usr/local/bin.
            source = source.replace("/usr/local/bin", str(self.root / "local/bin"))
            pipe = True
        if pipe:
            result = subprocess.run(["sh", "-s", "--", *options], input=source, text=True,
                                    env=self.env, capture_output=True, timeout=15)
        else:
            result = subprocess.run(["sh", str(SCRIPT), *options], text=True, env=self.env,
                                    capture_output=True, timeout=15)
        self.assertFalse(list(self.destination.glob(".readyrig-install.*")), result.stderr)
        return result

    def test_default_selects_writable_standard_path_directory(self):
        self.checksums()
        for directory in [self.root / ".local/bin", self.root / "bin", self.root / "local/bin"]:
            with self.subTest(directory=directory):
                directory.mkdir(parents=True)
                original_path = self.env["PATH"]
                self.env["PATH"] = str(directory) + os.pathsep + original_path
                result = self.install("--no-setup", auto=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual((directory / "readyrig").read_bytes(), BINARY)
                self.assertNotIn("Add this directory to PATH", result.stdout)
                self.env["PATH"] = original_path

    def test_default_prefers_user_directory_over_shared_directory(self):
        self.checksums()
        user = self.root / ".local/bin"
        shared = self.root / "local/bin"
        for directory in [user, shared]:
            directory.mkdir(parents=True)
        self.env["PATH"] = os.pathsep.join([str(shared), str(user), self.env["PATH"]])
        result = self.install("--no-setup", auto=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((user / "readyrig").read_bytes(), BINARY)
        self.assertFalse((shared / "readyrig").exists())

    @unittest.skipIf(os.geteuid() == 0, "root can write despite directory permission bits")
    def test_default_skips_unwritable_directory(self):
        self.checksums()
        user = self.root / ".local/bin"
        shared = self.root / "local/bin"
        for directory in [user, shared]:
            directory.mkdir(parents=True)
        user.chmod(0o555)
        self.addCleanup(user.chmod, 0o755)
        self.env["PATH"] = os.pathsep.join([str(user), str(shared), self.env["PATH"]])
        result = self.install("--no-setup", auto=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((shared / "readyrig").read_bytes(), BINARY)
        self.assertFalse((user / "readyrig").exists())

    def test_default_falls_back_without_modifying_arbitrary_path_directory(self):
        self.checksums()
        result = self.install("--no-setup", auto=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / ".local/bin/readyrig").read_bytes(), BINARY)
        self.assertIn("Add this directory to PATH", result.stdout)
        self.assertFalse((self.mock / "readyrig").exists())

    def test_explicit_destination_overrides_default_and_environment(self):
        self.checksums()
        user = self.root / ".local/bin"
        user.mkdir(parents=True)
        self.env["PATH"] = str(user) + os.pathsep + self.env["PATH"]
        self.env["READYRIG_INSTALL_DIR"] = str(self.root / "environment bin")
        result = self.install("--no-setup", auto=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / "environment bin/readyrig").read_bytes(), BINARY)
        result = self.install("--no-setup")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.installed.read_bytes(), BINARY)
        self.assertFalse((user / "readyrig").exists())

    def test_pipe_install_all_supported_platforms_and_repeat(self):
        for system, machine, platform, arch in [("Linux", "x86_64", "linux", "amd64"),
                                               ("Linux", "aarch64", "linux", "arm64"),
                                               ("Darwin", "x86_64", "darwin", "amd64"),
                                               ("Darwin", "arm64", "darwin", "arm64")]:
            with self.subTest(system=system, machine=machine):
                self.env.update(MOCK_OS=system, MOCK_ARCH=machine)
                self.checksums(platform, arch)
                result = self.install(pipe=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(self.installed.read_bytes(), BINARY)
                self.assertEqual(self.installed.stat().st_mode & 0o777, 0o755)
                self.assertIn(f"readyrig-web-{platform}-{arch}", (self.root / "requests").read_text())
                self.assertEqual(self.install("--version", "v1.2.3").returncode, 0)

    def test_pin_release_before_downloading(self):
        self.checksums()
        self.assertEqual(self.install().returncode, 0)
        self.assertEqual((self.root / "requests").read_text().splitlines(), [
            "https://github.com/jo32/readyrig/releases/latest",
            "https://github.com/jo32/readyrig/releases/download/v1.2.3/SHA256SUMS",
            "https://github.com/jo32/readyrig/releases/download/v1.2.3/readyrig-web-linux-amd64"])

    def test_checksum_errors_keep_existing_binary(self):
        correct = hashlib.sha256(BINARY).hexdigest() + "  readyrig-web-linux-amd64\n"
        for content in ["", "0" * 64 + "  readyrig-web-linux-amd64\n", correct * 2,
                        hashlib.sha256(BINARY).hexdigest() + "  readyrig-web-linux-arm64\n"]:
            with self.subTest(checksums=content):
                self.checksums(content=content)
                self.assertNotEqual(self.install().returncode, 0)
                self.assertEqual(self.installed.read_bytes(), b"existing installation")

    def test_download_failure_keeps_existing_binary(self):
        self.checksums()
        self.env["MOCK_FAIL"] = "1"
        self.assertNotEqual(self.install().returncode, 0)
        self.assertEqual(self.installed.read_bytes(), b"existing installation")

    def test_release_without_cli_commands_keeps_existing_binary(self):
        old = b"#!/bin/sh\nprintf 'old release\\n'\n"
        (self.root / "binary").write_bytes(old)
        self.checksums(content=hashlib.sha256(old).hexdigest() + "  readyrig-web-linux-amd64\n")
        result = self.install()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not provide the CLI commands", result.stderr)
        self.assertEqual(self.installed.read_bytes(), b"existing installation")

    def test_invalid_options_and_unsupported_platform_fail_before_downloading(self):
        for args in [("--version", "../bad"), ("--repo", "https://evil.example"), ("--unknown",), ("--version",)]:
            self.assertNotEqual(self.install(*args).returncode, 0)
        self.env["MOCK_OS"] = "FreeBSD"
        self.assertNotEqual(self.install().returncode, 0)
        self.assertFalse((self.root / "requests").exists())

    def test_unexpected_latest_redirect_is_rejected(self):
        self.env["MOCK_LATEST"] = "https://evil.example/releases/tag/v1.2.3"
        self.assertNotEqual(self.install().returncode, 0)
        self.assertEqual(self.installed.read_bytes(), b"existing installation")

    def test_help_and_no_configuration_changes(self):
        result = self.install("--help")
        self.assertEqual(result.returncode, 0)
        self.assertIn("--install-dir", result.stdout)
        self.assertFalse((self.root / "requests").exists())
        self.assertFalse((self.root / ".local/share").exists())

    def install_with_terminal(self, *arguments):
        # A real controlling terminal proves curl | sh cannot consume the guide's answers.
        command = 'cat ' + shlex.quote(str(SCRIPT)) + ' | sh -s -- ' + shlex.join([
            '--install-dir', str(self.destination), *arguments])
        pid, terminal = pty.fork()
        if pid == 0:
            os.execvpe('sh', ['sh', '-c', command], self.env)
        output = bytearray()
        answered = False
        status = None
        try:
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                readable, _, _ = select.select([terminal], [], [], 0.1)
                if readable:
                    try:
                        chunk = os.read(terminal, 65536)
                    except OSError:
                        break
                    if not chunk:
                        break
                    output.extend(chunk)
                    if b'Workspace: ' in output and not answered:
                        os.write(terminal, b'/tmp/my workspace\n')
                        answered = True
                waited, status = os.waitpid(pid, os.WNOHANG)
                if waited:
                    break
                status = None
            if status is None:
                waited, status = os.waitpid(pid, os.WNOHANG)
                if not waited:
                    os.kill(pid, signal.SIGKILL)
                    _, status = os.waitpid(pid, 0)
                    self.fail('installer did not return: ' + output.decode(errors='replace'))
        finally:
            os.close(terminal)
        self.assertFalse(list(self.destination.glob('.readyrig-install.*')))
        return os.waitstatus_to_exitcode(status), output.decode(errors='replace')

    def test_piped_installer_opens_guide_on_controlling_terminal(self):
        self.checksums()
        code, output = self.install_with_terminal()
        self.assertEqual(code, 0, output)
        self.assertIn('Opening the ReadyRig configuration guide', output)
        self.assertEqual((self.root / 'setup-invocations').read_text(), 'setup --if-needed\n')
        self.assertEqual((self.root / 'setup-answer').read_text(), '/tmp/my workspace\n')

    def test_no_setup_flag_and_environment_skip_guide_even_with_terminal(self):
        self.checksums()
        code, output = self.install_with_terminal('--no-setup')
        self.assertEqual(code, 0, output)
        self.assertFalse((self.root / 'setup-invocations').exists())
        self.env['READYRIG_NO_SETUP'] = '1'
        code, output = self.install_with_terminal()
        self.assertEqual(code, 0, output)
        self.assertFalse((self.root / 'setup-invocations').exists())

    def test_setup_failure_keeps_installed_binary_and_prints_resume_command(self):
        self.checksums()
        self.env['MOCK_SETUP_FAIL'] = '1'
        code, output = self.install_with_terminal()
        self.assertNotEqual(code, 0, output)
        self.assertEqual(self.installed.read_bytes(), BINARY)
        self.assertIn('Resume configuration with:', output)

    def test_noninteractive_install_prints_guide_command(self):
        self.checksums()
        result = self.install(pipe=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('No interactive terminal detected', result.stdout)
        self.assertIn('readyrig" setup', result.stdout)
        self.assertFalse((self.root / 'setup-invocations').exists())


if __name__ == "__main__":
    unittest.main()
