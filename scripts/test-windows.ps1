# Run native Go tests from stable executable paths, avoiding a fresh firewall
# application identity for every go-build temporary directory.
[CmdletBinding()]
param(
    [string[]]$Packages = @('./...'),
    [string]$Run = '',
    [ValidateRange(1, 10000)]
    [int]$Count = 1,
    [switch]$Race
)

$ErrorActionPreference = 'Stop'
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$testRoot = Join-Path $repoRoot 'bin/tests'

Push-Location -LiteralPath $repoRoot
try {
    $hostOS = & go env GOHOSTOS
    if ($LASTEXITCODE -ne 0) { throw 'Unable to query Go host OS.' }
    $hostArch = & go env GOHOSTARCH
    if ($LASTEXITCODE -ne 0) { throw 'Unable to query Go host architecture.' }
    $targetOS = & go env GOOS
    if ($LASTEXITCODE -ne 0) { throw 'Unable to query Go target OS.' }
    $targetArch = & go env GOARCH
    if ($LASTEXITCODE -ne 0) { throw 'Unable to query Go target architecture.' }
    if ($hostOS -ne 'windows' -or $targetOS -ne $hostOS -or $targetArch -ne $hostArch) {
        throw 'This script requires a native Windows Go target; clear cross-compilation overrides first.'
    }
    $testRoot = Join-Path $testRoot ("windows-" + $hostArch.Trim())
    $packageRows = @(& go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}|{{.Dir}}{{end}}' @Packages)
    if ($LASTEXITCODE -ne 0) { throw 'Unable to enumerate Go test packages.' }
    $failedPackages = @()
    foreach ($row in $packageRows) {
        if ([string]::IsNullOrWhiteSpace($row)) { continue }
        $parts = $row.Split('|', 2)
        $packageName = $parts[0]
        $packageDirectory = $parts[1]
        # Preserve the full import path to avoid collisions between packages
        # with the same final name. No existing outputs are recursively removed.
        $safeSegments = @($packageName.Split('/') | ForEach-Object {
            if ($_ -eq '' -or $_ -eq '.' -or $_ -eq '..' -or $_ -match '[\\:*?"<>|]') {
                throw "Unsupported package path component: $_"
            }
            $_
        })
        $packageOutput = $testRoot
        foreach ($segment in $safeSegments) { $packageOutput = Join-Path $packageOutput $segment }
        New-Item -ItemType Directory -Force -Path $packageOutput | Out-Null
        $executable = Join-Path $packageOutput 'tests.exe'
        $compileArguments = @('test', '-c', '-o', $executable)
        if ($Race) { $compileArguments += '-race' }
        $compileArguments += $packageName
        & go @compileArguments
        if ($LASTEXITCODE -ne 0) { throw "Test compilation failed: $packageName" }
        $testArguments = @('-test.timeout=10m', "-test.count=$Count")
        if ($Run -ne '') { $testArguments += "-test.run=$Run" }
        Write-Output "Testing $packageName"
        # Match go test's working directory so relative fixtures still work.
        Push-Location -LiteralPath $packageDirectory
        try {
            & $executable @testArguments
            if ($LASTEXITCODE -ne 0) { $failedPackages += $packageName }
        } finally {
            Pop-Location
        }
    }
    if ($failedPackages.Count -gt 0) { throw ('Tests failed: ' + ($failedPackages -join ', ')) }
    Write-Output 'All selected native Windows tests passed.'
} finally {
    Pop-Location
}
