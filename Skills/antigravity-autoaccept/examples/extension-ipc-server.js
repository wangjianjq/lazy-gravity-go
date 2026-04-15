/**
 * AntiGravity AutoAccept — IPC HTTP Server
 * Part of the antigravity-autoaccept skill examples.
 *
 * Copy the startIPCServer / stopIPCServer functions and the
 * _getWorkbenchSession / evalInWorkbench / injectText methods
 * into your extension.js and ConnectionManager.js respectively.
 *
 * Requirements:
 *   - connectionManager must be an initialized ConnectionManager instance
 *   - ConnectionManager must have _getWorkbenchSession(), evalInWorkbench(), injectText()
 */

'use strict';

const http = require('http');

// ─── Configuration ─────────────────────────────────────────────────────────

const IPC_PORT = 27182;  // Must match Go client's ipcBase constant
let ipcServer = null;

// ─── IPC Server ────────────────────────────────────────────────────────────

/**
 * Start the IPC HTTP server.
 * Binds to 127.0.0.1 only (loopback isolation).
 * Silently handles EADDRINUSE so multiple IDE windows don't crash.
 *
 * @param {ConnectionManager} connectionManager
 * @param {Function} log - logging function (msg: string) => void
 */
function startIPCServer(connectionManager, log) {
    if (ipcServer) return;

    ipcServer = http.createServer(async (req, res) => {
        res.setHeader('Content-Type', 'application/json');
        res.setHeader('Access-Control-Allow-Origin', '127.0.0.1');

        // ── GET /ping — health check ──────────────────────────────────────
        if (req.method === 'GET' && req.url === '/ping') {
            const sessions = connectionManager ? connectionManager.sessions.size : 0;
            const ws = connectionManager ? !!connectionManager._getWorkbenchSession() : false;
            res.end(JSON.stringify({ ok: true, sessions, workbenchReady: ws }));
            return;
        }

        // ── POST /inject — inject text and submit ─────────────────────────
        if (req.method === 'POST' && req.url === '/inject') {
            let body = '';
            req.on('data', c => body += c);
            req.on('end', async () => {
                try {
                    const { text } = JSON.parse(body);
                    if (typeof text !== 'string' || !text) {
                        res.statusCode = 400;
                        res.end(JSON.stringify({ ok: false, error: 'Missing or empty text field' }));
                        return;
                    }
                    if (!connectionManager) throw new Error('ConnectionManager not initialised');
                    await connectionManager.injectText(text);
                    log(`[IPC] Injected ${text.length} chars`);
                    res.end(JSON.stringify({ ok: true }));
                } catch (e) {
                    log(`[IPC] /inject error: ${e.message}`);
                    res.statusCode = 500;
                    res.end(JSON.stringify({ ok: false, error: e.message }));
                }
            });
            return;
        }

        // ── POST /eval — run JS in workbench context ──────────────────────
        if (req.method === 'POST' && req.url === '/eval') {
            let body = '';
            req.on('data', c => body += c);
            req.on('end', async () => {
                try {
                    const { expression } = JSON.parse(body);
                    if (typeof expression !== 'string' || !expression) {
                        res.statusCode = 400;
                        res.end(JSON.stringify({ ok: false, error: 'Missing expression' }));
                        return;
                    }
                    if (!connectionManager) throw new Error('ConnectionManager not initialised');
                    const value = await connectionManager.evalInWorkbench(expression);
                    res.end(JSON.stringify({ ok: true, value }));
                } catch (e) {
                    res.statusCode = 500;
                    res.end(JSON.stringify({ ok: false, error: e.message }));
                }
            });
            return;
        }

        res.statusCode = 404;
        res.end(JSON.stringify({ ok: false, error: 'Not found' }));
    });

    // EADDRINUSE is expected when a second IDE window tries to bind.
    // First window wins, second window runs without IPC (AutoAccept still works).
    ipcServer.on('error', e => {
        if (e.code !== 'EADDRINUSE') {
            log(`[IPC] Server error: ${e.message}`);
        }
    });

    ipcServer.listen(IPC_PORT, '127.0.0.1', () => {
        log(`[IPC] HTTP server listening on 127.0.0.1:${IPC_PORT}`);
    });
}

function stopIPCServer(log) {
    if (ipcServer) {
        ipcServer.close();
        ipcServer = null;
        log('[IPC] HTTP server stopped');
    }
}

// ─── ConnectionManager additions ───────────────────────────────────────────
// Add these methods to the ConnectionManager class in ConnectionManager.js

const ConnectionManagerAdditions = {
    /**
     * Find the primary workbench session (the vscode-file:// workbench target).
     * Called by /ping and /eval to determine which CDP session to use.
     */
    _getWorkbenchSession() {
        for (const [, info] of this.sessions) {
            if (info.url && (
                info.url.includes('workbench') ||
                info.url.startsWith('vscode-file://')
            )) {
                return info;
            }
        }
        return null;
    },

    /**
     * Evaluate arbitrary JS in the workbench context.
     * Returns the JS return value (JSON-serialized by CDP).
     * Throws if no workbench session is available.
     *
     * @param {string} expression - Synchronous JS expression to evaluate
     * @returns {*} The evaluated value
     */
    async evalInWorkbench(expression) {
        const session = this._getWorkbenchSession();
        if (!session) throw new Error('No workbench CDP session');
        const result = await this._workerEval(session.wsUrl, expression);
        // _workerEval resolves to {result: {result: {value: ...}}}
        return (result?.result?.result?.value) !== undefined
            ? result.result.result.value
            : null;
    },

    /**
     * Inject text into Antigravity's chat input and submit it.
     * Uses execCommand to avoid needing Input CDP domain permissions.
     * Throws if no workbench session or if no editor element is found.
     *
     * @param {string} text - The text to inject (may contain newlines)
     */
    async injectText(text) {
        const session = this._getWorkbenchSession();
        if (!session) {
            throw new Error('No workbench CDP session — AutoAccept may not be connected yet');
        }

        const escapedText = JSON.stringify(text);
        const script = `(function() {
            const editors = Array.from(document.querySelectorAll(
                'div[role="textbox"]:not(.xterm-helper-textarea)'));
            const visible = editors.filter(el => el.offsetParent !== null);
            const editor = visible[visible.length - 1];
            if (!editor) return { ok: false, error: 'No editor found' };
            editor.focus();
            document.execCommand('selectAll', false, null);
            document.execCommand('delete', false, null);
            document.execCommand('insertText', false, ${escapedText});
            ['keydown', 'keypress', 'keyup'].forEach(type => {
                editor.dispatchEvent(new KeyboardEvent(type, {
                    key: 'Enter', code: 'Enter', keyCode: 13, which: 13,
                    bubbles: true, cancelable: true
                }));
            });
            return { ok: true };
        })()`;

        const result = await this._workerEval(session.wsUrl, script);
        const val = result?.result?.result?.value;
        if (!val?.ok) throw new Error(val?.error || 'Text injection failed');
        return val;
    }
};

module.exports = { startIPCServer, stopIPCServer, ConnectionManagerAdditions };
