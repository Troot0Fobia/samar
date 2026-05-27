const map = L.map("map", { center: [49.612, 32.193], zoom: 7, maxZoom: 19, wheelDebounceTime: 10, wheelPxPerZoomLevel: 120 });
const sidebar_button = document.getElementById("sidebar-button");
const info_window = document.getElementById("info-window");
const sidebar = document.getElementById("sidebar");
const image_viewer = document.getElementById("image-viewer");
const slider = document.getElementById("slider");
const sidebar_tabs = sidebar.querySelector(".tabs");
const loadedCameras = new Map();
const camCardCache = new Map(); // `ip:port` → camera_info
const markerClusterOf = new Map(); // camId → cluster
const region_polygons = new Map();
const defaultCluster = L.markerClusterGroup();
map.addLayer(defaultCluster);
window.map = map;
const sidebarIndex = new Map();
const sectionCounts = new Map(); // key → {defined, total}
const countSpans = new Map(); // key → <span>
let totalDefinedCams = 0;
const definedCamCountEl = document.getElementById("defined-cam-count");
let activeCamLabel = null; // currently highlighted .cam-label in sidebar

function setActiveCamLabel(el) {
    if (activeCamLabel) activeCamLabel.classList.remove("cam-label--active");
    activeCamLabel = el ?? null;
    if (activeCamLabel) activeCamLabel.classList.add("cam-label--active");
}
const IS_MODER = document.body.dataset.moder === "1";
// ── Image viewer state ──
const IV_MIN = 0.5, IV_MAX = 8;
let ivScale = 1, ivRotation = 0, ivPanX = 0, ivPanY = 0;
let ivZoomMode = false, ivDragging = false;
let ivDragStartX = 0, ivDragStartY = 0, ivPanStartX = 0, ivPanStartY = 0;
let focusedMarker = null;
const icon = L.icon({
    iconUrl: "/assets/icons/camera.png",
    iconSize: [32, 32],
});
const focusedIcon = L.icon({
    iconUrl: "/assets/icons/camera.png",
    iconSize: [42, 42],
});
const ARROW_SVG = `<svg class="label-arrow" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>`;
const darkTiles = L.tileLayer(
    "https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png",
    {
        maxZoom: 19,
        minZoom: 3,
        attribution: "© OpenStreetMap contributors, © CARTO",
    },
);
const lightTiles = L.tileLayer(
    "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
    {
        maxZoom: 19,
        minZoom: 3,
        attribution: "© OpenStreetMap contributors",
    },
);
function getTilePref() {
    return (
        document.cookie
            .split("; ")
            .find((r) => r.startsWith("tile_theme="))
            ?.split("=")[1] ?? "dark"
    );
}
function setTilePref(val) {
    document.cookie = `tile_theme=${val}; path=/; max-age=31536000; SameSite=Lax`;
}

let darkTilesActive = getTilePref() !== "light";

if (darkTilesActive) {
    darkTiles.addTo(map);
} else {
    lightTiles.addTo(map);
}

function syncTileIcons() {
    document.getElementById("tile-icon-dark").style.display = darkTilesActive
        ? ""
        : "none";
    document.getElementById("tile-icon-light").style.display = darkTilesActive
        ? "none"
        : "";
}
syncTileIcons();

document.getElementById("tile-toggle").addEventListener("click", () => {
    if (darkTilesActive) {
        map.removeLayer(darkTiles);
        lightTiles.addTo(map);
    } else {
        map.removeLayer(lightTiles);
        darkTiles.addTo(map);
    }
    darkTilesActive = !darkTilesActive;
    setTilePref(darkTilesActive ? "dark" : "light");
    syncTileIcons();
});

map.on("drag", () =>
    map.panInsideBounds(L.latLngBounds(L.latLng(-85, -180), L.latLng(85, 180))),
);
map.on("click", () => {
    if (focusedMarker) {
        focusedMarker.setIcon(icon);
        focusedMarker = null;
    }
});

const mapCoordsEl = document.getElementById("map-coords");
const mapCoordsText = document.getElementById("map-coords-text");
let lastContextCoords = null;

map.on("contextmenu", (e) => {
    lastContextCoords = e.latlng;
    const lat = e.latlng.lat.toFixed(6);
    const lng = e.latlng.lng.toFixed(6);
    mapCoordsText.textContent = `${lat}°N, ${lng}°E`;
    mapCoordsEl.classList.add("visible");
});

document.getElementById("map-coords-copy").addEventListener("click", () => {
    if (!lastContextCoords) return;
    const text = `${lastContextCoords.lat.toFixed(6)}, ${lastContextCoords.lng.toFixed(6)}`;
    navigator.clipboard.writeText(text)
        .then(() => notifications.success("Координаты скопированы: " + text))
        .catch(() => notifications.error("Не удалось скопировать координаты"));
});

const mapCoordsFill = document.getElementById("map-coords-fill");
if (mapCoordsFill) {
    mapCoordsFill.addEventListener("click", () => {
        if (!lastContextCoords) return;
        if (!info_window.classList.contains("open")) {
            notifications.error("Откройте карточку камеры");
            return;
        }
        const latInput = info_window.querySelector("#cam-lat");
        const lngInput = info_window.querySelector("#cam-lng");
        if (!latInput || !lngInput || latInput.hasAttribute("readonly")) {
            notifications.error("Форма недоступна для редактирования");
            return;
        }
        latInput.value = lastContextCoords.lat.toFixed(6);
        lngInput.value = lastContextCoords.lng.toFixed(6);
        notifications.success("Координаты вставлены в форму");
    });
}

const api = {
    async fetch(url, options = {}) {
        const csrf_token = document.cookie
            .split("; ")
            .find((row) => row.startsWith("csrf_token="))
            ?.split("=")[1];

        const defaultOptions = {
            credentials: "include",
            headers: {
                "Content-Type": "application/json",
                "X-CSRF-Token": csrf_token,
            },
        };

        const finalOptions = {
            ...defaultOptions,
            ...options,
            headers: {
                ...defaultOptions.headers,
                ...options.headers,
            },
        };

        const response = await fetch(url, finalOptions);

        if (!response.ok) {
            if (response.status === 404) {
                const ans = await response.json();
                if (ans.redirect) {
                    window.location.href = "/auth";
                    throw new Error("Session expired");
                }
                throw new Error("Wrong gateway");
            }
            const ans = await response.json();
            let error_msg = "";
            if (ans?.error) error_msg = `. Message ${ans.error}`;
            throw new Error(`HTTP error! status ${response.status}${error_msg}`);
        }

        return response;
    },

    async get(url) {
        return this.fetch(url);
    },

    async post(url, data) {
        return this.fetch(url, {
            method: "POST",
            body: JSON.stringify(data),
        });
    },
};

const validators = {
    isValidIP: (ip) => {
        const regex = /^(\d{1,3}\.){3}\d{1,3}$/;
        if (!regex.test(ip)) return false;
        return ip
            .split(".")
            .every((num) => parseInt(num) >= 0 && parseInt(num) <= 255);
    },

    isValidPort: (port) => {
        const portNum = parseInt(port);
        return portNum > 0 && portNum <= 65535;
    },

    isValidCoords: (coords) => {
        try {
            let split_coords = null;
            if (coords.includes(",")) split_coords = coords.split(",");
            else if (coords.includes(" ")) split_coords = coords.split(" ");
            else return false;

            parseFloat(split_coords[0].trim());
            parseFloat(split_coords[1].trim());
            return true;
        } catch {
            return false;
        }
    },
};

