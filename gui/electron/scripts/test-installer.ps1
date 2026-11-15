param([Parameter(Mandatory = $true)][string]$Installer)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'This acceptance check requires native Windows and PowerShell 7.' }
$installerPath = (Resolve-Path -LiteralPath $Installer).Path
$projectPath = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$testRoot = [IO.Path]::GetFullPath((Join-Path $projectPath ('test-results\installer-' + [guid]::NewGuid().ToString('N'))))
$installPath = Join-Path $testRoot 'Application'
if (-not $installPath.StartsWith($projectPath + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Unsafe test destination' }
# A test must never upgrade an existing installation or stop an existing Host.
$registryRoots = @('HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall', 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall', 'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall')
foreach ($registryRoot in $registryRoots) {
    $existing = Get-ChildItem -LiteralPath $registryRoot -ErrorAction SilentlyContinue | Get-ItemProperty | Where-Object { $_.DisplayName -like '*ChannelTerm*' }
    if ($existing) { throw 'Existing ChannelTerm installation found; use a disposable Windows account/VM.' }
}
if (Get-Process -Name channelterm,channelterm-desktop -ErrorAction SilentlyContinue) { throw 'Existing ChannelTerm process found; leave it untouched and use a disposable account/VM.' }
New-Item -ItemType Directory -Path $testRoot | Out-Null
function Run-Installer([string]$Path, [string[]]$Arguments, [int]$Expected) {
    $child = Start-Process -FilePath $Path -ArgumentList $Arguments -WindowStyle Hidden -PassThru -Wait
    if ($child.ExitCode -ne $Expected) { throw "Expected exit $Expected, got $($child.ExitCode): $Path" }
}
$ownedHost = $null
$oldConfig = $env:CHANNELTERM_HOST_CONFIG_DIR
$uninstaller = $null
try {
    Run-Installer $installerPath @('/S', "/D=$installPath") 0
    $uninstaller = (Get-ChildItem -LiteralPath $installPath -Filter 'Uninstall*.exe').FullName
    if (-not $uninstaller) { throw 'Installed uninstaller is missing' }
    $hostPath = Join-Path $installPath 'resources\host\channelterm.exe'
    $filePath = Join-Path $installPath 'resources\files\channelterm.exe'
    if ((Get-FileHash -LiteralPath $hostPath).Hash -ne (Get-FileHash -LiteralPath $filePath).Hash) { throw 'Companion bytes differ' }
    $env:CHANNELTERM_HOST_CONFIG_DIR = Join-Path $testRoot 'host-config'
    $ownedHost = Start-Process -FilePath $hostPath -ArgumentList @('mcp', '--transport', 'http', '--listen', '127.0.0.1:0') -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $testRoot 'host-stderr.log')
    $ready = Join-Path $env:CHANNELTERM_HOST_CONFIG_DIR 'http-host.json'
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    while (-not (Test-Path -LiteralPath $ready)) {
        if ($ownedHost.HasExited -or [DateTime]::UtcNow -ge $deadline) { throw 'Isolated Host did not initialize' }
        Start-Sleep -Milliseconds 100
    }
    Run-Installer $installerPath @('/S', "/D=$installPath") 2
    Run-Installer $uninstaller @('/S', "_?=$installPath") 2
    if ($ownedHost.HasExited -or -not (Test-Path -LiteralPath $hostPath)) { throw 'Installer stopped or removed the shared Host' }
    # Only this exact child belongs to the test. It opened no device/Session.
    Stop-Process -Id $ownedHost.Id
    $ownedHost.WaitForExit()
    $ownedHost = $null
    Run-Installer $uninstaller @('/S', "_?=$installPath") 0
    foreach ($installedFile in @($hostPath, $filePath, (Join-Path $installPath 'channelterm-desktop.exe'))) {
        if (Test-Path -LiteralPath $installedFile) { throw 'Uninstall left application or companion files' }
    }
    if (-not (Test-Path -LiteralPath $ready)) { throw 'Uninstall removed isolated Host configuration' }
    foreach ($registryRoot in $registryRoots) {
        $remaining = Get-ChildItem -LiteralPath $registryRoot -ErrorAction SilentlyContinue | Get-ItemProperty | Where-Object { $_.InstallLocation -eq $installPath }
        if ($remaining) { throw 'Uninstall left its installation registration' }
    }
    Write-Output "PASS per-user install/uninstall, matching companions, live Host blocks upgrade/removal without termination, configuration preserved: $testRoot"
} finally {
    if ($ownedHost -and -not $ownedHost.HasExited) { Stop-Process -Id $ownedHost.Id; $ownedHost.WaitForExit() }
    $env:CHANNELTERM_HOST_CONFIG_DIR = $oldConfig
    # Failed acceptance retains its installation and logs for diagnosis. Never
    # recursively erase a possibly partial installation or another user's state.
}
