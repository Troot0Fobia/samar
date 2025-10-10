const map = L.map("map", { center: [49.612, 32.193], zoom: 7 });
const sidebar_button = document.getElementById("sidebar-button");
const info_window = document.getElementById("info-window");
const sidebar = document.getElementById("sidebar");
const image_viewer = document.getElementById("image-viewer");
const slider = document.getElementById("slider");
const sidebar_tabs = sidebar.querySelector(".tabs");
const main_image = image_viewer.querySelector(".main-image img");
const loadedCameras = new Map();
const region_polygons = new Map();
const scaleMax = 5,
    scaleMin = 0.5;
let scale = 1;
let zoomMode = false,
    isDraggin = false,
    isClicked = false;
let originX = 0,
    originY = 0,
    offsetX = 0,
    offsetY = 0;
let focusedMarker = null;
const icon = L.icon({
    iconUrl: "/assets/icons/camera.png",
    iconSize: [32, 32],
});
const focusedIcon = L.icon({
    iconUrl: "/assets/icons/camera.png",
    iconSize: [42, 42],
});
L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
    maxZoom: 19,
    minZoom: 3,
}).addTo(map);

map.on("drag", () =>
    map.panInsideBounds(L.latLngBounds(L.latLng(-85, -180), L.latLng(85, 180))),
);
map.on("click", () => {
    if (focusedMarker) {
        focusedMarker.setIcon(icon);
        focusedMarker = null;
    }
});

