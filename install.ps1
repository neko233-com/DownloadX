# DownloadX one-click installer for Windows.
# Usage:
#   irm https://raw.githubusercontent.com/neko233-com/DownloadX/main/install.ps1 | iex
#   $env:DOWNLOADX_VERSION = "0.1.0"; irm .../install.ps1 | iex
$ErrorActionPreference = "Stop"

if ($env:OS -ne "Windows_NT") {
    throw "DownloadX currently supports Windows only."
}

[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = "neko233-com/DownloadX"
$version = $env:DOWNLOADX_VERSION
$msiName = "DownloadX-windows-amd64.msi"
if ([string]::IsNullOrWhiteSpace($version) -or $version -eq "latest") {
    $url = "https://github.com/$repo/releases/latest/download/$msiName"
} else {
    $version = $version -replace '^v', ''
    $url = "https://github.com/$repo/releases/download/v$version/$msiName"
}

$msi = Join-Path $env:TEMP "DownloadX-$([guid]::NewGuid().ToString('N')).msi"
Write-Host "Downloading $url"
try {
    Invoke-WebRequest -Uri $url -OutFile $msi -UseBasicParsing
    Write-Host "Installing DownloadX (administrator permission is required)..."
    $process = Start-Process msiexec.exe -ArgumentList @(
        "/i", "`"$msi`"", "/passive", "/norestart"
    ) -Verb RunAs -Wait -PassThru
    if ($process.ExitCode -ne 0 -and $process.ExitCode -ne 3010) {
        throw "msiexec failed with exit code $($process.ExitCode)"
    }
    Write-Host ""
    Write-Host "DownloadX is installed."
    Write-Host "  App: C:\Program Files\DownloadX\downloadx-gui.exe (Start menu: DownloadX)"
    Write-Host "  CLI: C:\Program Files\DownloadX\downloadx.exe"
    Write-Host "  Browser: restart Chrome/Edge once; the DownloadX extension installs automatically."
} finally {
    Remove-Item -Force $msi -ErrorAction SilentlyContinue
}
