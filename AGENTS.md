# AGENTS.md

Instructions for coding agents working on DownloadX.

## Project

- Pure Go 1.27.3+. Windows 10/11 x64 is the only supported release platform.
- `downloadx`: CLI (HTTP/FTP/HLS/DASH/BitTorrent, 16 connections by default).
- `downloadx-gui`: native desktop app built with **MyGo** (`github.com/egoist/mygo`)
  and **Lobe MyGo** (`github.com/lobehub/lobe-mygo`). No webview, no HTML/CSS/JS
  for the app UI. The GUI is a `-H=windowsgui` binary, so it never opens a
  console window.
- The UI has a left sidebar with Downloads, Settings and About pages. Settings
  persist to `%LOCALAPPDATA%\DownloadX\settings.json`; language catalogs live in
  `internal/desktop/i18n/locales` and user overrides in
  `%LOCALAPPDATA%\DownloadX\locales`.
- `downloadx-host`: native messaging host for the Chrome/Edge extension.
- `extensions/downloadx`: MV3 extension, installed through the browsers'
  external-extension registry and auto-updated without the Web Store.
- MSI installer built with WiX Toolset 3.14.

## Hard rules

1. **No GitHub Actions / CI/CD.** Do not create `.github/workflows`. Tags and
   releases are manual; see `RELEASE.md`.
2. **Windows only for releases.** Do not add Linux/macOS release assets.
3. **Keep the MSI asset name** `DownloadX-windows-amd64.msi`; the one-line
   installers download `releases/latest/download/DownloadX-windows-amd64.msi`.
4. **Keep the install directory** `C:\Program Files\DownloadX`. The native
   messaging manifest contains that absolute path.
5. **Do not change the extension ID** `pdlpjmbjgipkbjekncinhchgbahpgbng`.
   It is derived from `extensions/public-key.txt`. Changing it requires updating
   `extensions/downloadx/manifest.json`, `extensions/id.txt`,
   `installer/com.neko233.downloadx.json`, and the external-extension registry
   entries in `installer/DownloadX.wxs`.
6. **Never commit** `extensions/downloadx.pem` (signing key) or generated CRX
   files. Back up the key with release materials.
7. The app UI must stay native MyGo + Lobe UI. Browser extension HTML/CSS/JS is
   expected and fine.

## On Windows: build and install the MSI every time

When running on Windows, after any change to shipped code, run the full cycle
below before claiming completion. This is the project's required verification.

```powershell
gofmt -w .
go vet ./...
go test ./...
powershell -ExecutionPolicy Bypass -File scripts\build-extension.ps1   # if the extension changed
powershell -ExecutionPolicy Bypass -File scripts\build-msi.ps1 -Version 0.3.0
```

Then install the MSI:

```powershell
# Preferred: elevated PowerShell/terminal
msiexec /i .\dist\DownloadX-windows-amd64.msi /passive /norestart
```

If the current shell is not elevated, request elevation:

```powershell
Start-Process msiexec.exe -ArgumentList @(
  "/i", (Resolve-Path ".\dist\DownloadX-windows-amd64.msi").Path, "/passive", "/norestart"
) -Verb RunAs -Wait
```

If elevation is impossible (no one to approve UAC), validate the package without
installing and say clearly that it was not installed:

```powershell
$target = Join-Path $env:TEMP "downloadx-msi-check"
msiexec /a .\dist\DownloadX-windows-amd64.msi /qn TARGETDIR="$target"
Get-ChildItem -Recurse (Join-Path $target "DownloadX")
```

After a real install, verify:

```powershell
& "C:\Program Files\DownloadX\downloadx.exe" version
Test-Path "C:\Program Files\DownloadX\downloadx-gui.exe"
Test-Path "C:\Program Files\DownloadX\downloadx-host.exe"
Get-ItemProperty "HKLM:\SOFTWARE\Google\Chrome\Extensions\pdlpjmbjgipkbjekncinhchgbahpgbng"
Get-ItemProperty "HKLM:\SOFTWARE\Wow6432Node\Google\Chrome\Extensions\pdlpjmbjgipkbjekncinhchgbahpgbng"
Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Edge\Extensions\pdlpjmbjgipkbjekncinhchgbahpgbng"
Get-ItemProperty "HKLM:\SOFTWARE\Google\Chrome\NativeMessagingHosts\com.neko233.downloadx"
Get-StartApps | Where-Object { $_.Name -eq "DownloadX" }
```

The Start-menu shortcut must be searchable. Its folder and `.lnk` must not carry
`NotContentIndexed`; the MSI clears that attribute after installation.

Leave DownloadX installed so the user can try it. Only uninstall when asked:

```powershell
$entry = Get-ChildItem "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall" |
  ForEach-Object { Get-ItemProperty $_.PSPath } |
  Where-Object DisplayName -eq "DownloadX" | Select-Object -First 1
msiexec /x $entry.PSChildName /qn /norestart
```

## Extension workflow

- Source: `extensions/downloadx`.
- Signing key: `extensions/downloadx.pem` (local, untracked).
- Build: `scripts\build-extension.ps1` → `dist\DownloadX-extension.crx`,
  `dist\DownloadX-extension.crx.sha256`, and refreshed `extensions/updates.xml`.
- The MSI registers:
  - native messaging host `com.neko233.downloadx` for Chrome and Edge;
  - external-extension `update_url` entries for both browsers.
- Browser-managed auto-update uses
  `https://cdn.jsdelivr.net/gh/neko233-com/DownloadX@main/docs/extension/updates.xml`.
  The update manifest points at
  `docs/extension/downloadx.crx`, which jsDelivr serves as
  `application/x-chrome-extension`. Commit refreshed `docs/extension/` and
  `extensions/updates.xml` with every extension release.
- After installing the MSI, the user restarts Chrome/Edge once. Because the
  extension is self-hosted, each browser asks **Keep extension** once on
  unmanaged Windows; after that it is enabled and auto-updates. There is no Web
  Store listing and no developer mode.
- The extension cancels browser downloads and sends them through the native host
  to `%LOCALAPPDATA%\DownloadX\inbox`; the GUI watches that folder and queues
  them with 16 connections.

## Manual release

Follow `RELEASE.md` exactly. In short:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build-extension.ps1
powershell -ExecutionPolicy Bypass -File scripts\build-msi.ps1 -Version 0.3.0
git add -A; git commit -m "release: v0.3.0"; git push origin main
git tag -a v0.3.0 -m "DownloadX 0.3.0"; git push origin v0.3.0
gh release create v0.3.0 `
  dist\DownloadX-windows-amd64.msi `
  dist\DownloadX-windows-amd64.msi.sha256 `
  dist\DownloadX-extension.crx `
  dist\DownloadX-extension.crx.sha256 `
  --title "DownloadX 0.3.0" --notes "..."
```

Then verify that
`https://github.com/neko233-com/DownloadX/releases/latest/download/DownloadX-windows-amd64.msi`
and `.../DownloadX-extension.crx` resolve.

After pushing `docs/extension/`, purge the CDN so browsers see the new version
immediately:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\purge-cdn.ps1
```