const api = {
    async fetch(url, options = {}) {
        const csrf_token = document.cookie
            .split("; ")
            .find((row) => row.startsWith("csrf_token="))
            ?.split("=")[1];

        const defaultOptions = {
            credential: "include",
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

const notifications = {
    show: (message, type = "info") => {
        const notification = document.createElement("div");
        notification.className = `notification ${type}`;
        notification.textContent = message;
        document.body.appendChild(notification);

        setTimeout(() => {
            notification.remove();
        }, 3000);
    },

    error: (message) => notifications.show(message, "error"),
    success: (message) => notifications.show(message, "success"),
};

document.addEventListener("keyup", (event) => {
    if (event.key === "Escape") {
        if (image_viewer.classList.contains("open")) {
            image_viewer.classList.remove("open");
            toggleZoom(true);
            return;
        }
        if (info_window.classList.contains("active"))
            info_window.classList.remove("active");
    }
});

info_window.addEventListener("click", (e) => {
    const el = e.target;
    if (el.closest("#close-button")) info_window.classList.remove("active");
    else if (el.matches('input[type="text"][readonly]') && el.value) {
        // if (!el.hasAttribute("readonly")) return;
        el.addEventListener("click", () => {
            el.select();
            el.setSelectionRange(0, 99999);
            navigator.clipboard.writeText(el.value);
            notifications.success("Copied value: " + el.value);
        });
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
                const marker = loadedCameras.get(label.dataset.id);
                const latlng = marker?.getLatLng();
                if (latlng) {
                    map.flyTo(latlng, 19);
                    marker.setIcon(focusedIcon);
                    focusedMarker = marker;
                }
                await receiveCamCard(label.dataset.ip, label.dataset.port);
                return;
            }
        }
        const next = label.nextElementSibling;
        if (next?.classList.contains("content")) {
            label.querySelector("img")?.classList.toggle("open");
            next.classList.toggle("expand");
        }
    } else if (el.closest("#expand-all")) {
        sidebar_tabs.querySelectorAll(".label").forEach((label) => {
            label.querySelector("img")?.classList.add("open");
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
            .forEach((label) => label.querySelector("img")?.classList.remove("open"));
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

document.getElementById("search-field").addEventListener(
    "input",
    debounce((e) => {
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

sidebar_button.addEventListener(
    "click",
    () =>
    (sidebar_button.querySelector("img").src = `/assets/icons/${sidebar.classList.toggle("sidebar-open") ? "close" : "open"
        }-sidebar.png`),
);

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
                        sidebar.classList.toggle("sidebar-open", true);
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

        const container = document.createDocumentFragment();
        renderCams(cameras, container);
        sidebar_tabs.appendChild(container);
    } catch (e) {
        console.error("Error in rendering cameras: " + e);
    }
});

function renderCams(cameras, container) {
    const type_list = ["cam", "type", "city", "region", "country"];

    const createLabel = (type, data) => {
        const label = document.createElement("div");
        let name;
        let cam_icon = "";
        if (type === "cam") {
            name = data.name;
            label.dataset.ip = data.ip;
            label.dataset.port = data.port;
            label.dataset.id = data.id;
            if (data.status !== "valid")
                cam_icon = `<img class="cam-icon-status" src="/assets/icons/${data.status}_cam.png" alt="${data.status}-cam"/>`;
        } else if (type !== "type") {
            label.dataset.name = data.name;
            name = data.name_rus;
        } else name = data.name;
        label.className = `label ${type}-label`;
        label.innerHTML = `<div class="label-text">${name}</div>
				<div class="cam-icons">
					${cam_icon}
	                <img class="label-arrow" src="/assets/icons/arrow.png" alt="camera"/>
				</div>`;
        return label;
    };

    const createContent = (type, data) => {
        const content = document.createElement("div");
        content.className = `content ${type === "type" ? data.class : type}-tab`;
        if (data) {
            content.dataset[type] = type === "type" ? data.class : data.name;
            if (type === "cam") {
                content.dataset.ip = data.ip;
                content.dataset.port = data.port;
            }
        }
        data.images?.forEach((image) => {
            const img = document.createElement("img");
            img.dataset.src = `/cam/image/${data.ip}/${image}`;
            img.alt = "camera";
            img.loading = "lazy";
            if (img) content.appendChild(img);
        });
        return content;
    };

    const renderElems = (root, index, data) => {
        if (index < 0 || index >= type_list.length) return [null, null];

        const type = type_list[index];
        const val = data[type];

        let content, label;
        if (type === "cam")
            content = root.querySelector(
                `.content[data-ip="${val.ip}"][data-port="${val.port}"]`,
            );
        else
            content = root.querySelector(
                `[data-${type}="${type === "type" ? val.class : val.name}"]`,
            );

        if (content) {
            label = content.previousElementSibling;
            if (!label?.classList.contains("label"))
                label = createLabel(
                    type,
                    type === "cam" ? val : { name: val.name, name_rus: val.name_rus },
                );
        } else {
            label = createLabel(
                type,
                type === "cam" ? val : { name: val.name, name_rus: val.name_rus },
            );
            content = createContent(type, val);
        }

        const [childLabel, childContent] = renderElems(content, index - 1, val);
        if (childLabel && childContent) content.append(childLabel, childContent);

        return [label, content];
    };

    cameras.forEach((camera) => {
        const id = String(camera.ID);
        const isDefined = camera.IsDefined;

        if (isDefined && !loadedCameras.has(id)) {
            const marker = L.marker([camera.Lat, camera.Lng], {
                icon: icon,
                title: camera.Comment,
            })
                .addTo(map)
                .on("click", async () => {
                    const content = sidebar.querySelector(
                        `[data-ip="${camera.IP}"][data-port="${camera.Port}"]`,
                    )?.nextElementSibling;
                    if (content?.classList.contains("content"))
                        content.querySelectorAll("img").forEach((img) => {
                            if (!img.getAttribute("src")) img.src = img.dataset.src;
                        });
                    await receiveCamCard(camera.IP, camera.Port);
                });
            region_polygons.get(camera.Region)?.addLayer(marker);
            loadedCameras.set(id, marker);
        }

        const data = {
            country: {
                name: camera.Country,
                name_rus: camera.Country_rus,
                region: {
                    name: camera.Region,
                    name_rus: camera.Region_rus,
                    city: {
                        name: camera.City,
                        name_rus: camera.City_rus,
                        type: {
                            name: isDefined ? "Найденные" : "Не найденные",
                            class: isDefined ? "identified" : "unidentified",
                            cam: {
                                name:
                                    (isDefined ? camera.Name : camera.IP + ":" + camera.Port) +
                                    (camera.Images?.length > 0
                                        ? ` (${camera.Images.length})`
                                        : ""),
                                ip: camera.IP,
                                port: camera.Port,
                                status: camera.Status,
                                id: isDefined ? id : null,
                                images: camera.Images,
                            },
                        },
                    },
                },
            },
        };

        const [label, content] = renderElems(container, type_list.length - 1, data);
        container.append(label, content);
    });
}

async function receiveCamCard(ip, port) {
    try {
        if (!validators.isValidIP(ip) || !validators.isValidPort(port))
            throw new Error("Invalid IP or port");

        const response = await api.get(
            `/cam/${encodeURIComponent(ip)}/${encodeURIComponent(port)}`,
        );
        const camera_info = await response.json();
        if (!camera_info) return;

        const cam_label = sidebar.querySelector(
            `[data-ip="${ip}"][data-port="${port}"]`,
        );
        const data = {
            "#cam-name": camera_info.Name ? camera_info.Name : camera_info.IP,
            "#cam-ip": camera_info.IP,
            "#cam-port": camera_info.Port,
            "#cam-login": camera_info.Login,
            "#cam-password": camera_info.Password,
            "#cam-coords": `${camera_info.Lat}, ${camera_info.Lng}`,
            "#cam-comment": camera_info.Comment,
            "#cam-address": camera_info.Address,
            "#cam-link": camera_info.Link,
            "#select-cam-status": camera_info.Status,
        };

        Object.entries(data).forEach(([selector, value]) => {
            const element = info_window.querySelector(selector);
            if (element) element.value = value;
        });

        const cam_images = info_window.querySelector(".cam-images");
        cam_images.innerHTML = "";
        const content_images = cam_label?.nextElementSibling;
        if (content_images?.classList.contains("content"))
            cam_images.innerHTML = content_images.innerHTML;

        info_window.classList.toggle("active", true);
    } catch (e) {
        console.error("Error while receiving camera info: " + e);
    }
}

function renderImageViewer(event) {
    const pressed_image = event.target;
    const images_content = pressed_image.parentElement;
    const split_title = pressed_image.src.split("/");
    main_image.src = pressed_image.src;
    image_viewer.querySelector(".image-title").innerText =
        split_title[split_title.length - 1];
    slider.innerHTML = images_content.innerHTML;
    slider.childNodes.forEach((image_elem) => {
        image_elem.addEventListener("click", (event) => {
            if (zoomMode) return;
            main_image.src = event.target.src;
            slider
                .querySelectorAll("img")
                .forEach((elem) => elem.classList.remove("active"));
            image_elem.classList.add("active");
        });
        if (image_elem.src === pressed_image.src)
            image_elem.classList.add("active");
    });

    image_viewer.classList.add("open");
    image_viewer.focus();
}

image_viewer.addEventListener("keydown", (e) => {
    if (e.key === "ArrowRight") changeActiveImage("right");
    else if (e.key === "ArrowLeft") changeActiveImage("left");
});

image_viewer.addEventListener("wheel", (e) => {
    if (zoomMode) {
        scale += e.deltaY > 0 ? scale * -0.05 : scale * 0.05;
        scale = Math.min(Math.max(scale, scaleMin), scaleMax);
        applyTransform();
    } else changeActiveImage(e.deltaY > 0 ? "right" : "left");
});

image_viewer.addEventListener("click", (e) => {
    const el = e.target;
    if (el.closest(".left-arrow")) changeActiveImage("left");
    else if (el.closest(".right-arrow")) changeActiveImage("right");
    else if (el.matches(".main-image") && !zoomMode)
        image_viewer.classList.remove("open");
});

main_image.addEventListener("mousedown", (e) => {
    e.preventDefault();
    if (!zoomMode) return;
    isClicked = true;
    originX = e.clientX - offsetX;
    originY = e.clientY - offsetY;
});

main_image.addEventListener("mouseup", () => {
    if (!isDraggin) toggleZoom();
    isClicked = isDraggin = false;
});

function toggleZoom(disable) {
    zoomMode = disable ? false : !zoomMode;
    main_image.classList.toggle("zoom", zoomMode);
    if (!zoomMode) {
        scale = 1;
        offsetX = offsetY = 0;
        main_image.style.transform = "";
    }
}

main_image.addEventListener("mousemove", (e) => {
    if (!zoomMode || !isClicked) return;
    isDraggin = true;
    offsetX = e.clientX - originX;
    offsetY = e.clientY - originY;

    const bounds = main_image.parentElement.getBoundingClientRect();
    const imgWidth = bounds.width * scale;
    const imgHeight = bounds.height * scale;

    const maxX = (imgWidth - bounds.width) / 2;
    const maxY = (imgHeight - bounds.height) / 2;

    offsetX = Math.min(Math.max(offsetX, -maxX), maxX);
    offsetY = Math.min(Math.max(offsetY, -maxY), maxY);

    applyTransform();
});

function applyTransform() {
    main_image.style.transform = `translate(${offsetX}px, ${offsetY}px) scale(${scale})`;
}

function changeActiveImage(direction) {
    const images_count = Number(slider.childElementCount);
    if (images_count < 2) return;

    const active_image = slider.querySelector(".active");
    let new_active = null;
    if (direction === "left")
        new_active =
            active_image.previousElementSibling ?? slider.children[images_count - 1];
    else if (direction === "right")
        new_active = active_image.nextElementSibling ?? slider.children[0];

    if (!new_active) return;

    image_viewer.querySelector("#main-image img").src = new_active.src;
    const split_title = new_active.src.split("/");
    image_viewer.querySelector(".image-title").innerText =
        split_title[split_title.length - 1];
    slider
        .querySelectorAll("img")
        .forEach((elem) => elem.classList.remove("active"));
    new_active.classList.add("active");
}
