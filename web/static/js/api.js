// TunnelPanel API Client
const API = {
    baseURL: '/api',

    async request(method, path, body = null) {
        const opts = {
            method,
            headers: { 'Content-Type': 'application/json' },
            credentials: 'same-origin'
        };
        if (body) opts.body = JSON.stringify(body);

        try {
            const resp = await fetch(this.baseURL + path, opts);

            // Handle auth redirects
            if (resp.status === 401) {
                window.location.href = '/login';
                return { success: false, error: 'Session expired' };
            }

            return await resp.json();
        } catch (err) {
            console.error('API Error:', err);
            return { success: false, error: 'Network error: ' + err.message };
        }
    },

    get(path) { return this.request('GET', path); },
    post(path, body) { return this.request('POST', path, body); },
    put(path, body) { return this.request('PUT', path, body); },
    delete(path) { return this.request('DELETE', path); },
};
