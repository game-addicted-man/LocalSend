/* LocalDrop 폰 화면 */
(function () {
  "use strict";
  const { el, $ } = LD;

  const token = $('meta[name="ld-token"]').content;
  let pcName = $('meta[name="ld-pc"]').content;

  const store = {
    get(k) {
      try {
        return localStorage.getItem(k);
      } catch (e) {
        return null;
      }
    },
    set(k, v) {
      try {
        localStorage.setItem(k, v);
      } catch (e) {
        /* 사생활 보호 모드 등 */
      }
    },
  };

  function guessDeviceName() {
    const ua = navigator.userAgent;
    if (/iPhone/.test(ua)) return "iPhone";
    if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return "iPad";
    if (/Android/.test(ua)) {
      const m = /Android [\d.]+; ([^;)]+)/.exec(ua);
      const model = m ? m[1].replace(/ Build\/.*$/, "").trim() : "";
      if (model && model !== "K" && model.length > 1) {
        if (/^SM-/i.test(model)) return "갤럭시 " + model;
        return model.slice(0, 24);
      }
      return /Mobile/.test(ua) ? "안드로이드 폰" : "안드로이드 태블릿";
    }
    if (/Windows/.test(ua)) return "Windows PC";
    if (/Macintosh/.test(ua)) return "Mac";
    return "휴대폰";
  }

  let deviceId = store.get("ld.deviceId");
  if (!deviceId) {
    deviceId = LD.cid();
    store.set("ld.deviceId", deviceId);
  }
  let deviceName = store.get("ld.deviceName") || guessDeviceName();

  const isIOS = /iPhone|iPad|iPod/.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1);

  // 주소에 연결 코드를 남겨 둡니다. 즐겨찾기나 홈 화면 아이콘으로 다시 열 때 필요해요.
  if (new URLSearchParams(location.search).get("t") !== token) {
    history.replaceState(null, "", "/?t=" + encodeURIComponent(token));
  }

  function headers(extra) {
    return Object.assign(
      { "X-LD": "1", "X-LD-Token": token, "X-LD-Device-Id": deviceId, "X-LD-Device": encodeURIComponent(deviceName) },
      extra || {}
    );
  }

  async function api(path, body) {
    const res = await fetch(path, {
      method: body ? "POST" : "GET",
      headers: headers(body ? { "Content-Type": "application/json" } : null),
      body: body ? JSON.stringify(body) : undefined,
      cache: "no-store",
    });
    let data = null;
    try {
      data = await res.json();
    } catch (e) {
      data = null;
    }
    if (res.status === 401) {
      showReset();
      throw new Error("연결 코드가 바뀌었어요");
    }
    if (!res.ok) throw new Error((data && data.error) || `오류 ${res.status}`);
    return data;
  }

  const fileUrl = (it, inline) => `/api/file/${encodeURIComponent(it.id)}?t=${encodeURIComponent(token)}${inline ? "&inline=1" : ""}`;

  // ---------- 타임라인 ----------

  const tl = new LD.Timeline($("#timeline"), {
    isMine: (it) => it.from === "phone" && it.deviceId === deviceId,
    senderKey: (it) => (it.from === "pc" ? "pc" : "phone:" + (it.deviceId || it.device)),
    senderLabel: (it) => (it.from === "pc" ? pcName : it.device || "휴대폰"),
    fileUrl,
    inlineVideo: true,
    actions: itemActions,
    onOpenImage: openViewer,
    onEmpty: (empty) => ($("#empty").hidden = !empty),
  });

  function itemActions(it, mine) {
    const acts = [];
    if (it.kind === "text") {
      acts.push(
        el("button", {
          class: "text-btn", type: "button",
          onclick: async () => LD.toast((await LD.copyText(it.text)) ? "복사했어요" : "복사하지 못했어요. 글을 길게 눌러 복사해 주세요."),
        }, "복사")
      );
    } else if (it.available && !mine) {
      acts.push(el("a", { class: "text-btn", href: fileUrl(it, false), download: it.name }, "저장"));
    }
    return acts;
  }

  // ---------- 사진 크게 보기 ----------

  const viewer = $("#viewer");
  function openViewer(it) {
    $("#viewerImg").src = fileUrl(it, true);
    $("#viewerImg").alt = it.name;
    $("#viewerName").textContent = it.name;
    const save = $("#viewerSave");
    save.href = fileUrl(it, false);
    save.setAttribute("download", it.name);
    $(".viewer-tip").hidden = !isIOS;
    viewer.hidden = false;
    history.pushState({ viewer: true }, "");
  }
  function closeViewer() {
    if (viewer.hidden) return;
    viewer.hidden = true;
    $("#viewerImg").removeAttribute("src");
  }
  $("#viewerClose").addEventListener("click", () => history.back());
  window.addEventListener("popstate", closeViewer);

  // ---------- 보내기: 파일 ----------

  const queue = [];
  let pumping = false;

  function enqueue(files) {
    const list = Array.from(files || []);
    if (!list.length) return;
    let total = 0;
    for (const f of list) {
      total += f.size;
      const job = { cid: LD.cid(), file: f, up: null, canceled: false };
      queue.push(job);
      tl.setPending(job.cid, {
        cid: job.cid, mine: true, sender: "phone:" + deviceId, name: f.name || "file",
        total: f.size, loaded: 0, state: "wait", onCancel: () => cancelJob(job),
      });
    }
    if (total > 50 * 1024 * 1024) LD.toast("다 보낼 때까지 이 화면을 켜 두세요", 3500);
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
      const meter = LD.speedMeter();
      tl.updatePending(job.cid, { state: "run", loaded: 0, speed: 0 });
      const qs = new URLSearchParams({
        name: job.file.name || "file", size: String(job.file.size),
        mtime: String(job.file.lastModified || 0), cid: job.cid,
      });
      job.up = LD.upload("/api/upload?" + qs, job.file, headers(), (loaded, total) => {
        tl.updatePending(job.cid, { loaded, total, speed: meter(loaded) });
      });
      try {
        const item = await job.up.promise;
        if (item && item.id) tl.upsert(item);
        else tl.dropPending(job.cid);
      } catch (err) {
        if (err.aborted || job.canceled) {
          tl.dropPending(job.cid);
        } else {
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
    }
    pumping = false;
  }

  const photoInput = $("#photoInput");
  const fileInput = $("#fileInput");
  for (const input of [photoInput, fileInput]) {
    input.addEventListener("change", () => {
      enqueue(input.files);
      input.value = "";
    });
  }

  const sheet = $("#sheet");
  const plusBtn = $("#plusBtn");
  plusBtn.innerHTML = LD.icon("plus");
  $("#icoPhotos").innerHTML = LD.icon("image");
  $("#icoFiles").innerHTML = LD.icon("file");
  function setSheet(open) {
    sheet.hidden = !open;
    plusBtn.setAttribute("aria-expanded", String(open));
  }
  plusBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    setSheet(sheet.hidden);
  });
  document.addEventListener("click", (e) => {
    if (!sheet.hidden && !sheet.contains(e.target)) setSheet(false);
  });
  $("#pickPhotos").addEventListener("click", () => {
    setSheet(false);
    photoInput.click();
  });
  $("#pickFiles").addEventListener("click", () => {
    setSheet(false);
    fileInput.click();
  });
  $("#emptyPhotos").addEventListener("click", () => photoInput.click());
  $("#emptyFiles").addEventListener("click", () => fileInput.click());

  // ---------- 보내기: 글 ----------

  const ta = $("#text");
  const sendBtn = $("#sendBtn");
  sendBtn.innerHTML = LD.icon("up");
  function autosize() {
    ta.style.height = "auto";
    ta.style.height = Math.min(ta.scrollHeight + 2, 140) + "px";
  }
  function updateSend() {
    sendBtn.disabled = !ta.value.trim();
  }
  ta.addEventListener("input", () => {
    autosize();
    updateSend();
  });
  $("#composer").addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = ta.value;
    if (!text.trim()) return;
    ta.value = "";
    autosize();
    updateSend();
    try {
      const it = await api("/api/text", { text, cid: LD.cid() });
      tl.upsert(it);
    } catch (err) {
      ta.value = text;
      autosize();
      updateSend();
      LD.toast("보내지 못했어요. " + err.message);
    }
  });

  // ---------- 실시간 연결 ----------

  let es = null;
  let connected = false;
  let gone = false;

  function setPCName(name) {
    const changed = name && name !== pcName;
    pcName = name || pcName;
    if (changed) tl.rebuild();
    $("#pcName").textContent = connected ? pcName : "다시 연결하는 중…";
  }

  function setConn(on) {
    connected = on;
    $("#connDot").classList.toggle("on", on);
    $("#connBtn").classList.toggle("off", !on);
    $("#pcName").textContent = on ? pcName : "다시 연결하는 중…";
  }

  function connect() {
    if (gone) return;
    if (es) es.close();
    const qs = new URLSearchParams({ t: token, d: deviceId, n: deviceName });
    es = new EventSource("/api/events?" + qs);
    es.addEventListener("hello", (e) => {
      const st = JSON.parse(e.data);
      setConn(true);
      setPCName(st.pcName);
      tl.setItems(st.items || []);
      maybeShowHomeTip();
    });
    es.addEventListener("item", (e) => tl.upsert(JSON.parse(e.data)));
    es.addEventListener("remove", (e) => tl.remove(JSON.parse(e.data).id));
    es.addEventListener("clear", () => tl.clear());
    es.addEventListener("pcname", (e) => setPCName(JSON.parse(e.data).pcName));
    es.addEventListener("reset", () => showReset());
    es.onerror = () => {
      setConn(false);
      if (es.readyState === EventSource.CLOSED) {
        // 연결 코드가 바뀐 건지, 그냥 네트워크 문제인지 확인합니다.
        api("/api/state").then(() => setTimeout(connect, 1500), () => setTimeout(connect, 4000));
      }
    };
  }

  function showReset() {
    gone = true;
    if (es) es.close();
    $("#resetNotice").hidden = false;
  }

  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && (!es || es.readyState === EventSource.CLOSED)) connect();
  });

  // ---------- 연결 정보 · 이름 ----------

  const menu = $("#menuDlg");
  function homeHelpText() {
    if (isIOS) return "Safari 아래쪽의 공유 버튼을 누르고 '홈 화면에 추가'를 고르면, 다음부터 아이콘을 눌러 바로 열 수 있어요.";
    return "브라우저 메뉴(⋮)에서 '홈 화면에 추가'를 고르면, 다음부터 아이콘을 눌러 바로 열 수 있어요.";
  }
  $("#connBtn").addEventListener("click", () => {
    $("#menuStatus").textContent = connected
      ? `${pcName}에 연결되어 있어요. 이 화면이 열려 있는 동안 PC와 바로 주고받을 수 있어요.`
      : "PC와 연결이 끊겼어요. 폰과 PC가 같은 와이파이에 있는지, PC에서 LocalDrop이 켜져 있는지 확인해 주세요.";
    $("#nameInput").value = deviceName;
    $("#menuHomeHelp").textContent = homeHelpText();
    menu.showModal();
  });
  menu.querySelector("[data-close]").innerHTML = LD.icon("close");
  menu.querySelector("[data-close]").addEventListener("click", () => menu.close());
  menu.addEventListener("click", (e) => {
    if (e.target === menu) menu.close();
  });
  $("#nameSave").addEventListener("click", () => {
    const v = $("#nameInput").value.trim().slice(0, 30);
    deviceName = v || guessDeviceName();
    store.set("ld.deviceName", deviceName);
    menu.close();
    connect(); // 새 이름으로 다시 연결해서 PC에 알려요
    LD.toast("이름을 저장했어요");
  });

  function maybeShowHomeTip() {
    const standalone = window.matchMedia("(display-mode: standalone)").matches || navigator.standalone;
    if (standalone || store.get("ld.homeTipDone")) return;
    $("#homeTipText").textContent = homeHelpText();
    $("#homeTip").hidden = false;
  }
  $("#homeTipClose").innerHTML = LD.icon("close");
  $("#homeTipClose").addEventListener("click", () => {
    $("#homeTip").hidden = true;
    store.set("ld.homeTipDone", "1");
  });
  $("#viewerClose").innerHTML = LD.icon("close");

  LD.everyMinute(() => tl.render());
  setConn(false);
  $("#pcName").textContent = pcName;
  connect();
})();
