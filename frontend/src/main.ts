import './style.css';
import { EventsOn } from '../wailsjs/runtime/runtime';

// ─── Config interface (matches Go Config struct JSON tags) ───────────────────
interface AppConfig {
    language: string;
    telegram_token: string;
    workspace_base_dir: string;
    tts_voice: string;
    allowed_chat_ids: string[] | null;
    theme: string;
}

// ─── Wails runtime type declarations ────────────────────────────────────────
declare global {
    interface Window {
        go: {
            main: {
                App: {
                    HasConfig(): Promise<boolean>;
                    GetConfig(): Promise<AppConfig>;
                    SaveConfig(config: AppConfig): Promise<void>;
                    ClearConfig(): Promise<void>;
                    StartBot(): Promise<void>;
                    StopBot(): Promise<void>;
                    IsBotRunning(): Promise<boolean>;
                    HideToTray(): Promise<void>;
                }
            }
        };
        runtime: any;
    }
}

// ─── i18n Dictionary ─────────────────────────────────────────────────────────
const translations: Record<string, Record<string, string>> = {
    en: {
        NavDashboard:    'Dashboard',
        NavSettings:     'Settings',
        WelcomeTitle:    'Welcome to LazyGravity',
        WelcomeSub:      'Let\'s set up your Telegram bridge.',
        SecTelegram:     'Telegram',
        SecGeneral:      'General',
        LblBotToken:     'Bot Token',
        HelpBotToken:    'From @BotFather on Telegram',
        LblChatIDs:      'Allowed Chat IDs',
        HelpChatIDs:     'One Chat ID per line. Leave empty to allow all users. Send /start to @userinfobot to find yours.',
        LblLanguage:     'Interface Language',
        OptLangEn:       'English',
        OptLangZh:       '简体中文',
        LblWorkspace:    'Workspace Directory',
        LblVoice:        'TTS Voice',
        OptVoiceEn:      'English (Aria Neural)',
        OptVoiceZh:      'Chinese (Yunyang Neural)',
        OptVoiceNone:    'Disable Voice (Text Only)',
        LblTheme:        'Theme',
        BtnSave:         'Save & Start',
        SystemDashboard: 'System Dashboard',
        TerminalOut:     'Live Output',
        SysInitWaiting:  'System initialized... waiting for events.',
        ConfigDetails:   'Configuration',
        BtnUpdateSet:    'Update Settings',
        DangerZone:      'Danger Zone',
        DangerZoneDesc:  'Delete all configuration and return to setup wizard. The bot will be stopped.',
        BtnClearConfig:  'Clear Configuration',
    },
    zh: {
        NavDashboard:    '控制台',
        NavSettings:     '设置',
        WelcomeTitle:    '欢迎使用 LazyGravity',
        WelcomeSub:      '请完成 Telegram 桥接配置。',
        SecTelegram:     'Telegram',
        SecGeneral:      '常规',
        LblBotToken:     'Bot 令牌 (Token)',
        HelpBotToken:    '在 Telegram 上向 @BotFather 获取',
        LblChatIDs:      '允许的 Chat ID',
        HelpChatIDs:     '每行一个 Chat ID，留空则允许所有人。向 @userinfobot 发送 /start 可查询自己的 ID。',
        LblLanguage:     '界面语言',
        OptLangEn:       'English (英文)',
        OptLangZh:       '简体中文',
        LblWorkspace:    '工作区路径',
        LblVoice:        'TTS 语音选项',
        OptVoiceEn:      '英语 (Aria Neural)',
        OptVoiceZh:      '中文 (Yunyang Neural)',
        OptVoiceNone:    '关闭语音',
        LblTheme:        '主题',
        BtnSave:         '保存并启动',
        SystemDashboard: '系统大盘',
        TerminalOut:     '实时日志',
        SysInitWaiting:  '系统就绪，等待消息中...',
        ConfigDetails:   '应用配置',
        BtnUpdateSet:    '保存设置',
        DangerZone:      '危险区域',
        DangerZoneDesc:  '删除所有配置并返回初始向导。机器人将自动停止。',
        BtnClearConfig:  '清空配置',
    },
};

