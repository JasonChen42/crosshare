"use strict";
// Import is a staged queue, not an immediate flat FileList upload. Targets are
// retained per item so retrying a partial batch cannot change completed imports.
let importQueue = [],
  importTargetID = 0,
  importTargetName = "",
  importSkipped = 0;
let stopImport = false,
  currentImportController;
const rootAlbums = new Map();
const mediaFile = (file) =>
  /^(image|video)\//.test(file.type) ||
  /\.(jpe?g|png|gif|webp|avif|heic|heif|bmp|tiff?|mp4|mov|m4v|webm|mkv)$/i.test(
    file.name,
  );
function openImport() {
  if (!$("#import-dialog").open) {
    if (!importQueue.length) {
      importTargetID = albumID;
      importTargetName = $("#heading").textContent;
      $("#import-mode").value = importTargetID ? "current" : "folders";
      $('#import-mode option[value="current"]').disabled = !importTargetID;
    }
    $("#import-dialog").showModal();
  }
  renderImport();
}
function stageFiles(files, paths) {
  if (uploading) {
    message("请先暂停当前导入，再添加文件夹。");
    return;
  }
  openImport();
  const known = new Set(importQueue.map((x) => x.key));
  files.forEach((file, i) => {
    if (!mediaFile(file)) {
      importSkipped++;
      return;
    }
    const path = paths?.[i] || file.webkitRelativePath || file.name;
    const key = path + "|" + file.size + "|" + file.lastModified;
    if (known.has(key)) return;
    known.add(key);
    importQueue.push({
      key,
      file,
      path,
      root: path.includes("/") ? path.split("/")[0] : null,
      state: "pending",
      error: "",
      album: null,
    });
  });
  renderImport();
}
async function stageDroppedItems(data) {
  // Read handles synchronously while the DataTransfer store is still accessible.
  const entries = [...data.items]
    .filter((x) => x.kind === "file")
    .map((x) => x.webkitGetAsEntry?.())
    .filter(Boolean);
  const fallback = [...data.files];
  if (!entries.length) {
    stageFiles(fallback);
    return;
  }
  if (uploading) {
    message("请先暂停当前导入，再添加文件夹。");
    return;
  }
  openImport();
  $("#import-summary").textContent = "正在读取文件夹…";
  const found = [],
    paths = [];
  async function walk(entry, parent = "") {
    const path = parent + entry.name;
    if (entry.isFile) {
      const file = await new Promise((resolve, reject) =>
        entry.file(resolve, reject),
      );
      found.push(file);
      paths.push(path);
      return;
    }
    if (entry.isDirectory) {
      const reader = entry.createReader();
      while (true) {
        const batch = await new Promise((resolve, reject) =>
          reader.readEntries(resolve, reject),
        );
        if (!batch.length) break;
        for (const child of batch) await walk(child, path + "/");
      }
    }
  }
  for (const entry of entries) await walk(entry);
  stageFiles(found, paths);
}
function renderImport() {
  const done = importQueue.filter((x) => x.state === "done").length;
  const failed = importQueue.filter((x) => x.state === "failed");
  $("#import-summary").textContent =
    `共 ${importQueue.length} 项 · 已完成 ${done} · 失败 ${failed.length} · 跳过 ${importSkipped} 个非媒体文件`;
  $("#import-destination").textContent =
    $("#import-mode").value === "current"
      ? `所有文件加入「${importTargetName}」。`
      : $("#import-mode").value === "library"
        ? "文件只进入照片库，不加入相册。"
        : "按顶层文件夹归组，子目录内容一起导入。原文件保持不变。";
  const groups = new Map();
  for (const x of importQueue) {
    const name = x.root || "选中的文件";
    groups.set(name, (groups.get(name) || 0) + 1);
  }
  $("#import-groups").replaceChildren(
    ...[...groups].map(([name, count]) => el("li", `${name} · ${count} 项`)),
  );
  $("#import-errors").replaceChildren(
    ...failed.slice(0, 20).map((x) => el("p", x.path + "：" + x.error)),
  );
  $("#start-import").disabled = uploading || done === importQueue.length;
  $("#start-import").textContent =
    done || failed.length ? "继续 / 重试未完成项" : "开始导入";
  for (const id of ["add-folders", "add-files", "clear-import", "close-import"])
    $("#" + id).disabled = uploading;
  $("#import-mode").disabled = uploading || done > 0;
  $("#stop-import").hidden = !uploading;
}
async function runImport() {
  if (uploading) return;
  uploading = true;
  stopImport = false;
  renderImport();
  const mode = $("#import-mode").value;
  try {
    for (const item of importQueue) {
      if (stopImport) break;
      if (item.state === "done") continue;
      item.error = "";
      item.state = "pending";
      $("#import-summary").textContent = `正在导入：${item.path}`;
      try {
        if (item.album === null) {
          if (mode === "current") item.album = importTargetID;
          else if (mode === "folders" && item.root) {
            if (!rootAlbums.has(item.root))
              rootAlbums.set(
                item.root,
                (await api("/albums", { name: item.root })).id,
              );
            item.album = rootAlbums.get(item.root);
          } else item.album = 0;
        }
        currentImportController = new AbortController();
        let binary = "";
        new TextEncoder()
          .encode(item.file.name)
          .forEach((b) => (binary += String.fromCharCode(b)));
        const response = await fetch(
          base + "/import" + (item.album ? "?album_id=" + item.album : ""),
          {
            method: "POST",
            signal: currentImportController.signal,
            headers: {
              "X-CrossHare-UI": "1",
              "X-CrossHare-File-Name-Base64": btoa(binary),
              "X-CrossHare-Date-Taken-Ms": String(item.file.lastModified),
            },
            body: item.file,
          },
        );
        if (!response.ok) throw Error(await response.text());
        item.state = "done";
      } catch (e) {
        if (e.name === "AbortError") {
          item.state = "pending";
          break;
        }
        item.state = "failed";
        item.error = e.message;
      }
      renderImport();
    }
    await loadChoices();
    if (importTargetID && mode === "current") {
      albumID = importTargetID;
      await load();
    } else if (["photos", "favorites"].includes(view)) await load(page);
    else if (view === "albums") await switchView("albums");
  } finally {
    uploading = false;
    currentImportController = null;
    renderImport();
  }
}
$("#import-folders").onclick = () => {
  openImport();
  $("#folders").click();
};
$("#add-folders").onclick = () => $("#folders").click();
$("#add-files").onclick = () => $("#files").click();
$("#folders").onchange = (e) => {
  stageFiles([...e.target.files]);
  e.target.value = "";
};
$("#files").onchange = (e) => {
  stageFiles([...e.target.files]);
  e.target.value = "";
};
$("#import-mode").onchange = () => {
  for (const item of importQueue) if (item.state !== "done") item.album = null;
  renderImport();
};
$("#start-import").onclick = guard(runImport);
$("#stop-import").onclick = () => {
  stopImport = true;
  currentImportController?.abort();
};
$("#clear-import").onclick = () => {
  importQueue = [];
  importSkipped = 0;
  rootAlbums.clear();
  $("#import-mode").disabled = false;
  renderImport();
};
$("#close-import").onclick = () => {
  if (importQueue.every((x) => x.state === "done")) {
    importQueue = [];
    importSkipped = 0;
    rootAlbums.clear();
  }
  $("#import-dialog").close();
};
$("#import-dialog").addEventListener("cancel", (e) => {
  if (uploading) e.preventDefault();
});
