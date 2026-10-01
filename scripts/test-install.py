#!/usr/bin/env python3
"""Exercise the curl installer with local download fixtures, never the network."""
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "website/public/install.sh"
BINARY = b"#!/bin/sh\nprintf 'ReadyRig fixture: config set\\n'\n"


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
        for name in ["READYRIG_VERSION", "READYRIG_INSTALL_DIR", "READYRIG_INSTALL_REPO"]:
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

    def install(self, *arguments, pipe=False):
        options = ["--install-dir", str(self.destination), *arguments]
        if pipe:
            result = subprocess.run(["sh", "-s", "--", *options], input=SCRIPT.read_text(), text=True,
                                    env=self.env, capture_output=True, timeout=15)
        else:
            result = subprocess.run(["sh", str(SCRIPT), *options], text=True, env=self.env,
                                    capture_output=True, timeout=15)
        self.assertFalse(list(self.destination.glob(".readyrig-install.*")), result.stderr)
        return result

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


if __name__ == "__main__":
    unittest.main()
