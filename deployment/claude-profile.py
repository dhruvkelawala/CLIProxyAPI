#!/usr/bin/env python3
"""Launch Claude with a dedicated gateway key and configuration directory."""
import os
from pathlib import Path
import sys


def launch(profile, binary, client_root, config_root, arguments):
    key_path = client_root / profile / 'client-key'
    if key_path.stat().st_mode & 0o077:
        raise SystemExit('Client key must be owner-only')
    key = key_path.read_text().strip()
    if not key:
        raise SystemExit('Client key is empty')
    os.environ['PATH'] = str(binary.parent) + os.pathsep + os.environ.get('PATH', '')
    os.environ['CLAUDE_CONFIG_DIR'] = str(config_root)
    os.environ['ANTHROPIC_BASE_URL'] = 'http://127.0.0.1:8317'
    os.environ['ANTHROPIC_AUTH_TOKEN'] = key
    os.environ['ANTHROPIC_API_KEY'] = ''
    for name in ('CLAUDE_CODE_OAUTH_TOKEN', 'CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR',
                 'CLAUDE_CODE_USE_BEDROCK', 'CLAUDE_CODE_USE_VERTEX', 'CLAUDE_CODE_USE_FOUNDRY'):
        os.environ.pop(name, None)
    os.environ['CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY'] = '1'
    os.execv(str(binary), ['claude', *arguments])


if __name__ == '__main__':
    if len(sys.argv) < 5 or sys.argv[1] not in ('a', 'b', 'automatic'):
        raise SystemExit('Usage: claude-profile.py a|b|automatic BINARY CLIENT_ROOT CONFIG_ROOT [Claude arguments]')
    launch(sys.argv[1], Path(sys.argv[2]), Path(sys.argv[3]), Path(sys.argv[4]), sys.argv[5:])
