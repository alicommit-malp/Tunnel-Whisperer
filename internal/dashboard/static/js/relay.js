// ── Relay wizard state ──────────────────────────────────────────────────────

let wizardState = {
  domain: '',
  providerKey: '',
  providerName: '',
  token: '',
  awsSecretKey: '',
};

function wizardNext(step) {
  // Validate current step before advancing.
  if (step === 2) {
    const domain = $('#domain').value.trim();
    if (!domain) { alert('Domain is required'); return; }
    wizardState.domain = domain;
  }
  if (step === 4) {
    // Populate confirmation.
    const details = $('#confirm-details');
    details.innerHTML = `
      <span class="kv-label">Domain</span><span class="kv-value">${wizardState.domain}</span>
      <span class="kv-label">Provider</span><span class="kv-value">${wizardState.providerName}</span>
      <span class="kv-label">Instance</span><span class="kv-value">Ubuntu 24.04 (smallest tier)</span>
      <span class="kv-label">Firewall</span><span class="kv-value">ports 80, 443 only</span>
      <span class="kv-label">Software</span><span class="kv-value">Caddy + Xray + SSH (localhost-only)</span>
    `;
  }
  showStep(step);
}

function wizardBack(step) {
  showStep(step);
}

function showStep(step) {
  $$('.wizard-panel').forEach(p => p.classList.remove('active'));
  const panel = $(`#step-${step}`);
  if (panel) panel.classList.add('active');

  $$('.wizard-step').forEach(s => {
    const n = parseInt(s.dataset.step);
    s.classList.remove('active', 'done');
    if (n < step) s.classList.add('done');
    if (n === step) s.classList.add('active');
  });
}

// ── Provider selection ──────────────────────────────────────────────────────

(function initProviders() {
  if (typeof providers === 'undefined') return;
  const list = $('#provider-list');
  if (!list) return;

  providers.forEach(p => {
    const btn = document.createElement('button');
    btn.className = 'btn mb-8';
    btn.style.display = 'block';
    btn.style.width = '100%';
    btn.textContent = p.name;
    btn.onclick = () => {
      wizardState.providerKey = p.key;
      wizardState.providerName = p.name;
      buildCredFields(p);
      wizardNext(3);
    };
    list.appendChild(btn);
  });
})();

function buildCredFields(provider) {
  const help = $('#cred-help');
  const fields = $('#cred-fields');
  help.textContent = `Generate here: ${provider.token_link}`;
  fields.innerHTML = '';

  if (provider.name === 'AWS') {
    fields.innerHTML = `
      <div class="form-group">
        <label>AWS Access Key ID</label>
        <input type="text" id="cred-token">
      </div>
      <div class="form-group">
        <label>AWS Secret Access Key</label>
        <input type="password" id="cred-secret">
      </div>
    `;
  } else {
    fields.innerHTML = `
      <div class="form-group">
        <label>${provider.token_name}</label>
        <input type="password" id="cred-token">
      </div>
    `;
  }
}

// ── Credential test ─────────────────────────────────────────────────────────

async function testCreds() {
  const btn = $('#btn-test-creds');
  const result = $('#cred-test-result');
  btn.disabled = true;
  result.className = 'hidden';

  wizardState.token = $('#cred-token').value.trim();
  const secretEl = $('#cred-secret');
  wizardState.awsSecretKey = secretEl ? secretEl.value.trim() : '';

  try {
    await api.post('/api/relay/test-creds', {
      provider_name: wizardState.providerName,
      token: wizardState.token,
      aws_secret_key: wizardState.awsSecretKey,
    });
    wizardNext(4);
  } catch (err) {
    result.className = 'alert alert-error mt-16';
    result.textContent = err.message;
  } finally {
    btn.disabled = false;
  }
}

// ── Provisioning ────────────────────────────────────────────────────────────

