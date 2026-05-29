const contextMenu = document.querySelector(".image-context-menu");
let activeImage = null;

// ── Update data ───────────────────────────────────────────────────────────────

document.getElementById("update-data").addEventListener("click", async (e) => {
    const btn = e.currentTarget;
    if (btn.disabled) return;
    const data = {};
    const required_fields = ["ip", "port", "name", "login", "password", "link", "comment"];
    const fields = info_window.querySelectorAll('input[type="text"], textarea');
    for (const field of fields) {
        const name = field.name.replace("cam_", "");
        const value = field.value.trim();
        if (!required_fields.includes(name)) continue;
        if (!value && name !== "comment" && name !== "link") {
            notifications.error(`Required parameter can not be empty: ${name}`);
            return;
        }
        data[name] = value;
    }

    // Include coordinates if they were changed
    const lat = info_window.querySelector("#cam-lat")?.value.trim();
    const lng = info_window.querySelector("#cam-lng")?.value.trim();
    if (lat && lng) {
        data["lat"] = parseFloat(lat);
        data["lng"] = parseFloat(lng);
    }

    if (!validators.isValidIP(data["ip"]) || !validators.isValidPort(data["port"])) {
        notifications.error("IP or port invalid");
        return;
    }

    const cityId = document.getElementById("cam-city-id")?.value;
    if (cityId) data["city_id"] = parseInt(cityId);
    const maintainerId = document.getElementById("cam-maintainer-id")?.value;
    if (maintainerId) data["maintainer_id"] = parseInt(maintainerId);

    btn.disabled = true;
    try {
        const resp = await api.post("/cam/update_data", data);
        const result = await resp.json();

        const ip = data["ip"], port = data["port"];

        const lat = result.Lat ?? result.lat;
        const lng = result.Lng ?? result.lng;
        if (lat && lng) {
            const camId = info_window.querySelector("[name=cam_ip]")?.closest(".cam-label")?.dataset.id;
            if (camId) {
                window.__removeCamMarker(camId);
                const status = (result.Status || document.getElementById("select-cam-status")?.value) ?? "valid";
                window.__syncCamMarker(camId, status, lat, lng, ip, port, result.Region || "");
            }
        }

        window.__updateCamInSidebar?.({
            IP: ip, Port: port,
            IsDefined:   result.IsDefined   ?? false,
            Name:        result.Name        ?? data["name"] ?? "",
            Status:      result.Status      ?? (document.getElementById("select-cam-status")?.value ?? "valid"),
            City:        result.City        ?? "",
            City_rus:    result.City_rus    ?? "",
            Region:      result.Region      ?? "",
            Region_rus:  result.Region_rus  ?? "",
            Country:     result.Country     ?? "",
            Country_rus: result.Country_rus ?? "",
            ID:          result.ID,
        });

        window.__invalidateCamCard?.(ip, port);
        notifications.success("Information was updated successfully");
    } catch (e) {
        console.error("Error while updating cam data: " + e);
        notifications.error("Error while updating cam data. See logs");
    } finally {
        btn.disabled = false;
    }
});

// ── Define cam ────────────────────────────────────────────────────────────────

