document.addEventListener('DOMContentLoaded', () => {
    loadDatabases();

    document.getElementById('create-db-form').addEventListener('submit', async (e) => {
        e.preventDefault();
        await createDatabase();
    });
});

function showCreateDBModal() {
    document.getElementById('create-db-modal').style.display = 'flex';
}

function hideCreateDBModal() {
    document.getElementById('create-db-modal').style.display = 'none';
    document.getElementById('create-db-form').reset();
}

async function loadDatabases() {
    try {
        const res = await api.get('/api/databases');
        const tbody = document.getElementById('dbs-list');
        tbody.innerHTML = '';

        if (!res.data || res.data.length === 0) {
            tbody.innerHTML = '<tr><td colspan="7" style="text-align: center; padding: 2rem;">No databases provisioned yet.</td></tr>';
            return;
        }

        res.data.forEach(db => {
            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td><strong>${db.name}</strong></td>
                <td><span class="badge ${db.db_type}">${db.db_type}</span></td>
                <td>${db.db_user}</td>
                <td><code style="background:var(--bg-card); padding:2px 4px; border-radius:4px; font-size:0.85em;">${db.container_id ? db.container_id.substring(0, 10) : 'none'}</code> (Port ${db.port})</td>
                <td>${db.pma_domain ? `<a href="https://${db.pma_domain}" target="_blank" style="color:var(--primary-color);">https://${db.pma_domain}</a>` : 'None'}</td>
                <td>${new Date(db.created_at).toLocaleString()}</td>
                <td>
                    <button class="btn btn-sm btn-danger" onclick="deleteDatabase(${db.id}, '${db.name}')" title="Delete Database & Containers">Delete</button>
                </td>
            `;
            tbody.appendChild(tr);
        });
    } catch (e) {
        console.error(e);
        document.getElementById('dbs-list').innerHTML = `<tr><td colspan="7" style="text-align: center; color: red;">Error loading databases: ${e.message}</td></tr>`;
    }
}

async function createDatabase() {
    const btn = document.querySelector('#create-db-form button[type="submit"]');
    const originalText = btn.textContent;
    btn.textContent = 'Provisioning...';
    btn.disabled = true;

    try {
        const payload = {
            name: document.getElementById('db-name').value,
            type: document.getElementById('db-type').value,
            root_password: document.getElementById('db-root-pass').value,
            user: document.getElementById('db-user').value,
            user_password: document.getElementById('db-user-pass').value,
            pma_domain: document.getElementById('pma-domain').value
        };

        const res = await api.post('/api/databases', payload);
        if (res.warning) {
            alert(res.warning);
        }

        hideCreateDBModal();
        loadDatabases();
    } catch (e) {
        alert('Failed to provision database: ' + e.message);
    } finally {
        btn.textContent = originalText;
        btn.disabled = false;
    }
}

async function deleteDatabase(id, name) {
    if (!confirm(`Are you sure you want to completely DELETE database "${name}"? This will stop and remove its container, the phpMyAdmin container, and all underlying data.`)) {
        return;
    }

    try {
        await api.delete(`/api/databases/${id}`);
        loadDatabases();
    } catch (e) {
        alert('Failed to delete database: ' + e.message);
    }
}