const notifications = (() => {
    let container = null;

    function getContainer() {
        if (!container) {
            container = document.createElement("div");
            container.id = "notification-container";
            document.body.appendChild(container);
        }
        return container;
    }

    function show(message, type = "info") {
        const el = document.createElement("div");
        el.className = `notification ${type}`;
        el.textContent = message;
        getContainer().appendChild(el);

        setTimeout(() => {
            el.remove();
        }, 3000);
    }

    return {
        show,
        error: (message) => show(message, "error"),
        success: (message) => show(message, "success"),
    };
})();

document.addEventListener("keyup", (event) => {
    if (event.key === "Escape") {
        if (image_viewer.classList.contains("open")) {
            closeViewer();
            return;
        }
        const _geoDrop = document.getElementById("topbar-geo-dropdown");
        if (_geoDrop?.classList.contains("open")) {
            _geoDrop.classList.remove("open");
            _geoDrop.innerHTML = "";
            document.getElementById("geo-search-input")?.blur();
            return;
        }
        const _cityPicker = document.getElementById("city-picker");
        if (_cityPicker?.classList.contains("open")) {
            _cityPicker.classList.remove("open");
            return;
        }
        if (info_window.classList.contains("open")) {
            info_window.classList.remove("open");
            setActiveCamLabel(null);
        }
    }
});

document.getElementById("fly-to-cam").addEventListener("click", () => {
    const lat = parseFloat(info_window.querySelector("#cam-lat").value);
    const lng = parseFloat(info_window.querySelector("#cam-lng").value);
    if (!isNaN(lat) && !isNaN(lng)) map.flyTo([lat, lng], 17);
});

document.getElementById("copy-coords").addEventListener("click", () => {
    const lat = info_window.querySelector("#cam-lat").value.trim();
    const lng = info_window.querySelector("#cam-lng").value.trim();
    if (!lat || !lng) return;
    navigator.clipboard.writeText(`${lat}, ${lng}`)
        .then(() => notifications.success("Координаты скопированы"))
        .catch(() => notifications.error("Не удалось скопировать координаты"));
});

function parseCoords(text) {
    const norm = text.replace(/\s+/g, " ");
    const m = norm.match(/(-?\d{1,3}(?:[.,]\d+)?)[,;\s]+(-?\d{1,3}(?:[.,]\d+)?)/);
    if (!m) return null;
    const lat = parseFloat(m[1].replace(",", "."));
    const lng = parseFloat(m[2].replace(",", "."));
    if (isNaN(lat) || isNaN(lng)) return null;
    if (lat < -90 || lat > 90 || lng < -180 || lng > 180) return null;
    return { lat: lat.toFixed(6), lng: lng.toFixed(6) };
}

const pasteBtn = document.getElementById("paste-coords");
if (pasteBtn) {
    pasteBtn.addEventListener("click", () => {
        navigator.clipboard.readText()
            .then(text => {
                const coords = parseCoords(text.trim());
                if (!coords) {
                    notifications.error("Не удалось распознать координаты");
                    return;
                }
                info_window.querySelector("#cam-lat").value = coords.lat;
                info_window.querySelector("#cam-lng").value = coords.lng;
                ["#cam-lat", "#cam-lng"].forEach(sel => {
                    info_window.querySelector(sel).dispatchEvent(new Event("input", { bubbles: true }));
                });
                notifications.success(`Координаты вставлены: ${coords.lat}, ${coords.lng}`);
            })
            .catch(() => notifications.error("Нет доступа к буферу обмена"));
    });
}

info_window.addEventListener("click", (e) => {
    const el = e.target;
    if (el.closest("#close-button")) { info_window.classList.remove("open"); setActiveCamLabel(null); }
    else if (el.matches('input[type="text"][readonly]') && el.value) {
        el.select();
        el.setSelectionRange(0, 99999);
        navigator.clipboard.writeText(el.value).catch(() => {});
        notifications.success("Copied value: " + el.value);
    } else if (el.matches("img") && el.closest(".cam-images")) {
        renderImageViewer(e);
    }
});

sidebar.addEventListener("click", async (e) => {
    const el = e.target;
    if (el.closest(".label")) {
        const label = el.closest(".label");
        if (el.closest(".cam-label")) {
            const content = label.nextElementSibling;
            content?.querySelectorAll("img").forEach((img) => {
                if (!img.getAttribute("src")) img.src = img.dataset.src;
            });

            if (el.classList.contains("label-text")) {
                if (!isCamCardOpen(label.dataset.ip, label.dataset.port))
                    await receiveCamCard(label.dataset.ip, label.dataset.port);
                return;
            }
        }
        const next = label.nextElementSibling;
        if (next?.classList.contains("content")) {
            label.querySelector(".label-arrow")?.classList.toggle("open");
            next.classList.toggle("expand");
        }
    } else if (el.closest("#expand-all")) {
        sidebar_tabs.querySelectorAll(".label").forEach((label) => {
            label.querySelector(".label-arrow")?.classList.add("open");
            const content = label.nextElementSibling;
            if (!content) return;
            content.querySelectorAll("img").forEach((img) => {
                if (!img.getAttribute("src")) img.src = img.dataset.src;
            });
        });
        sidebar_tabs.querySelectorAll(".content").forEach((content) => {
            content.classList.add("expand");
        });
    } else if (el.closest("#collapse-all")) {
        sidebar_tabs
            .querySelectorAll(".label")
            .forEach((label) =>
                label.querySelector(".label-arrow")?.classList.remove("open"),
            );
        sidebar_tabs
            .querySelectorAll(".content")
            .forEach((content) => content.classList.remove("expand"));
    } else if (el.matches("img") && el.closest(".content")) {
        renderImageViewer(e);
    }
});

function debounce(func, wait) {
    let timeout;
    return function exectutedFunction(...args) {
        clearTimeout(timeout);
        timeout = setTimeout(() => func.apply(this, args), wait);
    };
}

const searchField = document.getElementById("search-field");
const searchClear = document.getElementById("search-clear");

searchClear.addEventListener("click", () => {
    searchField.value = "";
    searchClear.style.display = "none";
    searchField.dispatchEvent(new Event("input"));
    searchField.focus();
});