document.getElementById("define-cam").addEventListener("click", async (e) => {
    const btn = e.currentTarget;
    if (btn.disabled) return;
    const lat = document.getElementById("cam-lat").value.trim();
    const lng = document.getElementById("cam-lng").value.trim();
    const address = document.getElementById("cam-address").value.trim();
    const name = document.getElementById("cam-name").value.trim();
    const login = document.getElementById("cam-login").value.trim();
    const password = document.getElementById("cam-password").value.trim();

    if (!name) { notifications.error("Укажите название камеры"); return; }
    if (!address) { notifications.error("Укажите адрес"); return; }
    if (!login) { notifications.error("Укажите логин"); return; }
    if (!password) { notifications.error("Укажите пароль"); return; }

    const comment = document.getElementById("cam-comment").value.trim();
    const ip = document.getElementById("cam-ip").value.trim();
    const port = document.getElementById("cam-port").value.trim();

    if (!validators.isValidIP(ip) || !validators.isValidPort(port)) {
        notifications.error("IP or port are invalid");
        return;
    }

    const currentStatus = document.getElementById("select-cam-status")?.value;
    if (currentStatus !== "valid" && currentStatus !== "duplicate") {
        notifications.error("Определить можно только камеры со статусом «Валидная» или «Дубль»");
        return;
    }

    const cityId = document.getElementById("cam-city-id")?.value;
    const maintainerId = document.getElementById("cam-maintainer-id")?.value;

    btn.disabled = true;
    try {
        const resp = await api.post("/cam/define_cam", {
            ip,
            port,
            login,
            password,
            name,
            lat: lat ? parseFloat(lat) : null,
            lng: lng ? parseFloat(lng) : null,
            address,
            comment,
            city_id: cityId ? parseInt(cityId) : null,
            maintainer_id: maintainerId ? parseInt(maintainerId) : null,
        });
        const result = await resp.json();
        window.__invalidateCamCard?.(ip, port);
        window.__setDefineBtnVisible?.(false);
        notifications.success("Camera was defined successfully");

        let camId = info_window.querySelector("[name=cam_ip]")?.closest(".cam-label")?.dataset.id;
        if (!camId && result.ID) camId = String(result.ID);
        const defLat = result.Lat ?? result.lat;
        const defLng = result.Lng ?? result.lng;
        if (camId && defLat && defLng) {
            const status = (result.Status || document.getElementById("select-cam-status")?.value) ?? "valid";
            window.__syncCamMarker(camId, status, defLat, defLng, ip, port, result.Region || "");
        }

        window.__updateCamInSidebar?.({
            IP: ip, Port: port,
            IsDefined:   result.IsDefined ?? true,
            Name:        result.Name || name,
            Status:      (result.Status || document.getElementById("select-cam-status")?.value) ?? "valid",
            City:        result.City        ?? "",
            City_rus:    result.City_rus    ?? "",
            Region:      result.Region      ?? "",
            Region_rus:  result.Region_rus  ?? "",
            Country:     result.Country     ?? "",
            Country_rus: result.Country_rus ?? "",
            ID:          result.ID,
        });
    } catch (e) {
        console.error("Error while define cam: " + e);
        notifications.error("Error while define cam");
    } finally {
        btn.disabled = false;
    }
});

// ── Delete cam ────────────────────────────────────────────────────────────────

document.getElementById("delete-cam").addEventListener("click", async (e) => {
    const btn = e.currentTarget;
    if (btn.disabled) return;

    const ip = info_window.querySelector("#cam-ip").value.trim();
    const port = info_window.querySelector("#cam-port").value.trim();

    if (!ip || !port) {
        notifications.error("No ip or port provided");
        return;
    }

    if (!confirm(`Удалить камеру ${ip}:${port}? Это действие необратимо.`)) return;

    btn.disabled = true;
    try {
        await api.fetch("/cam/delete_cam", {
            method: "DELETE",
            body: JSON.stringify({ ip, port }),
        });
        notifications.success("Camera deleted");
        info_window.classList.remove("open");

        const cam_label = document.querySelector(`[data-ip="${ip}"][data-port="${port}"]`);
        const camId = cam_label?.dataset.id;
        if (camId) window.__removeCamMarker?.(camId);
        window.__removeCamFromSidebar?.(ip, port);
    } catch (e) {
        btn.disabled = false;
        console.error("Error while deleting cam: " + e);
        notifications.error("Error while deleting cam. See logs");
    }
});

// ── Status chip ───────────────────────────────────────────────────────────────

const statusChip = document.getElementById("status-chip");
const statusDropdown = document.getElementById("status-dropdown");

const STATUS_LABELS = {
    valid: "Валидная",
    invalid: "Невалидная",
    duplicate: "Дубль",
    undetectable: "Трудноопределимая",
};

const isAddButtonVisible = () => {
    return info_window.querySelector("#add-camera");
}

let statusChangePending = false;

statusChip?.addEventListener("click", (e) => {
    e.stopPropagation();
    statusDropdown?.classList.toggle("open");
});

