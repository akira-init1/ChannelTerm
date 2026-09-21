#!/usr/bin/env bash

# Build one immutable, tagged ChannelTerm release and write its checksums.

set -euo pipefail

readonly repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly version="${1:-}"

if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  printf 'usage: %s VERSION (for example 0.1.0 or 0.1.0-rc.1)\n' "$0" >&2
  exit 2
fi

if [[ -n "$(git -C "${repo_root}" status --porcelain --untracked-files=all)" ]]; then
  printf 'release requires a clean worktree\n' >&2
  exit 1
fi

readonly expected_tag="v${version}"
tag_commit="$(git -C "${repo_root}" rev-list -n 1 "refs/tags/${expected_tag}" 2>/dev/null || true)"
head_commit="$(git -C "${repo_root}" rev-parse HEAD)"
if [[ -z "${tag_commit}" || "${tag_commit}" != "${head_commit}" ]]; then
  printf 'HEAD must have exact tag %s\n' "${expected_tag}" >&2
  exit 1
fi
if [[ "$(git -C "${repo_root}" cat-file -t "refs/tags/${expected_tag}")" != "tag" ]]; then
  printf 'release tag %s must be annotated (a signed tag is recommended)\n' "${expected_tag}" >&2
  exit 1
fi

(
  cd -- "${repo_root}"
  go test ./...
  go vet ./...
  go test -race ./...
)

CHANNELTERM_VERSION="${version}" "${repo_root}/scripts/build.sh"

readonly host_arch="$(go env GOARCH)"
readonly host_binary="${repo_root}/dist/channelterm-linux-${host_arch}"
if [[ ! -x "${host_binary}" ]]; then
  printf 'cannot verify release version for unsupported Linux host architecture %s\n' "${host_arch}" >&2
  exit 1
fi
mapfile -t version_lines < <("${host_binary}" version)
readonly expected_commit="$(git -C "${repo_root}" rev-parse --short=12 HEAD)"
readonly expected_go="$(go env GOVERSION)"
if [[ "${#version_lines[@]}" -ne 5 ||
      "${version_lines[0]}" != "channelterm ${version}" ||
      "${version_lines[1]}" != "commit:   ${expected_commit}" ||
      ! "${version_lines[2]}" =~ ^built:[[:space:]]{4}[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ||
      "${version_lines[3]}" != "go:       ${expected_go}" ||
      "${version_lines[4]}" != "platform: linux/${host_arch}" ]]; then
  printf 'built binary has unexpected version metadata:\n' >&2
  printf '  %s\n' "${version_lines[@]}" >&2
  exit 1
fi

(
  cd -- "${repo_root}/dist"
  LC_ALL=C sha256sum channelterm-* | sort -k2 > SHA256SUMS
)

printf 'Release %s is ready in %s with SHA256SUMS.\n' "${expected_tag}" "${repo_root}/dist"
