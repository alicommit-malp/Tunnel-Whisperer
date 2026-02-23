// ── Mapping editor ──────────────────────────────────────────────────────────

function addMapping() {
  const container = $('#mappings');
  const row = document.createElement('div');
  row.className = 'mapping-row';
  row.innerHTML = `
    <input type="number" class="client-port" placeholder="Client port" min="1" max="65535">
    <span class="arrow">-></span>
    <input type="number" class="server-port" placeholder="Server port" min="1" max="65535">
    <button class="btn btn-sm btn-danger" onclick="removeMapping(this)">x</button>
  `;
  container.appendChild(row);
  updateRemoveButtons();
}

function removeMapping(btn) {
  btn.closest('.mapping-row').remove();
  updateRemoveButtons();
}

function updateRemoveButtons() {
  const rows = $$('.mapping-row');
  rows.forEach((row, i) => {
    const btn = row.querySelector('.btn-danger');
    btn.style.visibility = rows.length > 1 ? 'visible' : 'hidden';
  });
}

function getMappings() {
  return $$('.mapping-row').map(row => {
    const cp = row.querySelector('.client-port').value.trim();
    const sp = row.querySelector('.server-port').value.trim();
    if (!cp || !sp) return null;
    return { client_port: parseInt(cp), server_port: parseInt(sp) };
  }).filter(Boolean);
}

// ── Create user ─────────────────────────────────────────────────────────────

async function createUser() {
  const name = $('#user-name').value.trim();
  if (!name) { alert('Username is required'); return; }

  const mappings = getMappings();
  if (mappings.length === 0) { alert('At least one port mapping is required'); return; }

  const btn = $('#btn-create-user');
  btn.disabled = true;

  $('#user-form').classList.add('hidden');
  $('#user-progress').classList.remove('hidden');

  try {
    const resp = await api.post('/api/users', { name, mappings });
    const log = $('#create-progress');

    connectSSE(resp.session_id, (event) => {
      renderProgressEvent(log, event);
    }, (err) => {
      if (err) {
        $('#create-error-msg').textContent = err.message;
        $('#create-error').classList.remove('hidden');
      } else {
        $('#download-link').href = `/api/users/${name}/download`;
        $('#create-done').classList.remove('hidden');
      }
    });
  } catch (err) {
    $('#create-error-msg').textContent = err.message;
    $('#create-error').classList.remove('hidden');
  }
}

// ── Delete user ─────────────────────────────────────────────────────────────

async function deleteUser(name) {
  if (!confirm(`Delete user "${name}"? This removes their keys, config, and authorized_keys entry.`)) return;

  const btn = $('#btn-delete');
  btn.disabled = true;

  try {
    await api.del(`/api/users/${name}`);
    window.location.href = '/users';
  } catch (err) {
    alert('Delete failed: ' + err.message);
    btn.disabled = false;
  }
}