async function startProvision() {
  const btn = $('#btn-provision');
  btn.disabled = true;
  showStep(5);

  try {
    const resp = await api.post('/api/relay/provision', {
      domain: wizardState.domain,
      provider_key: wizardState.providerKey,
      provider_name: wizardState.providerName,
      token: wizardState.token,
      aws_secret_key: wizardState.awsSecretKey,
    });

    const log = $('#provision-progress');
    connectSSE(resp.session_id, (event) => {
      renderProgressEvent(log, event);
      // Show sticky IP banner when step 7 completes with relay IP.
      if (event.step === 7 && event.status === 'completed' && event.data) {
        const banner = $('#relay-ip-banner');
        const ipVal = $('#relay-ip-value');
        if (banner && ipVal) {
          ipVal.textContent = event.data;
          banner.classList.remove('hidden');
        }
      }
    }, (err) => {
      if (err) {
        $('#provision-error-msg').textContent = err.message;
        $('#provision-error').classList.remove('hidden');
      } else {
        $('#provision-done').classList.remove('hidden');
      }
    });
  } catch (err) {
    $('#provision-error-msg').textContent = err.message;
    $('#provision-error').classList.remove('hidden');
  }
}

// ── Destroy ─────────────────────────────────────────────────────────────────

function showDestroyPrompt() {
  if (!confirm('Are you sure you want to destroy the relay? This cannot be undone.')) return;

  // AWS needs credentials re-entered; other providers have them in terraform.tfvars.
  if (typeof relayProvider !== 'undefined' && relayProvider === 'AWS') {
    const panel = $('#destroy-creds');
    if (panel) panel.classList.remove('hidden');
    const btn = $('#btn-destroy');
    if (btn) btn.classList.add('hidden');
  } else {
    destroyRelay();
  }
}

function hideDestroyPrompt() {
  const panel = $('#destroy-creds');
  if (panel) panel.classList.add('hidden');
  const btn = $('#btn-destroy');
  if (btn) btn.classList.remove('hidden');
}

async function destroyRelay() {
  const btn = $('#btn-destroy-confirm') || $('#btn-destroy');
  if (btn) btn.disabled = true;

  const credsPanel = $('#destroy-creds');
  if (credsPanel) credsPanel.classList.add('hidden');

  const log = $('#destroy-progress');
  log.classList.remove('hidden');
  log.innerHTML = '';

  // Collect AWS creds if provided.
  let creds = null;
  const keyEl = $('#destroy-aws-key');
  const secretEl = $('#destroy-aws-secret');
  if (keyEl && secretEl && keyEl.value.trim() && secretEl.value.trim()) {
    creds = {
      'AWS_ACCESS_KEY_ID': keyEl.value.trim(),
      'AWS_SECRET_ACCESS_KEY': secretEl.value.trim(),
    };
  }

  try {
    const resp = await api.post('/api/relay/destroy', { creds });
    connectSSE(resp.session_id, (event) => {
      renderProgressEvent(log, event);
    }, (err) => {
      if (err) {
        log.innerHTML += `<div class="progress-step failed"><span class="step-label">Error: ${err.message}</span></div>`;
      } else {
        setTimeout(() => { window.location.href = '/relay'; }, 1500);
      }
    });
  } catch (err) {
    log.innerHTML = `<div class="progress-step failed"><span class="step-label">Error: ${err.message}</span></div>`;
    if (btn) btn.disabled = false;
  }
}

// ── Copy relay IP ────────────────────────────────────────────────────────────

function copyRelayIP() {
  const ip = $('#relay-ip-value');
  if (!ip) return;
  navigator.clipboard.writeText(ip.textContent).then(() => {
    const btn = $('#btn-copy-ip');
    const orig = btn.textContent;
    btn.textContent = 'Copied!';
    setTimeout(() => { btn.textContent = orig; }, 1500);
  });
}

// ── Test relay connectivity ──────────────────────────────────────────────────

async function testRelay() {
  const btn = $('#btn-test-relay');
  const result = $('#test-result');
  if (!btn || !result) return;

  btn.disabled = true;
  btn.textContent = 'Testing...';
  result.innerHTML = '';
  result.className = 'progress-log mt-16';

  try {
    const { session_id } = await api.post('/api/relay/test', {});
    connectSSE(session_id, (ev) => {
      renderProgressEvent(result, ev);
    }, (err) => {
      if (err) {
        result.innerHTML += `<div class="progress-step failed"><span class="step-label">${err.message}</span></div>`;
      }
      btn.disabled = false;
      btn.textContent = 'Test Connectivity';
    });
  } catch (err) {
    result.innerHTML = `<div class="alert alert-error">${err.message}</div>`;
    btn.disabled = false;
    btn.textContent = 'Test Connectivity';
  }
}
