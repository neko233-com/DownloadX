# Manual release checklist

DownloadX does not use GitHub Actions. Tags and releases are created by hand on
a Windows machine.

## Prerequisites

- Go 1.27.3 or later
- WiX Toolset 3.14 with `candle.exe` and `light.exe` (or set `WIX_BIN`)
- PowerShell 5.1 or later
- Chrome or Edge for packing the CRX
- `extensions/downloadx.pem` (extension signing key, not committed)
- `gh` authenticated for `neko233-com/DownloadX`
- A clean working tree on `main`

## 1. Build and test

```powershell
go vet ./...
go test ./...
go build ./cmd/downloadx
go build -tags=mygo_nowebview ./cmd/downloadx-gui
go build ./cmd/downloadx-host
```

## 2. Bump versions

Keep these equal to the tag without `v`:

- `extensions/downloadx/manifest.json` → `"version"`
- MSI `-Version` argument

## 3. Build the extension CRX

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build-extension.ps1
```

This writes `dist\DownloadX-extension.crx`, its checksum, and refreshes
`extensions/updates.xml` plus `docs/extension/updates.xml` and
`docs/extension/downloadx.crx` (served by jsDelivr for the browsers'
external-extension `update_url`). Commit the `docs/extension/` changes before
tagging; browsers update from that copy.

## 4. Build the MSI

Pick the version without a leading `v`:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build-msi.ps1 -Version 2.0.3
```

This produces:

```
dist\DownloadX-windows-amd64.msi
dist\DownloadX-windows-amd64.msi.sha256
```

The MSI is per-machine (`C:\Program Files\DownloadX`), installs the CLI, the
desktop app and `downloadx-host.exe`, adds a Start-menu shortcut, registers the
native messaging host for Chrome/Edge, and adds the browsers'
external-extension `update_url` entries.

Validate the payload without installing:

```powershell
$target = Join-Path $env:TEMP "downloadx-msi-check"
msiexec /a dist\DownloadX-windows-amd64.msi /qn TARGETDIR="$target"
Get-ChildItem -Recurse $target
Remove-Item -Recurse -Force $target
```

A full install/uninstall check needs an elevated shell:

```powershell
msiexec /i dist\DownloadX-windows-amd64.msi /passive
& "C:\Program Files\DownloadX\downloadx.exe" version
```

Uninstall from **Settings → Apps → DownloadX**, or find the product code under
`HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall` and run
`msiexec /x {PRODUCT-CODE} /passive`.

## 5. Commit and tag

```powershell
git add -A
git commit -m "release: v2.0.3"
git push origin main
git tag -a v2.0.3 -m "DownloadX 2.0.3"
git push origin v2.0.3
```

## 6. Create the GitHub release

```powershell
gh release create v2.0.3 `
  dist\DownloadX-windows-amd64.msi `
  dist\DownloadX-windows-amd64.msi.sha256 `
  dist\DownloadX-extension.crx `
  dist\DownloadX-extension.crx.sha256 `
  --title "DownloadX 2.0.3" `
  --notes "Windows x64 installer, Chrome/Edge extension, segmented HTTP/FTP, HLS, DASH, BitTorrent, and the native MyGo + Lobe UI desktop app."
```

Keep the asset names stable: the one-line installers use
`releases/latest/download/DownloadX-windows-amd64.msi`, and the CRX copy in
`docs/extension/` is the one browsers auto-update from.

## 7. Verify the installers and browser update URLs

In a normal PowerShell window:

```powershell
irm https://raw.githubusercontent.com/neko233-com/DownloadX/main/install.ps1 | iex
```

In Git Bash:

```sh
curl -fsSL https://raw.githubusercontent.com/neko233-com/DownloadX/main/install.sh | sh
```

Both should download the MSI, request administrator permission, install to
`C:\Program Files\DownloadX`, and create the Start-menu shortcut.

Check jsDelivr (may take a few minutes to refresh; run the purge script first):

```powershell
powershell -ExecutionPolicy Bypass -File scripts\purge-cdn.ps1
```

```powershell
$u = "https://cdn.jsdelivr.net/gh/neko233-com/DownloadX@main/docs/extension/updates.xml"
Invoke-WebRequest $u -UseBasicParsing | Select-Object -ExpandProperty Content
```

The CRX response must have `Content-Type: application/x-chrome-extension`:

```powershell
$crx = "https://cdn.jsdelivr.net/gh/neko233-com/DownloadX@main/docs/extension/downloadx.crx"
(Invoke-WebRequest $crx -Method Head -UseBasicParsing).Headers["Content-Type"]
```

Check the browser registrations:

```powershell
Get-ItemProperty "HKLM:\SOFTWARE\Google\Chrome\Extensions\pdlpjmbjgipkbjekncinhchgbahpgbng"
Get-ItemProperty "HKLM:\SOFTWARE\Wow6432Node\Google\Chrome\Extensions\pdlpjmbjgipkbjekncinhchgbahpgbng"
Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Edge\Extensions\pdlpjmbjgipkbjekncinhchgbahpgbng"
```

Restart Chrome/Edge after installing the MSI. The extension appears without the
Web Store; on unmanaged Windows, click **Keep extension** once per browser (the
browser's security check for self-hosted extensions). Updates are automatic
afterwards. No developer mode is needed.

## Notes

- GitHub Actions is disabled for this repository. Do not add `.github/workflows`.
- The GUI and extension are Windows-only for now.
- Keep `extensions/downloadx.pem` backed up: losing it changes the extension ID
  and breaks the external-extension registrations on existing machines.
