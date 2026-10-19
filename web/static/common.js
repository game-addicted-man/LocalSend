/* LocalDrop 공통 스크립트 — PC 화면과 폰 화면이 함께 씁니다 */
(function () {
  "use strict";
  const LD = (window.LD = {});

  // ---------- DOM 도우미 ----------

  LD.$ = (sel, root) => (root || document).querySelector(sel);

  LD.el = function (tag, props, ...children) {
    const e = document.createElement(tag);
    if (props) {
      for (const [k, v] of Object.entries(props)) {
        if (v == null || v === false) continue;
        if (k === "class") e.className = v;
        else if (k === "dataset") Object.assign(e.dataset, v);
        else if (k === "icon") e.insertAdjacentHTML("afterbegin", LD.icon(v));
        else if (k.startsWith("on") && typeof v === "function") e.addEventListener(k.slice(2), v);
        else e.setAttribute(k, v === true ? "" : v);
      }
    }
    for (const c of children.flat()) {
      if (c == null || c === false) continue;
      e.append(c instanceof Node ? c : document.createTextNode(String(c)));
    }
    return e;
  };

  const ICONS = {
    clip: "M20.5 11.6l-8.3 8.3a5.2 5.2 0 0 1-7.4-7.4l8.6-8.6a3.5 3.5 0 0 1 4.9 4.9l-8.4 8.4a1.7 1.7 0 0 1-2.5-2.5l7.8-7.8",
    up: "M12 19V5M5.5 11.5L12 5l6.5 6.5",
    plus: "M12 5v14M5 12h14",
    copy: "M9 9.6C9 8.7 9.7 8 10.6 8h7.8c.9 0 1.6.7 1.6 1.6v8.8c0 .9-.7 1.6-1.6 1.6h-7.8c-.9 0-1.6-.7-1.6-1.6zM15 8V5.6c0-.9-.7-1.6-1.6-1.6H5.6C4.7 4 4 4.7 4 5.6v8.8c0 .9.7 1.6 1.6 1.6H9",
    folder: "M3 7.6C3 6.7 3.7 6 4.6 6h4.5l2 2.2h8.3c.9 0 1.6.7 1.6 1.6v8.6c0 .9-.7 1.6-1.6 1.6H4.6c-.9 0-1.6-.7-1.6-1.6z",
    download: "M12 4v11M7 10.5l5 5 5-5M5 20h14",
    close: "M6.5 6.5l11 11M17.5 6.5l-11 11",
    sliders: "M4 7h9M17 7h3M15 5v4M4 17h3M11 17h9M9 15v4",
    help: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM9.7 9.4a2.4 2.4 0 0 1 4.6 1c0 1.6-2.3 2-2.3 3.5M12 17.2v.1",
    image: "M4 6.6C4 5.7 4.7 5 5.6 5h12.8c.9 0 1.6.7 1.6 1.6v10.8c0 .9-.7 1.6-1.6 1.6H5.6c-.9 0-1.6-.7-1.6-1.6zM4 16l4.4-4.4 4 4 2.6-2.6L20 18M15.5 9.5v.1",
    file: "M14 3H7.6C6.7 3 6 3.7 6 4.6v14.8c0 .9.7 1.6 1.6 1.6h8.8c.9 0 1.6-.7 1.6-1.6V7zM14 3v4h4",
    more: "M5.5 12h.1M12 12h.1M18.5 12h.1",
    check: "M5 12.5l4.5 4.5L19 7.5",
    shield: "M12 3l7 3v5.2c0 4.4-3 8.1-7 9.8-4-1.7-7-5.4-7-9.8V6z",
    open: "M14 4h6v6M20 4l-8.5 8.5M18 14v4.4c0 .9-.7 1.6-1.6 1.6H5.6c-.9 0-1.6-.7-1.6-1.6V7.6C4 6.7 4.7 6 5.6 6H10",
    phone: "M8.6 3h6.8c.9 0 1.6.7 1.6 1.6v14.8c0 .9-.7 1.6-1.6 1.6H8.6c-.9 0-1.6-.7-1.6-1.6V4.6C7 3.7 7.7 3 8.6 3zM11 18h2",
  };

  LD.icon = function (name, cls) {
    const d = ICONS[name] || "";
    const sw = name === "more" ? ' stroke-width="3"' : "";
    return `<svg class="i ${cls || ""}" viewBox="0 0 24 24" aria-hidden="true"${sw}><path d="${d}"/></svg>`;
  };

  // ---------- 형식 ----------

  LD.fmtSize = function (n) {
    if (!(n >= 0)) return "";
    if (n < 1024) return n + "B";
    const units = ["KB", "MB", "GB", "TB"];
    let i = -1;
    do {
      n /= 1024;
      i++;
    } while (n >= 1024 && i < units.length - 1);
    const digits = n >= 100 ? 0 : n >= 10 ? 1 : 2;
    return String(Number(n.toFixed(digits))) + units[i];
  };

  LD.fmtTime = (d) => d.toLocaleTimeString("ko-KR", { hour: "numeric", minute: "2-digit" });

  const sameDay = (a, b) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
  const dayKey = (d) => `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`;
  const minuteKey = (d) => Math.floor(d.getTime() / 60000);

  LD.fmtDay = function (d) {
    const now = new Date();
    if (sameDay(d, now)) return "오늘";
    const y = new Date(now);
    y.setDate(now.getDate() - 1);
    if (sameDay(d, y)) return "어제";
    const wd = "일월화수목금토"[d.getDay()];
    const md = `${d.getMonth() + 1}월 ${d.getDate()}일 ${wd}요일`;
    return d.getFullYear() === now.getFullYear() ? md : `${d.getFullYear()}년 ${md}`;
  };

  LD.fmtAgo = function (d) {
    const s = (Date.now() - d.getTime()) / 1000;
    if (s < 60) return "방금";
    if (s < 3600) return `${Math.floor(s / 60)}분 전`;
    if (s < 86400) return `${Math.floor(s / 3600)}시간 전`;
    return `${Math.floor(s / 86400)}일 전`;
  };

  const EXT_KIND = {
    image: "jpg jpeg png gif webp heic heif avif bmp svg tif tiff ico",
    video: "mp4 m4v mov webm mkv avi 3gp wmv",
    audio: "mp3 m4a aac wav ogg flac amr wma",
    doc: "pdf doc docx hwp hwpx txt rtf md pages odt",
    sheet: "xls xlsx csv numbers ods",
    slide: "ppt pptx key odp",
    archive: "zip 7z rar tar gz tgz bz2 xz alz egg apk",
  };
  const KIND_BY_EXT = {};
  for (const [k, list] of Object.entries(EXT_KIND)) for (const e of list.split(" ")) KIND_BY_EXT[e] = k;

  LD.ext = function (name) {
    const m = /\.([^.]{1,8})$/.exec(name || "");
    return m ? m[1].toLowerCase() : "";
  };

  LD.kindOf = (name) => KIND_BY_EXT[LD.ext(name)] || "other";

  LD.extLabel = function (name) {
    const e = LD.ext(name);
    return e ? e.slice(0, 4).toUpperCase() : "FILE";
  };

  const PREVIEW = new Set(["jpg", "jpeg", "png", "gif", "webp", "avif", "bmp", "heic", "heif"]);
  const PLAYABLE = new Set(["mp4", "m4v", "mov", "webm"]);
  LD.canPreviewImage = (name) => PREVIEW.has(LD.ext(name));
  LD.canPlayVideo = (name) => PLAYABLE.has(LD.ext(name));

  // 글 안의 주소를 눌러서 열 수 있게 만듭니다 (innerHTML 없이 안전하게).
  LD.linkify = function (text) {
    const frag = document.createDocumentFragment();
    const re = /\b(https?:\/\/[^\s<>"']+|www\.[^\s<>"']+)/gi;
    let last = 0;
    let m;
    while ((m = re.exec(text))) {
      let url = m[0];
      const trail = /[.,!?;:)\]}'"…]+$/.exec(url);
      if (trail) url = url.slice(0, url.length - trail[0].length);
      if (m.index > last) frag.append(text.slice(last, m.index));
      const href = /^www\./i.test(url) ? "https://" + url : url;
      frag.append(LD.el("a", { href, target: "_blank", rel: "noopener noreferrer" }, url));
      last = m.index + url.length;
      re.lastIndex = last;
    }
    if (last < text.length) frag.append(text.slice(last));
    return frag;
  };

  // ---------- 복사 · 토스트 ----------

  LD.copyText = async function (text) {
    if (navigator.clipboard && window.isSecureContext) {
      try {
        await navigator.clipboard.writeText(text);
        return true;
      } catch (e) {
        /* 아래 방법으로 다시 시도 */
      }
    }
    // http 주소에서는 클립보드 API를 못 써서 예전 방식으로 복사합니다 (iOS 포함).
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.cssText = `position:absolute;left:-9999px;top:${window.pageYOffset || 0}px;font-size:12pt;opacity:0`;
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    ta.setSelectionRange(0, text.length);
    let ok = false;
    try {
      ok = document.execCommand("copy");
    } catch (e) {
      ok = false;
    }
    ta.remove();
    return ok;
  };

  let toastTimer = 0;
  LD.toast = function (msg, ms) {
    const t = document.getElementById("toast");
    if (!t) return;
    t.textContent = msg;
    t.hidden = false;
    t.classList.remove("show");
    void t.offsetWidth;
    t.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => (t.hidden = true), ms || 2000);
  };

  LD.cid = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 10);

  // ---------- 업로드 (진행률이 필요해서 XHR 사용) ----------

  LD.upload = function (url, file, headers, onProgress) {
    const xhr = new XMLHttpRequest();
    const promise = new Promise((resolve, reject) => {
      xhr.open("POST", url);
      for (const [k, v] of Object.entries(headers || {})) xhr.setRequestHeader(k, v);
      xhr.setRequestHeader("Content-Type", "application/octet-stream");
      xhr.upload.onprogress = (e) => {
        if (onProgress) onProgress(e.loaded, e.lengthComputable ? e.total : file.size);
      };
      xhr.onload = () => {
        let data = null;
        try {
          data = JSON.parse(xhr.responseText);
        } catch (e) {
          data = null;
        }
        if (xhr.status >= 200 && xhr.status < 300) resolve(data);
        else reject(new Error((data && data.error) || `오류 ${xhr.status}`));
      };
      xhr.onerror = () => reject(new Error("연결이 끊겨서 보내지 못했어요"));
      xhr.onabort = () => reject(Object.assign(new Error("취소했어요"), { aborted: true }));
      xhr.send(file);
    });
    return { promise, abort: () => xhr.abort() };
  };

  // 진행률과 속도를 계산합니다.
  LD.speedMeter = function () {
    let lastAt = performance.now();
    let lastBytes = 0;
    let speed = 0;
    return function (loaded) {
      const now = performance.now();
      const dt = (now - lastAt) / 1000;
      if (dt >= 0.5) {
        const inst = (loaded - lastBytes) / dt;
        speed = speed ? speed * 0.6 + inst * 0.4 : inst;
        lastAt = now;
        lastBytes = loaded;
      }
      return speed;
    };
  };

  // ---------- 타임라인 ----------
  //
  // opts: isMine(it), senderKey(it), senderLabel(it), fileUrl(it, inline), actions(it, mine),
  //       onOpenImage(it), inlineVideo, onEmpty(isEmpty)

  class Timeline {
    constructor(root, opts) {
      this.root = root;
      this.opts = opts;
      this.items = new Map();
      this.pending = new Map();
      this.nodes = new Map();
      this.stick = true;
      root.addEventListener("scroll", () => (this.stick = this.nearBottom()), { passive: true });
    }

    nearBottom() {
      const r = this.root;
      return r.scrollHeight - r.scrollTop - r.clientHeight < 90;
    }

    scrollToBottom() {
      this.root.scrollTop = this.root.scrollHeight;
      this.stick = true;
    }

    setItems(list) {
      this.items.clear();
      for (const it of list) this.items.set(it.id, it);
      this.dropMatchedPending();
      this.render(true);
    }

    upsert(it) {
      this.items.set(it.id, it);
      const forceBottom = this.dropMatchedPending() || this.opts.isMine(it);
      this.render(forceBottom);
    }

    remove(id) {
      if (this.items.delete(id)) this.render();
    }

    clear() {
      this.items.clear();
      this.render();
    }

    has(id) {
      return this.items.has(id);
    }

    // 보낸 사람 이름처럼 말풍선 밖의 정보가 바뀌었을 때 모두 다시 그립니다.
    rebuild() {
      this.nodes.clear();
      this.root.replaceChildren();
      this.render();
    }

    // 서버에 도착한 항목과 같은 cid의 임시 말풍선(전송 중)을 지웁니다.
    dropMatchedPending() {
      let dropped = false;
      if (!this.pending.size) return false;
      const cids = new Set();
      for (const it of this.items.values()) if (it.cid) cids.add(it.cid);
      for (const [key, p] of this.pending) {
        if (p.cid && cids.has(p.cid)) {
          this.pending.delete(key);
          dropped = true;
        }
      }
      return dropped;
    }

    setPending(key, data) {
      const prev = this.pending.get(key);
      const p = Object.assign({ key, time: new Date(), loaded: 0, total: 0, state: "run" }, prev || {}, data);
      this.pending.set(key, p);
      const node = this.nodes.get("p:" + key);
      if (prev && node && prev.state === p.state) {
        this.paintPending(node, p);
        return;
      }
      this.render(p.mine);
    }

    // 이미 있는 임시 말풍선만 고칩니다. (다 보낸 뒤 늦게 도착한 진행률 알림이 말풍선을 되살리지 않게)
    updatePending(key, data) {
      if (this.pending.has(key)) this.setPending(key, data);
    }

    dropPending(key) {
      if (this.pending.delete(key)) this.render();
    }

    hasPending(key) {
      return this.pending.has(key);
    }

    render(forceBottom) {
      const wasBottom = forceBottom || this.stick;
      const entries = [];
      for (const it of this.items.values()) {
        entries.push({ key: "i:" + it.id, it, time: new Date(it.time), mine: this.opts.isMine(it), sender: this.opts.senderKey(it) });
      }
      for (const p of this.pending.values()) {
        entries.push({ key: "p:" + p.key, p, time: p.time, mine: p.mine, sender: p.sender });
      }
      entries.sort((a, b) => a.time - b.time);

      const want = [];
      const keep = new Set();
      let prev = null;
      for (let i = 0; i < entries.length; i++) {
        const e = entries[i];
        const next = entries[i + 1];
        if (!prev || !sameDay(prev.time, e.time)) {
          const dk = "d:" + dayKey(e.time);
          let dn = this.nodes.get(dk);
          const label = LD.fmtDay(e.time);
          if (!dn) {
            dn = LD.el("div", { class: "day", role: "separator" }, label);
            this.nodes.set(dk, dn);
          } else if (dn.textContent !== label) dn.textContent = label;
          keep.add(dk);
          want.push(dn);
        }
        const groupStart = !prev || prev.sender !== e.sender || !sameDay(prev.time, e.time) || e.time - prev.time > 5 * 60 * 1000;
        const showTime = !next || next.sender !== e.sender || minuteKey(next.time) !== minuteKey(e.time);
        const sig = e.it ? this.itemSig(e.it, e.mine) : "p:" + e.p.state;
        let node = this.nodes.get(e.key);
        if (!node || node._sig !== sig) {
          node = e.it ? this.buildItem(e.it, e.mine) : this.buildPending(e.p);
          node._sig = sig;
          this.nodes.set(e.key, node);
        }
        node.classList.toggle("group-start", groupStart);
        node.classList.toggle("show-time", showTime);
        keep.add(e.key);
        want.push(node);
        prev = e;
      }
      for (const k of [...this.nodes.keys()]) if (!keep.has(k)) this.nodes.delete(k);

      // 이미 제자리에 있는 노드는 건드리지 않아요 (재생 중인 동영상이 멈추지 않게).
      const root = this.root;
      let cur = root.firstChild;
      for (const n of want) {
        if (cur === n) {
          cur = cur.nextSibling;
          continue;
        }
        root.insertBefore(n, cur);
      }
      while (cur) {
        const nx = cur.nextSibling;
        root.removeChild(cur);
        cur = nx;
      }
      if (this.opts.onEmpty) this.opts.onEmpty(entries.length === 0);
      if (wasBottom) this.scrollToBottom();
    }

    itemSig(it, mine) {
      return [mine, it.kind, it.name, it.size, it.available, it.expired, it.text && it.text.length, it.device].join("|");
    }

    buildItem(it, mine) {
      const row = LD.el("div", { class: "row " + (mine ? "mine" : "theirs"), dataset: { id: it.id } });
      row.append(LD.el("div", { class: "who" }, this.opts.senderLabel(it)));
      const line = LD.el("div", { class: "line" }, this.buildBubble(it, mine), LD.el("time", { class: "time" }, LD.fmtTime(new Date(it.time))));
      row.append(line);
      const acts = this.opts.actions(it, mine);
      if (acts && acts.length) row.append(LD.el("div", { class: "acts" }, acts));
      return row;
    }

    buildBubble(it, mine) {
      if (it.kind === "text") {
        const b = LD.el("div", { class: "bubble text" });
        b.append(LD.linkify(it.text || ""));
        return b;
      }
      const kind = LD.kindOf(it.name);
      if (it.available && kind === "image" && LD.canPreviewImage(it.name)) {
        const img = LD.el("img", { src: this.opts.fileUrl(it, true), alt: it.name, loading: "lazy", decoding: "async" });
        const fig = LD.el(
          "figure",
          { class: "bubble media" },
          LD.el("div", { class: "media-box" }, img),
          LD.el("figcaption", { class: "media-cap" }, LD.el("span", { class: "fname", title: it.name }, it.name), LD.el("span", { class: "fmeta" }, LD.fmtSize(it.size)))
        );
        img.addEventListener("load", () => {
          if (this.stick) this.scrollToBottom();
        });
        img.addEventListener("error", () => fig.replaceWith(this.fileBubble(it, kind)), { once: true });
        img.addEventListener("click", () => this.opts.onOpenImage && this.opts.onOpenImage(it));
        return fig;
      }
      if (it.available && kind === "video" && this.opts.inlineVideo && LD.canPlayVideo(it.name)) {
        const v = LD.el("video", { src: this.opts.fileUrl(it, true), controls: true, playsinline: true, preload: "metadata" });
        const fig = LD.el(
          "figure",
          { class: "bubble media" },
          LD.el("div", { class: "media-box" }, v),
          LD.el("figcaption", { class: "media-cap" }, LD.el("span", { class: "fname", title: it.name }, it.name), LD.el("span", { class: "fmeta" }, LD.fmtSize(it.size)))
        );
        v.addEventListener("loadedmetadata", () => {
          if (this.stick) this.scrollToBottom();
        });
        v.addEventListener("error", () => fig.replaceWith(this.fileBubble(it, kind)), { once: true });
        return fig;
      }
      return this.fileBubble(it, kind);
    }

    fileBubble(it, kind) {
      const meta = LD.el("div", { class: "fmeta" }, LD.fmtSize(it.size));
      if (!it.available) {
        meta.append(LD.el("span", { class: "gone-note" }, it.expired ? "만료됨" : "파일 없음"));
      }
      return LD.el(
        "div",
        { class: "bubble file" + (it.available ? "" : " gone") },
        LD.el("span", { class: "ftype", dataset: { k: kind } }, LD.extLabel(it.name)),
        LD.el("div", { class: "fbody" }, LD.el("div", { class: "fname", title: it.name }, it.name), meta)
      );
    }

    buildPending(p) {
      const row = LD.el("div", { class: "row " + (p.mine ? "mine" : "theirs") });
      row.append(LD.el("div", { class: "who" }, p.label || ""));
      const bar = LD.el("i");
      const meta = LD.el("div", { class: "fmeta prog" });
      const bubble = LD.el(
        "div",
        { class: "bubble file" },
        LD.el("span", { class: "ftype", dataset: { k: LD.kindOf(p.name) } }, LD.extLabel(p.name)),
        LD.el("div", { class: "fbody" }, LD.el("div", { class: "fname", title: p.name }, p.name), meta, LD.el("div", { class: "progress" }, bar))
      );
      row.append(LD.el("div", { class: "line" }, bubble, LD.el("time", { class: "time" }, LD.fmtTime(p.time))));
      const acts = [];
      if ((p.state === "run" || p.state === "wait") && p.onCancel) acts.push(LD.el("button", { class: "text-btn quiet", type: "button", onclick: p.onCancel }, "취소"));
      if (p.state === "fail" && p.onRetry) acts.push(LD.el("button", { class: "text-btn", type: "button", onclick: p.onRetry }, "다시 보내기"));
      if (p.state === "fail") acts.push(LD.el("button", { class: "text-btn quiet", type: "button", onclick: () => this.dropPending(p.key) }, "지우기"));
      if (acts.length) row.append(LD.el("div", { class: "acts" }, acts));
      row._bar = bar;
      row._meta = meta;
      this.paintPending(row, p);
      return row;
    }

    paintPending(row, p) {
      const pct = p.total > 0 ? Math.min(100, (p.loaded / p.total) * 100) : 0;
      row._bar.style.width = (p.state === "fail" ? 0 : pct).toFixed(1) + "%";
      const m = row._meta;
      m.replaceChildren();
      if (p.state === "fail") {
        m.append(LD.el("span", { class: "status-fail" }, p.error || "보내지 못했어요"));
        return;
      }
      if (p.state === "wait") {
        m.append(LD.el("span", null, "기다리는 중"), LD.el("span", null, LD.fmtSize(p.total)));
        return;
      }
      m.append(LD.el("span", null, Math.floor(pct) + "%"), LD.el("span", null, `${LD.fmtSize(p.loaded)} / ${LD.fmtSize(p.total)}`));
      if (p.speed > 0) m.append(LD.el("span", null, LD.fmtSize(p.speed) + "/s"));
    }
  }

  LD.Timeline = Timeline;

  // 날짜가 바뀌면(자정) '오늘/어제' 표시를 고칩니다.
  LD.everyMinute = function (fn) {
    setInterval(fn, 60 * 1000);
  };
})();