searchField.addEventListener(
    "input",
    debounce((e) => {
        searchClear.style.display = e.target.value ? "flex" : "none";
        const query = e.target.value.trim().toLowerCase();

        if (!query) {
            sidebar_tabs
                .querySelectorAll(".label, .content")
                .forEach((el) => el.classList.remove("filtered-out"));
            return;
        }

        sidebar_tabs
            .querySelectorAll(".label, .content")
            .forEach((el) => el.classList.add("filtered-out"));

        const matchesQuery = (el) => {
            if (!el) return false;

            if (el.classList.contains("label")) {
                const labelText =
                    el.querySelector(".label-text")?.textContent.toLowerCase() || "";
                const labelIPDataset = el.dataset.ip;
                return labelText.includes(query) || labelIPDataset?.includes(query);
            }

            if (el.classList.contains("cam-tab")) {
                const dataIp = el.dataset.ip?.toLowerCase() || "";
                const dataPort = el.dataset.port?.toLowerCase() || "";
                return dataIp.includes(query) || dataPort.includes(query);
            }

            return false;
        };

        const showParents = (element) => {
            let current = element;
            while (current && current !== sidebar_tabs) {
                current.classList.remove("filtered-out");

                if (current.classList.contains("label")) {
                    const nextContent = current.nextElementSibling;
                    if (nextContent?.classList.contains("content"))
                        nextContent.classList.remove("filtered-out");
                }

                const parent = current.parentElement;
                const parentLabel = parent?.previousElementSibling;
                if (parentLabel?.classList.contains("label"))
                    parentLabel.classList.remove("filtered-out");

                current = parent;
            }
        };

        const showChildren = (element) => {
            if (!element?.classList.contains("content")) return;

            element.classList.remove("filtered-out");
            for (const child of element.children) {
                child.classList.remove("filtered-out");
                if (child.classList.contains("content")) showChildren(child);
            }
        };

        sidebar_tabs.querySelectorAll(".label").forEach((label) => {
            if (!matchesQuery(label)) return;

            showChildren(label.nextElementSibling);
            showParents(label);
        });

        sidebar_tabs.querySelectorAll(".cam-tab").forEach((camTab) => {
            if (!matchesQuery(camTab)) return;

            const parentLabel = camTab.previousElementSibling;
            if (parentLabel?.classList.contains("label"))
                parentLabel.classList.remove("filtered-out");

            showParents(camTab);
        });
    }, 600),
);

sidebar_button.addEventListener("click", () => {
    const closed = sidebar.classList.toggle("sidebar-closed");
    sidebar_button.classList.toggle("open", !closed);
});

document.getElementById("logout-button").addEventListener("click", async () => {
    try {
        await api.post("/auth/logout", {});
        window.location.href = "/auth";
    } catch (e) {
        console.error("Error in log out: " + e);
    }
});

document.addEventListener("DOMContentLoaded", async () => {
    try {
        const response = await api.get("/cam/polygons");
        const text = await response.text();
        if (!text) return;
        const geo_region_json = JSON.parse(text);
        if (!geo_region_json) return;

        const geo_json = L.geoJSON(geo_region_json, {
            style: {
                color: "red",
                fillColor: "blue",
                fillOpacity: 0,
                weight: 1,
            },
            onEachFeature: function(feature, layer) {
                layer.on({
                    click: function() {
                        const regions = sidebar_tabs.querySelectorAll(".region-tab");
                        const toggleOutOfBounds = (region, isOut) => {
                            const label = region.previousElementSibling;
                            if (label?.classList.contains("label"))
                                label.classList.toggle("out-of-bounds", isOut);
                            region.classList.toggle("out-of-bounds", isOut);
                        };

                        if (layer.options.fillOpacity) {
                            regions.forEach((region) => toggleOutOfBounds(region, false));
                            layer.setStyle({ fillOpacity: 0 });
                            sidebar_tabs
                                .querySelectorAll(".country-tab")
                                .forEach((country) => toggleOutOfBounds(country, false));
                        } else {
                            geo_json.resetStyle();
                            sidebar_tabs
                                .querySelectorAll(".country-tab")
                                .forEach((country) => toggleOutOfBounds(country, true));
                            regions.forEach((region) => {
                                toggleOutOfBounds(
                                    region,
                                    region.dataset.region !== feature.properties.name,
                                );
                                if (region.dataset.region === feature.properties.name) {
                                    const country_tab = region.closest(".country-tab");
                                    country_tab.classList.toggle("out-of-bounds", false);
                                    const country_label = country_tab.previousElementSibling;
                                    if (country_label?.classList.contains("label"))
                                        country_label.classList.toggle("out-of-bounds", false);
                                }
                            });
                            layer.setStyle({ fillOpacity: 0.2 });
                        }
                        sidebar.classList.remove("sidebar-closed");
                    },
                });
                const cluster = L.markerClusterGroup();
                region_polygons.set(feature.properties.name, cluster);
                map.addLayer(cluster);
            },
        }).addTo(map);
    } catch (error) {
        console.error("Error in fetching polygons: " + error);
    }

    try {
        const response = await api.get("/cams");
        const cameras = await response.json();
        if (!cameras) return;

        // Sort cameras: Country_rus → Region_rus → City_rus → IsDefined → IP:Port
        cameras.sort((a, b) => {
            let c = (a.Country_rus || a.Country).localeCompare(b.Country_rus || b.Country, "ru");
            if (c !== 0) return c;
            c = (a.Region_rus || a.Region).localeCompare(b.Region_rus || b.Region, "ru");
            if (c !== 0) return c;
            c = (a.City_rus || a.City).localeCompare(b.City_rus || b.City, "ru");
            if (c !== 0) return c;
            c = (b.IsDefined ? 0 : 1) - (a.IsDefined ? 0 : 1);
            if (c !== 0) return c;
            return (a.IP + a.Port).localeCompare(b.IP + b.Port, "en");
        });

        const CHUNK = 50;
        let i = 0;
        const renderNext = () => {
            if (i >= cameras.length) return;
            renderCams(cameras.slice(i, i + CHUNK), sidebar_tabs);
            i += CHUNK;
            setTimeout(renderNext, 0);
        };
        setTimeout(renderNext, 0);
    } catch (e) {
        console.error("Error in rendering cameras: " + e);
    }
});

