"""Exercise the actual inline aggregate gate with Actions' Bash options."""
import os
import pathlib
import subprocess
import textwrap
import unittest

WORKFLOW = pathlib.Path(__file__).resolve().parents[2] / '.github/workflows/ci.yml'


class GateTests(unittest.TestCase):
    def test_required_status_names_stay_compatible(self):
        workflow = WORKFLOW.read_text()
        for name in ['Version consistency', 'Lint', 'Type check', 'Test']:
            with self.subTest(name=name):
                self.assertIn(f'    name: {name}\n', workflow)
        self.assertIn('    if: always()\n', workflow)
        self.assertNotIn('    paths:', workflow)
        self.assertNotIn('    paths-ignore:', workflow)

    def test_actual_gate_fails_closed(self):
        # The gate is the final run block; execute that shell, not a Python
        # reimplementation, to catch set -e and skipped-job edge cases.
        shell = textwrap.dedent(WORKFLOW.read_text().rsplit('        run: |\n', 1)[1])
        cases = [
            ('success', 'false', 'skipped', 'false', 'skipped', True),
            ('success', 'true', 'success', 'false', 'skipped', True),
            ('success', 'false', 'skipped', 'true', 'success', True),
            ('success', 'true', 'success', 'true', 'success', True),
            ('success', 'true', 'failure', 'true', 'success', False),
            ('success', 'true', 'success', 'true', 'failure', False),
            ('success', 'true', 'skipped', 'false', 'skipped', False),
            ('success', 'false', 'skipped', 'true', 'cancelled', False),
            ('failure', '', 'skipped', '', 'skipped', False),
            ('cancelled', 'false', 'skipped', 'false', 'skipped', False),
            ('success', '', 'skipped', '', 'skipped', False),
        ]
        keys = ['VERSION_RESULT', 'FRONTEND_REQUIRED', 'FRONTEND_RESULT',
                'BACKEND_REQUIRED', 'BACKEND_RESULT']
        # A failed native bridge compile cannot hide behind a passing Linux job.
        cases.append(('success', 'true', 'success', 'true', 'success', False))
        for *values, expected in cases:
            with self.subTest(values=values):
                result = subprocess.run(
                    ['bash', '--noprofile', '--norc', '-e', '-o', 'pipefail', '-c', shell],
                    env={**os.environ, **dict(zip(keys, values)), 'NATIVE_MENU_RESULT': 'failure' if tuple(values) == ('success', 'true', 'success', 'true', 'success') and not expected else ('success' if values[3] == 'true' else 'skipped')}, capture_output=True,
                )
                self.assertEqual(result.returncode == 0, expected, result.stderr)


if __name__ == '__main__':
    unittest.main()
