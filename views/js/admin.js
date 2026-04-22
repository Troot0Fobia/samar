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