statusDropdown?.querySelectorAll(".status-opt").forEach((opt) => {
    opt.addEventListener("click", async () => {
        const status = opt.dataset.status;

        if (isAddButtonVisible()) {
            window.__syncStatusPicker(status);
            statusDropdown.classList.remove("open");
            return;
        }

        if (statusChangePending) return;

        const ip = info_window.querySelector("#cam-ip").value.trim();
        const port = info_window.querySelector("#cam-port").value.trim();

        if (!ip || !port) {
            notifications.error("No ip or port provided");
            return;
        }

        statusChangePending = true;
        statusDropdown.classList.remove("open");
        try {
            const result = await api.post("/cam/change_status", { ip, port, status });
            const data = await result.json();
            window.__invalidateCamCard?.(ip, port);
            notifications.success("Status changed");
            window.__syncStatusPicker(status);

            const cam_label = document.querySelector(`[data-ip="${ip}"][data-port="${port}"]`);
            if (cam_label) {
                const camId = cam_label.dataset.id;
                if (!data.IsDefined) {
                    window.__removeCamMarker?.(camId);
                    window.__setDefineBtnVisible?.(true);
                } else if (status === "duplicate") {
                    window.__removeCamMarker?.(camId);
                } else if (status === "valid") {
                    const lat = parseFloat(info_window.querySelector("#cam-lat").value);
                    const lng = parseFloat(info_window.querySelector("#cam-lng").value);
                    const regionName = cam_label.closest(".region-tab")?.dataset.region ?? "";
                    window.__syncCamMarker?.(camId, "valid", lat, lng, ip, port, regionName);
                }

                window.__updateCamInSidebar?.({
                    IP: ip, Port: port,
                    IsDefined: data.IsDefined,
                    Name: info_window.querySelector("#cam-name")?.value.trim() ?? "",
                    Status: status,
                    City:        cam_label.closest(".city-tab")?.dataset.city ?? "",
                    City_rus:    cam_label.closest(".city-tab")?.dataset.sortName ?? "",
                    Region:      cam_label.closest(".region-tab")?.dataset.region ?? "",
                    Region_rus:  cam_label.closest(".region-tab")?.dataset.sortName ?? "",
                    Country:     cam_label.closest(".country-tab")?.dataset.country ?? "",
                    Country_rus: cam_label.closest(".country-tab")?.dataset.sortName ?? "",
                    ID: camId ? parseInt(camId) : null,
                });
            }
        } catch (e) {
            console.error("Error while changing status: " + e);
            notifications.error("Error while changing status");
        } finally {
            statusChangePending = false;
        }
    });
});

document.addEventListener("click", (e) => {
    if (!e.target.closest(".status-chip-wrap"))
        statusDropdown?.classList.remove("open");
});

window.__syncStatusPicker = (status) => {
    const wrap = info_window.querySelector(".status-wrap");
    if (wrap) wrap.dataset.status = status;
    const label = info_window.querySelector(".status-label");
    if (label) label.textContent = STATUS_LABELS[status] ?? status;
    const hiddenInput = document.getElementById("select-cam-status");
    if (hiddenInput) hiddenInput.value = status;
};

window.__setMaintainerPicker = (maintainerId, maintainer) => {
    document.getElementById("cam-maintainer-id").value = maintainerId ?? "";
    const display = document.getElementById("maintainer-picker-display");
    if (display) display.textContent = maintainer?.Name || maintainer || "Не выбрано";
};

window.__setDeleteBtnVisible = (visible) => {
    const btn = document.getElementById("delete-cam");
    if (btn) btn.style.display = visible ? "inline-flex" : "none";
};

window.__setDefineBtnVisible = (visible) => {
    const btn = document.getElementById("define-cam");
    if (btn) btn.style.display = visible ? "" : "none";
};

// ── Sidebar click: close panel on cam label click ─────────────────────────────

sidebar.addEventListener("click", (e) => {
    const el = e.target;
    if (
        el.closest(".cam-label") &&
        el.classList.contains("label-text") &&
        info_window.classList.contains("open")
    )
        cancel(true);
});

// ── Add-camera panel ──────────────────────────────────────────────────────────

const fields_name = [];
let addModeDataPending = false;

const cancel = (isClose) => {
    const wasAddMode = !!info_window.querySelector("#add-camera");
    for (const name of fields_name)
        info_window.querySelector(`input[name="${name}"]`)?.setAttribute("readonly", true);
    fields_name.length = 0;
    info_window.querySelector("#add-camera")?.remove();
    info_window.querySelector("#close-button").onclick = null;
    info_window.classList.toggle("open", isClose);
    if (!isClose) {
        window.__setDeleteBtnVisible(false);
        if (wasAddMode && newCameraDataExist()) addModeDataPending = true;
    } else {
        addModeDataPending = false;
    }
};

function newCameraDataExist() {
    return Array.prototype.some.call(
        info_window.querySelectorAll('[data-forfill="1"]'),
        el => el.value
    );
}

function askForRewriteData() {
    return confirm("Есть внесенные изменения. Действительно продолжить?");
}

