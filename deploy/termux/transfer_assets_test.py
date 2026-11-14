import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('asset_transfer', Path(__file__).with_name('transfer-assets.py'))
transfer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(transfer)


class TransferTest(unittest.TestCase):
    def test_roundtrip_and_refuse_existing_destination(self):
        with tempfile.TemporaryDirectory() as folder, contextlib.redirect_stdout(io.StringIO()):
            root = Path(folder)
            source = root/'source'
            source.mkdir()
            (source/'nested').mkdir()
            (source/'nested/content').write_bytes(b'frozen state')
            (source/'link').symlink_to('nested/content')
            output = root/'checkpoint'
            transfer.pack('openwa-data', source, output)
            archive, manifest = output/'openwa-data.tar.gz', output/'openwa-data.manifest.json'
            destination = root/'restored'
            transfer.restore('openwa-data', archive, manifest, destination)
            self.assertEqual((destination/'nested/content').read_bytes(), b'frozen state')
            self.assertEqual((destination/'link').readlink(), Path('nested/content'))
            with self.assertRaises(ValueError):
                transfer.restore('openwa-data', archive, manifest, destination)

    def test_paths_and_token_directory_are_role_scoped(self):
        token_directory = '123456:' + 'a'*32
        self.assertEqual(transfer.safe_name(token_directory+'/state', 'telegram-data'), token_directory+'/state')
        for name, role in [(token_directory+'/state', 'media'), ('../state', 'media'), ('/state', 'media'), ('C:/state', 'telegram-data'), ('file:stream', 'telegram-data')]:
            with self.assertRaises(ValueError):
                transfer.safe_name(name, role)

    def test_archive_digest_checked_before_restore(self):
        with tempfile.TemporaryDirectory() as folder, contextlib.redirect_stdout(io.StringIO()):
            root = Path(folder)
            source = root/'source'
            source.mkdir()
            (source/'content').write_bytes(b'frozen')
            output = root/'checkpoint'
            transfer.pack('media', source, output)
            archive, manifest = output/'media.tar.gz', output/'media.manifest.json'
            archive.write_bytes(archive.read_bytes()+b'damage')
            destination = root/'restored'
            with self.assertRaises(ValueError):
                transfer.restore('media', archive, manifest, destination)
            self.assertFalse(destination.exists())


if __name__ == '__main__':
    unittest.main()
