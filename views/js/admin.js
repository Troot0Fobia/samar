document.getElementById("upload-data").addEventListener("change", async (e) => {
	const file = e.target.files[0];

	if (!file) {
		notifications.error("No file was provided in uploading cameras");
		return;
	}

	const formData = new FormData();
	formData.append("file", file);
	try {
		await sendAdminReq("/admin/upload_cams", formData, "POST", true);
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