async function submitNewCamera() {
    const btn = info_window.querySelector("#add-camera");
    if (btn?.disabled) return;
    const data = {};
    info_window.querySelectorAll('input[type="text"], textarea').forEach((field) => {
        data[field.name.replace("cam_", "")] = field.value.trim();
    });

    if (!data["name"]) data["name"] = data["ip"];

    for (const param of ["ip", "port", "login", "password"])
        if (!data[param]) {
            notifications.error(`Required parameter was not provided: ${param}`);
            return;
        }

    if (!validators.isValidIP(data["ip"]) || !validators.isValidPort(data["port"])) {
        notifications.error("IP or port invalid");
        return;
    }

    if (data["lat"] || data["lng"]) {
        const lat = parseFloat(data["lat"]);
        const lng = parseFloat(data["lng"]);
        if (!isFinite(lat) || !isFinite(lng) || lat < -90 || lat > 90 || lng < -180 || lng > 180) {
            notifications.error("Coords are invalid");
            return;
        }
        data["lat"] = lat;
        data["lng"] = lng;
    } else {
        // Remove empty coord strings so Go's *float64 binding doesn't fail
        delete data["lat"];
        delete data["lng"];
    }

    data["status"] = document.getElementById("select-cam-status")?.value ?? "valid";

    const cityId = document.getElementById("cam-city-id")?.value;
    if (cityId) data["city_id"] = parseInt(cityId);

    // If coords are defined, city and address must be provided
    if (data["lat"] && data["lng"]) {
        if (!data["city_id"]) {
            notifications.error("Выберите город");
            return;
        }
        if (!data["address"]) {
            notifications.error("Укажите адрес");
            return;
        }
    }
    const maintainerId = document.getElementById("cam-maintainer-id")?.value;
    if (maintainerId) data["maintainer_id"] = parseInt(maintainerId);

    if (btn) btn.disabled = true;
    try {
        const response = await api.post("/cam/add_camera", data);
        const camera = await response.json();
        notifications.success("Camera was defined successfully");

        // Populate returned data into form fields
        if (camera.Lat) info_window.querySelector("#cam-lat").value = camera.Lat;
        if (camera.Lng) info_window.querySelector("#cam-lng").value = camera.Lng;
        if (camera.Address) info_window.querySelector("#cam-address").value = camera.Address;
        if (camera.City) {
            info_window.querySelector("#cam-city-id").value = "";
            document.getElementById("city-picker-display").textContent = camera.City_rus || camera.City;
        }
        if (camera.Maintainer) {
            document.getElementById("cam-maintainer-id").value = camera.MaintainerID || "";
            document.getElementById("maintainer-picker-display").textContent = camera.Maintainer;
        }

        cancel(true);
        window.__setDeleteBtnVisible(true);
        const nameField = info_window.querySelector("#cam-name");
        if (nameField && camera.Name) nameField.value = camera.Name;
        renderCams([camera], sidebar_tabs);
        reapplySearch();
    } catch (e) {
        if (btn) btn.disabled = false;
        console.error("Error while define cam: " + e);
        notifications.error("Error while define cam. See logs");
    }
}

function createAddButton() {
    const btn = document.createElement("input");
    btn.className = "foot-btn foot-btn--primary";
    btn.id = "add-camera";
    btn.type = "button";
    btn.value = "Добавить камеру";
    btn.onclick = submitNewCamera;
    return btn;
}

function reenterAddMode() {
    window.__setDeleteBtnVisible(false);
    window.__setDefineBtnVisible(false);
    info_window.querySelectorAll('[data-forfill="1"]').forEach(field => {
        if (field.hasAttribute("readonly")) {
            fields_name.push(field.name);
            field.removeAttribute("readonly");
        }
    });
    if (!info_window.querySelector("#add-camera"))
        info_window.querySelector(".cam-buttons").appendChild(createAddButton());
    info_window.querySelector("#close-button").onclick = () => cancel(false);
    info_window.classList.toggle("open", true);
    addModeDataPending = false;
}

window.__cancelAddMode = () => {
    if (info_window.querySelector("#add-camera")) cancel(true);
};

window.__newCameraDataExist   = newCameraDataExist;
window.__isAddModeActive      = () => !!info_window.querySelector("#add-camera");
window.__isAddModeDataPending = () => addModeDataPending;
window.__clearAddModePending  = () => { addModeDataPending = false; };
window.__reenterAddMode       = reenterAddMode;

document.getElementById("add-camera-panel").addEventListener("click", () => {
    api.get("/refresh_token").catch(() => {});

    if (isAddButtonVisible()) {
        if (newCameraDataExist() && !askForRewriteData()) return;
    } else if (addModeDataPending) {
        reenterAddMode();
        return;
    }

    addModeDataPending = false;
    window.__setDeleteBtnVisible(false);
    window.__setDefineBtnVisible(false);
    info_window.querySelectorAll('input[type="text"], textarea').forEach((field) => {
        if (field.hasAttribute("readonly")) {
            fields_name.push(field.name);
            field.removeAttribute("readonly");
        }
        field.value = "";
    });
    // Reset pickers
    document.getElementById("cam-city-id").value = "";
    document.getElementById("city-picker-display").textContent = "Не выбрано";
    cityPickerData = [];
    currentCityRegionId = null;
    citiesLoadedForRegion = null;
    citiesFetchInProgress = false;
    document.getElementById("cam-maintainer-id").value = "";
    document.getElementById("maintainer-picker-display").textContent = "Не выбрано";
    window.__syncStatusPicker("valid");
    info_window.querySelector(".cam-images").innerHTML = "";

    info_window.querySelector("#close-button").onclick = () => cancel(false);
    if (!info_window.querySelector("#add-camera"))
        info_window.querySelector(".cam-buttons").appendChild(createAddButton());

    info_window.classList.toggle("open", true);
});

