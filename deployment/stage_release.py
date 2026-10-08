#!/usr/bin/env python3
"""Check packaged artifacts on loopback with disposable accounts and client keys."""
import argparse
import hashlib
import http.server
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request
import uuid


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


class EgressTrap(http.server.BaseHTTPRequestHandler):
    attempts = 0

    def do_CONNECT(self):
        type(self).attempts += 1
        self.send_error(502, 'Staging blocks upstream inference')

    def log_message(self, *args):
        pass


def interrupt_stage(*_):
    raise KeyboardInterrupt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--release', type=Path, required=True)
    parser.add_argument('--rollback-binary', type=Path)
    parser.add_argument('--rollback-panel', type=Path)
    parser.add_argument('--keep-running', action='store_true')
    args = parser.parse_args()
    release = args.release.resolve()
    manifest = json.loads((release / 'release-manifest.json').read_text())
    for name, item in manifest['files'].items():
        if sha(release / name) != item['sha256']:
            raise SystemExit('Artifact digest mismatch: ' + name)
    root = Path(tempfile.mkdtemp(prefix='cpa-stage-'))
    root.chmod(0o700)
    print(json.dumps({'stage_root': str(root), 'release': str(release)}), flush=True)
    authdir = root / 'auths'
    authdir.mkdir()
    static = root / 'static'
    static.mkdir()
    panel = static / 'management.html'
    shutil.copyfile(release / 'management.html', panel)
    trap = http.server.ThreadingHTTPServer(('127.0.0.1', 0), EgressTrap)
    threading.Thread(target=trap.serve_forever, daemon=True).start()
    port = free_port()
    profiles, associations, keys = [], [], []
    accounts = {name: str(uuid.uuid4()) for name in ('a', 'b')}
    for name in ('a', 'b'):
        (authdir / (name + '.json')).write_text(json.dumps({'type': 'claude', 'email': name + '@example.invalid', 'account_ref': accounts[name], 'disabled': name == 'a'}))
    for name in ('a', 'b', 'automatic'):
        key = 'synthetic-' + name
        ref = str(uuid.uuid4())
        keys.append(key)
        policy = {'mode': 'automatic'} if name == 'automatic' else {'mode': 'only', 'account_ref': accounts[name]}
        profiles.append({'profile_ref': ref, 'label': 'Staging ' + name, 'revision': 1, 'policies': {'claude': policy, 'codex': {'mode': 'automatic'}}})
        associations.append({'key_ref': str(uuid.uuid4()), 'label': name, 'profile_ref': ref, 'revision': 1, 'fingerprint': hashlib.sha256(key.encode()).hexdigest()})
    keys.append('synthetic-legacy')
    cfg = {'config-version': 8, 'server': {'host': '127.0.0.1', 'port': port},
           'management': {'secret-key': 'synthetic-management', 'disable-auto-update-panel': True},
           'access': {'api-keys': keys, 'client-profiles': profiles, 'client-profile-keys': associations},
           'oauth': {'auth-dir': str(authdir), 'providers': {'aistudio': {'ws-auth': True}}},
           'routing': {'strategy': 'fill-first', 'session-affinity': True, 'retry': {'request-retry': 0, 'max-retry-credentials': 1}},
           'requests': {'proxy-url': 'http://127.0.0.1:' + str(trap.server_port)},
           'observability': {'logs': {'request-log': False}}, 'plugins': {'enabled': False}}
    config = root / 'config.yaml'
    original_config = json.dumps(cfg)
    config.write_text(original_config)
    env = {k: v for k, v in os.environ.items() if not k.startswith(('PGSTORE_', 'GITSTORE_', 'OBJECTSTORE_'))}
    env['MANAGEMENT_STATIC_PATH'] = str(static)
    env.pop('MANAGEMENT_PASSWORD', None)
    env.pop('WRITABLE_PATH', None)
    env.pop('writable_path', None)
    process = None
    log = (root / 'server.log').open('a')
    base = 'http://127.0.0.1:' + str(port)

    def request(path, key=None, body=None):
        headers = {'Authorization': 'Bearer ' + key} if key else {}
        if body is not None:
            headers['Content-Type'] = 'application/json'
        req = urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=8) as response:
                return response.status, response.read(), dict(response.headers)
        except urllib.error.HTTPError as response:
            return response.code, response.read(), dict(response.headers)

    def stop():
        nonlocal process
        if process is not None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
            process = None

    def start(binary):
        nonlocal process
        process = subprocess.Popen([str(binary.resolve()), '--config', str(config), '--local-model'], cwd=root, env=env, stdout=log, stderr=log)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise RuntimeError('Staging gateway exited, inspect ' + str(root / 'server.log'))
            try:
                status, raw, _ = request('/v1/models', 'synthetic-legacy')
                if status == 200 and any('sonnet' in m['id'] for m in json.loads(raw).get('data', [])):
                    return
            except OSError:
                pass
            time.sleep(0.1)
        raise RuntimeError('Staging gateway did not start')

    def matrix():
        models = json.loads(request('/v1/models', 'synthetic-legacy')[1])['data']
        model = next(m['id'] for m in models if 'claude' in m['id'] and 'sonnet' in m['id'])
        body = {'model': model, 'max_tokens': 8, 'messages': [{'role': 'user', 'content': 'synthetic staging check'}]}
        for path, stream in [('/v1/messages', False), ('/v1/messages', True), ('/v1/messages/count_tokens', False)]:
            payload = dict(body, stream=stream) if path.endswith('messages') else body
            before = EgressTrap.attempts
            status, raw, _ = request(path, 'synthetic-a', payload)
            parsed = json.loads(raw)
            assert status == 503 and parsed['error']['code'] == 'target_unavailable' and parsed['error']['field'] == 'claude', (status, parsed)
            assert EgressTrap.attempts == before, 'Disabled strict A attempted an upstream connection'
            print(json.dumps({'path': path, 'stream': stream, 'client': 'Only A', 'status': status, 'code': parsed['error']['code'], 'upstream_attempts': 0}), flush=True)
        for name in ('b', 'automatic', 'legacy'):
            status, raw, _ = request('/v1/messages/count_tokens', 'synthetic-' + name, body)
            assert status == 200 and json.loads(raw)['input_tokens'] > 0, (name, status, raw)
            print(json.dumps({'client': name, 'operation': 'local token-count', 'status': status, 'vendor_inference': False}), flush=True)
        status, raw, headers = request('/v8/management/client-profiles/capabilities', 'synthetic-management')
        assert status == 200 and json.loads(raw)['enforcement'] is True
        status, raw, _ = request('/management.html')
        assert status == 200 and hashlib.sha256(raw).hexdigest() == manifest['files']['management.html']['sha256']
        print(json.dumps({'panel_sha256': hashlib.sha256(raw).hexdigest(), 'enforcement': True}), flush=True)

    signal.signal(signal.SIGTERM, interrupt_stage)
    try:
        start(release / 'cli-proxy-api')
        matrix()
        if args.rollback_binary or args.rollback_panel:
            if not (args.rollback_binary and args.rollback_panel):
                raise ValueError('Supply both rollback artifacts')
            stop()
            saved_config = config.read_bytes()
            saved_auths = {p: p.read_bytes() for p in authdir.glob('*.json')}
            legacy_cfg = dict(cfg, access={'api-keys': keys})
            config.write_text(json.dumps(legacy_cfg))
            shutil.copyfile(args.rollback_panel, panel)
            start(args.rollback_binary)
            body = {'model': json.loads(request('/v1/models', 'synthetic-legacy')[1])['data'][0]['id'], 'messages': [{'role': 'user', 'content': 'synthetic rollback check'}]}
            assert request('/v1/messages/count_tokens', 'synthetic-legacy', body)[0] == 200
            assert hashlib.sha256(request('/management.html')[1]).hexdigest() == sha(args.rollback_panel)
            stop()
            config.write_bytes(saved_config)
            for p, content in saved_auths.items():
                p.write_bytes(content)
            shutil.copyfile(release / 'management.html', panel)
            start(release / 'cli-proxy-api')
            matrix()
            print(json.dumps({'rollback': 'passed', 'configuration_restored': True, 'panel_restored': True}), flush=True)
        info = {'stage_root': str(root), 'port': port, 'pid': process.pid, 'backend_commit': manifest['backend']['commit'], 'panel_commit': manifest['panel']['commit']}
        (root / 'stage-info.json').write_text(json.dumps(info))
        print(json.dumps(info), flush=True)
        if args.keep_running:
            signal.pause()
    except KeyboardInterrupt:
        pass
    finally:
        stop()
        trap.shutdown()
        log.close()


if __name__ == '__main__':
    main()
