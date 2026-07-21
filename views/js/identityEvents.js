// Identity events (device identity / IP-change review queue) — moder+
(function () {
    "use strict";

    const TRIGGER_LABELS = {
        A_moved: "Совпадение на другой точке",
        B_replaced: "Identity изменилась на месте",
        C_offline: "Точка молчит",
    };
    const CONFIDENCE_LABELS = { high: "Высокая", medium: "Средняя", low: "Низкая" };
    const OUTCOME_LABELS = {
        moved: "Переехавшая камера",
        new_camera: "Новая камера",
        offline: "Камера перестала работать",
    };
    // Mirrors controllers/identityEventsController.go's validResolveOutcomes —
    // the UI must never offer an outcome the backend would reject.
    const VALID_OUTCOMES = {
        A_moved: ["moved", "new_camera"],
        B_replaced: ["new_camera"],
        C_offline: ["offline"],
    };
    const RESOLVED_SUBTABS = [
        { key: "", label: "Все" },
        { key: "moved", label: "Переехавшая" },
        { key: "new_camera", label: "Новая камера" },
        { key: "offline", label: "Отключена" },
    ];

    let mainTab = "pending"; // "pending" | "resolved"
    let resolvedOutcome = ""; // subtab filter within "resolved"
    let triggerFilter = "";

    const btn = document.getElementById("identity-events-btn");
    if (btn) btn.addEventListener("click", () => openAppModalTab("identity"));
    registerAppModalTab("identity", "Изменения устройств", mountIdentityTab, unmountIdentityTab);

    refreshBadge();

    function mountIdentityTab(container) {
        const body = document.createElement("div");
        body.id = "idev-body";
        body.className = "snap-body";
        container.appendChild(body);
        showList();
    }

    function unmountIdentityTab() {
        refreshBadge();
    }

    async function refreshBadge() {
        const badge = document.getElementById("identity-events-badge");
        if (!badge) return;
        try {
            const res = await api.get("/identity/events?status=pending");
            const events = await res.json();
            const count = events.filter((e) => e.Confidence !== "low").length;
            if (count > 0) {
                badge.textContent = count > 99 ? "99+" : String(count);
                badge.style.display = "";
            } else {
                badge.style.display = "none";
            }
        } catch (_) {
            // silent — badge is a convenience, not critical
        }
    }

    function formatDate(value) {
        if (!value) return "—";
        const d = new Date(value);
        if (isNaN(d.getTime()) || d.getFullYear() < 1971) return "—"; // zero time.Time
        return d.toLocaleString("ru-RU");
    }

    // ─── List ───────────────────────────────────────────────────────────────

    function showList() {
        const body = document.getElementById("idev-body");
        if (!body) return;
        body.innerHTML = "";

        const tabs = document.createElement("div");
        tabs.className = "idev-tabs";
        [
            { key: "pending", label: "На рассмотрении" },
            { key: "resolved", label: "Разобрано" },
        ].forEach(({ key, label }) => {
            const t = document.createElement("button");
            t.className = "idev-tab";
            t.textContent = label;
            t.dataset.active = String(mainTab === key);
            t.addEventListener("click", () => {
                mainTab = key;
                showList();
            });
            tabs.appendChild(t);
        });
        body.appendChild(tabs);

        if (mainTab === "resolved") {
            const subtabs = document.createElement("div");
            subtabs.className = "idev-tabs";
            RESOLVED_SUBTABS.forEach(({ key, label }) => {
                const t = document.createElement("button");
                t.className = "idev-tab";
                t.textContent = label;
                t.dataset.active = String(resolvedOutcome === key);
                t.addEventListener("click", () => {
                    resolvedOutcome = key;
                    showList();
                });
                subtabs.appendChild(t);
            });
            body.appendChild(subtabs);
        }

        const filterBar = document.createElement("div");
        filterBar.className = "snap-filter-bar";
        [
            { key: "", label: "Все триггеры" },
            { key: "A_moved", label: "Переезд" },
            { key: "B_replaced", label: "Замена" },
            { key: "C_offline", label: "Молчит" },
        ].forEach(({ key, label }) => {
            const f = document.createElement("button");
            f.className = "snap-filter-toggle";
            f.textContent = label;
            f.dataset.active = String(triggerFilter === key);
            f.addEventListener("click", () => {
                triggerFilter = key;
                showList();
            });
            filterBar.appendChild(f);
        });
        body.appendChild(filterBar);

        if (mainTab === "pending") {
            const bulkBtn = document.createElement("button");
            bulkBtn.className = "show-box-close idev-bulk-resolve-btn";
            bulkBtn.textContent = "Разрешить все offline камеры";
            bulkBtn.addEventListener("click", () => {
                if (bulkBtn.disabled) return;
                resolveAllOffline(bulkBtn);
            });
            body.appendChild(bulkBtn);
        }

        const list = document.createElement("div");
        list.className = "snap-cards-list";
        list.id = "idev-list";
        body.appendChild(list);
        list.innerHTML = `<div class="idev-empty">Загрузка…</div>`;

        loadList(list);
    }

    async function loadList(list) {
        const params = new URLSearchParams();
        params.set("status", mainTab);
        if (mainTab === "resolved" && resolvedOutcome) params.set("outcome", resolvedOutcome);
        if (triggerFilter) params.set("trigger", triggerFilter);

        let events;
        try {
            const res = await api.get(`/identity/events?${params.toString()}`);
            events = await res.json();
        } catch (e) {
            list.innerHTML = `<div class="idev-empty">Ошибка загрузки: ${e.message}</div>`;
            return;
        }

        if (!list.isConnected) return; // modal closed / tab switched mid-fetch
        if (events.length === 0) {
            list.innerHTML = `<div class="idev-empty">Пусто</div>`;
            return;
        }

        list.innerHTML = "";
        events.forEach((ev) => list.appendChild(makeItem(ev)));
    }

    // ─── List item: summary row + inline expand ────────────────────────────

    function makeItem(ev) {
        const item = document.createElement("div");
        item.className = "idev-item";

        const card = document.createElement("div");
        card.className = "idev-card";

        const badges = document.createElement("div");
        badges.className = "idev-card-badges";
        const trigBadge = document.createElement("span");
        trigBadge.className = "status-badge";
        trigBadge.textContent = TRIGGER_LABELS[ev.TriggerType] || ev.TriggerType;
        badges.appendChild(trigBadge);
        const confBadge = document.createElement("span");
        confBadge.className = `status-badge idev-conf-${ev.Confidence}`;
        confBadge.textContent = CONFIDENCE_LABELS[ev.Confidence] || ev.Confidence;
        badges.appendChild(confBadge);
        card.appendChild(badges);

        const info = document.createElement("div");
        const names = document.createElement("div");
        names.className = "idev-card-names";
        names.textContent = ev.OldCamera?.Name || `Камера #${ev.OldCamera?.ID ?? "?"}`;
        info.appendChild(names);

        const route = document.createElement("div");
        route.className = "idev-card-route";
        const oldSpan = document.createElement("span");
        oldSpan.textContent = `${ev.OldCamera?.IP ?? "?"}:${ev.OldCamera?.Port ?? "?"}`;
        route.appendChild(oldSpan);
        if (ev.NewCamera) {
            const arrow = document.createElement("span");
            arrow.className = "arrow";
            arrow.textContent = "→";
            route.appendChild(arrow);
            const newSpan = document.createElement("span");
            newSpan.textContent = `${ev.NewCamera.IP}:${ev.NewCamera.Port}`;
            route.appendChild(newSpan);
        }
        if (ev.SameCity) {
            const cityBadge = document.createElement("span");
            cityBadge.className = "status-badge status-added";
            cityBadge.textContent = "тот же город";
            route.appendChild(cityBadge);
        }
        info.appendChild(route);
        card.appendChild(info);

        const meta = document.createElement("div");
        meta.className = "idev-card-meta";
        const lines = [`${ev.ConfirmingRuns} подтв.`, new Date(ev.CreatedAt).toLocaleString("ru-RU")];
        if (ev.Status === "resolved" && ev.Outcome) lines.push(OUTCOME_LABELS[ev.Outcome] || ev.Outcome);
        meta.innerHTML = lines.map((l) => `<div>${l}</div>`).join("");
        card.appendChild(meta);

        item.appendChild(card);

        const expand = document.createElement("div");
        expand.className = "idev-expand";
        item.appendChild(expand);

        let loaded = false;
        card.addEventListener("click", async () => {
            const isOpen = expand.classList.toggle("open");
            card.classList.toggle("idev-card--active", isOpen);
            if (isOpen && !loaded) {
                loaded = true;
                expand.innerHTML = `<div class="idev-empty">Загрузка…</div>`;
                await loadExpand(ev.ID, expand, item);
            }
        });

        return item;
    }

    async function loadExpand(id, expand, item) {
        let ev;
        try {
            const res = await api.get(`/identity/events/${id}`);
            ev = await res.json();
        } catch (e) {
            expand.innerHTML = `<div class="idev-empty">Ошибка загрузки: ${e.message}</div>`;
            return;
        }
        if (!expand.isConnected) return; // collapsed / list re-rendered mid-fetch

        expand.innerHTML = "";

        const compare = document.createElement("div");
        compare.className = "idev-compare";
        compare.appendChild(makeCompareCol("Было", ev.OldCamera, ev.OldIdentity, ev.NewIdentity));
        compare.appendChild(makeCompareCol("Стало", ev.NewCamera, ev.NewIdentity, ev.OldIdentity));
        expand.appendChild(compare);

        if (ev.Status === "resolved") {
            const resolvedNote = document.createElement("div");
            resolvedNote.className = "idev-empty";
            resolvedNote.textContent = `Разобрано: ${OUTCOME_LABELS[ev.Outcome] || ev.Outcome}`;
            expand.appendChild(resolvedNote);
        } else {
            const outcomes = document.createElement("div");
            outcomes.className = "idev-outcomes";
            (VALID_OUTCOMES[ev.TriggerType] || []).forEach((outcome) => {
                const b = document.createElement("button");
                b.className = "idev-outcome-btn";
                b.textContent = OUTCOME_LABELS[outcome] || outcome;
                b.addEventListener("click", () => resolve(ev.ID, outcome));
                outcomes.appendChild(b);
            });
            const skipBtn = document.createElement("button");
            skipBtn.className = "idev-outcome-btn idev-skip-btn";
            skipBtn.textContent = "Пропустить";
            skipBtn.addEventListener("click", () => skip(ev.ID));
            outcomes.appendChild(skipBtn);
            expand.appendChild(outcomes);
        }
    }

    function makeCompareCol(title, cam, ownIdentity, otherIdentity) {
        const col = document.createElement("div");
        col.className = "idev-compare-col";

        const h = document.createElement("div");
        h.className = "idev-field idev-compare-title-row";
        const hLabel = document.createElement("span");
        hLabel.className = "idev-compare-title";
        hLabel.textContent = title;
        const hValue = document.createElement("span");
        hValue.className = "idev-compare-date";
        hValue.textContent = formatDate(ownIdentity?.capturedAt);
        h.appendChild(hLabel);
        h.appendChild(hValue);
        col.appendChild(h);

        if (!cam) {
            const empty = document.createElement("div");
            empty.className = "idev-field-label";
            empty.textContent = "нет данных";
            col.appendChild(empty);
            return col;
        }

        const fields = [
            ["Name", cam.Name || "—"],
            ["IP:Port", `${cam.IP}:${cam.Port}`],
            ["Maintainer", cam.MaintainerRef?.Name || cam.Maintainer || "—"],
            ["Model", ownIdentity?.model || "—", ownIdentity?.model !== otherIdentity?.model],
            ["Serial", ownIdentity?.serial || "—", ownIdentity?.serial !== otherIdentity?.serial],
            ["MAC", ownIdentity?.mac || "—", ownIdentity?.mac !== otherIdentity?.mac],
            ["Firmware", ownIdentity?.firmware || "—", ownIdentity?.firmware !== otherIdentity?.firmware],
        ];
        fields.forEach(([label, value, diff]) => {
            const row = document.createElement("div");
            row.className = "idev-field";
            const l = document.createElement("span");
            l.className = "idev-field-label";
            l.textContent = label;
            const v = document.createElement("span");
            v.className = "idev-field-value" + (diff ? " idev-field-value--diff" : "");
            v.textContent = value;
            row.appendChild(l);
            row.appendChild(v);
            col.appendChild(row);
        });

        const openBtn = document.createElement("button");
        openBtn.className = "show-box-close idev-open-btn";
        openBtn.textContent = "Открыть";
        openBtn.addEventListener("click", () => {
            if (typeof receiveCamCard === "function") receiveCamCard(cam.IP, cam.Port);
        });
        col.appendChild(openBtn);

        return col;
    }

    async function resolve(id, outcome) {
        try {
            await api.post(`/identity/events/${id}/resolve`, { outcome });
            notifications.success("Событие разобрано");
            showList();
            refreshBadge();
        } catch (e) {
            notifications.error("Не удалось разобрать событие: " + e.message);
        }
    }

    async function resolveAllOffline(btn) {
        if (!confirm("Разрешить все камеры со статусом «молчит» как offline? Все они будут помечены невалидными.")) return;
        btn.disabled = true;
        try {
            const res = await api.post("/identity/events/resolve_all_offline", {});
            const data = await res.json();
            notifications.success(`Разрешено событий: ${data.resolved ?? 0}`);
            showList();
            refreshBadge();
        } catch (e) {
            notifications.error("Не удалось разрешить события: " + e.message);
        } finally {
            btn.disabled = false;
        }
    }

    async function skip(id) {
        if (!confirm("Пропустить это событие? Оно будет удалено из очереди без изменения записей камер.")) return;
        try {
            await api.fetch(`/identity/events/${id}`, { method: "DELETE" });
            notifications.success("Событие пропущено");
            showList();
            refreshBadge();
        } catch (e) {
            notifications.error("Не удалось пропустить событие: " + e.message);
        }
    }
})();
