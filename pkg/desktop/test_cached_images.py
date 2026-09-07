import importlib.util
from pathlib import Path
import sqlite3
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('cached_images', Path(__file__).with_name('cached_images.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class CacheTests(unittest.TestCase):
    def test_exact_lookup_external_files_and_guards(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            fs = base / 'fsCachedData'
            fs.mkdir()
            png = b'\x89PNG\r\n\x1a\nexample'
            (fs / 'valid').write_bytes(png)
            (fs / 'linked').symlink_to(fs / 'valid')
            db = sqlite3.connect(base / 'Cache.db')
            db.executescript('CREATE TABLE cfurl_cache_response(entry_ID,request_key,time_stamp); CREATE TABLE cfurl_cache_receiver_data(entry_ID,isDataOnFS,receiver_data);')
            values = [('https://example.test/a?x=1&y=2',0,png),('https://example.test/b',1,'valid'),('https://example.test/c',1,'../valid'),('https://example.test/d',1,'linked'),('https://example.test/e',0,b'<svg/>'),('https://example.test/unrelated',0,png)]
            for index, (url, external, value) in enumerate(values):
                db.execute('INSERT INTO cfurl_cache_response VALUES(?,?,?)',(index,url,index))
                db.execute('INSERT INTO cfurl_cache_receiver_data VALUES(?,?,?)',(index,external,value))
            db.commit()
            db.close()
            source = '<img src="https://example.test/a?x=1&amp;y=2">' + ''.join('<img src="https://example.test/'+key+'">' for key in 'bcde')
            found = module.cached_images(source,base)
            self.assertEqual(set(found),{'https://example.test/a?x=1&y=2','https://example.test/b'})
            self.assertEqual(module.cached_images('<img src="file:///secret">',base),{})


if __name__ == '__main__':
    unittest.main()
