# Builds the Windows x64 MSI into dist\.
#
#   powershell -ExecutionPolicy Bypass -File scripts\build-msi.ps1 -Version 0.1.0
#
# Requires Go 1.27.3+ and WiX Toolset 3.14 (candle.exe and light.exe).
param(
    [string]$Version = "",
    [string]$OutDir = "dist"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if ([string]::IsNullOrWhiteSpace($Version)) {
    try {
        $describe = (git describe --tags --always 2>$null).Trim()
        $Version = $describe -replace '^v', ''
    } catch {
        $Version = "0.1.0"
    }
}
$Version = $Version -replace '^v', ''
if ($Version -notmatch '^\d+\.\d+\.\d+') {
    throw "Version must look like 0.1.0 (got '$Version')"
}

$wixBin = $env:WIX_BIN
if ([string]::IsNullOrWhiteSpace($wixBin)) {
    $candidates = @(
        "C:\Program Files (x86)\WiX Toolset v3.14\bin",
        "C:\Program Files\WiX Toolset v3.14\bin"
    )
    foreach ($candidate in $candidates) {
        if (Test-Path (Join-Path $candidate "candle.exe")) {
            $wixBin = $candidate
            break
        }
    }
}
if ([string]::IsNullOrWhiteSpace($wixBin)) {
    throw "WiX 3.14 not found. Install it or set WIX_BIN to the folder with candle.exe and light.exe."
}
$candle = Join-Path $wixBin "candle.exe"
$light = Join-Path $wixBin "light.exe"
if (-not (Test-Path $candle) -or -not (Test-Path $light)) {
    throw "WiX not found in $wixBin"
}

$payload = Join-Path $root "build\payload"
$obj = Join-Path $root "build\wixobj"
Remove-Item -Recurse -Force $payload, $obj -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force $payload, $obj, $OutDir | Out-Null

$commit = "unknown"
try { $commit = ((git rev-parse --short HEAD) 2>$null).Trim() } catch {}
$date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-dd")
$ldflags = "-s -w -buildid= -X github.com/neko233-com/DownloadX/internal/version.Version=$Version -X github.com/neko233-com/DownloadX/internal/version.Commit=$commit -X github.com/neko233-com/DownloadX/internal/version.Date=$date"
$guiLdflags = "$ldflags -H=windowsgui"
$guiTags = "mygo_nowebview mygo_noinspector"

$env:CGO_ENABLED = "0"
Write-Host "Building downloadx.exe, downloadx-gui.exe and downloadx-host.exe ($Version) in parallel..."
$buildSpecs = @(
    @{ Name = "downloadx"; Pkg = "./cmd/downloadx"; Tags = ""; Ld = $ldflags },
    @{ Name = "downloadx-gui"; Pkg = "./cmd/downloadx-gui"; Tags = $guiTags; Ld = $guiLdflags },
    @{ Name = "downloadx-host"; Pkg = "./cmd/downloadx-host"; Tags = ""; Ld = $ldflags }
)
$buildJobs = @()
foreach ($spec in $buildSpecs) {
    $statusPath = Join-Path $obj ($spec.Name + ".status")
    $outPath = Join-Path $payload ($spec.Name + ".exe")
    $buildJobs += Start-Job -ScriptBlock {
        param($root, $out, $pkg, $tags, $ldflags, $status)
        Set-Location $root
        $env:CGO_ENABLED = "0"
        $code = 1
        try {
            if ($tags) {
                & go build -trimpath -buildvcs=false -tags $tags -ldflags $ldflags -o $out $pkg
            } else {
                & go build -trimpath -buildvcs=false -ldflags $ldflags -o $out $pkg
            }
            $code = $LASTEXITCODE
        } catch {
            Write-Error $_
        }
        Set-Content -Path $status -Value $code -Encoding ascii
    } -ArgumentList $root, $outPath, $spec.Pkg, $spec.Tags, $spec.Ld, $statusPath
}
$buildJobs | Wait-Job | Out-Null
$failedBuilds = @()
for ($i = 0; $i -lt $buildJobs.Count; $i++) {
    $job = $buildJobs[$i]
    $spec = $buildSpecs[$i]
    $jobOutput = Receive-Job -Job $job
    if ($jobOutput) { $jobOutput | ForEach-Object { Write-Host $_ } }
    $statusPath = Join-Path $obj ($spec.Name + ".status")
    $code = 1
    if (Test-Path $statusPath) { $code = [int](Get-Content $statusPath -Raw).Trim() }
    if ($code -ne 0) { $failedBuilds += $spec.Name }
}
$buildJobs | Remove-Job -Force
if ($failedBuilds.Count -gt 0) {
    throw "go build failed: $($failedBuilds -join ', ')"
}

$icon = Join-Path $root "assets\downloadx.ico"
$license = Join-Path $root "installer\License.rtf"
$manifest = Join-Path $root "installer\com.neko233.downloadx.json"
$shortcut = Join-Path $root "installer\DownloadX.lnk"
$wxs = Join-Path $root "installer\DownloadX.wxs"
$wixobj = Join-Path $obj "DownloadX.wixobj"
$msi = Join-Path $OutDir "DownloadX-windows-amd64.msi"

Write-Host "Compiling WiX..."
& $candle -nologo -arch x64 `
    "-dVersion=$Version" `
    "-dPayloadDir=$payload" `
    "-dIconFile=$icon" `
    "-dLicenseFile=$license" `
    "-dManifestFile=$manifest" `
    "-dShortcutFile=$shortcut" `
    -out $wixobj $wxs
if ($LASTEXITCODE -ne 0) { throw "candle failed" }

Write-Host "Linking MSI..."
& $light -nologo -ext WixUIExtension -cultures:en-us `
    -out $msi $wixobj
if ($LASTEXITCODE -ne 0) { throw "light failed" }

$hash = (Get-FileHash -Algorithm SHA256 $msi).Hash.ToLowerInvariant()
"$hash  $(Split-Path -Leaf $msi)" | Set-Content -Encoding ascii (Join-Path $OutDir "DownloadX-windows-amd64.msi.sha256")

Write-Host ""
Write-Host "MSI:      $msi"
Write-Host "SHA256:   $hash"
Write-Host "Size:     $([math]::Round((Get-Item $msi).Length / 1MB, 1)) MB"
