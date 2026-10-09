#!/usr/bin/env bash
# Run opt-in OpenSSH/PTY tests against an isolated, temporary localhost server.
# Requires Linux, Go, Python 3, OpenSSH server/client tools, Vim, and htop.
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
sshd_binary="${CHANNELTERM_TEST_SSHD:-$(command -v sshd || true)}"
htop_binary="${CHANNELTERM_TEST_HTOP:-$(command -v htop || true)}"
if [[ "$(uname -s)" != Linux || "$(id -u)" == 0 ]]; then
  printf 'Run this localhost test as a regular user on Linux or WSL2.\n' >&2
  exit 1
fi
for required in go python3 ssh-keygen vim; do
  command -v "${required}" >/dev/null || { printf 'Missing prerequisite: %s\n' "${required}" >&2; exit 1; }
done
if [[ ! -x "${sshd_binary}" || ! -x "${htop_binary}" ]]; then
  printf 'Install OpenSSH server and htop, or set CHANNELTERM_TEST_SSHD and CHANNELTERM_TEST_HTOP to their executable paths.\n' >&2
  exit 1
fi

ssh_test_dir="$(mktemp -d -t channelterm-openssh.XXXXXXXX)"
ssh_test_pid=""
cleanup() {
  if [[ -n "${ssh_test_pid}" ]]; then
    kill "${ssh_test_pid}" 2>/dev/null || true
    wait "${ssh_test_pid}" 2>/dev/null || true
  fi
  rm -rf -- "${ssh_test_dir}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

ssh-keygen -q -t ed25519 -N '' -f "${ssh_test_dir}/host_key"
ssh-keygen -q -t ed25519 -N '' -f "${ssh_test_dir}/client_key"
python3 - "${ssh_test_dir}" "${htop_binary}" <<'PY'
from pathlib import Path
import json, os, pwd, shlex, socket, sys
root = Path(sys.argv[1])
user = pwd.getpwuid(os.getuid()).pw_name
with socket.socket() as listener:
    listener.bind(('127.0.0.1', 0))
    port = listener.getsockname()[1]
(root / 'known_hosts').write_text(f'[127.0.0.1]:{port} ' + (root / 'host_key.pub').read_text())
# A clean shell avoids changing history or running personal interactive setup.
# LD_LIBRARY_PATH supports executables extracted into a private test directory.
shell = shlex.join(['/usr/bin/env', 'HISTFILE=/dev/null', 'PATH=/usr/bin:/bin',
                    'LD_LIBRARY_PATH=' + os.environ.get('LD_LIBRARY_PATH', ''),
                    '/bin/bash', '--noprofile', '--norc', '-i'])
(root / 'sshd_config').write_text(f'''Port {port}
ListenAddress 127.0.0.1
HostKey {root}/host_key
PidFile {root}/sshd.pid
AuthorizedKeysFile {root}/client_key.pub
AllowUsers {user}
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
PermitRootLogin no
AllowTcpForwarding no
AllowAgentForwarding no
X11Forwarding no
PermitUserRC no
PrintMotd no
PrintLastLog no
StrictModes no
ForceCommand {shell}
LogLevel VERBOSE
''')
(root / 'config.json').write_text(json.dumps({
    'user': user, 'host': '127.0.0.1', 'port': port,
    'identity': str(root / 'client_key'), 'known_hosts': str(root / 'known_hosts'),
    'htop': str(Path(sys.argv[2]).resolve()),
}))
PY
"${sshd_binary}" -t -f "${ssh_test_dir}/sshd_config"
"${sshd_binary}" -D -e -f "${ssh_test_dir}/sshd_config" >"${ssh_test_dir}/sshd.log" 2>&1 &
ssh_test_pid=$!
# Wait for the owned listener. A port collision or startup failure must not be
# mistaken for a successful test against another user's SSH service.
ssh_test_ready=false
for attempt in {1..100}; do
  if ! kill -0 "${ssh_test_pid}" 2>/dev/null; then
    cat "${ssh_test_dir}/sshd.log" >&2
    exit 1
  fi
  if python3 - "${ssh_test_dir}/sshd.log" <<'PY'
from pathlib import Path
import sys
sys.exit(0 if 'Server listening on 127.0.0.1' in Path(sys.argv[1]).read_text() else 1)
PY
  then
    ssh_test_ready=true
    break
  fi
  sleep 0.05
done

if [[ "${ssh_test_ready}" != true ]]; then
  cat "${ssh_test_dir}/sshd.log" >&2
  printf 'Timed out waiting for the owned SSH listener.\n' >&2
  exit 1
fi

cd -- "${repo_root}"
printf 'Testing real localhost OpenSSH, Linux PTYs, Vim and htop (including race detection).\n'
if CHANNELTERM_TEST_OPENSSH_CONFIG="${ssh_test_dir}/config.json" \
  go test -race ./internal/cli/command -run '^TestOpenSSH' -count=1 -v; then
  printf 'Local integration checks passed. Review the logged unsupported-resize limitation.\n'
else
  cat "${ssh_test_dir}/sshd.log" >&2
  exit 1
fi
