/* LocalDrop PC 화면 */
(function () {
  "use strict";
  const { el, $ } = LD;

  const admin = $('meta[name="ld-admin"]').content;
  let state = null;
  let selectedIP = null;
  let qrVersion = Date.now();
  let es = null;
  let quitting = false;

  async function api(path, body) {
    const res = await fetch(path, {
      method: body !== undefined ? "POST" : "GET",
      headers: { "X-LD-Admin": admin, "Content-Type": "application/json" },
      body: body !== undefined ? JSON.stringify(body) : undefined,
      cache: "no-store",
    });
    let data = null;
    try {
      data = await res.json();
    } catch (e) {
      data = null;
    }
    if (!res.ok) {
      const err = new Error((data && data.error) || `오류 ${res.status}`);
      err.status = res.status;
      throw err;
    }
    return data;
  }

  // ---------- 아이콘 채우기 ----------

  $("#copyAddr").innerHTML = LD.icon("copy");
  $("#saveDirIco").innerHTML = LD.icon("folder");
  $("#icoSettings").innerHTML = LD.icon("sliders");
  $("#icoHelp").innerHTML = LD.icon("help");
  $("#attachBtn").innerHTML = LD.icon("clip");
  $("#dropIco").innerHTML = LD.icon("up");
  $("#icoShield").innerHTML = LD.icon("shield");
  for (const b of document.querySelectorAll("[data-close]")) {
    b.innerHTML = LD.icon("close");
    b.addEventListener("click", () => b.closest("dialog").close());
  }
  for (const d of document.querySelectorAll("dialog")) {
    d.addEventListener("click", (e) => {
      if (e.target === d) d.close();
    });
  }

  // ---------- 타임라인 ----------

  const tl = new LD.Timeline($("#timeline"), {
    isMine: (it) => it.from === "pc",
    senderKey: (it) => (it.from === "pc" ? "pc" : "phone:" + (it.deviceId || it.device)),
    senderLabel: (it) => it.device || "휴대폰",
    fileUrl: (it, inline) => `/api/file/${encodeURIComponent(it.id)}${inline ? "?inline=1" : ""}`,
    inlineVideo: false,
    actions: itemActions,
    onOpenImage: (it) => openItem(it, "open"),
    onEmpty: (empty) => ($("#empty").hidden = !empty),
  });

  function itemActions(it) {
    const acts = [];
    const btn = (label, fn, cls, title) =>
      el("button", { class: "text-btn" + (cls ? " " + cls : ""), type: "button", onclick: fn, title }, label);
    if (it.kind === "text") {
      acts.push(btn("복사", () => copy(it.text)));
    } else if (it.available) {
      acts.push(btn("열기", () => openItem(it, "open")));
      if (!it.outbox) acts.push(btn("폴더에서 보기", () => openItem(it, "reveal")));
    }
    const removeTitle = it.from === "phone" && it.kind === "file" ? "파일은 저장 폴더에 그대로 두고 목록에서만 빼요" : undefined;
    acts.push(btn("목록에서 지우기", () => removeItem(it), "quiet", removeTitle));
    return acts;
  }

  async function copy(text) {
    let ok = await LD.copyText(text);
    if (!ok) {
      try {
        await api("/api/pc/copy", { text });
        ok = true;
      } catch (e) {
        ok = false;
      }
    }
    LD.toast(ok ? "복사했어요" : "복사하지 못했어요");
  }

  async function openItem(it, how) {
    try {
      await api("/api/pc/open", { id: it.id, how });
    } catch (e) {
      LD.toast(e.message);
    }
  }

  async function removeItem(it) {
    try {
      await api("/api/pc/remove", { id: it.id });
      tl.remove(it.id);
      if (it.from === "phone" && it.kind === "file") LD.toast("목록에서 뺐어요. 파일은 저장 폴더에 그대로 있어요.");
    } catch (e) {
      LD.toast(e.message);
    }
  }

  // ---------- 상태 그리기 ----------

  function applyState(st) {
    state = st;
    renderTicket();
    renderDevices();
    renderSettings();
    tl.setItems(st.items || []);
    for (const u of st.uploads || []) onProgress(u);
    $("#quitBtn").hidden = !st.browserMode;
    $("#sendToRow").hidden = !st.features.sendTo;
    $("#autoStartRow").hidden = !st.features.autoStart;
    $("#fwItem").querySelector(".help-act").hidden = !st.features.firewall;
    $("#verText").textContent = `LocalDrop ${st.version}`;
  }

  function renderTicket() {
    const addrs = state.addresses || [];
    $("#pcName").textContent = state.settings.pcName;
    const frame = $(".qr-frame");
    const img = $("#qr");
    const off = frame.querySelector(".qr-off");
    if (!addrs.length) {
      img.hidden = true;
      if (!off) frame.append(el("div", { class: "qr-off" }, "와이파이나 유선 네트워크에 연결되어 있지 않아요"));
      $("#addr").textContent = "네트워크 연결을 확인해 주세요";
      $("#addrPickWrap").hidden = true;
      $("#addrItem").hidden = true;
      return;
    }
    if (off) off.remove();
    img.hidden = false;
    if (!selectedIP || !addrs.some((a) => a.ip === selectedIP)) selectedIP = (addrs.find((a) => a.primary) || addrs[0]).ip;
    const a = addrs.find((x) => x.ip === selectedIP);
    const src = `/api/pc/qr.svg?ip=${encodeURIComponent(a.ip)}&v=${qrVersion}`;
    if (img.getAttribute("src") !== src) img.src = src;
    $("#addr").textContent = `${a.ip}:${state.port}`;
    $("#addr").title = a.url;

    const pick = $("#addrPick");
    $("#addrPickWrap").hidden = addrs.length < 2;
    $("#addrItem").hidden = addrs.length < 2;
    const sig = addrs.map((x) => x.ip + x.iface).join(",");
    if (pick.dataset.sig !== sig) {
      pick.replaceChildren(...addrs.map((x) => el("option", { value: x.ip }, `${x.ip}  (${x.iface})`)));
      pick.dataset.sig = sig;
    }
    pick.value = selectedIP;
  }

  $("#addrPick").addEventListener("change", (e) => {
    selectedIP = e.target.value;
    renderTicket();
  });

  $("#copyAddr").addEventListener("click", () => {
    const a = (state.addresses || []).find((x) => x.ip === selectedIP);
    if (a) copy(a.url);
  });

  function renderDevices() {
    const list = $("#deviceList");
    // 지금 연결된 기기와 최근 12시간 안에 쓴 기기만 보여 줘요.
    const recent = Date.now() - 12 * 3600 * 1000;
    const devs = (state.devices || []).filter((d) => d.online || new Date(d.lastSeen).getTime() > recent).slice(0, 6);
    if (!devs.length) {
      list.replaceChildren(el("li", { class: "none" }, "아직 없어요. 폰으로 QR을 찍으면 여기에 나타나요."));
      return;
    }
    list.replaceChildren(
      ...devs.map((d) =>
        el(
          "li",
          { class: d.online ? "on" : "" },
          el("span", { class: "dev-ico", icon: "phone" }),
          el(
            "span",
            { class: "dev-text" },
            el("span", { class: "dev-name" }, d.name || "휴대폰"),
            el("span", { class: "dev-state" }, d.online ? "연결됨" : "마지막 연결 " + LD.fmtAgo(new Date(d.lastSeen)))
          )
        )
      )
    );
  }

  function renderSettings() {
    const s = state.settings;
    const text = $("#saveDirText");
    text.replaceChildren(el("bdi", { dir: "ltr" }, s.saveDir));
    $("#openSaveDir").title = s.saveDir + "\n(눌러서 폴더 열기)";
    $("#setSaveDir").textContent = s.saveDir;
    if (document.activeElement !== $("#setPCName")) $("#setPCName").value = s.pcName;
    $("#setAutoCopy").checked = !!s.autoCopy;
    $("#setNotify").checked = !!s.notify;
    $("#setSendTo").checked = !!s.sendTo;
    $("#setAutoStart").checked = !!s.autoStart;
  }

  async function saveSettings(patch) {
    try {
      const st = await api("/api/pc/settings", patch);
      state.settings = st.settings;
      renderSettings();
      renderTicket();
      return true;
    } catch (e) {
      LD.toast(e.message, 3500);
      renderSettings();
      return false;
    }
  }

  // ---------- 설정 ----------

  $("#openSettings").addEventListener("click", () => {
    $("#manualDirRow").hidden = true;
    renderSettings();
    $("#settingsDlg").showModal();
  });
  $("#openHelp").addEventListener("click", () => $("#helpDlg").showModal());
  $("#openSaveDir").addEventListener("click", () => api("/api/pc/open", { how: "savedir" }).catch((e) => LD.toast(e.message)));

  $("#changeSaveDir").addEventListener("click", async () => {
    if (!state.features.pickFolder) {
      showManualDir();
      return;
    }
    try {
      const r = await api("/api/pc/pickfolder", {});
      if (r.path && (await saveSettings({ saveDir: r.path }))) LD.toast("저장 위치를 바꿨어요");
    } catch (e) {
      showManualDir();
    }
  });
  function showManualDir() {
    $("#manualDirRow").hidden = false;
    $("#manualDir").value = state.settings.saveDir;
    $("#manualDir").focus();
  }
  $("#manualDirSave").addEventListener("click", async () => {
    if (await saveSettings({ saveDir: $("#manualDir").value })) {
      $("#manualDirRow").hidden = true;
      LD.toast("저장 위치를 바꿨어요");
    }
  });

  $("#setPCName").addEventListener("change", (e) => saveSettings({ pcName: e.target.value }));
  $("#setPCName").addEventListener("keydown", (e) => {
    if (e.key === "Enter") e.target.blur();
  });
  $("#setAutoCopy").addEventListener("change", (e) => saveSettings({ autoCopy: e.target.checked }));
  $("#setNotify").addEventListener("change", (e) => saveSettings({ notify: e.target.checked }));
  $("#setSendTo").addEventListener("change", async (e) => {
    const on = e.target.checked;
    if (await saveSettings({ sendTo: on })) LD.toast(on ? "탐색기 '보내기' 메뉴에 넣었어요" : "'보내기' 메뉴에서 뺐어요");
  });

  $("#setAutoStart").addEventListener("change", async (e) => {
    const on = e.target.checked;
    if (await saveSettings({ autoStart: on })) LD.toast(on ? "Windows를 켤 때 자동으로 실행해요" : "자동 실행을 껐어요");
  });

  $("#resetPair").addEventListener("click", async () => {
    if (!confirm("새 QR 코드를 만들까요?\n지금 연결된 폰은 새 QR을 다시 찍어야 해요.")) return;
    try {
      const st = await api("/api/pc/reset", {});
      qrVersion = Date.now();
      state.addresses = st.addresses;
      state.devices = st.devices;
      renderTicket();
      renderDevices();
      LD.toast("새 QR 코드를 만들었어요");
    } catch (e) {
      LD.toast(e.message);
    }
  });

  $("#clearAll").addEventListener("click", async () => {
    if (!confirm("주고받은 기록을 모두 지울까요?\n저장 폴더에 받은 파일은 그대로 남아요.")) return;
    try {
      await api("/api/pc/clear", {});
      tl.clear();
      $("#settingsDlg").close();
    } catch (e) {
      LD.toast(e.message);
    }
  });

  $("#fixFirewall").addEventListener("click", async () => {
    try {
      LD.toast("관리자 권한을 묻는 창이 뜨면 '예'를 눌러 주세요", 4000);
      await api("/api/pc/firewall", {});
      LD.toast("방화벽에서 LocalDrop을 허용했어요. 폰에서 다시 열어 보세요.", 4500);
    } catch (e) {
      LD.toast(e.message, 4000);
    }
  });

  $("#quitBtn").addEventListener("click", async () => {
    if (!confirm("LocalDrop을 종료할까요? 종료하면 폰과 주고받을 수 없어요.")) return;
    quitting = true;
    try {
      await api("/api/pc/quit", {});
    } catch (e) {
      /* 이미 꺼졌을 수 있어요 */
    }
    if (es) es.close();
    $("#bye").hidden = false;
  });

  // ---------- 보내기: 글 ----------

  const ta = $("#text");
  const sendBtn = $("#sendBtn");
  function autosize() {
    ta.style.height = "auto";
    ta.style.height = Math.min(ta.scrollHeight, 180) + "px";
  }
  function updateSend() {
    sendBtn.disabled = !ta.value.trim();
  }
  ta.addEventListener("input", () => {
    autosize();
    updateSend();
  });
  ta.addEventListener("keydown", (e) => {
    // 한글 입력 중(조합 중)에 누른 Enter는 글자를 완성하는 용도라 보내지 않아요.
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing && e.keyCode !== 229) {
      e.preventDefault();
      $("#composer").requestSubmit();
    }
  });
  $("#composer").addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = ta.value;
    if (!text.trim()) return;
    ta.value = "";
    autosize();
    updateSend();
    try {
      tl.upsert(await api("/api/pc/text", { text, cid: LD.cid() }));
    } catch (err) {
      ta.value = text;
      autosize();
      updateSend();
      LD.toast("보내지 못했어요. " + err.message);
    }
  });

  // ---------- 보내기: 파일 ----------

  const queue = [];
  let pumping = false;

  function shareFiles(files) {
    const list = Array.from(files || []);
    if (!list.length) return;
    for (const f of list) {
      const job = { cid: LD.cid(), file: f, up: null, canceled: false };
      queue.push(job);
      tl.setPending(job.cid, {
        cid: job.cid, mine: true, sender: "pc", name: f.name || "file",
        total: f.size, loaded: 0, state: "wait", onCancel: () => cancelJob(job),
      });
    }
    pump();
  }

  function cancelJob(job) {
    job.canceled = true;
    if (job.up) job.up.abort();
    tl.dropPending(job.cid);
  }

  async function pump() {
    if (pumping) return;
    pumping = true;
    while (queue.length) {
      const job = queue.shift();
      if (job.canceled) continue;
      tl.updatePending(job.cid, { state: "run", loaded: 0 });
      const meter = LD.speedMeter();
      const qs = new URLSearchParams({
        name: job.file.name || "file", size: String(job.file.size),
        mtime: String(job.file.lastModified || 0), cid: job.cid,
      });
      job.up = LD.upload("/api/pc/share?" + qs, job.file, { "X-LD-Admin": admin }, (loaded, total) =>
        tl.updatePending(job.cid, { loaded, total, speed: meter(loaded) })
      );
      try {
        const item = await job.up.promise;
        if (item && item.id) tl.upsert(item);
        else tl.dropPending(job.cid);
      } catch (err) {
        if (err.aborted || job.canceled) tl.dropPending(job.cid);
        else
          tl.updatePending(job.cid, {
            state: "fail", error: err.message,
            onRetry: () => {
              job.canceled = false;
              tl.updatePending(job.cid, { state: "wait", loaded: 0, error: null });
              queue.push(job);
              pump();
            },
          });
      }
    }
    pumping = false;
  }

  $("#attachBtn").addEventListener("click", async () => {
    if (!state || !state.features.pickFiles) {
      $("#fileInput").click();
      return;
    }
    try {
      const r = await api("/api/pc/pick", {});
      for (const it of r.items || []) tl.upsert(it);
    } catch (e) {
      $("#fileInput").click(); // 선택 창을 못 열면 브라우저 방식으로
    }
  });
  $("#fileInput").addEventListener("change", (e) => {
    shareFiles(e.target.files);
    e.target.value = "";
  });

  // 창에 끌어다 놓기: 파일이 창에 들어오면 전체를 덮는 안내가 뜨고, 그 위에서 놓기를 받아요.
  const overlay = $("#dropOverlay");
  const hasFiles = (e) => e.dataTransfer && Array.from(e.dataTransfer.types || []).includes("Files");
  window.addEventListener("dragenter", (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    overlay.hidden = false;
  });
  window.addEventListener("dragover", (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
  });
  overlay.addEventListener("dragleave", (e) => {
    if (e.target === overlay) overlay.hidden = true;
  });
  window.addEventListener("drop", (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    overlay.hidden = true;
    const files = [];
    let folders = 0;
    const items = Array.from(e.dataTransfer.items || []);
    Array.from(e.dataTransfer.files).forEach((f, i) => {
      const entry = items[i] && items[i].webkitGetAsEntry ? items[i].webkitGetAsEntry() : null;
      if (entry && entry.isDirectory) folders++;
      else files.push(f);
    });
    if (folders) LD.toast("폴더는 클립 버튼으로 골라 보내거나, 탐색기 '보내기' 메뉴를 써 주세요", 4000);
    shareFiles(files);
  });

  // 캡처한 화면(클립보드 이미지)을 Ctrl+V로 바로 보내기
  document.addEventListener("paste", (e) => {
    const cd = e.clipboardData;
    const files = Array.from((cd && cd.files) || []);
    if (!files.length) return;
    // 엑셀 셀처럼 글과 그림이 함께 복사된 경우, 입력칸에서는 글로 붙여 넣어요.
    if (document.activeElement === ta && Array.from(cd.types || []).includes("text/plain")) return;
    e.preventDefault();
    const stamp = new Date();
    const pad = (n) => String(n).padStart(2, "0");
    const name = `캡처 ${stamp.getFullYear()}-${pad(stamp.getMonth() + 1)}-${pad(stamp.getDate())} ${pad(stamp.getHours())}${pad(stamp.getMinutes())}${pad(stamp.getSeconds())}`;
    shareFiles(
      files.map((f, i) =>
        /^image\.(png|jpe?g|gif|bmp|webp)$/i.test(f.name) ? new File([f], `${name}${i ? "-" + i : ""}.${LD.ext(f.name)}`, { type: f.type }) : f
      )
    );
  });

  // ---------- 폰에서 받는 중 ----------

  const endedUploads = new Set();
  function onProgress(u) {
    if (endedUploads.has(u.id)) return;
    tl.setPending("u:" + u.id, {
      cid: u.cid, mine: false, sender: "phone:" + (u.deviceId || u.device), label: u.device || "휴대폰",
      name: u.name, total: u.total, loaded: u.received, speed: u.speed, state: "run",
    });
  }

  // ---------- 실시간 연결 ----------

  function connect() {
    if (es) es.close();
    es = new EventSource("/api/pc/events");
    es.addEventListener("hello", async () => {
      try {
        applyState(await api("/api/pc/state"));
      } catch (e) {
        LD.toast(e.message);
      }
    });
    es.addEventListener("item", (e) => tl.upsert(JSON.parse(e.data)));
    es.addEventListener("remove", (e) => tl.remove(JSON.parse(e.data).id));
    es.addEventListener("clear", () => tl.clear());
    es.addEventListener("devices", (e) => {
      if (!state) return;
      state.devices = JSON.parse(e.data);
      renderDevices();
    });
    es.addEventListener("settings", (e) => {
      if (!state) return;
      state.settings = JSON.parse(e.data);
      renderSettings();
      renderTicket();
    });
    es.addEventListener("progress", (e) => onProgress(JSON.parse(e.data)));
    es.addEventListener("upload-end", (e) => {
      const u = JSON.parse(e.data);
      endedUploads.add(u.id);
      tl.dropPending("u:" + u.id);
    });
    es.onerror = () => {
      if (quitting) return;
      if (es.readyState === EventSource.CLOSED) setTimeout(connect, 2000);
    };
  }

  // 네트워크가 바뀌면(와이파이 변경 등) 주소와 QR을 새로 고칩니다.
  setInterval(async () => {
    if (!state || quitting || document.hidden) return;
    try {
      const st = await api("/api/pc/state");
      state.addresses = st.addresses;
      state.devices = st.devices;
      renderTicket();
      renderDevices();
    } catch (e) {
      /* 다음에 다시 */
    }
  }, 20000);
  LD.everyMinute(() => tl.render());

  connect();
  ta.focus();
})();
