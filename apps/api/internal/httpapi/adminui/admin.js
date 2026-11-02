"use strict";

const csrfMeta = document.querySelector('meta[name="csrf-token"]');
const csrf = csrfMeta ? csrfMeta.content : "";

function setError(element, message) {
  if (!element) return;
  element.textContent = message || "";
}

function errorText(payload, response) {
  const code = payload && payload.error && payload.error.code;
  const requestId = payload && payload.error && payload.error.request_id;
  if (code) return requestId ? `${code} · ${requestId}` : code;
  return `HTTP ${response.status}`;
}

async function request(path, options) {
  let response;
  try {
    response = await fetch(path, { credentials: "same-origin", ...options });
  } catch {
    return { response: { ok: false, status: 0 }, payload: null };
  }
  let payload = null;
  try {
    payload = await response.json();
  } catch {
    payload = null;
  }
  return { response, payload };
}

function newIdempotencyKey() {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function setupSignIn() {
  const form = document.getElementById("sign-in");
  if (!form) return;
  const error = document.getElementById("error");
  const button = form.querySelector('button[type="submit"]');
  const totp = form.querySelector('input[name="totp_code"]');
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    setError(error, "");
    button.disabled = true;
    const formData = new FormData(form);
    const { response, payload } = await request("/admin/v1/auth/sign-in", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username: formData.get("username"),
        password: formData.get("password"),
        totp_code: formData.get("totp_code"),
      }),
    });
    if (response.ok) {
      window.location.reload();
      return;
    }
    setError(error, errorText(payload, response));
    button.disabled = false;
    if (totp) {
      totp.value = "";
      totp.focus();
    }
  });
}

function setupSignOut() {
  const button = document.getElementById("sign-out");
  if (!button) return;
  button.addEventListener("click", async () => {
    button.disabled = true;
    const { response } = await request("/admin/v1/auth/sign-out", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
      body: "{}",
    });
    if (response.ok || response.status === 401) {
      window.location.href = "/admin/";
      return;
    }
    button.disabled = false;
  });
}

function syncQueueState() {
  const items = document.querySelectorAll(".item");
  const count = document.getElementById("pending-count");
  if (count) count.textContent = `${items.length} 条`;
  document.querySelectorAll("[data-group]").forEach((heading) => {
    const kind = heading.getAttribute("data-group");
    const remaining = document.querySelectorAll(`.item[data-kind="${kind}"]`).length;
    const badge = heading.querySelector("span");
    if (badge) badge.textContent = String(remaining);
    heading.hidden = remaining === 0;
  });
  const queue = document.querySelector(".queue");
  if (!queue) return;
  let empty = document.getElementById("empty");
  if (items.length === 0 && !empty) {
    empty = document.createElement("p");
    empty.id = "empty";
    empty.className = "empty";
    empty.textContent = "没有待审内容。";
    const firstGroup = queue.querySelector(".group-title");
    if (firstGroup) queue.insertBefore(empty, firstGroup);
    else queue.appendChild(empty);
  } else if (items.length > 0 && empty) {
    empty.remove();
  }
}

function setupQueue() {
  const notice = document.getElementById("notice");
  let noticeTimer = 0;
  const showNotice = (message) => {
    if (!notice) return;
    notice.textContent = message;
    notice.hidden = false;
    window.clearTimeout(noticeTimer);
    noticeTimer = window.setTimeout(() => {
      notice.hidden = true;
    }, 2400);
  };

  const refresh = document.getElementById("refresh");
  if (refresh) refresh.addEventListener("click", () => window.location.reload());

  document.querySelectorAll(".item").forEach((item) => {
    const kind = item.getAttribute("data-kind");
    const id = item.getAttribute("data-id");
    const textarea = item.querySelector("textarea");
    const error = item.querySelector(".error");
    const buttons = Array.from(item.querySelectorAll(".approve, .reject"));
    buttons.forEach((button) => {
      button.addEventListener("click", async () => {
        const decision = button.classList.contains("approve") ? "approve" : "reject";
        const reason = textarea ? textarea.value.trim() : "";
        setError(error, "");
        if ([...reason].length < 10) {
          setError(error, "理由至少需要 10 个字。");
          if (textarea) textarea.focus();
          return;
        }
        buttons.forEach((candidate) => {
          candidate.disabled = true;
        });
        const { response, payload } = await request(`/admin/v1/${kind}/${id}/decision`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": csrf,
            "Idempotency-Key": newIdempotencyKey(),
          },
          body: JSON.stringify({ decision, reason }),
        });
        if (response.status === 401) {
          window.location.href = "/admin/";
          return;
        }
        if (!response.ok) {
          setError(error, errorText(payload, response));
          buttons.forEach((candidate) => {
            candidate.disabled = false;
          });
          return;
        }
        item.classList.add("is-resolved");
        window.setTimeout(() => {
          item.remove();
          syncQueueState();
          showNotice(decision === "approve" ? "已通过并留档。" : "已拒绝并留档。");
        }, 240);
      });
    });
  });
  syncQueueState();
}

setupSignIn();
setupSignOut();
setupQueue();
