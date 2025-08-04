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

	if (!name || !address || !coords) {
		notifications.error("Name or address or coords do not defined in defining camera");
		return;
	}

	const comment = document.getElementById("cam-comment").textContent.trim();
	const ip = document.getElementById("cam-ip").value.trim();
	const port = document.getElementById("cam-port").value.trim();

	try {
		await api.post("/cam/define_cam", {
			ip: ip,
			port: port,
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

		const response = await fetch("/cam/upload_cams", options);
		
		if (!response.ok) {
			const error = await response.json();
			console.error("Error with upload file: " + error.error);
			notifications.error("Error while uploading file");
			return;
		}

		notifications.success("Cameras was uploaded successfully");
	} catch (e) {
		console.error("Error while send file: " + e);
		notifications.error("Error while send file");
	}
	e.target.value = "";
});

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