// ── City picker ───────────────────────────────────────────────────────────────

const cityPickerBtn = document.getElementById("city-picker-btn");
const cityPickerDropdown = document.getElementById("city-picker");
const citySearch = document.getElementById("city-search");
const cityList = document.getElementById("city-list");

let cityPickerData = [];
let currentCityRegionId = null; // null=unknown, 0=all, N=specific region
let citiesLoadedForRegion = null; // tracks which region's cities are already loaded
let citiesFetchInProgress = false; // prevents duplicate concurrent fetches
let lastPickerCoordKey = null;    // "lat:lng" used on last successful load — skips re-fetch if unchanged

window.__setCityPicker = (cityId, city, cityRus, regionId) => {
    document.getElementById("cam-city-id").value = cityId ?? "";
    const display = document.getElementById("city-picker-display");
    if (display) display.textContent = cityRus || city || "Не выбрано";
    if (regionId) {
        currentCityRegionId = regionId;
        window.__loadCityList(regionId);
    }
};

window.__loadCityList = async (regionId) => {
    const normalizedRegionId = regionId || 0;

    // Deduplication: skip if we already have cities for this exact region
    if (citiesLoadedForRegion !== null && citiesLoadedForRegion === normalizedRegionId && cityPickerData.length > 0) {
        renderCityList(cityPickerData);
        return;
    }

    // Prevent concurrent fetches
    if (citiesFetchInProgress) return;
    citiesFetchInProgress = true;

    try {
        const url = normalizedRegionId ? `/cam/cities?region_id=${encodeURIComponent(normalizedRegionId)}` : "/cam/cities";
        const resp = await api.get(url);
        cityPickerData = await resp.json();
        currentCityRegionId = normalizedRegionId;
        citiesLoadedForRegion = normalizedRegionId;
        renderCityList(cityPickerData);
    } catch (e) {
        console.error("Error loading cities:", e);
    } finally {
        citiesFetchInProgress = false;
    }
};

function cityLabel(c) { return c.Name_rus || c.Name || c.City_rus || c.City || ""; }
function cityRegion(c) { return c.RegionNameRus || c.RegionName || ""; }

function showPickerSkeleton(listEl) {
    if (!listEl) return;
    listEl.innerHTML = "";
    for (let i = 0; i < 5; i++) {
        const row = document.createElement("div");
        row.className = "picker-skel-row";
        listEl.appendChild(row);
    }
}

function makeCityOpt(c) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "picker-opt";
    btn.dataset.id = c.ID;
    const nameSpan = document.createElement("span");
    nameSpan.className = "picker-opt-name";
    nameSpan.textContent = cityLabel(c);
    btn.appendChild(nameSpan);
    const reg = cityRegion(c);
    if (reg) {
        const regSpan = document.createElement("span");
        regSpan.className = "picker-opt-region";
        regSpan.textContent = reg;
        btn.appendChild(regSpan);
    }
    if (String(c.ID) === document.getElementById("cam-city-id").value)
        btn.classList.add("selected");
    btn.addEventListener("click", () => {
        document.getElementById("cam-city-id").value = c.ID;
        document.getElementById("city-picker-display").textContent = cityLabel(c);
        cityPickerDropdown?.classList.remove("open");
    });
    return btn;
}

function renderCityList(cities, filter = "") {
    if (!cityList) return;
    cityList.innerHTML = "";
    const filtered = filter
        ? cities.filter((c) => cityLabel(c).toLowerCase().includes(filter.toLowerCase()))
        : cities;
    const sorted = [...filtered].sort((a, b) => cityLabel(a).localeCompare(cityLabel(b), undefined, { sensitivity: "base" }));
    sorted.forEach((c) => cityList.appendChild(makeCityOpt(c)));
}