function renderCams(cameras, container) {
    const clusterBatches = new Map();

    // Pass 1: map markers — без DOM, только данные
    for (const camera of cameras) {
        const id = String(camera.ID);
        if (
            camera.IsDefined &&
            camera.Status === "valid" &&
            !loadedCameras.has(id)
        ) {
            const marker = L.marker([camera.Lat, camera.Lng], {
                icon,
                title: camera.Comment,
            }).on("click", async () => {
                const content = sidebarIndex.get(`cam:${camera.IP}:${camera.Port}`);
                content?.querySelectorAll("img").forEach((img) => {
                    if (!img.getAttribute("src")) img.src = img.dataset.src;
                });
                if (!isCamCardOpen(camera.IP, camera.Port))
                    await receiveCamCard(camera.IP, camera.Port);
            });
            const cluster = region_polygons.get(camera.Region) ?? defaultCluster;
            if (!clusterBatches.has(cluster)) clusterBatches.set(cluster, []);
            clusterBatches.get(cluster).push(marker);
            loadedCameras.set(id, marker);
            markerClusterOf.set(id, cluster);
        }
    }
    clusterBatches.forEach((markers, cluster) => cluster.addLayers(markers));

    // Pass 2: сайдбар — O(1) поиск через sidebarIndex вместо querySelector
    for (const camera of cameras) {
        const isDefined = camera.IsDefined;
        const id = String(camera.ID);
        const typeClass = isDefined ? "identified" : "unidentified";
        const typeName = isDefined ? "Найденные" : "Не найденные";

        const ck = `c:${camera.Country}`;
        let countryContent = sidebarIndex.get(ck);
        if (!countryContent) {
            countryContent = makeSectionContent("country", camera.Country, camera.Country_rus);
            countryContent.dataset.indexKey = ck;
            sidebarIndex.set(ck, countryContent);
            insertSectionSorted(
                container,
                makeSectionLabel("country", camera.Country, camera.Country_rus, ck),
                countryContent,
            );
        }

        const rk = `r:${camera.Country}:${camera.Region}`;
        let regionContent = sidebarIndex.get(rk);
        if (!regionContent) {
            regionContent = makeSectionContent("region", camera.Region, camera.Region_rus);
            regionContent.dataset.indexKey = rk;
            sidebarIndex.set(rk, regionContent);
            insertSectionSorted(
                countryContent,
                makeSectionLabel("region", camera.Region, camera.Region_rus, rk),
                regionContent,
            );
        }

        const cik = `ci:${camera.Country}:${camera.Region}:${camera.City}`;
        let cityContent = sidebarIndex.get(cik);
        if (!cityContent) {
            cityContent = makeSectionContent("city", camera.City, camera.City_rus);
            cityContent.dataset.indexKey = cik;
            sidebarIndex.set(cik, cityContent);
            insertSectionSorted(
                regionContent,
                makeSectionLabel("city", camera.City, camera.City_rus, cik),
                cityContent,
            );
        }

        const tk = `t:${camera.Country}:${camera.Region}:${camera.City}:${typeClass}`;
        let typeContent = sidebarIndex.get(tk);
        if (!typeContent) {
            typeContent = makeTypeContent(typeClass);
            typeContent.dataset.indexKey = tk;
            sidebarIndex.set(tk, typeContent);
            cityContent.append(makeTypeLabel(typeName, tk), typeContent);
        }

        // Update section counts
        const defVal = isDefined ? 1 : 0;
        for (const key of [ck, rk, cik, tk]) {
            if (!sectionCounts.has(key))
                sectionCounts.set(key, { defined: 0, total: 0 });
            const c = sectionCounts.get(key);
            c.total++;
            c.defined += defVal;
            const span = countSpans.get(key);
            if (span)
                span.textContent = IS_MODER
                    ? `(${c.defined}/${c.total})`
                    : `(${c.defined})`;
        }
        if (isDefined) {
            totalDefinedCams++;
            if (definedCamCountEl) definedCamCountEl.textContent = totalDefinedCams;
        }

        const camk = `cam:${camera.IP}:${camera.Port}`;
        if (!sidebarIndex.has(camk)) {
            const camName =
                getCamDisplayName(camera.IP, camera.Port, camera.Name, camera.IsDefined) +
                (camera.Images?.length > 0 ? ` (${camera.Images.length})` : "");
            const camContent = makeCamContent(camera.IP, camera.Port, camera.Images);
            sidebarIndex.set(camk, camContent);
            typeContent.append(
                makeCamLabel({
                    name: camName,
                    ip: camera.IP,
                    port: camera.Port,
                    id: isDefined ? id : null,
                    status: camera.Status,
                }),
                camContent,
            );
        }
    }
}

window.__removeCamMarker = function(camId) {
    if (!camId) return;
    const marker = loadedCameras.get(camId);
    if (!marker) return;
    const cluster = markerClusterOf.get(camId) ?? defaultCluster;
    cluster.removeLayer(marker);
    loadedCameras.delete(camId);
    markerClusterOf.delete(camId);
};

window.__invalidateCamCard = function(ip, port) {
    camCardCache.delete(`${ip}:${port}`);
};

window.__removeCamFromSidebar = function(ip, port) {
    const cam_label = sidebar.querySelector(`[data-ip="${ip}"][data-port="${port}"]`);
    if (!cam_label) return;
    const cam_content = cam_label.nextElementSibling?.classList.contains("cam-tab")
        ? cam_label.nextElementSibling : null;

    const typeContent    = cam_label.closest(".identified-tab, .unidentified-tab");
    const cityContent    = cam_label.closest(".city-tab");
    const regionContent  = cam_label.closest(".region-tab");
    const countryContent = cam_label.closest(".country-tab");

    const typeKey    = typeContent?.dataset.indexKey;
    const cityKey    = cityContent?.dataset.indexKey;
    const regionKey  = regionContent?.dataset.indexKey;
    const countryKey = countryContent?.dataset.indexKey;
    const wasIdentified = typeContent?.classList.contains("identified-tab") ?? false;
    const defVal = wasIdentified ? 1 : 0;

    sidebarIndex.delete(`cam:${ip}:${port}`);
    cam_label.remove();
    cam_content?.remove();

    for (const key of [typeKey, cityKey, regionKey, countryKey]) {
        if (!key) continue;
        const c = sectionCounts.get(key);
        if (!c) continue;
        c.total--;
        c.defined -= defVal;
        const span = countSpans.get(key);
        if (span) span.textContent = IS_MODER ? `(${c.defined}/${c.total})` : `(${c.defined})`;
    }

    if (typeContent?.children.length === 0) {
        typeContent.previousElementSibling?.remove();
        typeContent.remove();
        sidebarIndex.delete(typeKey);
        sectionCounts.delete(typeKey);
        countSpans.delete(typeKey);
    }
    if (cityContent?.children.length === 0) {
        cityContent.previousElementSibling?.remove();
        cityContent.remove();
        sidebarIndex.delete(cityKey);
        sectionCounts.delete(cityKey);
        countSpans.delete(cityKey);
    }
    if (regionContent?.children.length === 0) {
        regionContent.previousElementSibling?.remove();
        regionContent.remove();
        sidebarIndex.delete(regionKey);
        sectionCounts.delete(regionKey);
        countSpans.delete(regionKey);
    }
    if (countryContent?.children.length === 0) {
        countryContent.previousElementSibling?.remove();
        countryContent.remove();
        sidebarIndex.delete(countryKey);
        sectionCounts.delete(countryKey);
        countSpans.delete(countryKey);
    }

    if (wasIdentified) {
        totalDefinedCams--;
        if (definedCamCountEl) definedCamCountEl.textContent = totalDefinedCams;
    }
};

window.__syncCamMarker = function(camId, status, lat, lng, ip, port, regionName) {
    const id = String(camId);
    if (status === "valid") {
        if (loadedCameras.has(id) || !lat || !lng) return;
        const marker = L.marker([lat, lng], { icon }).on("click", async () => {
            const content = sidebarIndex.get(`cam:${ip}:${port}`);
            content?.querySelectorAll("img").forEach((img) => {
                if (!img.getAttribute("src")) img.src = img.dataset.src;
            });
            if (!isCamCardOpen(ip, port)) await receiveCamCard(ip, port);
        });
        const cluster = region_polygons.get(regionName) ?? defaultCluster;
        cluster.addLayer(marker);
        loadedCameras.set(id, marker);
        markerClusterOf.set(id, cluster);
    } else {
        window.__removeCamMarker(id);
    }
};

function makeSectionLabel(type, name, nameRus, key) {
    const el = document.createElement("div");
    el.className = `label ${type}-label`;
    el.dataset.name = name;
    el.innerHTML = `<div class="label-text"></div><div class="cam-icons"><span class="label-cam-count"></span>${ARROW_SVG}</div>`;
    el.querySelector(".label-text").textContent = nameRus ?? name;
    if (key) countSpans.set(key, el.querySelector(".label-cam-count"));
    return el;
}

function makeSectionContent(type, name, nameRus) {
    const el = document.createElement("div");
    el.className = `content ${type}-tab`;
    el.dataset[type] = name;
    el.dataset.sortName = nameRus || name;
    return el;
}

