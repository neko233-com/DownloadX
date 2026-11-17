# Packs the Chrome/Edge extension into dist\ and refreshes extensions\updates.xml.
#
#   powershell -ExecutionPolicy Bypass -File scripts\build-extension.ps1
#
# The signing key is extensions\downloadx.pem. It is NOT committed: losing it
# changes the extension ID and breaks the force-install policies on machines
# that already have the old ID. Back it up with the release materials.
param(
    [string]$OutDir = "dist"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$extDir = Join-Path $root "extensions\downloadx"
$manifestPath = Join-Path $extDir "manifest.json"
$keyPath = Join-Path $root "extensions\downloadx.pem"
$idPath = Join-Path $root "extensions\id.txt"
$pubPath = Join-Path $root "extensions\public-key.txt"
$updatesPath = Join-Path $root "extensions\updates.xml"

if (-not (Test-Path $manifestPath)) { throw "missing $manifestPath" }
if (-not (Test-Path $keyPath)) {
    throw "missing $keyPath. Restore the extension signing key, or generate a new one and update installer/com.neko233.downloadx.json and installer/DownloadX.wxs."
}

$manifest = Get-Content $manifestPath -Raw | ConvertFrom-Json
$version = [string]$manifest.version
$pubKey = [string]$manifest.key
$id = (Get-Content $idPath -Raw).Trim()
$expected = (Get-Content $pubPath -Raw).Trim()
if ($pubKey -ne $expected) {
    throw "manifest.json key does not match extensions\public-key.txt"
}

New-Item -ItemType Directory -Force $OutDir | Out-Null

$chrome = @(
    "C:\Program Files\Google\Chrome\Application\chrome.exe",
    "C:\Program Files (x86)\Google\Chrome\Application\chrome.exe",
    "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
    "C:\Program Files\Microsoft\Edge\Application\msedge.exe"
) | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $chrome) {
    throw "Chrome or Edge is required to pack the CRX."
}

$profile = Join-Path $env:TEMP ("downloadx-pack-" + [guid]::NewGuid().ToString("N"))
$crxNextToExt = Join-Path $root "extensions\downloadx.crx"
Remove-Item -Force $crxNextToExt -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force $profile | Out-Null

Write-Host "Packing DownloadX $version (extension id $id)..."
$packArgs = @(
    "--user-data-dir=$profile",
    "--no-first-run",
    "--no-default-browser-check",
    "--pack-extension=$extDir",
    "--pack-extension-key=$keyPath"
)
& $chrome @packArgs 2>$null | Out-Null
$deadline = (Get-Date).AddSeconds(30)
while (-not (Test-Path $crxNextToExt) -and (Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 200
}
Remove-Item -Recurse -Force $profile -ErrorAction SilentlyContinue
if (-not (Test-Path $crxNextToExt)) {
    throw "Chrome did not produce extensions\downloadx.crx"
}

if (-not (Test-Path $crxNextToExt)) {
    throw "Chrome did not produce extensions\downloadx.crx"
}

$crx = Join-Path $OutDir "DownloadX-extension.crx"
Move-Item -Force $crxNextToExt $crx
$hash = (Get-FileHash -Algorithm SHA256 $crx).Hash.ToLowerInvariant()
"$hash  $(Split-Path -Leaf $crx)" | Set-Content -Encoding ascii (Join-Path $OutDir "DownloadX-extension.crx.sha256")

$docsExt = Join-Path $root "docs\extension"
New-Item -ItemType Directory -Force $docsExt | Out-Null
Copy-Item -Force $crx (Join-Path $docsExt "downloadx.crx")

$updateURL = "https://cdn.jsdelivr.net/gh/neko233-com/DownloadX@main/docs/extension/downloadx.crx?v=$version"
$updates = @"
<?xml version='1.0' encoding='UTF-8'?>
<gupdate xmlns='http://www.google.com/update2/response' protocol='2.0'>
  <app appid='$id'>
    <updatecheck codebase='$updateURL' version='$version' />
  </app>
</gupdate>
"@
Set-Content -Path $updatesPath -Value $updates -Encoding ascii
Set-Content -Path (Join-Path $docsExt "updates.xml") -Value $updates -Encoding ascii

Write-Host ""
Write-Host "CRX:      $crx"
Write-Host "Docs CRX: $(Join-Path $docsExt 'downloadx.crx')"
Write-Host "SHA256:   $hash"
Write-Host "Version:  $version"
Write-Host "Ext ID:   $id"
Write-Host "Update:   $updatesPath"
Write-Host "          $(Join-Path $docsExt 'updates.xml')"
