# Manual release checklist

DownloadX does not use GitHub Actions. Tags and releases are created by hand on
a Windows machine.

## Prerequisites

- Go 1.27.3 or later
- WiX Toolset 3.14 with `candle.exe` and `light.exe` (or set `WIX_BIN`)
- PowerShell 5.1 or later
- `gh` authenticated for `neko233-com/DownloadX`
- A clean working tree on `main`

## 1. Build and test

```powershell
go vet ./...
go test ./...
go build ./cmd/downloadx
go build -tags=mygo_nowebview ./cmd/downloadx-gui
```

## 2. Build the MSI

Pick the version without a leading `v`:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build-msi.ps1 -Version 0.1.0
```

This produces:

```
dist\DownloadX-windows-amd64.msi
dist\DownloadX-windows-amd64.msi.sha256
```

The MSI is per-machine (`C:\Program Files\DownloadX`), installs both
`downloadx.exe` and `downloadx-gui.exe`, and adds a Start-menu shortcut.

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

## 3. Commit and tag

```powershell
git add -A
git commit -m "release: v0.1.0"
git push origin main
git tag -a v0.1.0 -m "DownloadX 0.1.0"
git push origin v0.1.0
```

## 4. Create the GitHub release

```powershell
gh release create v0.1.0 `
  dist\DownloadX-windows-amd64.msi `
  dist\DownloadX-windows-amd64.msi.sha256 `
  --title "DownloadX 0.1.0" `
  --notes "Windows x64 installer. Segmented HTTP/FTP, HLS, DASH, BitTorrent, and the native MyGo + Lobe UI desktop app."
```

Keep the asset name `DownloadX-windows-amd64.msi` stable: the one-line
installers use `releases/latest/download/DownloadX-windows-amd64.msi`.

## 5. Verify the installers

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

## Notes

- GitHub Actions is disabled for this repository. Do not add `.github/workflows`.
- The GUI is Windows-only for now. Other platforms are not supported releases.
- The version embedded in the executables comes from `-Version` in the MSI build
  script; keep it equal to the tag without the `v`.
