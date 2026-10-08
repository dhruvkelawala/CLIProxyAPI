import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

LAUNCHER = Path(__file__).with_name('claude-profile.py')


class ClientLauncherTests(unittest.TestCase):
    def test_inherited_credentials_cannot_override_selected_profile(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client_root = root / 'clients'
            binary = root / 'claude'
            binary.write_text('#!' + sys.executable + '\nimport os,json,sys\nprint(json.dumps({"env":dict(os.environ),"args":sys.argv[1:]}))\n')
            binary.chmod(0o700)
            for profile in ('a', 'b', 'automatic'):
                key_file = client_root / profile / 'client-key'
                key_file.parent.mkdir(parents=True)
                key_file.write_text('synthetic-' + profile)
                key_file.chmod(0o600)
                env = dict(os.environ, ANTHROPIC_AUTH_TOKEN='wrong-inherited', ANTHROPIC_API_KEY='wrong-inherited',
                           ANTHROPIC_BASE_URL='http://invalid', CLAUDE_CODE_OAUTH_TOKEN='wrong-oauth',
                           CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR='42', CLAUDE_CODE_USE_BEDROCK='1',
                           CLAUDE_CODE_USE_VERTEX='1', CLAUDE_CODE_USE_FOUNDRY='1')
                home = root / ('home-' + profile)
                output = subprocess.check_output([sys.executable, str(LAUNCHER), profile, str(binary),
                                                  str(client_root), str(home), '--model', 'claude-sonnet-5-5'], env=env)
                result = json.loads(output)
                self.assertEqual(result['env']['ANTHROPIC_AUTH_TOKEN'], 'synthetic-' + profile)
                self.assertEqual(result['env']['ANTHROPIC_API_KEY'], '')
                self.assertEqual(result['env']['ANTHROPIC_BASE_URL'], 'http://127.0.0.1:8317')
                self.assertEqual(result['env']['CLAUDE_CONFIG_DIR'], str(home))
                self.assertFalse(any(k in result['env'] for k in env if k.startswith('CLAUDE_CODE_USE_') or k.startswith('CLAUDE_CODE_OAUTH_')))
                self.assertEqual(result['args'], ['--model', 'claude-sonnet-5-5'])
            key_file.chmod(0o644)
            denied = subprocess.run([sys.executable, str(LAUNCHER), 'automatic', str(binary), str(client_root), str(home)], capture_output=True)
            self.assertNotEqual(denied.returncode, 0)
            self.assertIn(b'owner-only', denied.stderr)


if __name__ == '__main__':
    unittest.main()
