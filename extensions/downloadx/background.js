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
  handOff({
    url: item.url,
    filename: item.filename || "",
    referrer: item.referrer || "",
  });
});

chrome.contextMenus.onClicked.addListener((info) => {
  if (info.menuItemId === "downloadx-link" && info.linkUrl) {
    handOff({ url: info.linkUrl });
  }
});

function handOff(payload) {
  chrome.runtime.sendNativeMessage(
    HOST,
    { action: "download", ...payload },
    (response) => {
      if (chrome.runtime.lastError || !response || !response.ok) {
        chrome.notifications.create({
          type: "basic",
          iconUrl: "icons/icon128.png",
          title: "DownloadX",
          message:
            "Could not reach the DownloadX desktop app. Open DownloadX and try again.",
        });
      }
    }
  );
}
