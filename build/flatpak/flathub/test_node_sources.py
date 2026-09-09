"""Regression checks for npm cache generation; no network or npm needed."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest

HERE = Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name, HERE / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


generator = load('gen-node-sources')
validator = load('validate-node-sources')


class NodeSourcesTests(unittest.TestCase):
    def setUp(self):
        self.lock = json.loads((HERE.parents[2] / 'frontend/package-lock.json').read_text())
        self.sources = json.loads((HERE / 'node-sources.json').read_text())

    def test_all_locked_packages_are_represented(self):
        validator.check_sources(self.lock, self.sources)

    def test_missing_tarball_fails(self):
        sources = copy.deepcopy(self.sources)
        sources.pop(next(i for i, s in enumerate(sources) if s['type'] == 'file'))
        with self.assertRaisesRegex(ValueError, 'Missing vendored dependency'):
            validator.check_sources(self.lock, sources)

    def test_wrong_integrity_fails(self):
        sources = copy.deepcopy(self.sources)
        source = next(s for s in sources if s['type'] == 'file')
        source['sha512'] = '00'
        with self.assertRaisesRegex(ValueError, 'Incorrect vendored dependency'):
            validator.check_sources(self.lock, sources)

    def test_invalid_index_checksum_fails(self):
        sources = copy.deepcopy(self.sources)
        source = next(s for s in sources if s['type'] == 'inline')
        source['contents'] = source['contents'].replace('request-cache:', 'broken-cache:')
        with self.assertRaisesRegex(ValueError, 'Invalid npm cache index checksum'):
            validator.check_sources(self.lock, sources)

    def test_generator_hashes_record_not_key(self):
        import hashlib
        source = generator.make_cache_index_entry('https://example.test/a.tgz', 'sha512-AA==')
        checksum, record = source['contents'].strip().split('\t', 1)
        self.assertEqual(checksum, hashlib.sha1(record.encode()).hexdigest())
        self.assertEqual(json.loads(record)['integrity'], 'sha512-AA==')


if __name__ == '__main__':
    unittest.main()
