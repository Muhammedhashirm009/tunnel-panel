document.addEventListener('DOMContentLoaded', () => {
    const terminalContainer = document.getElementById('terminal');
    const loginOverlay = document.getElementById('ssh-login-overlay');
    const connectBtn = document.getElementById('ssh-connect-btn');

    if (!terminalContainer) return;

    // Initialize xterm.js
    const term = new Terminal({
        cursorBlink: true,
        fontFamily: '"Fira Code", monospace, courier-new, courier, sans-serif',
        fontSize: 14,
        theme: {
            background: '#0a0a0a',
            foreground: '#f0f0f0',
            cursor: '#f0f0f0',
            black: '#000000',
            red: '#e06c75',
            green: '#98c379',
            yellow: '#d19a66',
            blue: '#61afef',
            magenta: '#c678dd',
            cyan: '#56b6c2',
            white: '#dcdfe4',
            brightBlack: '#5c6370',
            brightRed: '#e06c75',
            brightGreen: '#98c379',
            brightYellow: '#d19a66',
            brightBlue: '#61afef',
            brightMagenta: '#c678dd',
            brightCyan: '#56b6c2',
            brightWhite: '#ffffff'
        }
    });

    const fitAddon = new FitAddon.FitAddon();
    term.loadAddon(fitAddon);

    term.open(terminalContainer);
    fitAddon.fit();

    let ws = null;

    // Re-fit on window resize
    window.addEventListener('resize', () => {
        fitAddon.fit();
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({
                type: 'resize',
                cols: term.cols,
                rows: term.rows
            }));
        }
    });

    term.writeln('\x1b[1;36mReady to connect...\x1b[0m');

    if (connectBtn) {
        connectBtn.addEventListener('click', () => {
            const host = document.getElementById('ssh-host').value;
            const user = document.getElementById('ssh-user').value;
            const pass = document.getElementById('ssh-pass').value;

            if (!host || !user || !pass) {
                alert('Host, username, and password are required.');
                return;
            }

            connectBtn.disabled = true;
            connectBtn.textContent = 'Connecting...';

            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = `${protocol}//${window.location.host}/api/terminal/ws`;

            ws = new WebSocket(wsUrl);

            ws.onopen = () => {
                // Hide overlay
                loginOverlay.style.display = 'none';
                term.clear();

                // Send auth message
                ws.send(JSON.stringify({
                    type: 'auth',
                    host: host,
                    user: user,
                    pass: pass,
                    cols: term.cols,
                    rows: term.rows
                }));
            };

            ws.onmessage = (event) => {
                term.write(event.data);
            };

            ws.onclose = () => {
                term.writeln('\r\n\x1b[1;31m✗ Connection closed.\x1b[0m');
                loginOverlay.style.display = 'flex';
                connectBtn.disabled = false;
                connectBtn.textContent = 'Connect';
            };

            ws.onerror = (error) => {
                term.writeln('\r\n\x1b[1;31m✗ WebSocket error occurred.\x1b[0m');
                console.error('WebSocket Error:', error);
                connectBtn.disabled = false;
                connectBtn.textContent = 'Connect';
            };
        });
    }

    // Forward terminal input to WebSocket
    term.onData((data) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({
                type: 'input',
                data: data
            }));
        }
    });
});