// Insert a label+content pair into a container at the correct alphabetical position (by sortName = _rus)
function insertSectionSorted(container, label, content) {
    const name = content.dataset.sortName || "";
    const children = Array.from(container.children).filter(
        (el) => el.classList.contains("content") && el.className.includes("-tab")
    );
    let insertBefore = null;
    for (const child of children) {
        const childName = child.dataset.sortName || "";
        if (childName.localeCompare(name, "ru") > 0) {
            insertBefore = child;
            break;
        }
    }
    if (insertBefore) {
        container.insertBefore(label, insertBefore);
        container.insertBefore(content, insertBefore.nextSibling);
    } else {
        container.append(label, content);
    }
}

function makeTypeLabel(name, key) {
    const el = document.createElement("div");
    el.className = "label type-label";
    el.innerHTML = `<div class="label-text">${name}</div><div class="cam-icons"><span class="label-cam-count"></span>${ARROW_SVG}</div>`;
    if (key) countSpans.set(key, el.querySelector(".label-cam-count"));
    return el;
}

function makeTypeContent(typeClass) {
    const el = document.createElement("div");
    el.className = `content ${typeClass}-tab`;
    el.dataset.type = typeClass;
    return el;
}

function makeCamLabel({ name, ip, port, id, status }) {
    const el = document.createElement("div");
    el.className = "label cam-label";
    el.dataset.ip = ip;
    el.dataset.port = port;
    if (id) el.dataset.id = id;
    const STATUS_ICONS = {
        invalid:       `<svg class="cam-icon-status" viewBox="0 0 24 24" fill="none" stroke="#f87171" stroke-width="2.5" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>`,
        duplicate:     `<svg class="cam-icon-status" viewBox="0 0 24 24" fill="none" stroke="#fbbf24" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>`,
        undetectable:   `<svg class="cam-icon-status" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" stroke-width="2.5" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`,
    };
    const statusIcon = STATUS_ICONS[status] ?? "";
    el.innerHTML = `<div class="label-text"></div><div class="cam-icons">${statusIcon}${ARROW_SVG}</div>`;
    el.querySelector(".label-text").textContent = name;
    return el;
}

function makeCamContent(ip, port, images) {
    const el = document.createElement("div");
    el.className = "content cam-tab";
    el.dataset.ip = ip;
    el.dataset.port = port;
    images?.forEach((image) => {
        const img = document.createElement("img");
        img.dataset.src = `/cam/image/${ip}/${image}`;
        img.alt = "camera";
        img.loading = "lazy";
        el.appendChild(img);
    });
    return el;
}

function getCamDisplayName(ip, port, name, isDefined) {
    if (!isDefined && (!name || name === ip)) return `${ip}:${port}`;
    return name || `${ip}:${port}`;
}

function updateCamLabelDisplay(cam_label, ip, port, isDefined, name, status, id) {
    const labelText = cam_label.querySelector(".label-text");
    if (labelText) {
        const imgCount = cam_label.nextElementSibling?.querySelectorAll?.("img").length ?? 0;
        const displayName = getCamDisplayName(ip, port, name, isDefined);
        labelText.textContent = displayName + (imgCount > 0 ? ` (${imgCount})` : "");
    }
    if (id) cam_label.dataset.id = String(id);
    else delete cam_label.dataset.id;
    const STATUS_ICONS = {
        invalid:     `<svg class="cam-icon-status" viewBox="0 0 24 24" fill="none" stroke="#f87171" stroke-width="2.5" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>`,
        duplicate:   `<svg class="cam-icon-status" viewBox="0 0 24 24" fill="none" stroke="#fbbf24" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>`,
        undetectable: `<svg class="cam-icon-status" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" stroke-width="2.5" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`,
    };
    const cam_icons = cam_label.querySelector(".cam-icons");
    cam_icons?.querySelector(".cam-icon-status")?.remove();
    if (STATUS_ICONS[status]) cam_icons?.insertAdjacentHTML("afterbegin", STATUS_ICONS[status]);
}

window.__updateCamInSidebar = function(camData) {
    const { IP, Port, IsDefined, Name, Status, City, City_rus, Region, Region_rus, Country, Country_rus, ID } = camData;

    const cam_label = sidebar.querySelector(`[data-ip="${IP}"][data-port="${Port}"]`);
    if (!cam_label) return;
    const cam_content = cam_label.nextElementSibling?.classList.contains("cam-tab")
        ? cam_label.nextElementSibling : null;

    const oldTypeContent   = cam_label.closest(".identified-tab, .unidentified-tab");
    const oldCityContent   = cam_label.closest(".city-tab");
    const oldRegionContent = cam_label.closest(".region-tab");
    const oldCountryContent = cam_label.closest(".country-tab");

    const oldTypeKey    = oldTypeContent?.dataset.indexKey;
    const oldCityKey    = oldCityContent?.dataset.indexKey;
    const oldRegionKey  = oldRegionContent?.dataset.indexKey;
    const oldCountryKey = oldCountryContent?.dataset.indexKey;
    const oldTypeClass  = oldTypeContent?.classList.contains("identified-tab") ? "identified" : "unidentified";

    const newTypeClass = IsDefined ? "identified" : "unidentified";
    const ck  = `c:${Country}`;
    const rk  = `r:${Country}:${Region}`;
    const cik = `ci:${Country}:${Region}:${City}`;
    const tk  = `t:${Country}:${Region}:${City}:${newTypeClass}`;

    updateCamLabelDisplay(cam_label, IP, Port, IsDefined, Name, Status, ID);

    if (oldCityKey === cik && oldTypeKey === tk) return;

    // Decrement counts on old path
    const oldDefVal = oldTypeClass === "identified" ? 1 : 0;
    for (const key of [oldCountryKey, oldRegionKey, oldCityKey, oldTypeKey]) {
        if (!key) continue;
        const c = sectionCounts.get(key);
        if (!c) continue;
        c.total--;
        c.defined -= oldDefVal;
        const span = countSpans.get(key);
        if (span) span.textContent = IS_MODER ? `(${c.defined}/${c.total})` : `(${c.defined})`;
    }
    if (oldTypeClass === "identified") {
        totalDefinedCams--;
        if (definedCamCountEl) definedCamCountEl.textContent = totalDefinedCams;
    }

    cam_label.remove();
    cam_content?.remove();

    // Remove empty type section
    if (oldTypeContent?.children.length === 0) {
        oldTypeContent.previousElementSibling?.remove();
        oldTypeContent.remove();
        sidebarIndex.delete(oldTypeKey);
        sectionCounts.delete(oldTypeKey);
        countSpans.delete(oldTypeKey);
    }

    // Remove empty city section (only when city changed)
    if (oldCityKey !== cik && oldCityContent?.children.length === 0) {
        oldCityContent.previousElementSibling?.remove();
        oldCityContent.remove();
        sidebarIndex.delete(oldCityKey);
        sectionCounts.delete(oldCityKey);
        countSpans.delete(oldCityKey);
    }

    // Ensure country → region → city → type sections exist
    let countryContent = sidebarIndex.get(ck);
    if (!countryContent) {
        countryContent = makeSectionContent("country", Country, Country_rus);
        countryContent.dataset.indexKey = ck;
        sidebarIndex.set(ck, countryContent);
        insertSectionSorted(sidebar_tabs, makeSectionLabel("country", Country, Country_rus, ck), countryContent);
    }
    let regionContent = sidebarIndex.get(rk);
    if (!regionContent) {
        regionContent = makeSectionContent("region", Region, Region_rus);
        regionContent.dataset.indexKey = rk;
        sidebarIndex.set(rk, regionContent);
        insertSectionSorted(countryContent, makeSectionLabel("region", Region, Region_rus, rk), regionContent);
    }
    let cityContent = sidebarIndex.get(cik);
    if (!cityContent) {
        cityContent = makeSectionContent("city", City, City_rus);
        cityContent.dataset.indexKey = cik;
        sidebarIndex.set(cik, cityContent);
        insertSectionSorted(regionContent, makeSectionLabel("city", City, City_rus, cik), cityContent);
    }
    let typeContent = sidebarIndex.get(tk);
    if (!typeContent) {
        typeContent = makeTypeContent(newTypeClass);
        typeContent.dataset.indexKey = tk;
        sidebarIndex.set(tk, typeContent);
        cityContent.append(makeTypeLabel(IsDefined ? "Найденные" : "Не найденные", tk), typeContent);
    }

    // Increment counts on new path
    const newDefVal = IsDefined ? 1 : 0;
    for (const key of [ck, rk, cik, tk]) {
        if (!sectionCounts.has(key)) sectionCounts.set(key, { defined: 0, total: 0 });
        const c = sectionCounts.get(key);
        c.total++;
        c.defined += newDefVal;
        const span = countSpans.get(key);
        if (span) span.textContent = IS_MODER ? `(${c.defined}/${c.total})` : `(${c.defined})`;
    }
    if (IsDefined) {
        totalDefinedCams++;
        if (definedCamCountEl) definedCamCountEl.textContent = totalDefinedCams;
    }

    typeContent.append(cam_label);
    if (cam_content) typeContent.append(cam_content);
};

