const HOST = "com.neko233.downloadx";
const enabled = document.getElementById("enabled");
const statusEl = document.getElementById("status");

chrome.storage.local.get({ enabled: true }, (stored) => {
  enabled.checked = stored.enabled;
});

enabled.addEventListener("change", () => {
  chrome.storage.local.set({ enabled: enabled.checked });
});

document.getElementById("open").addEventListener("click", () => {
  chrome.runtime.sendNativeMessage(HOST, { action: "open" }, () => {
    void chrome.runtime.lastError;
  });
});

chrome.runtime.sendNativeMessage(HOST, { action: "ping" }, (response) => {
  if (chrome.runtime.lastError || !response || !response.ok) {
    statusEl.textContent = "offline";
    statusEl.classList.add("off");
    return;
  }
  statusEl.textContent = "connected";
  statusEl.classList.add("on");
});
