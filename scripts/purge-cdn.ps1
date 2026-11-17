# Purges jsDelivr's cache for the extension update manifest and CRX.
# Run after pushing docs/extension so browsers see a new version immediately.
$ErrorActionPreference = "Stop"

$paths = @(
    "gh/neko233-com/DownloadX@main/docs/extension/updates.xml",
    "gh/neko233-com/DownloadX@main/docs/extension/downloadx.crx"
)

foreach ($path in $paths) {
    $url = "https://purge.jsdelivr.net/$path"
    Write-Host "Purging $url"
    Invoke-WebRequest -Uri $url -UseBasicParsing | Out-Null
}

Write-Host "jsDelivr cache purged."
