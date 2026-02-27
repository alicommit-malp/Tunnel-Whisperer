// ── Proxy settings ──────────────────────────────────────────────────────────

async function saveProxy() {
  const input = $('#proxy-url');
  const url = input.value.trim();

  if (!url) {
    showProxyError('Enter a proxy URL or click Clear to remove.');
    return;
  }

  const btn = $('#btn-proxy-save');
  btn.disabled = true;

  try {
    await api.post('/api/proxy', { proxy: url });
    showProxySuccess('Proxy saved. Takes effect on next start.');
    updateProxyBadge(url);
  } catch (err) {
    showProxyError(err.message);
  } finally {
    btn.disabled = false;
  }
}

async function clearProxy() {
  try {
    await api.post('/api/proxy', { proxy: '' });
    $('#proxy-url').value = '';
    showProxySuccess('Proxy cleared.');
    updateProxyBadge('');
  } catch (err) {
    showProxyError(err.message);
  }
}

function updateProxyBadge(url) {
  const badge = $('#proxy-badge');
  if (!badge) return;
  if (url) {
    badge.textContent = 'configured';
    badge.className = 'badge badge-green';
  } else {
    badge.textContent = 'none';
    badge.className = 'badge badge-dim';
  }
}

function showProxyError(msg) {
  const el = $('#proxy-error');
  const ok = $('#proxy-success');
  if (ok) ok.classList.add('hidden');
  if (el) {
    el.textContent = msg;
    el.classList.remove('hidden');
  }
}

function showProxySuccess(msg) {
  const el = $('#proxy-success');
  const err = $('#proxy-error');
  if (err) err.classList.add('hidden');
  if (el) {
    el.textContent = msg;
    el.classList.remove('hidden');
  }
}
