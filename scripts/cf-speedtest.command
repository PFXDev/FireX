#!/bin/sh
set -eu
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
STATE_DIR=${CF_SPEEDTEST_HOME:-"$HOME/.config/cf-speedtest"}
if python3 -c 'import yaml' >/dev/null 2>&1; then
    exec python3 "$SCRIPT_DIR/cf-speedtest.py" "$@"
fi
RUNTIME="$STATE_DIR/venv"
if [ ! -x "$RUNTIME/bin/python" ]; then
    echo '准备独立 Python 运行环境…'
    python3 -m venv "$RUNTIME"
fi
if ! "$RUNTIME/bin/python" -c 'import yaml' >/dev/null 2>&1; then
    "$RUNTIME/bin/python" -m pip install --disable-pip-version-check -r "$SCRIPT_DIR/cf-speedtest-requirements.txt"
fi
exec "$RUNTIME/bin/python" "$SCRIPT_DIR/cf-speedtest.py" "$@"
