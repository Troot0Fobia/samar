function escapeHtml(s) {
    return String(s).replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;");
}

const REPORT_KEY = "last_upload_report";
let lastUploadReport = (() => {
    try { return JSON.parse(localStorage.getItem(REPORT_KEY)) || null; } catch { return null; }
})();

document.getElementById("upload-data").addEventListener("change", async (e) => {
    const file = e.target.files[0];

    if (!file) {
        notifications.error("No file was provided in uploading cameras");
        return;
    }

    const formData = new FormData();
    formData.append("file", file);
    try {
        const resp = await sendAdminReq("/admin/upload_cams", formData, "POST", true);
        lastUploadReport = await resp.json();
        localStorage.setItem(REPORT_KEY, JSON.stringify(lastUploadReport));
        notifications.success(
            `+${lastUploadReport.added_count} добавлено | ${lastUploadReport.dup_count} дублей | ${lastUploadReport.error_count} ошибок`
        );
    } catch (e) {
        console.error("Error while send file: " + e);
        notifications.error("Error while send file");
    }
    e.target.value = "";
});

document.getElementById("upload-report-btn").addEventListener("click", () => {
    if (!lastUploadReport) {
        showNoReport();
        return;
    }
    showUploadReport(lastUploadReport);
});

function showNoReport() {
    document.getElementById("upload-report-modal")?.remove();
    const box = document.createElement("div");
    box.id = "upload-report-modal";
    box.classList.add("show-box");
    const label = document.createElement("div");
    label.className = "token-label";
    label.textContent = "Отчет об импорте";
    const msg = document.createElement("div");
    msg.style.cssText = "color:var(--t2);font-size:12px;padding:8px 0";
    msg.textContent = "Отчетов нет. Загрузите файл для импорта.";
    const foot = document.createElement("div");
    foot.className = "show-box-foot";
    foot.innerHTML = `<button class="show-box-close"><svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>Закрыть</button>`;
    foot.querySelector(".show-box-close").addEventListener("click", () => box.remove());
    box.appendChild(label);
    box.appendChild(msg);
    box.appendChild(foot);
    document.body.appendChild(box);
}

function showUploadReport(report) {
    document.getElementById("upload-report-modal")?.remove();

    const box = document.createElement("div");
    box.id = "upload-report-modal";
    box.classList.add("show-box", "show-box--large");

    const label = document.createElement("div");
    label.className = "token-label";
    label.textContent = "Отчет об импорте";
    box.appendChild(label);

    const summary = document.createElement("div");
    summary.className = "report-summary";
    summary.innerHTML =
        `<span class="status-badge status-added">+${report.added_count} добавлено</span>` +
        `<span class="status-badge status-duplicate">${report.dup_count} дублей</span>` +
        `<span class="status-badge status-error">${report.error_count} ошибок</span>`;
    box.appendChild(summary);

    const tableWrap = document.createElement("div");
    tableWrap.className = "report-table-wrap";
    const table = document.createElement("table");
    table.className = "report-table";
    table.innerHTML =
        `<thead><tr>` +
        `<th>IP:Порт</th><th>Статус</th><th>Регион</th><th>Город</th><th>Подробности</th>` +
        `</tr></thead>`;
    const tbody = document.createElement("tbody");
    const STATUS_LABELS = { added: "Добавлен", duplicate: "Дубликат", error: "Ошибка" };
    for (const r of report.results) {
        const tr = document.createElement("tr");
        const errorText = r.error ? escapeHtml(r.error) : "—";
        tr.innerHTML =
            `<td class="report-ipport">${r.ip}:${r.port}</td>` +
            `<td><span class="status-badge status-${r.status}">${STATUS_LABELS[r.status] ?? r.status}</span></td>` +
            `<td>${r.region || "—"}</td>` +
            `<td>${r.city || "—"}</td>` +
            `<td class="report-err-cell">${errorText}</td>`;
        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    tableWrap.appendChild(table);
    box.appendChild(tableWrap);

    if (report.new_cities && report.new_cities.length > 0) {
        const citySection = document.createElement("div");
        citySection.className = "report-new-cities";
        const cityLabel = document.createElement("div");
        cityLabel.className = "token-label";
        cityLabel.textContent = `Новые города (${report.new_cities.length})`;
        citySection.appendChild(cityLabel);
        const cityList = document.createElement("div");
        cityList.className = "report-city-list";
        for (const c of report.new_cities) {
            const chip = document.createElement("span");
            chip.className = "report-city-chip";
            chip.textContent = c.name_rus ? `${c.name_rus} (${c.name})` : c.name;
            cityList.appendChild(chip);
        }
        citySection.appendChild(cityList);
        box.appendChild(citySection);
    }

    const foot = document.createElement("div");
    foot.className = "show-box-foot";
    const dlBtn = document.createElement("button");
    dlBtn.className = "show-box-close";
    dlBtn.innerHTML =
        `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">` +
        `<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>` +
        `<polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>` +
        `</svg>Скачать отчет`;
    dlBtn.addEventListener("click", () => downloadReportCsv(report));
    const closeBtn = document.createElement("button");
    closeBtn.className = "show-box-close";
    closeBtn.innerHTML =
        `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">` +
        `<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>` +
        `</svg>Закрыть`;
    closeBtn.addEventListener("click", () => box.remove());
    foot.appendChild(dlBtn);
    foot.appendChild(closeBtn);
    box.appendChild(foot);

    document.body.appendChild(box);
}

function downloadReportCsv(report) {
    const rows = [["IP", "Port", "Status", "Region", "City", "Error"]];
    for (const r of report.results) {
        rows.push([r.ip, r.port, r.status, r.region || "", r.city || "", r.error || ""]);
    }
    const csv = rows
        .map((r) => r.map((v) => `"${String(v).replace(/"/g, '""')}"`).join(","))
        .join("\n");
    const blob = new Blob(["﻿" + csv], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `import_report_${new Date().toISOString().slice(0, 19).replace(/:/g, "-")}.csv`;
    a.click();
    URL.revokeObjectURL(url);
}

document.getElementById("receive-token").addEventListener("click", async () => {
    const select = document.getElementById("select-role");
    const selected_role = select.value;

    if (!selected_role) {
        notifications.error("No role was selected in getting token");
        return;
    }

    try {
        const response = await api.post("/admin/get_token", {
            role: selected_role,
        });

        const ans = await response.json();
        const show_box = document.createElement("div");
        show_box.classList.add("show-box");
        const tokenLabel = document.createElement("div");
        tokenLabel.className = "token-label";
        tokenLabel.textContent = "Токен регистрации";
        const tokenValue = document.createElement("div");
        tokenValue.className = "token-value";
        tokenValue.textContent = ans.token;
        const foot = document.createElement("div");
        foot.className = "show-box-foot";
        foot.innerHTML = `<button class="show-box-close"><svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>Закрыть</button>`;
        show_box.appendChild(tokenLabel);
        show_box.appendChild(tokenValue);
        show_box.appendChild(foot);
        show_box
            .querySelector(".show-box-close")
            .addEventListener("click", () => show_box.remove());
        document.body.appendChild(show_box);
    } catch (e) {
        console.error("Error while getting token: " + e);
        notifications.error("Error while getting token");
    }
});
