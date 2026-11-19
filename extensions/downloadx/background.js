const HOST = "com.neko233.downloadx";
const DEFAULTS = { enabled: true };

chrome.runtime.onInstalled.addListener(async () => {
  const stored = await chrome.storage.local.get(DEFAULTS);
  await chrome.storage.local.set(stored);
  chrome.contextMenus.create(
    {
      id: "downloadx-link",
      title: "Download with DownloadX",
      contexts: ["link"],
    },
    () => void chrome.runtime.lastError
  );
});

async function interceptEnabled() {
  const { enabled } = await chrome.storage.local.get(DEFAULTS);
  return enabled;
}

chrome.downloads.onCreated.addListener(async (item) => {
  if (!/^https?:/i.test(item.url)) return;
  if (item.byExtensionId) return;
  if (!(await interceptEnabled())) return;

  // Stop the browser's own download and hand the URL to DownloadX.
  chrome.downloads.cancel(item.id, () => {
    chrome.downloads.erase({ id: item.id });
  });
  // DownloadItem.filename is an absolute path. Send only its base name so
  // the desktop app does not turn C:\Users\...\file.zip into a mangled name.
  const filename = (item.filename || "").split(/[\\/]/).pop() || "";
  handOff(
    {
      url: item.url,
      filename,
      referrer: item.referrer || "",
    },
    item.url,
    filename
  );
});

chrome.contextMenus.onClicked.addListener((info) => {
  if (info.menuItemId === "downloadx-link" && info.linkUrl) {
    handOff({ url: info.linkUrl }, info.linkUrl, "");
  }
});

function handOff(payload, fallbackURL, fallbackFilename) {
  chrome.runtime.sendNativeMessage(
    HOST,
    { action: "download", ...payload },
    (response) => {
      if (!chrome.runtime.lastError && response && response.ok) return;
      // The browser download was already cancelled. Put it back rather than
      // losing it when the desktop app is not available.
      if (fallbackURL) {
        chrome.downloads.download({
          url: fallbackURL,
          filename: fallbackFilename || undefined,
        });
      }
      chrome.notifications.create({
        type: "basic",
        iconUrl: "icons/icon128.png",
        title: "DownloadX",
        message:
          "Could not reach the DownloadX desktop app. The browser download was restored.",
      });
    }
  );
}