function isCamCardOpen(ip, port) {
    if (!info_window.classList.contains("open")) return false;
    const currentIp = info_window.querySelector("#cam-ip")?.value.trim();
    const currentPort = info_window.querySelector("#cam-port")?.value.trim();
    return currentIp === ip && currentPort === String(port);
}

async function receiveCamCard(ip, port) {
    try {
        if (!validators.isValidIP(ip) || !validators.isValidPort(port))
            throw new Error("Invalid IP or port");

        // If there's pending add-mode data, ask before overwriting
        const isAddActive = window.__isAddModeActive?.();
        const hasPending  = window.__isAddModeDataPending?.();
        if ((isAddActive || hasPending) && window.__newCameraDataExist?.()) {
            if (!confirm("Есть внесенные изменения. Действительно продолжить?")) {
                if (hasPending && !isAddActive) window.__reenterAddMode?.();
                return;
            }
            window.__clearAddModePending?.();
        }

        // Exit add-camera mode cleanly before showing existing camera
        window.__cancelAddMode?.();

        const cacheKey = `${ip}:${port}`;
        let camera_info = camCardCache.get(cacheKey);
        if (!camera_info) {
            const response = await api.get(
                `/cam/${encodeURIComponent(ip)}/${encodeURIComponent(port)}`,
            );
            camera_info = await response.json();
            if (!camera_info) return;
            camCardCache.set(cacheKey, camera_info);
        }

        const cam_label = sidebar.querySelector(
            `[data-ip="${ip}"][data-port="${port}"]`,
        );
        const data = {
            "#cam-name": camera_info.Name ? camera_info.Name : camera_info.IP,
            "#cam-ip": camera_info.IP,
            "#cam-port": camera_info.Port,
            "#cam-login": camera_info.Login,
            "#cam-password": camera_info.Password,
            "#cam-lat": camera_info.Lat ?? "",
            "#cam-lng": camera_info.Lng ?? "",
            "#cam-comment": camera_info.Comment,
            "#cam-address": camera_info.Address,
            "#cam-link": camera_info.Link,
            "#select-cam-status": camera_info.Status,
        };

        Object.entries(data).forEach(([selector, value]) => {
            const element = info_window.querySelector(selector);
            if (element) element.value = value;
        });

        const statusWrap = info_window.querySelector(".status-wrap");
        if (statusWrap) statusWrap.dataset.status = camera_info.Status;
        const statusLabel = info_window.querySelector(".status-label");
        if (statusLabel) {
            const _sl = { valid: "Валидная", invalid: "Невалидная", duplicate: "Дубль", undetectable: "Трудноопределимая" };
            statusLabel.textContent = _sl[camera_info.Status] ?? camera_info.Status;
        }
        if (window.__syncStatusPicker)
            window.__syncStatusPicker(camera_info.Status);

        if (window.__setCityPicker)
            window.__setCityPicker(camera_info.CityID, camera_info.City, camera_info.City_rus, camera_info.RegionID);

        if (window.__setMaintainerPicker)
            window.__setMaintainerPicker(camera_info.MaintainerID, camera_info.Maintainer);

        const cam_images = info_window.querySelector(".cam-images");
        cam_images.innerHTML = "";
        const content_images = cam_label?.nextElementSibling;
        if (content_images?.classList.contains("content"))
            cam_images.innerHTML = content_images.innerHTML;

        setActiveCamLabel(cam_label ?? null);
        info_window.classList.add("open");
        info_window.dataset.camId = camera_info.ID;
        const maintainerName = (camera_info.Maintainer?.Name || camera_info.Maintainer || "").toLowerCase();
        const canConnect = !!(camera_info.Link) || maintainerName === "dahua";
        updateConnectBtn(camera_info.ID, canConnect);
        window.__setDeleteBtnVisible?.(true);
        const showDefine = !camera_info.IsDefined;
        window.__setDefineBtnVisible?.(showDefine);
    } catch (e) {
        console.error("Error while receiving camera info: " + e);
    }
}

// ── Image viewer element references ──
const ivImg = document.getElementById("viewer-img");
const ivWrapper = document.getElementById("img-wrapper");
const ivMainImage = document.getElementById("main-image");
const ivTitle = document.getElementById("viewer-title");
const ivCounter = document.getElementById("viewer-counter");
const ivZoomSlider = document.getElementById("iv-zoom-slider");
const ivZoomFill = document.getElementById("iv-zoom-fill");
const ivZoomPct = document.getElementById("iv-zoom-pct");

function renderImageViewer(event) {
    const pressed_image = event.target;
    const images_content = pressed_image.parentElement;

    ivImg.src = pressed_image.src;
    ivSetTitle(pressed_image.src);
    ivResetTransform();
    ivExitZoom();

    slider.innerHTML = images_content.innerHTML;
    slider.childNodes.forEach((image_elem) => {
        image_elem.addEventListener("click", () => {
            ivSetActiveThumb(image_elem);
        });
        if (image_elem.src === pressed_image.src) {
            image_elem.classList.add("active");
        }
    });
    ivUpdateCounter();

    image_viewer.classList.add("open");
    image_viewer.focus();
}

function ivSetTitle(src) {
    const parts = src.split("/");
    ivTitle.textContent = parts[parts.length - 1];
}

