#!/usr/bin/env bash
set -euo pipefail

project="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../gui/electron" && pwd -P)"
exec npm --prefix "$project" run package -- "$@"