async function detectAndLoadCities() {
    const lat = info_window.querySelector("#cam-lat")?.value.trim();
    const lng = info_window.querySelector("#cam-lng")?.value.trim();
    const ip  = info_window.querySelector("#cam-ip")?.value.trim();
    try {
        let regionId = 0;
        if (lat && lng) {
            const r = await api.get(`/cam/region?lat=${encodeURIComponent(lat)}&lng=${encodeURIComponent(lng)}`);
            const rd = await r.json();
            regionId = rd.id ?? 0;
        } else if (ip) {
            const r = await api.get(`/cam/region_by_ip?ip=${encodeURIComponent(ip)}`);
            const rd = await r.json();
            regionId = rd.id ?? 0;
        }
        await window.__loadCityList(regionId || "");
    } catch (e) {
        console.error("City region detection failed:", e);
    }
}

cityPickerBtn?.addEventListener("click", async (e) => {
    e.stopPropagation();

    // In add-camera mode, block city picker if no coords
    const isAddMode = !!info_window.querySelector("#add-camera");
    const lat = info_window.querySelector("#cam-lat")?.value.trim();
    const lng = info_window.querySelector("#cam-lng")?.value.trim();

    if (isAddMode && (!lat || !lng)) {
        notifications.error("Укажите координаты для выбора города");
        return;
    }

    cityPickerDropdown?.classList.toggle("open");
    if (!cityPickerDropdown?.classList.contains("open")) return;
    citySearch?.focus();

    const ip = info_window.querySelector("#cam-ip")?.value.trim();
    const coordKey = lat && lng ? `${lat}:${lng}` : null;

    // Same coords as last open and data already loaded — render from memory, no requests
    if (coordKey && coordKey === lastPickerCoordKey && cityPickerData.length > 0) {
        renderCityList(cityPickerData);
        return;
    }

    // Show skeleton immediately so stale cities never flash during region detection
    showPickerSkeleton(document.getElementById("city-list"));

    try {
        let newRegionId = 0;
        if (coordKey) {
            const r = await api.get(`/cam/region?lat=${encodeURIComponent(lat)}&lng=${encodeURIComponent(lng)}`);
            const rd = await r.json();
            newRegionId = rd.id ?? 0;
            lastPickerCoordKey = coordKey;
        } else if (cityPickerData.length === 0 && ip) {
            const r = await api.get(`/cam/region_by_ip?ip=${encodeURIComponent(ip)}`);
            const rd = await r.json();
            newRegionId = rd.id ?? 0;
        }
        if (newRegionId !== citiesLoadedForRegion || cityPickerData.length === 0) {
            await window.__loadCityList(newRegionId || "");
        } else {
            renderCityList(cityPickerData);
        }
    } catch (e) {
        console.error("City region detection failed:", e);
        const cityListEl = document.getElementById("city-list");
        if (cityListEl) {
            cityListEl.innerHTML = "";
            const msg = document.createElement("div");
            msg.className = "picker-list-msg picker-list-msg--error";
            msg.textContent = "Ошибка получения городов";
            cityListEl.appendChild(msg);
        }
    }
});


citySearch?.addEventListener("input", (e) => {
    renderCityList(cityPickerData, e.target.value);
});

const cityAddBtn  = document.getElementById("city-add-btn");
const cityAddForm = document.getElementById("city-add-form");

function showCityAddForm() {
    cityAddBtn.style.display  = "none";
    cityAddForm.style.display = "flex";
    document.getElementById("city-add-name-rus").focus();
}
function hideCityAddForm() {
    cityAddForm.style.display = "none";
    cityAddBtn.style.display  = "block";
    document.getElementById("city-add-name-rus").value = "";
}

cityAddBtn?.addEventListener("click", showCityAddForm);
document.getElementById("city-add-cancel")?.addEventListener("click", hideCityAddForm);

document.getElementById("city-add-submit")?.addEventListener("click", async (e) => {
    const btn = e.currentTarget;
    if (btn.disabled) return;
    const nameRus = document.getElementById("city-add-name-rus").value.trim();
    if (!nameRus) {
        notifications.error("Укажите название города");
        return;
    }
    if (!currentCityRegionId) {
        notifications.error("Невозможно добавить город: регион не определён");
        return;
    }
    btn.disabled = true;
    try {
        const resp = await api.post("/cam/add_city", {
            name_rus: nameRus,
            region_id: currentCityRegionId,
        });
        const city = await resp.json();

        // Insert the new city into the list in alphabetical order
        cityPickerData.push(city);
        cityPickerData.sort((a, b) => cityLabel(a).localeCompare(cityLabel(b), "ru"));
        renderCityList(cityPickerData);

        // Select the newly added city
        document.getElementById("cam-city-id").value = city.ID;
        document.getElementById("city-picker-display").textContent = cityLabel(city);
        hideCityAddForm();
        cityPickerDropdown?.classList.remove("open");
        notifications.success("Город добавлен");
    } catch (e) {
        console.error("Error adding city:", e);
        notifications.error("Ошибка при добавлении города");
    } finally {
        btn.disabled = false;
    }
});

