// Snapshot manager — loaded for moder+ roles
(function () {
    "use strict";

    const IS_ADMIN = document.body.dataset.admin === "1";

    let snapModal = null;
    let snapWS = null;
    let currentRunId = null; // which run is shown in detail view
    let cardMap = new Map(); // cameraId → .snap-card element
    let snapResultData = new Map(); // cameraId → raw data object
    let snapFilter = { status: "all", type: "all", hasName: false, generatedOnly: false };

    document.getElementById("snapshot-btn").addEventListener("click", openModal);

    // ─── ESC to close ────────────────────────────────────────────────────────

    // keydown fires before map.js's keyup → viewer is still "open" when we check
    function onKeyDown(e) {
        if (e.key !== "Escape" || !snapModal) return;
        const viewer = document.getElementById("image-viewer");
        if (viewer && viewer.classList.contains("open")) return; // map.js will close it on keyup
        closeModal();
    }

    // ─── Modal lifecycle ──────────────────────────────────────────────────────

    function openModal() {
        if (snapModal) {
            snapModal.style.display = "";
            return;
        }
        buildModalShell();
        showRunList();
        document.addEventListener("keydown", onKeyDown);
    }

    function closeModal() {
        document.removeEventListener("keydown", onKeyDown);
        disconnectWS();
        snapModal?.remove();
        snapModal = null;
        currentRunId = null;
        cardMap.clear();
        snapResultData.clear();
    }

    // ─── Shell (always-visible parts: header + footer) ────────────────────────

    function buildModalShell() {
        const box = document.createElement("div");
        box.id = "snapshot-modal";
        box.className = "show-box show-box--large";

        // Head bar — title + status text
        const head = document.createElement("div");
        head.className = "snap-head";

        const titleEl = document.createElement("span");
        titleEl.className = "snap-head-title";
        titleEl.textContent = "Снапшоты";
        head.appendChild(titleEl);

        const statusEl = document.createElement("span");
        statusEl.id = "snap-head-status";
        statusEl.className = "snap-head-status";
        head.appendChild(statusEl);

        box.appendChild(head);

        // Scrollable body
        const body = document.createElement("div");
        body.id = "snap-body";
        body.className = "snap-body";
        box.appendChild(body);

        // Footer
        const foot = document.createElement("div");
        foot.className = "show-box-foot";
        if (IS_ADMIN) {
            const dlBtn = document.createElement("button");
            dlBtn.id = "snap-dl-btn";
            dlBtn.className = "show-box-close";
            dlBtn.style.display = "none";
            dlBtn.innerHTML =
                `<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">` +
                `<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>` +
                `<polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>` +
                `</svg>CSV`;
            dlBtn.addEventListener("click", () => {
                window.location.href = currentRunId
                    ? `/admin/snapshot/download?runId=${currentRunId}`
                    : "/admin/snapshot/download";
            });
            foot.appendChild(dlBtn);
        }
        const closeBtn = document.createElement("button");
        closeBtn.className = "show-box-close";
        closeBtn.innerHTML =
            `<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">` +
            `<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>` +
            `</svg>Закрыть`;
        closeBtn.addEventListener("click", closeModal);
        foot.appendChild(closeBtn);
        box.appendChild(foot);

        document.body.appendChild(box);
        snapModal = box;
    }

    // ─── View: Run List ───────────────────────────────────────────────────────

    async function showRunList(skipAutoOpen = false) {
        currentRunId = null;
        cardMap.clear();
        snapResultData.clear();
        disconnectWS();

        // Remove detail header and filter bar placed outside snap-body
        snapModal?.querySelector(".snap-detail-head")?.remove();
        snapModal?.querySelector(".snap-filter-bar")?.remove();

        setHeadStatus("");
        setDlBtnVisible(false);

        const body = document.getElementById("snap-body");
        if (!body) return;
        body.innerHTML = "";
        body.appendChild(buildRunListSkeleton());

        let runs = [];
        try {
            const resp = await fetch("/api/snapshot/runs");
            runs = await resp.json();
        } catch { runs = []; }

        const activeRun = Array.isArray(runs) ? runs.find(r => r.status === "running") : null;

        body.innerHTML = "";

        // Admin: new-run form at top
        if (IS_ADMIN) {
            const form = document.createElement("div");
            form.className = "snap-new-form";
            form.innerHTML =
                `<label>Потоков:</label>` +
                `<input id="snap-workers-input" class="snap-workers-input" type="number" min="1" max="20" value="1" />` +
                `<select id="snap-filter-select" class="snap-filter-select">` +
                `<option value="known">Dahua и RTSP</option>` +
                `<option value="unknown">Неизвестные</option>` +
                `<option value="all">Все камеры</option>` +
                `</select>` +
                `<button class="show-box-close" id="snap-start-btn">` +
                `<svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"/></svg>Старт</button>`;
            const startBtn = form.querySelector("#snap-start-btn");
            startBtn.addEventListener("click", () => withBusy(startBtn, startNewRun));
            body.appendChild(form);

            if (activeRun) {
                const stopWrap = document.createElement("div");
                stopWrap.style.cssText = "display:flex;align-items:center;gap:6px;padding:2px 0 6px";
                stopWrap.innerHTML =
                    `<span class="snap-running-dot"></span>` +
                    `<span style="font-size:13px;color:var(--t2)">Запуск #${activeRun.id} выполняется</span>` +
                    `<button class="show-box-close" id="snap-stop-btn">` +
                    `<svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor">` +
                    `<rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg>Стоп</button>`;
                const stopBtnList = stopWrap.querySelector("#snap-stop-btn");
                stopBtnList.addEventListener("click", () => withBusy(stopBtnList, stopRun));
                body.appendChild(stopWrap);
            }
        }

        if (!Array.isArray(runs) || runs.length === 0) {
            const empty = document.createElement("div");
            empty.style.cssText = "padding:16px 2px;font-size:13px;color:var(--t3)";
            empty.textContent = "Запусков пока нет";
            body.appendChild(empty);
            return;
        }

        const list = document.createElement("div");
        list.className = "snap-runs-list";
        for (const run of runs) {
            list.appendChild(buildRunListCard(run));
        }
        body.appendChild(list);

        // Auto-open active run detail (skip when navigating back from detail view)
        if (activeRun && !skipAutoOpen) {
            await showRunDetail(activeRun.id, true);
        }
    }

    function filterLabel(f) {
        return f === "all" ? "Все камеры" : f === "unknown" ? "Неизвестные" : "Dahua и RTSP";
    }

    function buildRunListCard(run) {
        const card = document.createElement("div");
        card.className = "snap-run-card" + (run.status === "running" ? " snap-run-card--active" : "");
        card.addEventListener("click", () => showRunDetail(run.id, true));

        const statusBadge = run.status === "running"
            ? `<span class="snap-running-dot"></span><span class="snap-run-status-text">выполняется</span>`
            : run.status === "finished"
            ? `<span class="status-badge status-added">завершён</span>`
            : run.status === "stopped"
            ? `<span class="status-badge status-duplicate">остановлен</span>`
            : `<span class="status-badge">${escHtml(run.status)}</span>`;

        const date = new Date(run.createdAt).toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" });
        const workers = run.workers > 1 ? ` · ${run.workers} потока` : "";

        card.innerHTML =
            `<div class="snap-run-card-head">` +
            statusBadge +
            `<span class="snap-run-date">${date}</span>` +
            `<span class="snap-run-filter">${escHtml(filterLabel(run.filter))}${workers}</span>` +
            `</div>` +
            `<div class="snap-run-card-stats">` +
            `<span>${run.processedCount} / ${run.totalCameras} камер</span>` +
            `<span class="ok">✓ ${run.successCount}</span>` +
            `<span class="err">✗ ${run.errorCount}</span>` +
            `</div>`;

        return card;
    }

    // ─── View: Run Detail ─────────────────────────────────────────────────────

    async function showRunDetail(runId, showBackBtn) {
        currentRunId = runId;
        cardMap.clear();
        disconnectWS();
        setDlBtnVisible(IS_ADMIN);

        // Remove any previous detail header
        snapModal?.querySelector(".snap-detail-head")?.remove();

        const body = document.getElementById("snap-body");
        if (!body) return;
        body.innerHTML = "";
        body.appendChild(buildCamCardsSkeleton());

        let report;
        try {
            const resp = await fetch(`/api/snapshot/report?runId=${runId}`);
            report = await resp.json();
        } catch {
            body.innerHTML = `<div style="padding:8px;color:var(--danger);font-size:13px">Ошибка загрузки</div>`;
            return;
        }

        body.innerHTML = "";

        // Detail header — inserted BEFORE snap-body so it doesn't scroll
        const detHead = document.createElement("div");
        detHead.className = "snap-detail-head";

        if (showBackBtn) {
            const backBtn = document.createElement("button");
            backBtn.className = "show-box-close";
            backBtn.innerHTML =
                `<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">` +
                `<polyline points="15 18 9 12 15 6"/></svg>Назад`;
            backBtn.addEventListener("click", () => withBusy(backBtn, () => showRunList(true)));
            detHead.appendChild(backBtn);
        }

        const date = new Date(report.createdAt || 0).toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" });
        const info = document.createElement("span");
        info.className = "snap-detail-info";
        info.textContent = `#${runId} · ${date} · ${filterLabel(report.filter || "")} · ${report.workers || 1} поток(а)`;
        detHead.appendChild(info);

        const progressEl = document.createElement("span");
        progressEl.id = "snap-detail-progress";
        progressEl.className = "snap-detail-progress";
        updateDetailProgress(progressEl, report);
        detHead.appendChild(progressEl);

        if (IS_ADMIN) {
            if (report.status === "stopped") {
                appendResumeControls(detHead, runId);
            }
            if (report.status === "running") {
                const stopBtn = document.createElement("button");
                stopBtn.id = "snap-stop-btn-detail";
                stopBtn.className = "show-box-close";
                stopBtn.innerHTML =
                    `<svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor">` +
                    `<rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg>Стоп`;
                stopBtn.addEventListener("click", () => withBusy(stopBtn, stopRun));
                detHead.appendChild(stopBtn);
            }
        }

        // Place header outside snap-body (before it in the modal flex column)
        body.parentNode.insertBefore(detHead, body);

        // Reset filter state when entering a new detail view
        snapFilter = { status: "all", type: "all", hasName: false, generatedOnly: false };

        // Filter bar — outside snap-body so it doesn't scroll
        const filterBar = buildFilterBar();
        body.parentNode.insertBefore(filterBar, body);

        // Camera cards
        const cardsList = document.createElement("div");
        cardsList.id = "snap-cards-list";
        cardsList.className = "snap-cards-list";
        body.appendChild(cardsList);

        if (Array.isArray(report.results)) {
            for (const r of report.results) {
                upsertCard(r);
            }
        }

        // Update filter count after initial render
        applyAllFilters();

        if (report.status === "running") {
            connectWS(progressEl);
        }

        updateHeadStatusFromReport(report);
    }

    // Resume controls: workers input + Продолжить button
    function appendResumeControls(container, runId) {
        const wrap = document.createElement("span");
        wrap.className = "snap-resume-controls";
        wrap.innerHTML =
            `<label style="font-size:12px;color:var(--t2)">Потоков:</label>` +
            `<input id="snap-resume-workers" class="snap-workers-input" type="number" min="1" max="20" value="1" />`;
        const resumeBtn = document.createElement("button");
        resumeBtn.className = "show-box-close snap-resume-btn";
        resumeBtn.textContent = "Продолжить";
        resumeBtn.addEventListener("click", () => withBusy(resumeBtn, () => resumeRun(runId)));
        wrap.appendChild(resumeBtn);
        container.appendChild(wrap);
    }

    function updateDetailProgress(el, report) {
        if (!el) return;
        if (report.status === "running") {
            el.innerHTML = `<span class="snap-running-dot"></span>${report.processed || 0}/${report.total || "?"}`;
        } else if (report.status === "finished") {
            el.textContent = `Завершён: ${report.processed || 0} / ${report.total || 0}`;
        } else if (report.status === "stopped") {
            el.textContent = `Остановлен: ${report.processed || 0} / ${report.total || 0}`;
        }
    }

    function updateHeadStatusFromReport(report) {
        if (report.status === "running") {
            setHeadStatus(`<span class="snap-running-dot"></span>${report.processed || 0}/${report.total || "?"}`);
        } else {
            setHeadStatus("");
        }
    }

    function setHeadStatus(html) {
        const el = document.getElementById("snap-head-status");
        if (el) el.innerHTML = html;
    }

    function setDlBtnVisible(visible) {
        const btn = document.getElementById("snap-dl-btn");
        if (btn) btn.style.display = visible ? "" : "none";
    }

    // ─── WebSocket ────────────────────────────────────────────────────────────

    function connectWS(progressEl) {
        if (snapWS) return;
        const proto = location.protocol === "https:" ? "wss" : "ws";
        const ws = new WebSocket(`${proto}://${location.host}/ws/snapshot`);
        ws.onmessage = (e) => {
            try { handleWSEvent(JSON.parse(e.data), progressEl); } catch {}
        };
        ws.onerror = () => {};
        ws.onclose = () => { snapWS = null; };
        snapWS = ws;
    }

    function disconnectWS() {
        if (snapWS) { snapWS.close(); snapWS = null; }
    }

    function handleWSEvent(evt, progressEl) {
        if (evt.type === "ping") return;

        if (evt.type === "progress" && evt.runId === currentRunId) {
            upsertCard(evt);
            const countEl = document.getElementById("sflt-count");
            if (countEl) {
                let shown = 0;
                snapResultData.forEach((_d, id) => { if (cardMap.get(id)?.style.display !== "none") shown++; });
                countEl.textContent = `${shown} из ${snapResultData.size}`;
            }
            const txt = `<span class="snap-running-dot"></span>${evt.processed || 0}/${evt.total || "?"}`;
            if (progressEl) progressEl.innerHTML = txt;
            setHeadStatus(txt);
        } else if (evt.type === "statusChange") {
            const done = (evt.success || 0) + (evt.errors || 0);
            if (progressEl) {
                progressEl.textContent = evt.status === "finished"
                    ? `Завершён: ${done} / ${evt.total || 0}`
                    : `Остановлен: ${done} / ${evt.total || 0}`;
            }
            setHeadStatus("");

            const stopBtn = document.getElementById("snap-stop-btn-detail");
            if (stopBtn) stopBtn.remove();

            if (IS_ADMIN && evt.status === "stopped" && currentRunId) {
                const detHead = document.querySelector(".snap-detail-head");
                if (detHead && !detHead.querySelector(".snap-resume-controls")) {
                    appendResumeControls(detHead, currentRunId);
                }
            }
        }
    }

    // ─── Camera card ──────────────────────────────────────────────────────────

    function upsertCard(data) {
        if (!data || !data.cameraId) return;
        const list = document.getElementById("snap-cards-list");
        if (!list) return;

        snapResultData.set(data.cameraId, data);

        let card = cardMap.get(data.cameraId);
        if (!card) {
            card = document.createElement("div");
            card.className = "snap-card";
            list.appendChild(card);
            cardMap.set(data.cameraId, card);
        }
        applyCardFilter(card, data);

        // Status col
        let statusEl = card.querySelector(".snap-card-status");
        if (!statusEl) {
            statusEl = document.createElement("div");
            statusEl.className = "snap-card-status";
            card.appendChild(statusEl);
        }
        const done  = data.channelsDone  || 0;
        const found = data.channelsFound || 0;
        const isPartial = !data.errorType && done > 0 && done < found;
        statusEl.innerHTML = data.errorType
            ? `<span class="status-badge status-error">${escHtml(errLabel(data.errorType))}</span>`
            : `<span class="status-badge ${isPartial ? "status-duplicate" : "status-added"}">${done}/${found}</span>`;

        // Info col
        let infoEl = card.querySelector(".snap-card-info");
        if (!infoEl) {
            infoEl = document.createElement("div");
            infoEl.className = "snap-card-info";
            card.appendChild(infoEl);
        }
        infoEl.innerHTML = buildInfoHTML(data);
        wireChErrors(infoEl);

        // Photos col
        let photosEl = card.querySelector(".snap-photos-container");
        if (!photosEl) {
            photosEl = document.createElement("div");
            photosEl.className = "snap-photos-container";
            card.appendChild(photosEl);
            photosEl.addEventListener("click", (e) => {
                if (e.target.matches("img") && typeof renderImageViewer === "function") {
                    renderImageViewer(e);
                }
            });
        }
        photosEl.innerHTML = buildPhotosHTML(data);

        // Button col
        let btnEl = card.querySelector(".snap-card-btn");
        if (!btnEl) {
            btnEl = document.createElement("div");
            btnEl.className = "snap-card-btn";
            const openBtn = document.createElement("button");
            openBtn.className = "show-box-close";
            openBtn.textContent = "Открыть";
            openBtn.addEventListener("click", () => {
                if (typeof receiveCamCard === "function") receiveCamCard(data.ip, data.port);
            });
            btnEl.appendChild(openBtn);
            card.appendChild(btnEl);
        }
    }

    function buildInfoHTML(data) {
        const name = escHtml(data.name || "—");
        const ip = escHtml(data.ip || "");
        const port = escHtml(data.port || "");
        const login = escHtml(data.login || "—");
        const pass = escHtml(data.pass || "—");
        const coords = (data.lat || data.lng)
            ? escHtml(`${(+data.lat).toFixed(4)}, ${(+data.lng).toFixed(4)}`)
            : "—";

        const sep = `<span class="snap-meta-sep">·</span>`;
        let html = `<span class="snap-cam-name">${name}</span>`;
        html +=
            `<span class="snap-cam-meta">` +
            `<span class="snap-meta-item">${ip}:${port}</span>` +
            sep +
            `<span class="snap-meta-item">${login}:${pass}</span>` +
            sep +
            `<span class="snap-meta-item">${coords}</span>` +
            `</span>`;

        if (data.link) {
            html += `<span class="snap-cam-link" title="${escHtml(data.link)}">${escHtml(truncate(data.link, 50))}</span>`;
        }

        if (data.errorType && data.errorMsg) {
            html += `<span class="snap-cam-err">${escHtml(truncate(data.errorMsg, 90))}</span>`;
        }

        if (!data.errorType && data.usedMethod) {
            const leftRaw = data.maintainerName
                ? data.maintainerName.toLowerCase()
                : (data.link ? "rtsp" : "-");
            let rightParts = [escHtml(data.usedMethod)];
            if (data.generatedLink) {
                rightParts.push(escHtml(data.generatedLink));
            }
            html += `<span class="snap-unknown-notice">` +
                `${escHtml(leftRaw)} ` +
                `<span class="snap-meta-sep">·</span> ` +
                `${rightParts.join(" — ")}` +
                `</span>`;
        }

        const chErrs = Array.isArray(data.channelErrors) ? data.channelErrors : [];
        const failedCh = chErrs.filter(e => e && e.errType);
        if (failedCh.length > 0) {
            html += `<span class="snap-ch-errors-toggle">▶ ${failedCh.length} каналов не удалось</span>`;
            html += `<ul class="snap-ch-errors-list">`;
            for (const ce of failedCh) {
                html += `<li>Кан.&nbsp;${ce.ch}: ${escHtml(errLabel(ce.errType))}` +
                    (ce.errMsg ? ` — ${escHtml(truncate(ce.errMsg, 70))}` : "") + `</li>`;
            }
            html += `</ul>`;
        }

        return html;
    }

    function buildPhotosHTML(data) {
        const snaps = (data.snaps || []).map((f) =>
            `<img class="snap-thumb snap-thumb--new" ` +
            `src="/cam/image/${encodeURIComponent(data.ip)}/${f}" ` +
            `title="Новый снапшот" loading="lazy" />`
        );
        const prevSnaps = (data.prevSnaps || []).map((f) =>
            `<img class="snap-thumb snap-thumb--prev" ` +
            `src="/cam/image/${encodeURIComponent(data.ip)}/${f}" ` +
            `title="Предыдущий снапшот" loading="lazy" />`
        );
        return [...snaps, ...prevSnaps].join("");
    }

    function wireChErrors(infoEl) {
        const toggle = infoEl.querySelector(".snap-ch-errors-toggle");
        const list = infoEl.querySelector(".snap-ch-errors-list");
        if (!toggle || !list) return;
        toggle.addEventListener("click", () => {
            const open = list.classList.toggle("open");
            toggle.textContent = toggle.textContent.replace(/^[▶▼]/, open ? "▼" : "▶");
        });
    }

    // ─── Admin actions ────────────────────────────────────────────────────────

    async function startNewRun() {
        const workers = parseInt(document.getElementById("snap-workers-input")?.value || "1");
        const filter = document.getElementById("snap-filter-select")?.value || "known";
        try {
            await api.fetch("/admin/snapshot/start", {
                method: "POST",
                body: JSON.stringify({ resume: false, workers, filter }),
            });
            await showRunList();
        } catch (err) {
            console.error("Snapshot start failed:", err);
        }
    }

    async function resumeRun(runId) {
        const workers = parseInt(document.getElementById("snap-resume-workers")?.value || "1");
        try {
            await api.fetch("/admin/snapshot/start", {
                method: "POST",
                body: JSON.stringify({ resume: true, workers, filter: "known" }),
            });
            await showRunDetail(runId, true);
        } catch (err) {
            console.error("Snapshot resume failed:", err);
        }
    }

    async function stopRun() {
        try {
            await api.fetch("/admin/snapshot/stop", { method: "POST" });
        } catch (err) {
            console.error("Snapshot stop failed:", err);
        }
    }

    // ─── Helpers ──────────────────────────────────────────────────────────────

    // ─── Filter ───────────────────────────────────────────────────────────────

    function matchesFilter(data) {
        const { status, type, hasName, generatedOnly } = snapFilter;

        if (hasName && !data.name) return false;
        if (generatedOnly && !data.generatedLink) return false;

        // type filter
        if (type === "rtsp"    && !data.link) return false;
        if (type === "dahua"   && data.maintainerName !== "Dahua") return false;
        if (type === "unknown" && !data.wasUnknown) return false;

        // status filter
        const done  = data.channelsDone  || 0;
        const found = data.channelsFound || 0;
        const err   = data.errorType || "";
        switch (status) {
            case "has_snaps":    if (done === 0) return false; break;
            case "partial":      if (!(done > 0 && done < found)) return false; break;
            case "errors_only":  if (!err) return false; break;
            case "timeout":      if (err !== "timeout") return false; break;
            case "network_error":if (err !== "network_error") return false; break;
            case "wrong_creds":  if (err !== "wrong_creds") return false; break;
            case "camera_error": if (err !== "camera_error") return false; break;
        }
        return true;
    }

    function applyCardFilter(card, data) {
        card.style.display = matchesFilter(data) ? "" : "none";
    }

    function applyAllFilters() {
        let shown = 0;
        snapResultData.forEach((data, cameraId) => {
            const card = cardMap.get(cameraId);
            if (!card) return;
            const visible = matchesFilter(data);
            card.style.display = visible ? "" : "none";
            if (visible) shown++;
        });
        const countEl = document.getElementById("sflt-count");
        if (countEl) countEl.textContent = `${shown} из ${snapResultData.size}`;
    }

    function buildFilterBar() {
        const bar = document.createElement("div");
        bar.className = "snap-filter-bar";
        bar.id = "snap-filter-bar";

        const statusSel = document.createElement("select");
        statusSel.className = "snap-filter-select";
        statusSel.id = "sflt-status";
        [
            ["all",           "Все статусы"],
            ["has_snaps",     "Есть снапшоты"],
            ["partial",       "Частичный"],
            ["errors_only",   "Только ошибки"],
            ["timeout",       "Таймаут"],
            ["network_error", "Нет сети"],
            ["wrong_creds",   "Неверные данные"],
            ["camera_error",  "Ошибка камеры"],
        ].forEach(([val, label]) => {
            const o = document.createElement("option");
            o.value = val; o.textContent = label;
            statusSel.appendChild(o);
        });
        statusSel.value = snapFilter.status;
        statusSel.addEventListener("change", () => {
            snapFilter.status = statusSel.value;
            applyAllFilters();
        });

        const typeSel = document.createElement("select");
        typeSel.className = "snap-filter-select";
        typeSel.id = "sflt-type";
        [
            ["all",     "Все типы"],
            ["rtsp",    "RTSP ссылка"],
            ["dahua",   "Dahua"],
            ["unknown", "Неизвестные"],
        ].forEach(([val, label]) => {
            const o = document.createElement("option");
            o.value = val; o.textContent = label;
            typeSel.appendChild(o);
        });
        typeSel.value = snapFilter.type;
        typeSel.addEventListener("change", () => {
            snapFilter.type = typeSel.value;
            applyAllFilters();
        });

        function makeToggle(id, label, key) {
            const btn = document.createElement("button");
            btn.className = "snap-filter-toggle";
            btn.id = id;
            btn.textContent = label;
            btn.dataset.active = String(snapFilter[key]);
            btn.addEventListener("click", () => {
                snapFilter[key] = !snapFilter[key];
                btn.dataset.active = String(snapFilter[key]);
                applyAllFilters();
            });
            return btn;
        }

        const countEl = document.createElement("span");
        countEl.className = "snap-filter-count";
        countEl.id = "sflt-count";

        bar.appendChild(statusSel);
        bar.appendChild(typeSel);
        bar.appendChild(makeToggle("sflt-name", "С именем", "hasName"));
        bar.appendChild(makeToggle("sflt-gen",  "Generated RTSP", "generatedOnly"));
        bar.appendChild(countEl);
        return bar;
    }

    function buildRunListSkeleton() {
        const wrap = document.createElement("div");
        wrap.className = "snap-runs-list";
        for (let i = 0; i < 3; i++) {
            const card = document.createElement("div");
            card.className = "snap-skel-run-card";
            card.innerHTML =
                `<div class="snap-skel-run-head">` +
                `<div class="snap-skel" style="width:54px;height:18px"></div>` +
                `<div class="snap-skel" style="width:80px;height:13px"></div>` +
                `<div class="snap-skel" style="width:110px;height:13px"></div>` +
                `</div>` +
                `<div class="snap-skel-run-stats">` +
                `<div class="snap-skel" style="width:90px;height:12px"></div>` +
                `<div class="snap-skel" style="width:28px;height:12px"></div>` +
                `<div class="snap-skel" style="width:28px;height:12px"></div>` +
                `</div>`;
            wrap.appendChild(card);
        }
        return wrap;
    }

    function buildCamCardsSkeleton(count = 5) {
        const wrap = document.createElement("div");
        wrap.className = "snap-cards-list";
        for (let i = 0; i < count; i++) {
            const card = document.createElement("div");
            card.className = "snap-skel-cam-card";
            card.innerHTML =
                `<div class="snap-skel-status">` +
                `<div class="snap-skel" style="width:54px;height:18px"></div>` +
                `</div>` +
                `<div class="snap-skel-info">` +
                `<div class="snap-skel" style="width:55%;height:14px"></div>` +
                `<div class="snap-skel" style="width:85%;height:11px"></div>` +
                `</div>` +
                `<div class="snap-skel-photos">` +
                `<div class="snap-skel" style="width:44px;height:33px;border-radius:3px"></div>` +
                `<div class="snap-skel" style="width:44px;height:33px;border-radius:3px"></div>` +
                `</div>` +
                `<div class="snap-skel-btn">` +
                `<div class="snap-skel" style="width:62px;height:26px;border-radius:5px"></div>` +
                `</div>`;
            wrap.appendChild(card);
        }
        return wrap;
    }

    async function withBusy(btn, fn) {
        if (btn.disabled) return;
        btn.disabled = true;
        try {
            await fn();
        } finally {
            btn.disabled = false;
        }
    }

    function errLabel(type) {
        const labels = {
            timeout: "Таймаут",
            wrong_creds: "Неверные данные",
            network_error: "Сеть",
            camera_error: "Ошибка камеры",
        };
        return labels[type] || type;
    }

    function truncate(s, n) {
        s = String(s);
        return s.length > n ? s.slice(0, n) + "…" : s;
    }

    function escHtml(s) {
        return String(s)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;");
    }
})();
