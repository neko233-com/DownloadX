#!/bin/sh
# DownloadX one-click installer for Windows shells.
# Works in Git Bash / MSYS / Cygwin and in WSL with Windows interop.
# The MSI is per-machine, so Windows asks for administrator permission.
#
#   curl -fsSL https://raw.githubusercontent.com/neko233-com/DownloadX/main/install.sh | sh
#   DOWNLOADX_VERSION=0.1.0 curl -fsSL .../install.sh | sh
set -eu

repo="neko233-com/DownloadX"
msi_name="DownloadX-windows-amd64.msi"
version="${DOWNLOADX_VERSION:-latest}"

if [ "$version" = "latest" ]; then
    url="https://github.com/$repo/releases/latest/download/$msi_name"
else
    version="${version#v}"
    url="https://github.com/$repo/releases/download/v$version/$msi_name"
fi

tmp="${TMPDIR:-/tmp}/DownloadX-$$.msi"
cleanup() { rm -f "$tmp"; }
trap cleanup EXIT INT TERM

echo "Downloading $url"
if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --connect-timeout 20 -o "$tmp" "$url"
elif command -v wget >/dev/null 2>&1; then
    wget -O "$tmp" "$url"
else
    echo "error: curl or wget is required" >&2
    exit 1
fi

# Turn the POSIX path into a Windows path for msiexec.
to_windows_path() {
    if command -v cygpath >/dev/null 2>&1; then
        cygpath -w "$1"
    elif command -v wslpath >/dev/null 2>&1; then
        wslpath -w "$1"
    else
        printf '%s\n' "$1"
    fi
}

install_msi() {
    winpath="$(to_windows_path "$1")"
    if command -v powershell.exe >/dev/null 2>&1; then
        powershell.exe -NoProfile -Command \
            "\$p = Start-Process msiexec.exe -ArgumentList @('/i','$winpath','/passive','/norestart') -Verb RunAs -Wait -PassThru; exit \$p.ExitCode"
    else
        msiexec.exe /i "$winpath" /passive /norestart
    fi
}

case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*)
        code=0
        install_msi "$tmp" || code=$?
        ;;
    Linux)
        if grep -qi microsoft /proc/version 2>/dev/null && command -v msiexec.exe >/dev/null 2>&1; then
            code=0
            install_msi "$tmp" || code=$?
        else
            echo "DownloadX currently supports Windows only. Run install.ps1 in PowerShell on Windows." >&2
            exit 1
        fi
        ;;
    *)
        echo "DownloadX currently supports Windows only. Run install.ps1 in PowerShell on Windows." >&2
        exit 1
        ;;
esac

if [ "$code" -ne 0 ] && [ "$code" -ne 3010 ]; then
    echo "error: msiexec failed with exit code $code" >&2
    exit "$code"
fi

echo ""
echo "DownloadX is installed."
echo "  App: C:\\Program Files\\DownloadX\\downloadx-gui.exe (Start menu: DownloadX)"
echo "  CLI: C:\\Program Files\\DownloadX\\downloadx.exe"
echo "  Browser: restart Chrome/Edge once; keep the DownloadX extension when asked."
