import hashlib
import json
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import tempfile
import unittest


class StagingLifecycleTests(unittest.TestCase):
    def test_termination_during_startup_stops_gateway(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fifo = root / 'child-ready'
            os.mkfifo(fifo)
            descriptor = os.open(fifo, os.O_RDONLY | os.O_NONBLOCK)
            binary = root / 'cli-proxy-api'
            binary.write_text('#!' + sys.executable + '\nimport os,signal\n'
                              'with open(os.environ["CPA_STAGE_TEST_FIFO"], "w") as f:\n'
                              ' f.write(str(os.getpid())); f.flush()\n'
                              'signal.pause()\n')
            binary.chmod(0o700)
            panel = root / 'management.html'
            panel.write_text('synthetic panel')
            manifest = {'files': {p.name: {'sha256': hashlib.sha256(p.read_bytes()).hexdigest()}
                                  for p in (binary, panel)}}
            (root / 'release-manifest.json').write_text(json.dumps(manifest))
            process = subprocess.Popen([sys.executable, str(Path(__file__).with_name('stage_release.py')),
                                        '--release', str(root)], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       env=dict(os.environ, CPA_STAGE_TEST_FIFO=str(fifo)))
            child = None
            try:
                self.assertTrue(select.select([descriptor], [], [], 10)[0], 'Gateway did not start')
                child = int(os.read(descriptor, 100))
                process.terminate()
                _, stderr = process.communicate(timeout=12)
                self.assertEqual(process.returncode, 0, stderr.decode())
                with self.assertRaises(ProcessLookupError):
                    os.kill(child, 0)
            finally:
                os.close(descriptor)
                if process.poll() is None:
                    process.kill()
                    process.communicate()
                if child is not None:
                    try:
                        os.kill(child, signal.SIGTERM)
                    except ProcessLookupError:
                        pass
