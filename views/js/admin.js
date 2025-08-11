document.getElementById("save-comment").addEventListener("click", async () => {
	const comment = document.getElementById("cam-comment").value.trim();
	const ip = document.getElementById("cam-ip").value.trim();
	const port = document.getElementById("cam-port").value.trim();

	if (!ip || !port) {
		notifications.error("IP or port do not defined in saving comment");
		return;
	}

	try {
		await api.post("/cam/save_comment", {
			ip: ip,
			port: port,
			comment: comment,
		});

		notifications.success("Comment saved");
	} catch (e) {
		console.error("Error while saving comment: " + e);
		notifications.error("Error with saving comment");
	}
});

document.getElementById("define-cam").addEventListener("click", async () => {
	const coords = document.getElementById("cam-coords").value.trim();
	const address = document.getElementById("cam-address").value.trim();
	const name = document.getElementById("cam-name").value.trim();
	const login = document.getElementById("cam-login").value.trim();
	const password = document.getElementById("cam-password").value.trim();

	if (!name || !address || !login || !password) {
		notifications.error("Name, address, login or password do not defined in defining camera");
		return;
	}

	if (!validators.isValidCoords(coords)) {
		notifications.error("Coords do not defined in defining camera");
		return;
	}

	const comment = document.getElementById("cam-comment").textContent.trim();
	const ip = document.getElementById("cam-ip").value.trim();
	const port = document.getElementById("cam-port").value.trim();

	if (!validators.isValidIP(ip) || !validators.isValidPort(port)) {
		notifications.error("IP or port are invalid");
		return;
	}

	try {
		await api.post("/cam/define_cam", {
			ip: ip,
			port: port,
			login: login,
			password: password,
			name: name,
			coords: coords,
			address: address,
			comment: comment,
		});

		notifications.success("Camera was defined successfully");
	} catch (e) {
		console.error("Error while define cam: " + e);
		notifications.error("Error while define cam");
	}
});

document.getElementById("upload-data").addEventListener("change", async (e) => {
	const file = e.target.files[0];

	if (!file) {
		notifications.error("No file was provided in uploading cameras");
		return;
	}

	const formData = new FormData();
	formData.append("file", file);
	try {
		await sendForm("/admin/upload_cams", formData);
		notifications.success("Cameras was uploaded successfully");
	} catch (e) {
		console.error("Error while send file: " + e);
		notifications.error("Error while send file");
	}
	e.target.value = "";
});

async function sendForm(url, formData) {
	const csrf_token = document.cookie
		.split("; ")
		.find((row) => row.startsWith("csrf_token="))
		?.split("=")[1];

	const options = {
		credential: "include",
		method: "POST",
		headers: {
			"X-CSRF-Token": csrf_token,
		},
		body: formData,
	};

	const response = await fetch(url, options);

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
		show_box.innerHTML = `<div>${ans.token}</div><img class="icon" src="/assets/icons/close.png" alt="close" />`;
		show_box.querySelector("img").addEventListener("click", () => show_box.remove());
		document.body.appendChild(show_box);
	} catch (e) {
		console.error("Error while gettign token: " + e);
		notifications.error("Error while gettign token");
	}
});

document.getElementById("define-cam-status").addEventListener("click", async () => {
	const select = document.getElementById("select-cam-status");
	const selected_status = select.value;

	if (!selected_status) {
		notifications.error("No status was selected in definig");
		return;
	}

	const ip = info_window.querySelector("#cam-ip").value.trim();
	const port = info_window.querySelector("#cam-port").value.trim();

	if (!ip || !port) {
		notifications.error("No ip or port provided");
		return;
	}

	try {
		await api.post("/cam/change_status", {
			ip: ip,
			port: port,
			status: selected_status,
		});

		notifications.success("Status changed");

		const cam_label = document.querySelector(`[data-ip="${ip}"][data-port="${port}"]`);
		const cam_icons = cam_label.querySelector(".cam-icons");
		if (cam_icons.querySelector(".cam-icon-status")) {
			if (selected_status === "valid") cam_icons.querySelector(".cam-icon-status").remove();
			else {
				const status_icon = cam_icons.querySelector(".cam-icon-status");
				status_icon.src = `/assets/icons/${selected_status}_cam.png`;
				status_icon.alt = `${selected_status}-cam`;
			}
		} else {
			const cam_icon = document.createElement("img");
			cam_icon.className = "cam-icon-status";
			cam_icon.src = `/assets/icons/${selected_status}_cam.png`;
			cam_icon.alt = `${selected_status}-cam`;
			cam_icons.prepend(cam_icon);
		}
	} catch (e) {
		console.error("Error while changing status: " + e);
		notifications.error("Error while changing status");
	}
});

sidebar.addEventListener("click", (e) => {
	const el = e.target;
	if (el.closest(".cam-label") && el.classList.contains("label-text") && info_window.classList.contains("active")) cancel(true);
});

const fields_name = new Array();
const cancel = (isClose) => {
	for (const name of fields_name) info_window.querySelector(`input[name="${name}"]`)?.setAttribute("readonly", true);
	info_window.querySelector("#add-camera")?.remove();
	info_window.classList.toggle("active", isClose);
};

document.getElementById("add-camera").addEventListener("click", () => {
	info_window.querySelectorAll('input[type="text"], textarea').forEach((field) => {
		if (field.hasAttribute("readonly")) {
			fields_name.push(field.name);
			field.removeAttribute("readonly");
		}
		field.value = "";
	});
	info_window.querySelector(".cam-images").innerHTML = "";

	const add_button = document.createElement("input");
	add_button.className = "button";
	add_button.id = "add-camera";
	add_button.type = "button";
	add_button.value = "Добавить камеру";
	add_button.onclick = async () => {
		const data = {};
		info_window.querySelectorAll('input[type="text"], textarea').forEach((field) => {
			data[field.name.replace("cam_", "")] = field.value.trim();
		});

		if (!data["name"] || !data["ip"] || !data["port"] || !data["login"] || !data["password"]) {
			notifications.error("Required data do not provided");
			return;
		}

		if (!validators.isValidIP(data["ip"]) || !validators.isValidPort(data["port"])) {
			notifications.error("IP or port invalid");
			return;
		}

		if (data["coords"] && !validators.isValidCoords(data["coords"])) {
			notifications.error("Coords are invalid");
			return;
		}

		data["status"] = info_window.querySelector("#select-cam-status")?.value ?? "valid";

		try {
			await api.post("/cam/add_camera", data);
			notifications.success("Camera was defined successfully");
			cancel(true);
		} catch (e) {
			console.error("Error while define cam: " + e);
			notifications.error("Error while define cam");
		}
	};

	info_window.querySelector("#close-button").onclick = () => cancel(false);
	if (!info_window.querySelector("#add-camera")) info_window.querySelector(".cam-buttons").appendChild(add_button);

	info_window.classList.toggle("active", true);
});

const images_container = info_window.querySelector(".cam-images");

document.getElementById("add-photos").addEventListener("change", async (e) => {
	await handleFiles(e.target.files);
	e.target.value = "";
});

images_container.addEventListener("dragover", (e) => {
	e.preventDefault();
	images_container.classList.add("drag-over");
});

images_container.addEventListener("dragleave", () => images_container.classList.remove("drag-over"));

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
		const res = await sendForm("/cam/upload_photos", formData);
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