document.addEventListener("click", (e) => {
    if (!e.target.closest("#city-picker") && !e.target.closest("#city-picker-btn")) {
        cityPickerDropdown?.classList.remove("open");
        hideCityAddForm();
    }
});

// ── Maintainer picker ─────────────────────────────────────────────────────────

const maintainerPickerBtn = document.getElementById("maintainer-picker-btn");
const maintainerPickerDropdown = document.getElementById("maintainer-picker");
const maintainerSearch = document.getElementById("maintainer-search");
const maintainerList = document.getElementById("maintainer-list");

let maintainerPickerData = [];

async function loadMaintainerList() {
    try {
        const resp = await api.get("/cam/maintainers");
        maintainerPickerData = await resp.json();
        renderMaintainerList(maintainerPickerData);
    } catch (e) {
        console.error("Error loading maintainers:", e);
    }
}

function makeMaintainerOpt(m) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "picker-opt";
    btn.dataset.id = m.ID;
    btn.textContent = m.Name;
    if (String(m.ID) === document.getElementById("cam-maintainer-id").value)
        btn.classList.add("selected");
    btn.addEventListener("click", () => {
        document.getElementById("cam-maintainer-id").value = m.ID;
        document.getElementById("maintainer-picker-display").textContent = m.Name;
        maintainerPickerDropdown?.classList.remove("open");
    });
    return btn;
}

function renderMaintainerList(maintainers, filter = "") {
    if (!maintainerList) return;
    maintainerList.innerHTML = "";
    const filtered = filter
        ? maintainers.filter((m) => m.Name.toLowerCase().includes(filter.toLowerCase()))
        : maintainers;
    filtered.forEach((m) => maintainerList.appendChild(makeMaintainerOpt(m)));
}

maintainerPickerBtn?.addEventListener("click", (e) => {
    e.stopPropagation();
    maintainerPickerDropdown?.classList.toggle("open");
    if (maintainerPickerDropdown?.classList.contains("open")) maintainerSearch?.focus();
});

maintainerSearch?.addEventListener("input", (e) => {
    renderMaintainerList(maintainerPickerData, e.target.value);
});

const maintainerAddBtn  = document.getElementById("maintainer-add-btn");
const maintainerAddForm = document.getElementById("maintainer-add-form");

function showMaintainerAddForm() {
    maintainerAddBtn.style.display  = "none";
    maintainerAddForm.style.display = "flex";
    document.getElementById("maintainer-add-name").focus();
}
function hideMaintainerAddForm() {
    maintainerAddForm.style.display = "none";
    maintainerAddBtn.style.display  = "block";
    document.getElementById("maintainer-add-name").value = "";
}

maintainerAddBtn?.addEventListener("click", showMaintainerAddForm);
document.getElementById("maintainer-add-cancel")?.addEventListener("click", hideMaintainerAddForm);

document.getElementById("maintainer-add-submit")?.addEventListener("click", async (e) => {
    const btn = e.currentTarget;
    if (btn.disabled) return;
    const name = document.getElementById("maintainer-add-name").value.trim();
    if (!name) {
        notifications.error("Укажите название производителя");
        return;
    }
    btn.disabled = true;
    try {
        const resp = await api.post("/cam/add_maintainer", { name });
        const maintainer = await resp.json();
        maintainerPickerData.push(maintainer);
        maintainerList?.appendChild(makeMaintainerOpt(maintainer));
        document.getElementById("cam-maintainer-id").value = maintainer.ID;
        document.getElementById("maintainer-picker-display").textContent = maintainer.Name;
        hideMaintainerAddForm();
        maintainerPickerDropdown?.classList.remove("open");
        notifications.success("Производитель добавлен");
    } catch (e) {
        console.error("Error adding maintainer:", e);
        notifications.error("Ошибка при добавлении производителя");
    } finally {
        btn.disabled = false;
    }
});

document.addEventListener("click", (e) => {
    if (!e.target.closest("#maintainer-picker") && !e.target.closest("#maintainer-picker-btn")) {
        maintainerPickerDropdown?.classList.remove("open");
        hideMaintainerAddForm();
    }
});

loadMaintainerList();

// ── Photos ────────────────────────────────────────────────────────────────────

