$ErrorActionPreference = "Stop"

$project = Join-Path $PSScriptRoot "../gui/electron"
& npm.cmd --prefix $project run package -- @args
exit $LASTEXITCODE
