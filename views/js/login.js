document.getElementById("log-in-label").addEventListener("click", (e) => {
	e.target.classList.add("active-label");
	document.getElementById("register-label").classList.remove("active-label");
	if (document.getElementById("credentials-form").dataset.filled)
		document.getElementById("credentials-form").classList.add("hidden-input-form");
	else document.getElementById("registration-input-form").classList.add("hidden-input-form");
	document.getElementById("log-in-input-form").classList.remove("hidden-input-form");
});

document.querySelectorAll("input[type=text]").forEach((element) => {
	element.addEventListener("input", (e) => {
		e.target.value = e.target.value.replace(/[^A-Za-z0-9_]/g, "");
		if (e.target.value.length > 4) e.target.style.border = "1px solid green";
		else e.target.style.border = "1px solid red";
	});
});

document.getElementById("register-label").addEventListener("click", (e) => {
	e.target.classList.add("active-label");
	document.getElementById("log-in-label").classList.remove("active-label");
	if (document.getElementById("credentials-form").dataset.filled)
		document.getElementById("credentials-form").classList.remove("hidden-input-form");
	else document.getElementById("registration-input-form").classList.remove("hidden-input-form");
	document.getElementById("log-in-input-form").classList.add("hidden-input-form");
});

const loginForm = document.getElementById("log-in-input-form");
const registerForm = document.getElementById("registration-input-form");

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
		document.getElementById("registration-input-form").classList.add("hidden-input-form");
		const creds = document.getElementById("credentials-form");
		creds.classList.remove("hidden-input-form");
		creds.dataset.filled = true;
	} catch (e) {
		console.error("Error while registration: " + e);
	}
});

document.getElementById("toggle-password").addEventListener("click", (e) => {
	const password_input = document.getElementById("log-in-password");
	const isPassword = password_input.getAttribute("type") === "password";
	password_input.setAttribute("type", isPassword ? "text" : "password");
	e.target.style.backgroundImage = isPassword ? "url('/assets/icons/open.png')" : "url('/assets/icons/hide.png')";
});

function copyValue(elem) {
	const input_field = elem.parentElement.querySelector("input[type=text]");
	input_field.select();
	input_field.setSelectionRange(0, 99999);
	navigator.clipboard.writeText(input_field.value);
	notifications.success("Copied value: " + el.value);
}
