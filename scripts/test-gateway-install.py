#!/usr/bin/env python3
"""Test macOS installation and an unpacked gateway runtime without SII login."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[1]


class GatewayInstallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="sii-install-", dir="/tmp")
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        self.env = dict(os.environ, HOME=str(self.home), SII_LINK_INSTALL_DIR=str(self.home / ".local/bin"))

    def installer(self, managed):
        asset = "sii-link_darwin_arm64.zip"
        stage = self.home / "source"
        stage.mkdir()
        candidate = stage / "sii-link"
        candidate.write_text("#!/bin/sh\necho '" + ("Gateway protocol: 1" if managed else "Legacy binary") + "'\n")
        candidate.chmod(0o755)
        archive = self.home / asset
        with zipfile.ZipFile(archive, "w") as z:
            z.write(candidate, "sii-link_darwin_arm64/sii-link")
            if managed:
                z.write(ROOT / "scripts/sii", "sii-link_darwin_arm64/sii")
        checksum = self.home / (asset + ".sha256")
        checksum.write_text(hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + asset + "\n")
        fake = self.home / "fake-bin"
        fake.mkdir()
        for name, contents in {
            "uname": "#!/bin/sh\necho arm64\n",
            "curl": '''#!/bin/sh
while [ "$#" -gt 0 ]; do
    if [ "$1" = -o ]; then
        destination="$2"
        shift 2
    else shift; fi
done
cp "$SII_TEST_ASSETS/$(basename "$destination")" "$destination"
''',
        }.items():
            path = fake / name
            path.write_text(contents)
            path.chmod(0o755)
        env = dict(self.env, PATH=str(fake) + os.pathsep + os.environ["PATH"], SII_TEST_ASSETS=str(self.home))
        return subprocess.run(["sh", str(ROOT / "scripts/install-macos.sh")], env=env, capture_output=True, text=True)

    def test_old_release_cannot_replace_managed_installation(self):
        binary = self.home / ".local/bin/sii-link"
        binary.parent.mkdir(parents=True)
        binary.write_text("preserved binary")
        state = self.home / ".local/state/sii-link/gateway.json"
        state.parent.mkdir(parents=True)
        state.write_text('{"role":"client"}')
        result = self.installer(False)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(binary.read_text(), "preserved binary")
        self.assertEqual(json.loads(state.read_text())["role"], "client")

    def test_new_archive_installs_adjacent_wrapper(self):
        result = self.installer(True)
        self.assertEqual(result.returncode, 0, result.stderr)
        installed = self.home / ".local/bin/sii"
        self.assertTrue(os.access(installed, os.X_OK))
        self.assertIn("Gateway protocol: 1", subprocess.check_output([str(installed), "help"], text=True))
        self.assertFalse((self.home / ".local/state/sii-link/gateway.json").exists())

    def test_real_packaged_runtime_starts_off_and_holds_one_supervisor(self):
        archive = ROOT / "dist/gateway-check-darwin/sii-link_darwin_arm64.zip"
        self.assertTrue(archive.is_file(), "build the darwin-arm64 package before this smoke")
        with zipfile.ZipFile(archive) as z:
            z.extractall(self.home)
        installed = self.home / "sii-link_darwin_arm64"
        for name in ("sii", "sii-link"):
            (installed / name).chmod(0o755)
        sii = str(installed / "sii")
        self.assertIn("server", subprocess.check_output([sii, "help"], env=self.env, text=True))
        state_dir = self.home / "state"
        command = [sii, "run", "--state-dir", str(state_dir), "--config", str(self.home / "missing.toml")]
        worker = subprocess.Popen(command, env=self.env, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 8
            while not (state_dir / "gateway.sock").exists():
                self.assertIsNone(worker.poll(), "packaged controller exited")
                if time.monotonic() >= deadline:
                    self.fail("controller did not start")
                time.sleep(0.02)
            status = json.loads(subprocess.check_output([sii, "status", "--state-dir", str(state_dir), "--json"], env=self.env, text=True))
            self.assertEqual(status["role"], "off")
            self.assertTrue(status["supervisor_running"])
            self.assertFalse(status["provider_running"])
            duplicate = subprocess.run(command, env=self.env, capture_output=True, timeout=5)
            self.assertNotEqual(duplicate.returncode, 0)
            self.assertIn(b"already supervised", duplicate.stderr)
            subprocess.run([sii, "off", "--state-dir", str(state_dir)], env=self.env, check=True, stdout=subprocess.DEVNULL)
            self.assertEqual((state_dir / "gateway.json").stat().st_mode & 0o077, 0)
        finally:
            worker.terminate()
            worker.communicate(timeout=10)
        self.assertEqual(worker.returncode, 0)
        self.assertFalse((state_dir / "gateway.sock").exists())


if __name__ == "__main__":
    unittest.main()
