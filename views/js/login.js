const loginLabel = document.getElementById("log-in-label");
const registerLabel = document.getElementById("register-label");
const loginForm = document.getElementById("log-in-input-form");
const registerForm = document.getElementById("registration-input-form");
const credsForm = document.getElementById("credentials-form");

loginLabel.addEventListener("click", () => {
    loginLabel.classList.add("active");
    registerLabel.classList.remove("active");
    if (credsForm.dataset.filled) credsForm.classList.add("hidden");
    else registerForm.classList.add("hidden");
    loginForm.classList.remove("hidden");
});

registerLabel.addEventListener("click", () => {
    registerLabel.classList.add("active");
    loginLabel.classList.remove("active");
    if (credsForm.dataset.filled) credsForm.classList.remove("hidden");
    else registerForm.classList.remove("hidden");
    loginForm.classList.add("hidden");
});

document.querySelectorAll("input[type=text]:not([readonly])").forEach((el) => {
    el.addEventListener("input", (e) => {
        e.target.value = e.target.value.replace(/[^A-Za-z0-9_]/g, "");
        if (e.target.value.length > 4)
            e.target.style.borderColor = "var(--accent)";
        else e.target.style.borderColor = "var(--danger)";
    });
});

const sendRequest = async (url, data = {}) => {
    const response = await fetch(url, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify(data),
    });

    if (!response.ok) {
        const ans = await response.json();
        if (ans.error) {
            const error_message_box = document.getElementById("error-message-box");
            error_message_box.textContent = ans.error;
            error_message_box.style.display = "block";
        }
        throw new Error(`HTTP error! Status: ${response.status}`);
    }
    return response;
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

loginForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const fd = new FormData(loginForm);

    try {
        await sendRequest("/auth/login", {
            username: fd.get("login_username"),
            password: fd.get("login_password"),
        });

        window.location.href = "/";
    } catch (e) {
        console.error("Error while log in: " + e);
    }
});

registerForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const fd = new FormData(registerForm);

    try {
        const response = await sendRequest("/auth/register", {
            username: fd.get("register_username"),
            token: fd.get("register_token"),
        });
        const response_json = await response.json();
        document.getElementById("username").value = response_json.username;
        document.getElementById("password").value = response_json.password;
        document.getElementById("registration-input-form").classList.add("hidden");
        const creds = document.getElementById("credentials-form");
        creds.classList.remove("hidden");
        creds.dataset.filled = true;
    } catch (e) {
        console.error("Error while registration: " + e);
    }
});

document.getElementById("toggle-password").addEventListener("click", (e) => {
    const password_input = document.getElementById("log-in-password");
    const eyeOpen = e.currentTarget.querySelector(".eye-open");
    const eyeClosed = e.currentTarget.querySelector(".eye-closed");
    const isPassword = password_input.getAttribute("type") === "password";
    password_input.setAttribute("type", isPassword ? "text" : "password");
    eyeOpen.style.display = isPassword ? "none" : "flex";
    eyeClosed.style.display = isPassword ? "flex" : "none";
});

function copyValue(elem) {
    const input_field = elem.parentElement.querySelector("input[type=text]");
    input_field.select();
    input_field.setSelectionRange(0, 99999);
    navigator.clipboard.writeText(input_field.value);
    notifications.success("Copied value: " + input_field.value);
}