let currentLang = 'en';

function applyI18n(lang: string) {
    currentLang = lang;
    const dict = translations[lang] || translations['en'];
    document.querySelectorAll('[data-i18n]').forEach((el) => {
        const key = el.getAttribute('data-i18n');
        if (key && dict[key]) {
            el.textContent = dict[key];
        }
    });
}

// ─── Theme management ────────────────────────────────────────────────────────
type Theme = 'dark' | 'light' | 'system';

function applyTheme(theme: Theme) {
    document.body.classList.remove('dark-theme', 'light-theme');
    if (theme === 'dark')  document.body.classList.add('dark-theme');
    if (theme === 'light') document.body.classList.add('light-theme');
    // 'system' → no class, CSS @media takes over

    document.querySelectorAll<HTMLButtonElement>('.theme-btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.theme === theme);
    });
}

// ─── Chat ID helpers ─────────────────────────────────────────────────────────
function parseIds(raw: string): string[] {
    return raw.split('\n')
        .map(s => s.trim())
        .filter(s => s.length > 0);
}

function idsToText(ids: string[] | null | undefined): string {
    if (!ids || ids.length === 0) return '';
    return ids.join('\n');
}

// ─── Logger ─────────────────────────────────────────────────────────────────
let logOutput: HTMLDivElement;

function appendLog(msg: string, type: 'sys' | 'msg' | 'err' = 'msg') {
    const line = document.createElement('div');
    line.className = `log-line ${type}`;
    const now = new Date();
    const ts = `${now.getHours().toString().padStart(2,'0')}:${now.getMinutes().toString().padStart(2,'0')}:${now.getSeconds().toString().padStart(2,'0')}`;
    const timeSpan = document.createElement('span');
    timeSpan.style.color = 'var(--text-dim)';
    timeSpan.textContent = `[${ts}] `;
    line.appendChild(timeSpan);
    line.appendChild(document.createTextNode(msg));
    logOutput.appendChild(line);
    logOutput.scrollTop = logOutput.scrollHeight;
}