async function sendAdminReq(url, data, method, isForm = false) {
    if (!isForm) {
        return api.fetch(url, { method, body: JSON.stringify(data) });
    }
    const csrf_token = document.cookie
        .split("; ")
        .find((row) => row.startsWith("csrf_token="))
        ?.split("=")[1];
    const response = await fetch(url, {
        credentials: "include",
        method,
        headers: { "X-CSRF-Token": csrf_token },
        body: data,
    });
    if (!response.ok) {
        if (response.status === 404) {
            const ans = await response.json();
            if (ans.redirect) {
                window.location.href = "/auth";
                throw new Error("Session expired");
            }
            throw new Error("Wrong gateway");
        }
        const error = await response.json();
        throw new Error(`HTTP error! status ${response.status}. ${error.error}`);
    }
    return response;
}

const images_container = info_window.querySelector(".cam-images");

document.getElementById("add-photos").addEventListener("change", async (e) => {
    await handleFiles(e.target.files);
    e.target.value = "";
});

images_container.addEventListener("dragstart", (e) => {
    if (e.target.tagName === "IMG") e.preventDefault();
});

images_container.addEventListener("dragover", (e) => {
    e.preventDefault();
    images_container.classList.add("drag-over");
});

images_container.addEventListener("dragleave", () =>
    images_container.classList.remove("drag-over"),
);

images_container.addEventListener("drop", async (e) => {
    e.preventDefault();
    images_container.classList.remove("drag-over");
    await handleFiles(e.dataTransfer.files);
});

async function handleFiles(files) {
    const previews = [];
    const ip = info_window.querySelector("#cam-ip").value.trim();
    const port = info_window.querySelector("#cam-port").value.trim();

    if (!validators.isValidIP(ip) || !validators.isValidPort(port)) {
        notifications.error("IP or port are invalid or empty");
        return;
    }

    for (const file of files) {
        if (!file.type.startsWith("image/")) continue;

        const img = document.createElement("img");
        img.classList.add("uploading");
        img.src = URL.createObjectURL(file);
        img.alt = "camera";
        img.loading = "lazy";
        img.dataset.filename = file.name;

        images_container.appendChild(img);
        previews.push({ file, img });
    }
    await uploadFiles(previews, ip, port);
}

async function uploadFiles(previews, ip, port) {
    if (!previews || !ip || !port) return;

    const formData = new FormData();
    previews.forEach((p, index) => {
        formData.append("photos", p.file);
        formData.append("indexes", index);
    });
    formData.append("ip", ip);
    formData.append("port", port);

    try {
        const res = await sendAdminReq("/cam/upload_photos", formData, "POST", true);
        const results = await res.json();

        results.forEach((r) => {
            const preview = previews[r.index];
            if (r.success) {
                preview.img.src = `/cam/image/${ip}/${r.filename}`;
                preview.img.classList.remove("uploading");
            } else preview.img.remove();
        });

        notifications.success("Successfully added photos");
    } catch (err) {
        previews.forEach((p) => p.img.remove());
        console.error("Error while send photos: " + err);
        notifications.error("Error while send photos");
    }
}

// ── Context menu ──────────────────────────────────────────────────────────────

info_window.querySelector(".cam-images").addEventListener("contextmenu", (e) => {
    e.preventDefault();
    const el = e.target;
    if (el.tagName === "IMG") {
        contextMenu.classList.add("show");

        // Measure the menu and clamp position so it stays within viewport
        const rect = contextMenu.getBoundingClientRect();
        const x = e.clientX;
        const y = e.clientY;
        const maxX = window.innerWidth - rect.width;
        const maxY = window.innerHeight - rect.height;

        contextMenu.style.left = `${Math.min(x, maxX)}px`;
        contextMenu.style.top = `${Math.min(y, maxY)}px`;

        activeImage = el;
    }
});

document.addEventListener("click", (e) => {
    contextMenu.classList.toggle("show", false);
    if (!e.target.closest(".image-context-menu")) activeImage = null;
});

contextMenu.addEventListener("click", async (e) => {
    const el = e.target;
    if (el.id === "delete-photo") {
        const src = activeImage.dataset.src || activeImage.src;
        const splitData = src.split("/");
        const filename = splitData[splitData.length - 1];
        const ip = info_window.querySelector("#cam-ip").value.trim();
        const port = info_window.querySelector("#cam-port").value.trim();

        if (!validators.isValidIP(ip) || !validators.isValidPort(port)) {
            notifications.error("Invalid port or IP");
            return;
        }

        await sendAdminReq("/cam/delete_photo", { ip, port, filename }, "DELETE");
        notifications.success("Photo successfully deleted");
        activeImage.remove();
        activeImage = null;
    }
});