function ivSetActiveThumb(thumb) {
    ivImg.src = thumb.src;
    ivSetTitle(thumb.src);
    ivResetTransform();
    ivExitZoom();
    slider.querySelectorAll("img").forEach(el => el.classList.remove("active"));
    thumb.classList.add("active");
    ivUpdateCounter();
}

function ivUpdateCounter() {
    const all = slider.querySelectorAll("img");
    const active = slider.querySelector("img.active");
    const idx = Array.from(all).indexOf(active) + 1;
    ivCounter.textContent = `${idx} / ${all.length}`;
}

function ivApplyTransform(animated = true) {
    if (!animated) ivWrapper.classList.add("no-transition");
    else ivWrapper.classList.remove("no-transition");
    ivWrapper.style.transform = `translate(${ivPanX}px, ${ivPanY}px) scale(${ivScale}) rotate(${ivRotation}deg)`;
    ivUpdateZoomUI();
}

function ivUpdateZoomUI() {
    const pct = Math.round(ivScale * 100);
    ivZoomPct.textContent = pct + "%";
    ivZoomPct.classList.toggle("zoomed", ivScale !== 1);
    ivZoomSlider.value = pct;
    const fill = ((ivScale * 100 - 50) / (800 - 50)) * 100;
    ivZoomFill.style.width = Math.min(Math.max(fill, 0), 100) + "%";
    const pill = document.getElementById("iv-zoom-pill");
    if (pill) { pill.textContent = pct + "%"; pill.style.display = ivScale !== 1 ? "block" : "none"; }
}

function ivResetTransform() {
    ivScale = 1; ivPanX = 0; ivPanY = 0; ivRotation = 0;
    ivApplyTransform(false);
}

function ivResetZoomPan() {
    ivScale = 1; ivPanX = 0; ivPanY = 0;
    ivApplyTransform();
}

function ivEnterZoom() {
    ivZoomMode = true;
    ivMainImage.classList.add("zoom-active");
}

function ivExitZoom() {
    ivZoomMode = false;
    ivDragging = false;
    ivMainImage.classList.remove("zoom-active", "grabbing");
    ivResetZoomPan();
}

function closeViewer() {
    image_viewer.classList.remove("open");
    ivExitZoom();
    ivResetTransform();
}

// Keyboard
image_viewer.addEventListener("keydown", (e) => {
    if (e.key === "ArrowRight") changeActiveImage("right");
    else if (e.key === "ArrowLeft") changeActiveImage("left");
    else if (e.key === "+" || e.key === "=") { ivScale = Math.min(ivScale * 1.3, IV_MAX); ivApplyTransform(); }
    else if (e.key === "-") { ivScale = Math.max(ivScale / 1.3, IV_MIN); if (ivScale <= 1) { ivPanX = ivPanY = 0; } ivApplyTransform(); }
    else if (e.key === "0") ivResetTransform();
});

// Wheel
image_viewer.addEventListener("wheel", (e) => {
    e.preventDefault();
    if (ivZoomMode) {
        const delta = e.deltaY > 0 ? -1 : 1;
        ivScale = Math.min(Math.max(ivScale + delta * ivScale * 0.1, IV_MIN), IV_MAX);
        if (ivScale <= 1) { ivPanX = ivPanY = 0; }
        ivApplyTransform();
    } else {
        changeActiveImage(e.deltaY > 0 ? "right" : "left");
    }
}, { passive: false });

// Click handling: close when clicking outside interactive zones, but only when not in zoom mode
image_viewer.addEventListener("click", (e) => {
    if (ivZoomMode) return;
    if (!e.target.closest("#img-wrapper") &&
        !e.target.closest(".side-arrow") &&
        !e.target.closest(".viewer-topbar") &&
        !e.target.closest(".viewer-toolbar") &&
        !e.target.closest(".slider")) {
        closeViewer();
    }
});