// ─── Main ────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', async () => {
    logOutput = document.getElementById('log-output') as HTMLDivElement;

    // Detect OS locale for first-run default
    const osLang = navigator.language.startsWith('zh') ? 'zh' : 'en';
    applyI18n(osLang);
    (document.getElementById('input-language') as HTMLSelectElement).value = osLang;

    const App = window.go.main.App;

    // ── Minimize to tray ───────────────────────────────────────────────────
    // Hides window completely (removed from taskbar); tray icon stays visible.
    document.getElementById('btn-minimize')?.addEventListener('click', () => {
        App.HideToTray();
    });

    // ── View references ──────────────────────────────────────────────────────
    const viewSetup     = document.getElementById('view-setup')     as HTMLDivElement;
    const viewDashboard = document.getElementById('view-dashboard') as HTMLDivElement;
    const viewSettings  = document.getElementById('view-settings')  as HTMLDivElement;
    const sidebar       = document.getElementById('sidebar')        as HTMLElement;

    const dot         = document.getElementById('connection-dot')  as HTMLSpanElement;
    const connText    = document.getElementById('connection-text') as HTMLSpanElement;
    const lblState    = document.getElementById('lbl-bot-state')   as HTMLSpanElement;
    const lblWorkspace= document.getElementById('lbl-workspace-path') as HTMLSpanElement;
    const btnToggle   = document.getElementById('btn-toggle-bot')  as HTMLButtonElement;

    // ── Navigation ───────────────────────────────────────────────────────────
    const navLinks = document.querySelectorAll<HTMLElement>('.nav-links li');
    navLinks.forEach(link => {
        link.addEventListener('click', () => {
            const target = link.dataset.target;
            navLinks.forEach(l => l.classList.remove('active'));
            link.classList.add('active');
            viewDashboard.style.display = target === 'dashboard' ? 'block' : 'none';
            viewSettings.style.display  = target === 'settings'  ? 'block' : 'none';
        });
    });

    // ── Theme switcher ────────────────────────────────────────────────────────
    document.querySelectorAll<HTMLButtonElement>('.theme-btn').forEach(btn => {
        btn.addEventListener('click', async () => {
            const theme = btn.dataset.theme as Theme;
            applyTheme(theme);
            // Persist theme in config
            try {
                const cfg = await App.GetConfig();
                cfg.theme = theme;
                await App.SaveConfig(cfg);
            } catch { /* non-blocking, ignore */ }
        });
    });

    // ── Live language switching (setup wizard) ────────────────────────────────
    const inputLang = document.getElementById('input-language') as HTMLSelectElement;
    inputLang.addEventListener('change', () => {
        applyI18n(inputLang.value);
        inputLang.value = inputLang.value; // re-assert after DOM update
    });

    // ── Live language switching (settings page) ───────────────────────────────
    const updateLang = document.getElementById('update-language') as HTMLSelectElement;
    updateLang.addEventListener('change', () => {
        applyI18n(updateLang.value);
        updateLang.value = updateLang.value;
    });

    // ── Clear log button ─────────────────────────────────────────────────────
    document.getElementById('btn-clear-log')?.addEventListener('click', () => {
        logOutput.innerHTML = '';
        appendLog('Logs cleared.', 'sys');
    });

    // ── Setup Wizard form submit ──────────────────────────────────────────────
    const setupForm = document.getElementById('setup-form') as HTMLFormElement;
    setupForm.addEventListener('submit', async (e) => {
        e.preventDefault();

        const cfg: AppConfig = {
            language:          inputLang.value,
            telegram_token:    (document.getElementById('input-token')     as HTMLInputElement).value,
            workspace_base_dir:(document.getElementById('input-workspace') as HTMLInputElement).value,
            tts_voice:         (document.getElementById('input-voice')     as HTMLSelectElement).value,
            allowed_chat_ids:  parseIds((document.getElementById('input-chatids') as HTMLTextAreaElement).value),
            theme:             (document.querySelector<HTMLButtonElement>('.theme-btn.active')?.dataset.theme ?? 'dark'),
        };

        try {
            await App.SaveConfig(cfg);
            applyI18n(cfg.language);
            viewSetup.style.display     = 'none';
            sidebar.style.display       = 'flex';
            viewDashboard.style.display = 'block';
            lblWorkspace.innerText      = cfg.workspace_base_dir;
            (document.getElementById('nav-dashboard') as HTMLElement).classList.add('active');
            (document.getElementById('nav-settings')  as HTMLElement).classList.remove('active');
            appendLog('Configuration saved. Starting bot...', 'sys');
            await toggleBot();
        } catch (err: any) {
            alert(`Error saving config: ${err}`);
        }
    });

    // ── Bot toggle ────────────────────────────────────────────────────────────
    let isRunning = false;

    async function updateStatusUI() {
        isRunning = await App.IsBotRunning();
        if (isRunning) {
            btnToggle.classList.add('on');
            dot.className         = 'dot online';
            connText.innerText    = 'Connected';
            lblState.innerText    = 'Running';
            lblState.style.color  = 'var(--success)';
        } else {
            btnToggle.classList.remove('on');
            dot.className         = 'dot offline';
            connText.innerText    = 'Disconnected';
            lblState.innerText    = 'Idle';
            lblState.style.color  = 'var(--text-dim)';
        }
    }

    async function toggleBot() {
        if (isRunning) {
            await App.StopBot();
            appendLog('Bot stopped.', 'sys');
        } else {
            try {
                btnToggle.style.pointerEvents = 'none';
                appendLog('Connecting to Telegram...', 'sys');
                await App.StartBot();
                appendLog('Connection established.', 'sys');
            } catch (err: any) {
                appendLog(`Connection failed: ${err}`, 'err');
                alert(`Error starting bot:\n${err}`);
            } finally {
                btnToggle.style.pointerEvents = 'auto';
            }
        }
        await updateStatusUI();
    }

    btnToggle.addEventListener('click', toggleBot);

    // ── Settings form submit ──────────────────────────────────────────────────
    const updateSettingsForm = document.getElementById('update-settings-form') as HTMLFormElement;
    updateSettingsForm.addEventListener('submit', async (e) => {
        e.preventDefault();

        const cfg = await App.GetConfig();
        cfg.language          = updateLang.value;
        cfg.telegram_token    = (document.getElementById('update-token')    as HTMLInputElement).value;
        cfg.workspace_base_dir= (document.getElementById('update-workspace') as HTMLInputElement).value;
        cfg.tts_voice         = (document.getElementById('update-voice')    as HTMLSelectElement).value;
        cfg.allowed_chat_ids  = parseIds((document.getElementById('update-chatids') as HTMLTextAreaElement).value);

        try {
            await App.SaveConfig(cfg);
            applyI18n(cfg.language);
            lblWorkspace.innerText = cfg.workspace_base_dir;
            appendLog('Settings updated.', 'sys');
            if (isRunning) {
                appendLog('Notice: Toggle bot off/on to apply new token or voice.', 'sys');
            }
        } catch (err: any) {
            alert(`Error updating settings: ${err}`);
        }
    });

    // ── Clear Config button ───────────────────────────────────────────────────
    document.getElementById('btn-clear-config')?.addEventListener('click', async () => {
        const confirmMsg = currentLang === 'zh'
            ? '确定要清空所有配置吗？此操作不可撤销。'
            : 'Clear all configuration? This cannot be undone.';
        if (!confirm(confirmMsg)) return;

        try {
            await App.ClearConfig();
            // Reset to setup wizard
            sidebar.style.display       = 'none';
            viewDashboard.style.display = 'none';
            viewSettings.style.display  = 'none';
            viewSetup.style.display     = 'block';
            appendLog('Configuration cleared.', 'sys');
        } catch (err: any) {
            alert(`Error clearing config: ${err}`);
        }
    });

    // ── Initial state ─────────────────────────────────────────────────────────
    try {
        const hasConfig = await App.HasConfig();

        if (!hasConfig) {
            viewSetup.style.display = 'block';
            applyI18n(osLang);
        } else {
            const cfg = await App.GetConfig();

            // Apply saved language
            const lang = cfg.language || osLang;
            applyI18n(lang);
            (inputLang).value   = lang;
            (updateLang).value  = lang;

            // Apply saved theme
            const theme = (cfg.theme as Theme) || 'dark';
            applyTheme(theme);

            // Populate settings fields
            (document.getElementById('update-token')     as HTMLInputElement).value    = cfg.telegram_token || '';
            (document.getElementById('update-workspace') as HTMLInputElement).value    = cfg.workspace_base_dir || '';
            (document.getElementById('update-voice')     as HTMLSelectElement).value   = cfg.tts_voice || '';
            (document.getElementById('update-chatids')   as HTMLTextAreaElement).value = idsToText(cfg.allowed_chat_ids);

            lblWorkspace.innerText = cfg.workspace_base_dir || '';

            viewSetup.style.display     = 'none';
            sidebar.style.display       = 'flex';
            viewDashboard.style.display = 'block';

            await updateStatusUI();
        }
    } catch (e) {
        console.error('Failed to check config:', e);
        viewSetup.style.display = 'block';
    }

    // ── Wails event listeners ─────────────────────────────────────────────────
    EventsOn('bot-log', (msg: string) => {
        appendLog(msg);
    });

    EventsOn('bot-error', (msg: string) => {
        appendLog(msg, 'err');
    });

    EventsOn('bot-status-changed', () => {
        updateStatusUI();
    });
});
