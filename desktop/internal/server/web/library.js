"use strict";
const $ = (s) => document.querySelector(s),
  base = "/api/ui/library";
let navigationVersion = 0;
let transferPage = 0;
let items = [],
  selected = new Set(),
  devices = [],
  albums = [],
  view = "photos",
  albumID = 0,
  page = 0,
  pageSize = 48,
  total = 0,
  pageLoading = false,
  loadController,
  hasMore = false,
  viewerIndex = 0,
  requestID = 0,
  uploading = false;
const titles = {
  photos: "照片库",
  favorites: "收藏",
  albums: "相册",
  transfers: "传输",
  devices: "设备",
};
function message(text, error = false) {
  $("#message").textContent = text;
  $("#message").classList.toggle("error", error);
}
async function api(path = "", body) {
  const r = await fetch(base + path, {
    headers: body
      ? { "Content-Type": "application/json", "X-CrossHare-UI": "1" }
      : {},
    method: body ? "POST" : "GET",
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok) throw Error(await r.text());
  return r.json();
}
function el(tag, text, cls) {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = text;
  if (cls) e.className = cls;
  return e;
}
function size(n) {
  return n >= 1048576
    ? (n / 1048576).toFixed(1) + " MB"
    : Math.ceil(n / 1024) + " KB";
}
function date(s) {
  return new Date(s).toLocaleDateString("zh-CN");
}
function guard(fn) {
  return async (...args) => {
    try {
      await fn(...args);
    } catch (e) {
      message(e.message, true);
    }
  };
}
function selection() {
  const show = selected.size > 0;
  $("#selection").hidden = !show;
  $("#selected-count").textContent = `已选 ${selected.size} 张`;
  $("#send").disabled = !devices.length;
  $("#add-album").disabled = !albums.length;
}
function renderGrid() {
  const grid = $("#grid");
  grid.replaceChildren();
  for (const item of items) {
    const card = el("article", undefined, "photo");
    card.classList.toggle("selected", selected.has(item.hash));
    const button = el("button", undefined, "preview");
    button.setAttribute("aria-label", "预览 " + item.name);
    button.append(
      el(
        "span",
        item.kind === "video" ? "视频 · 打开查看" : "原图 · 打开查看",
        "fallback",
      ),
    );
    const img = el("img");
    img.loading = "lazy";
    img.decoding = "async";
    img.alt = item.name;
    img.src = base + "/thumbnail/" + item.hash;
    img.onerror = () => (img.hidden = true);
    button.append(img);
    button.onclick = () => openViewer(items.indexOf(item));
    const check = el("input");
    check.type = "checkbox";
    check.checked = selected.has(item.hash);
    check.setAttribute("aria-label", "选择 " + item.name);
    check.onchange = () => {
      check.checked ? selected.add(item.hash) : selected.delete(item.hash);
      card.classList.toggle("selected", check.checked);
      selection();
    };
    card.append(
      button,
      check,
      el("span", item.name, "caption"),
      el(
        "span",
        `${date(item.date)} · ${size(item.size)}${item.favorite ? " · 已收藏" : ""}`,
        "meta",
      ),
    );
    grid.append(card);
  }
  $("#empty").hidden = items.length > 0;
  $("#count").textContent =
    `共 ${total.toLocaleString()} 项 · 本页 ${items.length} 项`;
  updatePager();
  selection();
}
function updatePager() {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  $("#page-label").textContent = `第 ${page + 1} / ${pages} 页`;
  $("#page-number").max = String(pages);
  $("#page-number").value = String(page + 1);
  $("#page-prev").disabled = pageLoading || page === 0;
  $("#page-next").disabled = pageLoading || !hasMore;
}
async function load(target = 0) {
  const id = ++requestID;
  loadController?.abort();
  loadController = new AbortController();
  pageLoading = true;
  updatePager();
  const q = new URLSearchParams({
    offset: String(target * pageSize),
    limit: String(pageSize),
    q: $("#search").value,
    album: String(albumID),
    favorite: view === "favorites" ? "1" : "0",
  });
  try {
    const response = await fetch(base + "?" + q, {
      signal: loadController.signal,
    });
    if (!response.ok) throw Error(await response.text());
    const data = await response.json();
    if (id !== requestID) return;
    const last = Math.max(0, Math.ceil(data.total / pageSize) - 1);
    if (target > last) {
      return await load(last);
    }
    items = data.items;
    page = target;
    total = data.total;
    hasMore = data.has_more;
    selected.clear();
    renderGrid();
  } catch (e) {
    if (e.name !== "AbortError") throw e;
  } finally {
    if (id === requestID) {
      pageLoading = false;
      updatePager();
    }
  }
}
async function loadChoices() {
  const [d, a] = await Promise.all([api("/devices"), api("/albums")]);
  devices = d.devices;
  albums = a.items;
  $("#device").replaceChildren(...devices.map((d) => new Option(d.name, d.id)));
  $("#album").replaceChildren(...albums.map((a) => new Option(a.name, a.id)));
  return d;
}
async function switchView(next) {
  const navigation = ++navigationVersion;
  view = next;
  albumID = 0;
  selected.clear();
  ++requestID;
  loadController?.abort();
  message("");
  $("#heading").textContent = titles[view];
  $("#subtitle").textContent =
    view === "transfers"
      ? "手机保存成功后，任务才会显示已送达。"
      : "手机与电脑的照片，在这里汇合。";
  document
    .querySelectorAll("nav button")
    .forEach((b) => b.toggleAttribute("aria-current", b.dataset.view === view));
  const photos = ["photos", "favorites"].includes(view);
  $("#photo-panel").hidden = !photos;
  $("#other-panel").hidden = photos;
  await loadChoices();
  if (navigation !== navigationVersion) return;
  if (photos) {
    await load();
    return;
  }
  const panel = $("#other-panel");
  panel.replaceChildren();
  if (view === "albums") {
    const create = el("button", "＋ 新建相册");
    create.onclick = () => $("#album-dialog").showModal();
    panel.append(create);
    const grid = el("div", undefined, "album-grid");
    for (const a of albums) {
      const b = el("button", a.name, "album-tile");
      b.append(el("span", a.count + " 项"));
      b.onclick = guard(async () => {
        albumID = a.id;
        $("#heading").textContent = a.name;
        $("#photo-panel").hidden = false;
        panel.hidden = true;
        await load();
      });
      grid.append(b);
    }
    panel.append(grid);
    if (!albums.length)
      panel.append(el("p", "创建相册，然后在照片库多选照片加入。"));
  }
  if (view === "transfers") {
    const data = await api("/transfers?limit=48&offset=" + transferPage * 48);
    if (navigation !== navigationVersion) return;
    const controls = el("div", undefined, "pagination");
    const prev = el("button", "上一页"),
      next = el("button", "下一页");
    prev.disabled = transferPage === 0;
    next.disabled = !data.has_more;
    prev.onclick = guard(async () => {
      transferPage--;
      await switchView("transfers");
    });
    next.onclick = guard(async () => {
      transferPage++;
      await switchView("transfers");
    });
    controls.append(prev, el("span", `第 ${transferPage + 1} 页`), next);
    panel.append(controls);
    if (!data.items.length)
      panel.append(
        el("p", "还没有发送任务。在照片库选择图片，发送到已配对的手机。"),
      );
    for (const t of data.items) {
      const row = el("div", undefined, "row"),
        info = el("div");
      info.append(
        el("strong", t.name),
        el(
          "p",
          (devices.find((d) => d.id === t.device_id)?.name || t.device_id) +
            " · " +
            (t.status === "done" ? "已送达（保存回执）" : "等待手机接收") +
            (t.error ? " · " + t.error : ""),
        ),
      );
      row.append(info);
      if (t.status !== "done") {
        const b = el("button", "重试");
        b.onclick = guard(async () => {
          await api("/send", { hash: t.hash, device_id: t.device_id });
          await switchView("transfers");
        });
        row.append(b);
      }
      panel.append(row);
    }
  }
  if (view === "devices") {
    const data = await api("/devices");
    if (navigation !== navigationVersion) return;
    panel.append(
      el("h2", data.name),
      el("p", "首次配对：在手机设置中输入这台电脑的局域网 IP、端口和配对码。"),
      el("h2", "配对码 " + data.pair_code),
    );
    for (const d of devices) {
      const row = el("div", undefined, "row"),
        info = el("div");
      info.append(
        el("strong", d.name),
        el(
          "p",
          d.last_seen
            ? "最近连接：" + new Date(d.last_seen).toLocaleString("zh-CN")
            : "尚无连接记录",
        ),
      );
      row.append(info);
      panel.append(row);
    }
  }
}
function openViewer(index) {
  viewerIndex = (index + items.length) % items.length;
  const f = items[viewerIndex];
  $("#viewer-name").textContent = f.name;
  $("#viewer-info").textContent =
    `${date(f.date)} · ${size(f.size)} · 电脑原图`;
  $("#favorite").textContent = f.favorite ? "取消收藏" : "收藏";
  $("#download").href = base + "/asset/" + f.hash + "?download=1";
  const media = el(f.kind === "video" ? "video" : "img");
  if (f.kind === "video") media.controls = true;
  else media.alt = f.name;
  media.src = base + "/asset/" + f.hash;
  media.onerror = () => {
    $("#viewer-media").replaceChildren(
      el("p", "此格式无法在浏览器预览，请下载原图。"),
    );
  };
  $("#viewer-media").replaceChildren(media);
  if (!$("#viewer").open) $("#viewer").showModal();
}
function importFiles(files) {
  stageFiles(files);
}
$("nav").onclick = guard(async (e) => {
  const b = e.target.closest("[data-view]");
  if (b) await switchView(b.dataset.view);
});
$("#import").onclick = $("#empty-import").onclick = () => $("#files").click();
$("#files").onchange = guard((e) => importFiles([...e.target.files]));
$("#refresh").onclick = guard(() => load(page));
$("#page-prev").onclick = guard(() => load(Math.max(0, page - 1)));
$("#page-next").onclick = guard(() => load(page + 1));
$("#page-size").onchange = guard(async () => {
  pageSize = Number($("#page-size").value);
  await load();
});
$("#page-form").onsubmit = guard(async (e) => {
  e.preventDefault();
  await load(
    Math.max(
      0,
      Math.min(
        Math.ceil(total / pageSize) - 1,
        Number($("#page-number").value) - 1,
      ),
    ),
  );
});
let timer;
$("#search").oninput = () => {
  clearTimeout(timer);
  timer = setTimeout(
    guard(() => load()),
    250,
  );
};
$("#clear").onclick = () => {
  selected.clear();
  renderGrid();
};
$("#send").onclick = guard(async () => {
  const b = $("#send");
  b.disabled = true;
  try {
    for (const hash of selected)
      await api("/send", { hash, device_id: $("#device").value });
    message("已加入发送队列。请在手机传输页开启接收，保存完成后显示已送达。");
    selected.clear();
    renderGrid();
  } finally {
    b.disabled = !devices.length;
  }
});
$("#add-album").onclick = guard(async () => {
  for (const hash of selected)
    await api("/albums/" + $("#album").value, { hash });
  message("已加入相册");
  selected.clear();
  renderGrid();
});
$("#close").onclick = () => $("#viewer").close();
$("#viewer").onclose = () => $("#viewer-media").replaceChildren();
$("#previous").onclick = () => openViewer(viewerIndex - 1);
$("#next").onclick = () => openViewer(viewerIndex + 1);
$("#viewer").onkeydown = (e) => {
  if (e.key === "ArrowLeft") openViewer(viewerIndex - 1);
  if (e.key === "ArrowRight") openViewer(viewerIndex + 1);
};
$("#favorite").onclick = guard(async () => {
  const f = items[viewerIndex];
  await api("/favorite/" + f.hash, { favorite: !f.favorite });
  f.favorite = !f.favorite;
  $("#favorite").textContent = f.favorite ? "取消收藏" : "收藏";
  renderGrid();
});
$("#cancel-album").onclick = () => $("#album-dialog").close();
$("#album-form").onsubmit = guard(async (e) => {
  e.preventDefault();
  const name = $("#album-name").value;
  const created = await api("/albums", { name });
  $("#album-dialog").close();
  $("#album-name").value = "";
  await switchView("albums");
  albumID = created.id;
  $("#heading").textContent = name;
  $("#photo-panel").hidden = false;
  $("#other-panel").hidden = true;
  await load();
});
for (const event of ["dragenter", "dragover"])
  document.addEventListener(event, (e) => {
    e.preventDefault();
    document.body.classList.add("dragging");
  });
document.addEventListener("dragleave", (e) => {
  if (!e.relatedTarget) document.body.classList.remove("dragging");
});
document.addEventListener(
  "drop",
  guard(async (e) => {
    e.preventDefault();
    document.body.classList.remove("dragging");
    await stageDroppedItems(e.dataTransfer);
  }),
);
guard(() => switchView("photos"))();

setInterval(
  guard(async () => {
    if (view === "transfers" && !document.hidden) await switchView("transfers");
  }),
  5000,
);
