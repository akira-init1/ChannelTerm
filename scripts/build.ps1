$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$distDir = Join-Path $repoRoot "dist"
$artifacts = @(
    [PSCustomObject]@{ Name = "channelterm-windows-amd64.exe"; GOOS = "windows"; GOARCH = "amd64" },
    [PSCustomObject]@{ Name = "channelterm-windows-arm64.exe"; GOOS = "windows"; GOARCH = "arm64" },
    [PSCustomObject]@{ Name = "channelterm-linux-amd64"; GOOS = "linux"; GOARCH = "amd64" },
    [PSCustomObject]@{ Name = "channelterm-linux-arm64"; GOOS = "linux"; GOARCH = "arm64" },
    [PSCustomObject]@{ Name = "channelterm-darwin-amd64"; GOOS = "darwin"; GOARCH = "amd64" },
    [PSCustomObject]@{ Name = "channelterm-darwin-arm64"; GOOS = "darwin"; GOARCH = "arm64" }
)

function Get-ArtifactInfo {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    $path = Join-Path $distDir $Name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        return $null
    }

    $item = Get-Item -LiteralPath $path
    return [PSCustomObject]@{
        Name          = $Name
        Length        = $item.Length
        LastWriteTime = $item.LastWriteTime
        SHA256        = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash
    }
}

function Show-ArtifactInfo {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [AllowNull()]
        [object]$Info
    )

    if ($null -eq $Info) {
        Write-Host "  $($Name): not found"
        return
    }

    Write-Host ("  {0}: {1:N0} bytes | {2:yyyy-MM-dd HH:mm:ss} | SHA256 {3}" -f $Info.Name, $Info.Length, $Info.LastWriteTime, $Info.SHA256)
}

$previousArtifacts = @{}
Write-Host "Previous artifacts:"
foreach ($artifact in $artifacts) {
    $info = Get-ArtifactInfo -Name $artifact.Name
    $previousArtifacts[$artifact.Name] = $info
    Show-ArtifactInfo -Name $artifact.Name -Info $info
}

if (Test-Path -LiteralPath $distDir) {
    Write-Host "Clearing previous dist directory..."
    Remove-Item -LiteralPath $distDir -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $distDir | Out-Null

$hadGOOS = Test-Path -LiteralPath "Env:GOOS"
$previousGOOS = $env:GOOS
$hadGOARCH = Test-Path -LiteralPath "Env:GOARCH"
$previousGOARCH = $env:GOARCH
$hadCGOEnabled = Test-Path -LiteralPath "Env:CGO_ENABLED"
$previousCGOEnabled = $env:CGO_ENABLED

Push-Location -LiteralPath $repoRoot
try {
    $env:CGO_ENABLED = "0"

    foreach ($artifact in $artifacts) {
        Write-Host "Building $($artifact.GOOS) $($artifact.GOARCH)..."
        $env:GOOS = $artifact.GOOS
        $env:GOARCH = $artifact.GOARCH
        & go build -o (Join-Path $distDir $artifact.Name) ./cmd/channelterm
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $($artifact.Name)"
        }
    }
}
finally {
    Pop-Location

    if ($hadGOOS) {
        $env:GOOS = $previousGOOS
    }
    else {
        Remove-Item -LiteralPath "Env:GOOS" -ErrorAction SilentlyContinue
    }

    if ($hadGOARCH) {
        $env:GOARCH = $previousGOARCH
    }
    else {
        Remove-Item -LiteralPath "Env:GOARCH" -ErrorAction SilentlyContinue
    }

    if ($hadCGOEnabled) {
        $env:CGO_ENABLED = $previousCGOEnabled
    }
    else {
        Remove-Item -LiteralPath "Env:CGO_ENABLED" -ErrorAction SilentlyContinue
    }
}

Write-Host "New artifacts:"
foreach ($artifact in $artifacts) {
    $current = Get-ArtifactInfo -Name $artifact.Name
    if ($null -eq $current) {
        throw "expected build artifact was not created: $($artifact.Name)"
    }
    Show-ArtifactInfo -Name $artifact.Name -Info $current

    $previous = $previousArtifacts[$artifact.Name]
    if ($null -eq $previous) {
        Write-Host "    Created."
    }
    elseif ($previous.SHA256 -eq $current.SHA256) {
        Write-Host "    Rebuilt; content is unchanged."
    }
    else {
        Write-Host "    Updated."
    }
}

Write-Host "Build complete: fresh artifacts are available in $distDir."
