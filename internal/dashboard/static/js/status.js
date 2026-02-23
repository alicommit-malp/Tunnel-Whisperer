// ── Server start/stop ────────────────────────────────────────────────────────

async function serverStart() {
  const btn = $('#btn-server-start');
  if (btn) btn.disabled = true;

  const log = $('#server-progress');
  log.classList.remove('hidden');
  log.innerHTML = '';

  try {
    const { session_id } = await api.post('/api/server/start', {});
    connectSSE(session_id, (ev) => renderProgressEvent(log, ev), (err) => {
      if (err) {
        log.innerHTML += `<div class="progress-step failed"><span class="step-label">${err.message}</span></div>`;
      }
      setTimeout(() => window.location.reload(), 1000);
    });
  } catch (e) {
    log.innerHTML = `<div class="alert alert-error">${e.message}</div>`;
    if (btn) btn.disabled = false;
  }
}

async function serverStop() {
  const btn = $('#btn-server-stop');
  if (btn) btn.disabled = true;

  const log = $('#server-progress');
  log.classList.remove('hidden');
  log.innerHTML = '';

  try {
    const { session_id } = await api.post('/api/server/stop', {});
    connectSSE(session_id, (ev) => renderProgressEvent(log, ev), () => {
      setTimeout(() => window.location.reload(), 1000);
    });
  } catch (e) {
    log.innerHTML = `<div class="alert alert-error">${e.message}</div>`;
    if (btn) btn.disabled = false;
  }
}

// ── Client start/stop ────────────────────────────────────────────────────────

async function clientStart() {
  const btn = $('#btn-client-start');
  if (btn) btn.disabled = true;

  const log = $('#client-progress');
  log.classList.remove('hidden');
  log.innerHTML = '';

  try {
    const { session_id } = await api.post('/api/client/start', {});
    connectSSE(session_id, (ev) => renderProgressEvent(log, ev), (err) => {
      if (err) {
        log.innerHTML += `<div class="progress-step failed"><span class="step-label">${err.message}</span></div>`;
      }
      setTimeout(() => window.location.reload(), 1000);
    });
  } catch (e) {
    log.innerHTML = `<div class="alert alert-error">${e.message}</div>`;
    if (btn) btn.disabled = false;
  }
}

async function clientStop() {
  const btn = $('#btn-client-stop');
  if (btn) btn.disabled = true;

  const log = $('#client-progress');
  log.classList.remove('hidden');
  log.innerHTML = '';

  try {
    const { session_id } = await api.post('/api/client/stop', {});
    connectSSE(session_id, (ev) => renderProgressEvent(log, ev), () => {
      setTimeout(() => window.location.reload(), 1000);
    });
  } catch (e) {
    log.innerHTML = `<div class="alert alert-error">${e.message}</div>`;
    if (btn) btn.disabled = false;
  }
}

// ── Config zip upload ────────────────────────────────────────────────────────

(function() {
  const form = $('#upload-form');
  if (!form) return;

  const fileInput = $('#config-file');
  const uploadArea = $('#upload-area');
  const filenameEl = $('#upload-filename');
  const uploadBtn = $('#btn-upload');
  const errorEl = $('#upload-error');

  fileInput.addEventListener('change', () => {
    if (fileInput.files.length > 0) {
      filenameEl.textContent = fileInput.files[0].name;
      uploadBtn.disabled = false;
    }
  });

  // Drag and drop.
  uploadArea.addEventListener('dragover', (e) => {
    e.preventDefault();
    uploadArea.classList.add('dragover');
  });
  uploadArea.addEventListener('dragleave', () => {
    uploadArea.classList.remove('dragover');
  });
  uploadArea.addEventListener('drop', (e) => {
    e.preventDefault();
    uploadArea.classList.remove('dragover');
    if (e.dataTransfer.files.length > 0) {
      fileInput.files = e.dataTransfer.files;
      filenameEl.textContent = e.dataTransfer.files[0].name;
      uploadBtn.disabled = false;
    }
  });

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    errorEl.classList.add('hidden');
    uploadBtn.disabled = true;
    uploadBtn.textContent = 'Uploading...';

    const fd = new FormData();
    fd.append('config', fileInput.files[0]);

    try {
      const resp = await fetch('/api/client/upload', { method: 'POST', body: fd });
      if (!resp.ok) {
        const data = await resp.json();
        throw new Error(data.error || 'Upload failed');
      }
      window.location.reload();
    } catch (err) {
      errorEl.textContent = err.message;
      errorEl.classList.remove('hidden');
      uploadBtn.disabled = false;
      uploadBtn.textContent = 'Upload Config';
    }
  });
})();
