# DownloadX browser extension

The Chrome/Edge extension hands new browser downloads to the DownloadX desktop
app through native messaging.

## One-line install

The MSI registers for both browsers:

- a native messaging host (`com.neko233.downloadx`) pointing at
  `C:\Program Files\DownloadX\downloadx-host.exe`;
- an `ExtensionInstallForcelist` policy that force-installs this extension from
  `extensions/updates.xml` and keeps it updated.

Restart Chrome/Edge after installing DownloadX. The extension then appears
automatically without the Web Store. Users can disable interception in the
extension popup.

## How the hand-off works

1. `downloads.onCreated` cancels the browser's own download.
2. The service worker sends `{action:"download", url}` through native messaging.
3. `downloadx-host.exe` writes a JSON request into
   `%LOCALAPPDATA%\DownloadX\inbox\` and launches `downloadx-gui.exe`.
4. The desktop app watches that inbox and queues the download with 16
   connections.

The native host is also used by the popup's "Open DownloadX" button and status.

## Extension ID and signing key

- ID: `pdlpjmbjgipkbjekncinhchgbahpgbng`
- Public key: `extensions/public-key.txt` (also embedded in `manifest.json`)
- Signing key: `extensions/downloadx.pem` — **not committed**; back it up.

The ID is computed from the public key. If the signing key is lost, the ID
changes; update `installer/com.neko233.downloadx.json`,
`installer/DownloadX.wxs`, and rebuild the MSI.

## Build the CRX

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build-extension.ps1
```

This produces:

```
dist\DownloadX-extension.crx
dist\DownloadX-extension.crx.sha256
extensions\updates.xml
docs\extension\downloadx.crx
docs\extension\updates.xml
```

Upload the CRX and its checksum to the GitHub release. Commit the refreshed
`extensions/updates.xml` and `docs/extension/`; the policy points at
`https://cdn.jsdelivr.net/gh/neko233-com/DownloadX@main/docs/extension/updates.xml`
and jsDelivr serves `downloadx.crx` as `application/x-chrome-extension`.

Chrome/Edge check this manifest periodically and update the extension in place.
No Web Store account is required.

## Local testing

Load `extensions/downloadx` as an unpacked extension in
`chrome://extensions` or `edge://extensions`. For native messaging without the
MSI, point the browser's
`...\NativeMessagingHosts\com.neko233.downloadx` registry value at
`installer/com.neko233.downloadx.json` after installing the host binary at the
path written in that manifest.
