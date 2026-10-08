#!/usr/bin/env python3
"""Build pinned Darwin arm64 gateway and single-file panel artifacts."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile


def run(arguments, cwd, env=None):
    subprocess.run(arguments, cwd=cwd, env=env, check=True)


def git(root, *arguments):
    return subprocess.check_output(['git', *arguments], cwd=root, text=True).strip()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backend', type=Path, required=True)
    parser.add_argument('--panel', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--backend-tag', required=True)
    parser.add_argument('--panel-tag', required=True)
    args = parser.parse_args()
    for root in (args.backend, args.panel):
        if git(root, 'status', '--porcelain'):
            raise SystemExit('Source checkout must be clean: ' + str(root))
    for tag in (args.backend_tag, args.panel_tag):
        if not tag.startswith('sumo-v') or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-' for c in tag):
            raise SystemExit('Use a fork-specific sumo-v tag')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    backend_commit = git(args.backend, 'rev-parse', 'HEAD')
    panel_commit = git(args.panel, 'rev-parse', 'HEAD')
    epoch = int(git(args.backend, 'show', '-s', '--format=%ct', 'HEAD'))
    env = dict(os.environ, GOOS='darwin', GOARCH='arm64', CGO_ENABLED='1')
    binary = output / 'cli-proxy-api'
    flags = '-s -w -X main.Version=' + args.backend_tag + ' -X main.Commit=' + backend_commit
    run(['go', 'build', '-trimpath', '-ldflags=' + flags, '-o', str(binary), './cmd/server'], args.backend, env)
    run(['bunx', 'bun@1.3.14', 'run', 'build'], args.panel, dict(os.environ, VERSION=args.panel_tag))
    panel = output / 'management.html'
    shutil.copyfile(args.panel / 'dist/index.html', panel)
    panel_license = output / 'panel-LICENSE.txt'
    shutil.copyfile(args.panel / 'LICENSE', panel_license)
    archive = output / ('CLIProxyAPI_' + args.backend_tag + '_darwin_aarch64.tar.gz')
    with archive.open('wb') as raw, gzip.GzipFile(fileobj=raw, mode='wb', mtime=epoch, filename='') as zipped, tarfile.open(fileobj=zipped, mode='w') as tar:
        for source, name in [(binary, 'cli-proxy-api'), *[(args.backend / n, n) for n in ('LICENSE', 'README.md', 'README_CN.md', 'config.example.yaml')]]:
            info = tar.gettarinfo(str(source), arcname=name)
            info.mtime, info.uid, info.gid, info.uname, info.gname = epoch, 0, 0, '', ''
            info.mode = 0o755 if name == 'cli-proxy-api' else 0o644
            with source.open('rb') as content:
                tar.addfile(info, content)
    manifest = {
        'schema_version': 1, 'platform': 'darwin/arm64',
        'backend': {'repository': 'dhruvkelawala/CLIProxyAPI', 'commit': backend_commit, 'tag': args.backend_tag, 'upstream_base': 'v8.0.15'},
        'panel': {'repository': 'dhruvkelawala/Cli-Proxy-API-Management-Center', 'commit': panel_commit, 'tag': args.panel_tag, 'upstream_base': 'v1.25.4'},
        'builder': {'go': subprocess.check_output(['go', 'version'], text=True).strip(), 'bun': '1.3.14', 'cgo': True, 'model_catalog': 'committed source, no updater during build'},
        'files': {p.name: {'sha256': digest(p), 'bytes': p.stat().st_size} for p in (binary, panel, panel_license, archive)}
    }
    (output / 'release-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    with (output / 'SHA256SUMS').open('w') as sums:
        for p in (binary, panel, panel_license, archive, output / 'release-manifest.json'):
            sums.write(digest(p) + '  ' + p.name + '\n')
    print(json.dumps(manifest, indent=2))


if __name__ == '__main__':
    main()
