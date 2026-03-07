// TunnelPanel — Dashboard Logic

async function loadDashboard() {
    const data = await API.get('/dashboard/stats');
    if (!data.success) return;

    const s = data.data;

    // CPU
    const cpuPct = s.cpu.usage_total.toFixed(1);
    document.getElementById('cpuValue').textContent = cpuPct + '%';
    document.getElementById('cpuBar').style.width = cpuPct + '%';

    // RAM
    const ramPct = s.memory.used_percent.toFixed(1);
    document.getElementById('ramValue').textContent = ramPct + '%';
    document.getElementById('ramBar').style.width = ramPct + '%';

    // Disk
    if (s.disk && s.disk.length > 0) {
        const rootDisk = s.disk.find(d => d.mountpoint === '/') || s.disk[0];
        const diskPct = rootDisk.used_percent.toFixed(1);
        document.getElementById('diskValue').textContent = diskPct + '%';
        document.getElementById('diskBar').style.width = diskPct + '%';
    }

    // Network
    document.getElementById('networkValue').textContent =
        '↑ ' + formatBytes(s.network.bytes_sent) + '  ↓ ' + formatBytes(s.network.bytes_recv);

    // System info
    document.getElementById('sysHostname').textContent = s.hostname;
    document.getElementById('sysPlatform').textContent = s.platform;
    document.getElementById('sysKernel').textContent = s.kernel;
    document.getElementById('sysUptime').textContent = s.uptime;
    document.getElementById('sysCPU').textContent = s.cpu.model + ' (' + s.cpu.threads + ' threads)';
    document.getElementById('sysLoad').textContent =
        s.load_average.load1.toFixed(2) + ' / ' + s.load_average.load5.toFixed(2) + ' / ' + s.load_average.load15.toFixed(2);
    document.getElementById('sysRAM').textContent =
        formatBytes(s.memory.used) + ' / ' + formatBytes(s.memory.total);
    document.getElementById('sysSwap').textContent =
        formatBytes(s.memory.swap_used) + ' / ' + formatBytes(s.memory.swap_total);
}

async function loadServices() {
    const data = await API.get('/dashboard/services');
    const grid = document.getElementById('servicesGrid');

    if (!data.success || !data.data || data.data.length === 0) {
        grid.innerHTML = '<div class="empty-state"><p>No services detected</p></div>';
        return;
    }

    grid.innerHTML = data.data.map(svc => `
        <div class="service-item">
            <div class="service-dot ${svc.running ? 'running' : 'stopped'}"></div>
            <span class="service-name">${svc.name}</span>
            <span class="service-status">${svc.status}</span>
        </div>
    `).join('');
}

async function loadTunnelStatus() {
    const data = await API.get('/tunnels/status');
    if (!data.success) return;

    const s = data.data;
    const updateBadge = (id, running) => {
        const el = document.getElementById(id);
        if (!el) return;
        el.className = 'badge ' + (running ? 'badge-success' : 'badge-danger');
        el.textContent = running ? 'Connected' : 'Disconnected';
    };

    updateBadge('panelTunnelBadge', s.panel_tunnel.running);
    updateBadge('appsTunnelBadge', s.apps_tunnel.running);
}

// Initialize dashboard
loadDashboard();
loadServices();
loadTunnelStatus();

// Auto-refresh every 5 seconds
setInterval(loadDashboard, 5000);
setInterval(loadServices, 15000);
setInterval(loadTunnelStatus, 10000);
