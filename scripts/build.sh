#!/usr/bin/env bash

# Build ChannelTerm for every supported desktop target from Linux.
#
# The project intentionally disables CGO so the Go toolchain can cross-compile
# all artifacts without a target-specific C compiler or SDK.

set -euo pipefail

readonly repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly dist_dir="${repo_root}/dist"

readonly -a targets=(
  "windows amd64 channelterm-windows-amd64.exe"
  "windows arm64 channelterm-windows-arm64.exe"
  "linux amd64 channelterm-linux-amd64"
  "linux arm64 channelterm-linux-arm64"
  "darwin amd64 channelterm-darwin-amd64"
  "darwin arm64 channelterm-darwin-arm64"
)

declare -A previous_hashes=()

require_command() {
  local command_name="$1"

  if ! command -v "${command_name}" >/dev/null 2>&1; then
    printf 'required command is not available: %s\n' "${command_name}" >&2
    exit 127
  fi
}

for required_command in go stat sha256sum awk; do
  require_command "${required_command}"
done

show_artifact_info() {
  local artifact_name="$1"
  local artifact_path="${dist_dir}/${artifact_name}"

  if [[ ! -f "${artifact_path}" ]]; then
    printf '  %s: not found\n' "${artifact_name}"
    return
  fi

  local size modified sha256
  size="$(stat --format='%s' -- "${artifact_path}")"
  modified="$(stat --format='%y' -- "${artifact_path}")"
  sha256="$(sha256sum -- "${artifact_path}" | awk '{print $1}')"
  printf '  %s: %s bytes | %s | SHA256 %s\n' "${artifact_name}" "${size}" "${modified}" "${sha256}"
}

printf 'Previous artifacts:\n'
for target in "${targets[@]}"; do
  read -r _ _ artifact_name <<< "${target}"
  artifact_path="${dist_dir}/${artifact_name}"
  if [[ -f "${artifact_path}" ]]; then
    previous_hashes["${artifact_name}"]="$(sha256sum -- "${artifact_path}" | awk '{print $1}')"
  fi
  show_artifact_info "${artifact_name}"
done

printf 'Clearing previous dist directory...\n'
rm -rf -- "${dist_dir}"
mkdir -p -- "${dist_dir}"

cd -- "${repo_root}"
for target in "${targets[@]}"; do
  read -r goos goarch artifact_name <<< "${target}"
  printf 'Building %s %s...\n' "${goos}" "${goarch}"
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -o "${dist_dir}/${artifact_name}" ./cmd/channelterm
done

printf 'New artifacts:\n'
for target in "${targets[@]}"; do
  read -r _ _ artifact_name <<< "${target}"
  artifact_path="${dist_dir}/${artifact_name}"
  if [[ ! -f "${artifact_path}" ]]; then
    printf 'expected build artifact was not created: %s\n' "${artifact_name}" >&2
    exit 1
  fi

  show_artifact_info "${artifact_name}"
  current_hash="$(sha256sum -- "${artifact_path}" | awk '{print $1}')"
  if [[ -v "previous_hashes[${artifact_name}]" ]]; then
    if [[ "${previous_hashes[${artifact_name}]}" == "${current_hash}" ]]; then
      printf '    Rebuilt; content is unchanged.\n'
    else
      printf '    Updated.\n'
    fi
  else
    printf '    Created.\n'
  fi
done

printf 'Build complete: fresh artifacts are available in %s.\n' "${dist_dir}"
