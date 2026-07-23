(() => {
  "use strict";

  const page = document.querySelector("[data-pk-page]");
  if (!page) return;

  const decodeConfig = (encoded) => {
    const padded = encoded + "=".repeat((4 - (encoded.length % 4)) % 4);
    const binary = window.atob(padded);
    const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
    return JSON.parse(new TextDecoder().decode(bytes));
  };

  const resource = decodeConfig(page.dataset.resourceConfig);
  const listPath = page.dataset.listPath;

  const request = async (url, options = {}) => {
    const response = await fetch(url, {
      credentials: "same-origin",
      headers: { Accept: "application/json", ...(options.headers || {}) },
      ...options,
    });
    const text = await response.text();
    let body = null;
    if (text) {
      try {
        body = JSON.parse(text);
      } catch (_) {
        body = text.trim();
      }
    }
    if (!response.ok) {
      const detail = typeof body === "string" && body
        ? body
        : `The server returned HTTP ${response.status}.`;
      const error = new Error(detail);
      error.status = response.status;
      throw error;
    }
    return body;
  };

  const notify = (message) => {
    const toast = document.getElementById("pk-toast");
    if (!toast) return;
    toast.textContent = message;
    toast.hidden = false;
    window.clearTimeout(notify.timer);
    notify.timer = window.setTimeout(() => {
      toast.hidden = true;
    }, 3200);
  };

  const valueAt = (row, key) => {
    if (!row || !key) return null;
    return key.split(".").reduce((value, part) => {
      if (value && typeof value === "object") return value[part];
      return null;
    }, row);
  };

  const asRows = (payload) => {
    if (Array.isArray(payload)) return payload;
    if (payload && Array.isArray(payload.items)) return payload.items;
    if (payload && Array.isArray(payload.data)) return payload.data;
    return payload && typeof payload === "object" ? [payload] : [];
  };

  const matchesCondition = (row, condition) => {
    if (!condition) return true;
    const value = valueAt(row, condition.field);
    const empty = value === null
      || value === undefined
      || value === ""
      || (Array.isArray(value) && value.length === 0);
    if (condition.operator === "empty") return empty;
    if (condition.operator === "not_empty") return !empty;
    if (condition.operator === "equals") return String(value) === condition.value;
    return false;
  };

  const displayDate = (value) => {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return String(value);
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(date);
  };

  const textValue = (value, kind) => {
    if (value === null || value === undefined || value === "") return "—";
    if (kind === "boolean") return value ? "Yes" : "No";
    if (kind === "datetime") return displayDate(value);
    if (kind === "count" && typeof value === "object") {
      const count = Object.keys(value || {}).length;
      return `${count} ${count === 1 ? "check" : "checks"}`;
    }
    if (Array.isArray(value)) return value.join(", ");
    if (typeof value === "object") {
      const keys = Object.keys(value);
      return `${keys.length} ${keys.length === 1 ? "item" : "items"}`;
    }
    return String(value);
  };

  const appendValue = (cell, value, column) => {
    if (column.kind === "status" && value) {
      const pill = document.createElement("span");
      const normalized = String(value).toLowerCase();
      pill.className = `pk-status pk-status-${normalized.replace(/[^a-z0-9]+/g, "-")}`;
      pill.textContent = String(value);
      cell.appendChild(pill);
      return;
    }
    if (column.kind === "tags" && Array.isArray(value)) {
      const group = document.createElement("span");
      group.className = "pk-tag-list";
      value.forEach((item) => {
        const tag = document.createElement("span");
        tag.className = "pk-tag";
        tag.textContent = item;
        group.appendChild(tag);
      });
      cell.appendChild(group);
      return;
    }
    cell.textContent = textValue(value, column.kind);
    if (column.primary) cell.classList.add("pk-primary-cell");
  };

  const setBusy = (button, busy, busyLabel) => {
    if (!button) return;
    if (busy) {
      button.dataset.label = button.textContent;
      button.textContent = busyLabel;
      button.disabled = true;
      button.setAttribute("aria-busy", "true");
    } else {
      button.textContent = button.dataset.label || button.textContent;
      button.disabled = false;
      button.removeAttribute("aria-busy");
    }
  };

  const initList = () => {
    const table = document.getElementById("pk-resource-table");
    const tbody = table.querySelector("tbody");
    const status = document.getElementById("pk-resource-status");
    const empty = document.getElementById("pk-resource-empty");
    const emptyCopy = document.getElementById("pk-resource-empty-copy");
    const search = document.getElementById("pk-resource-search");
    const refresh = document.getElementById("pk-resource-refresh");
    const previous = document.getElementById("pk-page-prev");
    const next = document.getElementById("pk-page-next");
    const pageLabel = document.getElementById("pk-page-label");
    const pageSize = 25;
    let offset = 0;
    let rows = [];

    const actionButton = (label, className, handler) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = className;
      button.textContent = label;
      button.addEventListener("click", handler);
      return button;
    };

    const runAction = async (button, row, action) => {
      const id = valueAt(row, resource.id_key || "id");
      if (!id) return;
      if (action.confirm && !window.confirm(action.confirm)) return;
      setBusy(button, true, "Working…");
      try {
        await request(
          `${resource.api_path}/${encodeURIComponent(id)}${action.path_suffix}`,
          { method: action.method || "POST" },
        );
        notify(`${action.label} completed.`);
        await load();
      } catch (error) {
        status.textContent = error.message;
        status.classList.add("pk-inline-status-error");
      } finally {
        setBusy(button, false);
      }
    };

    const deleteRow = async (button, row) => {
      const id = valueAt(row, resource.id_key || "id");
      if (!id) return;
      if (!window.confirm(`Delete this ${resource.singular_label}? This cannot be undone.`)) return;
      setBusy(button, true, "Deleting…");
      try {
        await request(`${resource.api_path}/${encodeURIComponent(id)}`, { method: "DELETE" });
        notify(`${resource.singular_label} deleted.`);
        await load();
      } catch (error) {
        status.textContent = error.message;
        status.classList.add("pk-inline-status-error");
      } finally {
        setBusy(button, false);
      }
    };

    const render = () => {
      const query = search.value.trim().toLocaleLowerCase();
      const visible = query
        ? rows.filter((row) => JSON.stringify(row).toLocaleLowerCase().includes(query))
        : rows;
      tbody.replaceChildren();

      visible.forEach((row) => {
        const tr = document.createElement("tr");
        const id = valueAt(row, resource.id_key || "id");
        const canEdit = Boolean(
          resource.can_edit && id && matchesCondition(row, resource.edit_when),
        );
        const canDelete = Boolean(
          resource.can_delete && id && matchesCondition(row, resource.delete_when),
        );
        const visibleActions = (resource.actions || []).filter(
          (action) => matchesCondition(row, action.visible_when),
        );
        resource.columns.forEach((column) => {
          const cell = document.createElement("td");
          cell.dataset.label = column.label || column.key;
          const value = valueAt(row, column.key);
          if (column.primary && canEdit) {
            const link = document.createElement("a");
            link.href = `${listPath}/${encodeURIComponent(id)}`;
            appendValue(link, value, column);
            cell.appendChild(link);
          } else {
            appendValue(cell, value, column);
          }
          tr.appendChild(cell);
        });

        if (resource.can_edit || resource.can_delete || (resource.actions || []).length) {
          const actions = document.createElement("td");
          actions.className = "pk-row-actions";
          actions.dataset.label = "Actions";
          if (canEdit) {
            const edit = document.createElement("a");
            edit.className = "pk-table-action";
            edit.href = `${listPath}/${encodeURIComponent(id)}`;
            edit.textContent = "Edit";
            actions.appendChild(edit);
          }
          visibleActions.forEach((action) => {
            actions.appendChild(actionButton(
              action.label,
              action.variant === "danger" ? "pk-table-action pk-table-action-danger" : "pk-table-action",
              (event) => runAction(event.currentTarget, row, action),
            ));
          });
          if (canDelete) {
            actions.appendChild(actionButton(
              "Delete",
              "pk-table-action pk-table-action-danger",
              (event) => deleteRow(event.currentTarget, row),
            ));
          }
          if (!canEdit && !canDelete && visibleActions.length === 0) {
            const unavailable = document.createElement("span");
            unavailable.className = "pk-no-actions";
            unavailable.textContent = "No actions available";
            actions.appendChild(unavailable);
          }
          tr.appendChild(actions);
        }
        tbody.appendChild(tr);
      });

      const hasRows = visible.length > 0;
      table.hidden = !hasRows;
      empty.hidden = hasRows;
      emptyCopy.textContent = query
        ? "No records on this page match your filter."
        : "There is nothing to show on this page yet.";
      status.textContent = `${visible.length} ${visible.length === 1 ? "record" : "records"} on this page`;
      status.classList.remove("pk-inline-status-error");
    };

    const load = async () => {
      status.textContent = `Loading ${resource.plural_label}…`;
      status.classList.remove("pk-inline-status-error");
      refresh.disabled = true;
      try {
        const separator = resource.api_path.includes("?") ? "&" : "?";
        const payload = await request(
          `${resource.api_path}${separator}limit=${pageSize}&offset=${offset}`,
        );
        rows = asRows(payload);
        render();
        previous.disabled = offset === 0;
        next.disabled = rows.length < pageSize;
        pageLabel.textContent = `Page ${Math.floor(offset / pageSize) + 1}`;
      } catch (error) {
        rows = [];
        tbody.replaceChildren();
        table.hidden = true;
        empty.hidden = false;
        emptyCopy.textContent = "The data could not be loaded. Check your connection and try again.";
        status.textContent = error.message;
        status.classList.add("pk-inline-status-error");
        previous.disabled = true;
        next.disabled = true;
      } finally {
        refresh.disabled = false;
      }
    };

    search.addEventListener("input", render);
    refresh.addEventListener("click", load);
    previous.addEventListener("click", () => {
      offset = Math.max(0, offset - pageSize);
      load();
    });
    next.addEventListener("click", () => {
      offset += pageSize;
      load();
    });
    load();
  };

  const initForm = () => {
    const form = document.getElementById("pk-resource-form");
    const status = document.getElementById("pk-form-status");
    const submit = document.getElementById("pk-form-submit");
    const id = page.dataset.resourceId;

    if (!id) {
      resource.fields.forEach((field) => {
        if (!field.default_value) return;
        const element = form.elements.namedItem(field.key);
        if (!element) return;
        if (field.kind === "boolean") {
          element.checked = field.default_value === "true";
        } else {
          element.value = field.default_value;
        }
      });
    }

    const showError = (message) => {
      status.textContent = message;
      status.hidden = false;
      status.focus();
    };

    const fieldElement = (field) => form.elements.namedItem(field.key);

    const validateUTF8ByteLimit = (field) => {
      const element = fieldElement(field);
      if (!element || field.kind !== "password" || !field.max) return;
      const byteLength = new TextEncoder().encode(element.value).length;
      element.setCustomValidity(
        byteLength > field.max
          ? `${field.label} must be at most ${field.max} UTF-8 bytes.`
          : "",
      );
    };

    resource.fields.forEach((field) => {
      if (field.kind !== "password" || !field.max) return;
      const element = fieldElement(field);
      if (element) {
        element.addEventListener("input", () => validateUTF8ByteLimit(field));
      }
    });

    const setField = (field, value) => {
      const element = fieldElement(field);
      if (!element) return;
      if (field.kind === "boolean") {
        element.checked = Boolean(value);
      } else if (field.kind === "tags") {
        element.value = Array.isArray(value) ? value.join(", ") : (value || "");
      } else if (field.kind === "datetime" && value) {
        const date = new Date(value);
        element.value = Number.isNaN(date.getTime())
          ? ""
          : new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
      } else {
        element.value = value === null || value === undefined ? "" : String(value);
      }
    };

    const formBody = () => {
      const body = {};
      resource.fields.forEach((field) => {
        if (field.read_only) return;
        const element = fieldElement(field);
        if (!element) return;
        if (field.kind === "boolean") {
          body[field.key] = element.checked;
          return;
        }
        const value = element.value.trim();
        if (!value && !field.required) return;
        if (field.kind === "number") {
          body[field.key] = Number(value);
        } else if (field.kind === "tags") {
          body[field.key] = value
            ? value.split(",").map((item) => item.trim()).filter(Boolean)
            : [];
        } else if (field.kind === "datetime") {
          body[field.key] = value ? new Date(value).toISOString() : null;
        } else {
          body[field.key] = value;
        }
      });
      return body;
    };

    const load = async () => {
      if (!id) return;
      setBusy(submit, true, "Loading…");
      try {
        const row = await request(`${resource.api_path}/${encodeURIComponent(id)}`);
        resource.fields.forEach((field) => setField(field, valueAt(row, field.key)));
      } catch (error) {
        showError(error.message);
        submit.hidden = true;
      } finally {
        setBusy(submit, false);
      }
    };

    const revealSecret = (value) => {
      form.hidden = true;
      const panel = document.getElementById("pk-secret-panel");
      const output = document.getElementById("pk-secret-value");
      const copy = document.getElementById("pk-secret-copy");
      output.textContent = value;
      panel.hidden = false;
      panel.focus();
      copy.addEventListener("click", async () => {
        try {
          await navigator.clipboard.writeText(value);
          notify("Copied to clipboard.");
        } catch (_) {
          const range = document.createRange();
          range.selectNode(output);
          window.getSelection().removeAllRanges();
          window.getSelection().addRange(range);
          document.execCommand("copy");
          window.getSelection().removeAllRanges();
          notify("Copied to clipboard.");
        }
      }, { once: true });
    };

    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      status.hidden = true;
      resource.fields.forEach(validateUTF8ByteLimit);
      if (!form.reportValidity()) return;
      setBusy(submit, true, id ? "Saving…" : "Creating…");
      try {
        const response = await request(
          id ? `${resource.api_path}/${encodeURIComponent(id)}` : resource.api_path,
          {
            method: id ? "PUT" : "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(formBody()),
          },
        );
        const secret = resource.success_field && valueAt(response, resource.success_field);
        if (secret) {
          revealSecret(String(secret));
          return;
        }
        window.location.assign(listPath);
      } catch (error) {
        showError(error.message);
        setBusy(submit, false);
      }
    });

    load();
  };

  if (page.dataset.pkPage === "resource-list") initList();
  if (page.dataset.pkPage === "resource-form") initForm();
})();
