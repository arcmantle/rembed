import { html, render } from 'lit-html';
import { unsafeHTML } from 'lit-html/directives/unsafe-html.js';
import DOMPurify from 'dompurify';
import { marked } from 'marked';
import { createHighlighterCore } from 'shiki/core';
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript';
import langShellscript from '@shikijs/langs/shellscript';
import langPowerShell from '@shikijs/langs/powershell';
import langJavaScript from '@shikijs/langs/javascript';
import langTypeScript from '@shikijs/langs/typescript';
import langJSON from '@shikijs/langs/json';
import langYAML from '@shikijs/langs/yaml';
import langGo from '@shikijs/langs/go';
import langMarkdown from '@shikijs/langs/markdown';
import langHTML from '@shikijs/langs/html';
import langCSS from '@shikijs/langs/css';
import langDiff from '@shikijs/langs/diff';
import themeGitHubLight from '@shikijs/themes/github-light';
import themeGitHubDarkDimmed from '@shikijs/themes/github-dark-dimmed';
import './styles.css';

const STORAGE_KEY = 'rembed.theme';

const payload = globalThis.__REMBED_DATA__ || {
  title: 'Documentation',
  version: 'dev',
  sourcePath: 'unknown',
  generated: new Date().toISOString(),
  markdown: '# Missing payload\nNo docs payload was provided.'
};

const SHIKI_THEMES = {
  light: 'github-light',
  dark: 'github-dark-dimmed'
};

const SHIKI_LANGS = [
  'shellscript',
  'powershell',
  'javascript',
  'typescript',
  'json',
  'yaml',
  'go',
  'markdown',
  'html',
  'css',
  'diff',
  'text'
];

const LANG_ALIASES = {
  bash: 'shellscript',
  shell: 'shellscript',
  sh: 'shellscript',
  zsh: 'shellscript',
  ps1: 'powershell',
  pwsh: 'powershell',
  js: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  ts: 'typescript',
  yml: 'yaml',
  md: 'markdown',
  htm: 'html'
};

const SHIKI_LANG_MODULES = [
  langShellscript,
  langPowerShell,
  langJavaScript,
  langTypeScript,
  langJSON,
  langYAML,
  langGo,
  langMarkdown,
  langHTML,
  langCSS,
  langDiff
];

let highlighter = null;
let rendererReady = false;

const state = {
  theme: detectInitialTheme(),
  html: '<p>Loading documentation renderer...</p>'
};

const app = document.getElementById('app');

function detectInitialTheme() {
  const saved = globalThis.localStorage?.getItem(STORAGE_KEY);
  if (saved === 'light' || saved === 'dark') {
    return saved;
  }
  const prefersDark = globalThis.matchMedia?.('(prefers-color-scheme: dark)').matches;
  return prefersDark ? 'dark' : 'light';
}

function normalizeLang(lang) {
  const candidate = (lang || '').toLowerCase().trim().split(/\s+/)[0];
  if (!candidate) {
    return 'text';
  }
  return LANG_ALIASES[candidate] || candidate;
}

function isKnownLang(lang) {
  return SHIKI_LANGS.includes(lang);
}

function escapeHtml(raw) {
  return String(raw)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function renderCodeBlock(code, lang, theme) {
  const normalized = normalizeLang(lang);
  const resolvedLang = isKnownLang(normalized) ? normalized : 'text';

  if (!highlighter) {
    return `<pre data-lang="${resolvedLang}"><code>${escapeHtml(code)}</code></pre>`;
  }

  const highlighted = highlighter.codeToHtml(code, {
    lang: resolvedLang,
    theme: theme === 'dark' ? SHIKI_THEMES.dark : SHIKI_THEMES.light
  });

  return highlighted.replace('<pre class="shiki', `<pre data-lang="${resolvedLang}" class="shiki`);
}

function toSafeHtml(markdown, theme) {
  const renderer = new marked.Renderer();
  renderer.code = (token) => {
    const text = token?.text || '';
    const lang = token?.lang || 'text';
    return renderCodeBlock(text, lang, theme);
  };

  const rendered = marked.parse(markdown || '', {
    gfm: true,
    breaks: false,
    renderer
  });

  return DOMPurify.sanitize(rendered, {
    ADD_ATTR: ['class', 'style', 'data-lang', 'target', 'rel']
  });
}

function setTheme(theme) {
  state.theme = theme;
  document.documentElement.setAttribute('data-theme', theme);
  try {
    globalThis.localStorage?.setItem(STORAGE_KEY, theme);
  } catch (_) {
    // ignore storage errors in restricted environments
  }
}

function toggleTheme() {
  setTheme(state.theme === 'dark' ? 'light' : 'dark');
  if (rendererReady) {
    state.html = toSafeHtml(payload.markdown, state.theme);
  }
  update();
}

function annotateCodeBlocks() {
  const nodes = document.querySelectorAll('pre > code');
  for (const code of nodes) {
    const pre = code.parentElement;
    if (!pre) {
      continue;
    }
    const classes = (code.className || '').split(/\s+/);
    for (const cls of classes) {
      if (cls.startsWith('language-')) {
        const lang = cls.slice('language-'.length).trim();
        if (lang) {
          pre.dataset.lang = lang;
        }
      }
    }
  }
}

const page = () => html`
  <main class="wrap">
    <section class="hero">
      <div class="hero-head">
        <h1>${payload.title || 'Documentation'}</h1>
        <button class="theme-toggle" @click=${toggleTheme} type="button">
          ${state.theme === 'dark' ? 'Light mode' : 'Dark mode'}
        </button>
      </div>
      <p>
        Version: ${payload.version || 'dev'} | Generated: ${payload.generated || 'unknown'} | Source:
        ${payload.sourcePath || 'embedded payload'}
      </p>
    </section>
    <article class="doc markdown-body">${unsafeHTML(state.html)}</article>
  </main>
`;

function update() {
  render(page(), app);
  annotateCodeBlocks();
}

async function initRenderer() {
  try {
    highlighter = await createHighlighterCore({
      themes: [themeGitHubLight, themeGitHubDarkDimmed],
      langs: SHIKI_LANG_MODULES,
      engine: createJavaScriptRegexEngine()
    });
  } catch (err) {
    console.error('Failed to initialize Shiki highlighter, falling back to plain code blocks.', err);
  }

  rendererReady = true;
  state.html = toSafeHtml(payload.markdown, state.theme);
  update();
}

setTheme(state.theme);
update();
void initRenderer();