// Drag / click on image wrapper — single mousedown/mouseup chain handles both
// enter-zoom (normal → zoom) and exit-zoom (zoom → normal, or pan in zoom)
ivWrapper.addEventListener("mousedown", (e) => {
    e.preventDefault();
    e.stopPropagation();
    let didDrag = false;
    const startX = e.clientX;
    const startY = e.clientY;
    const panStartX = ivPanX;
    const panStartY = ivPanY;
    const wasZoomed = ivZoomMode;

    if (wasZoomed) ivMainImage.classList.add("grabbing");

    const onMove = (me) => {
        const dx = me.clientX - startX;
        const dy = me.clientY - startY;
        if (!didDrag && (Math.abs(dx) > 3 || Math.abs(dy) > 3)) didDrag = true;
        if (!didDrag || !wasZoomed) return;
        ivPanX = panStartX + dx;
        ivPanY = panStartY + dy;
        ivApplyTransform(false);
    };
    const onUp = () => {
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
        ivMainImage.classList.remove("grabbing");
        if (!didDrag) {
            // True click: toggle zoom mode
            if (wasZoomed) ivExitZoom();
            else ivEnterZoom();
        }
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
});

// Arrow buttons
document.getElementById("viewer-prev").addEventListener("click", (e) => {
    e.stopPropagation();
    changeActiveImage("left");
});
document.getElementById("viewer-next").addEventListener("click", (e) => {
    e.stopPropagation();
    changeActiveImage("right");
});

// Close button
document.getElementById("viewer-close").addEventListener("click", (e) => {
    e.stopPropagation();
    closeViewer();
});

// Toolbar buttons
document.getElementById("iv-zoom-out").addEventListener("click", (e) => {
    e.stopPropagation();
    ivScale = Math.max(ivScale / 1.3, IV_MIN);
    if (ivScale <= 1) { ivPanX = ivPanY = 0; }
    ivApplyTransform();
});
document.getElementById("iv-zoom-in").addEventListener("click", (e) => {
    e.stopPropagation();
    ivScale = Math.min(ivScale * 1.3, IV_MAX);
    ivApplyTransform();
});
document.getElementById("iv-zoom-pct").addEventListener("click", (e) => {
    e.stopPropagation();
    ivResetZoomPan();
});
document.getElementById("iv-rotate").addEventListener("click", (e) => {
    e.stopPropagation();
    ivRotation = (ivRotation + 90) % 360;
    ivApplyTransform();
});
document.getElementById("iv-fit").addEventListener("click", (e) => {
    e.stopPropagation();
    ivExitZoom();
    ivResetTransform();
});
document.getElementById("iv-download").addEventListener("click", (e) => {
    e.stopPropagation();
    if (!ivImg.src) return;
    const a = document.createElement("a");
    a.href = ivImg.src;
    a.download = ivTitle.textContent || "image.jpg";
    a.click();
});

ivZoomSlider.addEventListener("input", (e) => {
    ivScale = e.target.value / 100;
    if (ivScale <= 1) { ivPanX = ivPanY = 0; }
    ivApplyTransform(false);
});

function changeActiveImage(direction) {
    const images_count = Number(slider.childElementCount);
    if (images_count < 2) return;

    const active_image = slider.querySelector(".active");
    let new_active = null;
    if (direction === "left")
        new_active = active_image.previousElementSibling ?? slider.children[images_count - 1];
    else if (direction === "right")
        new_active = active_image.nextElementSibling ?? slider.children[0];

    if (!new_active) return;
    ivSetActiveThumb(new_active);
}

// ─── Topbar geo search ───
const geoSearchInput = document.getElementById("geo-search-input");
const geoSearchClear = document.getElementById("geo-search-clear");
const geoDropdown = document.getElementById("topbar-geo-dropdown");
let geoSearchMarker = null;
let lastGeoResults = null;

const geoIcon = L.divIcon({
    className: "",
    html: `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="36" viewBox="0 0 24 36"><path d="M12 0C7.6 0 4 3.58 4 8c0 5.25 8 28 8 28S20 13.25 20 8c0-4.42-3.58-8-8-8z" fill="#5b8def" stroke="rgba(0,0,0,0.25)" stroke-width="1"/><circle cx="12" cy="8" r="3.5" fill="#fff"/></svg>`,
    iconSize: [24, 36],
    iconAnchor: [12, 36],
});

function closeGeoDropdown() {
    geoDropdown.classList.remove("open");
    geoDropdown.innerHTML = "";
}

function positionDropdown() {
    const rect = document
        .getElementById("topbar-geo-wrap")
        .getBoundingClientRect();
    geoDropdown.style.top = rect.bottom + 3 + "px";
    geoDropdown.style.left = rect.left + "px";
    geoDropdown.style.width = rect.width + "px";
}

function placeGeoMarker(lat, lng) {
    if (geoSearchMarker) map.removeLayer(geoSearchMarker);
    geoSearchMarker = L.marker([lat, lng], { icon: geoIcon }).addTo(map);
}

function renderGeoResults(results) {
    lastGeoResults = results;
    geoDropdown.innerHTML = "";
    positionDropdown();
    if (!results?.length) {
        const item = document.createElement("div");
        item.className = "topbar-geo-result no-results";
        item.textContent = "Ничего не найдено";
        geoDropdown.appendChild(item);
        geoDropdown.classList.add("open");
        return;
    }
    results.forEach((r) => {
        const item = document.createElement("div");
        item.className = "topbar-geo-result";
        item.textContent = r.display_name;
        item.title = r.display_name;
        item.addEventListener("click", () => {
            const lat = parseFloat(r.lat);
            const lng = parseFloat(r.lon);
            if (isNaN(lat) || isNaN(lng)) return;
            map.flyTo([lat, lng], 14);
            placeGeoMarker(lat, lng);
            mapCoordsText.textContent = `${lat.toFixed(6)}°N, ${lng.toFixed(6)}°E`;
            mapCoordsEl.classList.add("visible");
            closeGeoDropdown();
            geoSearchInput.blur();
        });
        geoDropdown.appendChild(item);
    });
    geoDropdown.classList.add("open");
}

geoSearchClear.addEventListener("click", () => {
    geoSearchInput.value = "";
    geoSearchClear.style.display = "none";
    lastGeoResults = null;
    closeGeoDropdown();
    geoSearchInput.focus();
});

geoSearchInput.addEventListener("focus", () => {
    if (lastGeoResults !== null && geoSearchInput.value.trim()) {
        renderGeoResults(lastGeoResults);
    }
});

geoSearchInput.addEventListener(
    "input",
    debounce(async (e) => {
        const raw = e.target.value;
        geoSearchClear.style.display = raw ? "flex" : "none";
        const q = raw.trim();
        if (!q) {
            closeGeoDropdown();
            return;
        }

        // Determine if input looks like coordinates vs address
        // Coordinates: only digits, dots, minus, commas, whitespace
        const looksLikeCoords = /^-?\d{1,3}(\.\d+)?[,\s]+-?\d{1,3}(\.\d+)?$/.test(q.trim());

        // Fast path: direct coordinate input — only when it looks like coords
        if (looksLikeCoords) {
            const coords = (() => {
                const parts = q.split(/[,\s]+/);
                if (parts.length < 2) return null;
                const lat = parseFloat(parts[0].trim());
                const lng = parseFloat(parts[1].trim());
                if (!isFinite(lat) || !isFinite(lng)) return null;
                if (lat < -90 || lat > 90 || lng < -180 || lng > 180) return null;
                return { lat, lng };
            })();
            if (coords) {
                closeGeoDropdown();
                map.flyTo([coords.lat, coords.lng], 14);
                placeGeoMarker(coords.lat, coords.lng);
                mapCoordsText.textContent = `${coords.lat.toFixed(6)}°N, ${coords.lng.toFixed(6)}°E`;
                mapCoordsEl.classList.add("visible");
                return;
            }
        }

        // Address geocoding via backend proxy → Nominatim
        try {
            const resp = await api.get(`/geo/search?q=${encodeURIComponent(q)}`);
            renderGeoResults(await resp.json());
        } catch (err) {
            console.error("Geo search error:", err);
            closeGeoDropdown();
        }
    }, 500),
);

// Close dropdown on outside click
document.addEventListener("click", (e) => {
    if (
        !document.getElementById("topbar-geo-wrap").contains(e.target) &&
        !geoDropdown.contains(e.target)
    )
        closeGeoDropdown();
});

// ── Кинотеатр ─────────────────────────────────────────────────────────────────

// Register this tab so cinema can switch back to it.
window.name = "samar_map";

const _cinemaBroadcast = new BroadcastChannel("samar_cinema");

_cinemaBroadcast.onmessage = e => {
    const msg = e.data;
    if (msg.type === 'open_cam') {
        receiveCamCard(msg.ip, msg.port);
    } else if (msg.type === 'cinema_cam_removed') {
        updateConnectBtn(msg.id);
    }
};

// window.open('', name) focuses an existing named tab without navigating it.
// window.open(url, name) would reload the tab even if it's already at url.
function openOrFocusTab(url, tabName) {
    const win = window.open("", tabName);
    if (!win) return;
    try {
        if (!win.location.href || win.location.href === "about:blank") {
            win.location.href = url;
        }
    } catch (_) {}
    win.focus();
}

const CINEMA_KEY = "cinema_cams";

function getCinemaCams() {
    try { return JSON.parse(localStorage.getItem(CINEMA_KEY)) || []; }
    catch { return []; }
}

function saveCinemaCams(ids) {
    localStorage.setItem(CINEMA_KEY, JSON.stringify(ids));
}

function updateConnectBtn(camId, canConnect) {
    const btn = document.getElementById("connect-cam");
    if (!btn) return;
    if (canConnect !== undefined) {
        btn.disabled = !canConnect;
    }
    const ids     = getCinemaCams();
    const isAdded = !btn.disabled && ids.includes(camId);
    btn.classList.toggle("action-btn--active", isAdded);
    const textNode = btn.lastChild;
    if (textNode && textNode.nodeType === Node.TEXT_NODE) {
        textNode.textContent = isAdded ? " Добавлено" : " Подключиться";
    }
}

document.getElementById("connect-cam")?.addEventListener("click", () => {
    const camId = parseInt(document.getElementById("info-window")?.dataset.camId, 10);
    if (!camId) return;
    const ids = getCinemaCams();
    const idx = ids.indexOf(camId);
    const adding = idx === -1;
    if (adding) ids.push(camId);
    else ids.splice(idx, 1);
    saveCinemaCams(ids);
    updateConnectBtn(camId);
    _cinemaBroadcast.postMessage({ type: adding ? "cam_add" : "cam_remove", id: camId });
});

document.getElementById("cinema-btn")?.addEventListener("click", () => {
    openOrFocusTab("/cinema", "samar_cinema");
});

