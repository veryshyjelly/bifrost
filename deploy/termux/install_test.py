import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('phone_install', Path(__file__).with_name('install.py'))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class InstallationTest(unittest.TestCase):
    def test_install_and_repeat_preserve_private_state_and_service_choice(self):
        with tempfile.TemporaryDirectory() as folder:
            home = Path(folder)
            root = installer.install(home)
            state = root/'state/runtime.env.json'
            proof = root/'state/CUTOVER-VERIFIED.json'
            state.write_text('private configuration sentinel')
            proof.write_text('verified activation sentinel')
            down = root/'services/bifrost/down'
            self.assertTrue(down.exists())
            down.unlink()
            installer.install(home)
            self.assertFalse(down.exists())
            self.assertEqual(state.read_text(), 'private configuration sentinel')
            self.assertEqual(proof.read_text(), 'verified activation sentinel')
            lines = (home/'.termux/boot/20-bifrost').read_text().splitlines()
            self.assertIn('termux-wake-lock', lines[1])
            self.assertEqual((root/'phone-runtime.py').stat().st_mode & 0o777, 0o700)

    def test_refuses_symlink_root(self):
        with tempfile.TemporaryDirectory() as folder:
            home = Path(folder)
            target = home/'elsewhere'
            target.mkdir()
            (home/'bifrost-phone').symlink_to(target, target_is_directory=True)
            with self.assertRaises(RuntimeError):
                installer.install(home)


if __name__ == '__main__':
    unittest.main()
