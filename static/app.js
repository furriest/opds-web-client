'use strict'

// ── State ─────────────────────────────────────────────────────────────────────
let cfg = null          // {url, username, hasPassword}
let navStack = []       // [{title, url}]
let searchUrl = null    // OpenSearch URL template with {searchTerms}

// ── DOM refs ──────────────────────────────────────────────────────────────────
const $ = id => document.getElementById(id)

const dom = {
  content:       $('content'),
  loading:       $('loading'),
  errorBar:      $('error-bar'),
  pagination:    $('pagination'),
  breadcrumb:    $('breadcrumb'),
  btnHome:       $('btn-home'),
  btnTheme:      $('btn-theme'),
  iconSun:       $('icon-sun'),
  iconMoon:      $('icon-moon'),
  btnSettings:   $('btn-settings'),
  searchWrap:    $('search-wrap'),
  searchInput:   $('search-input'),
  btnSearch:     $('btn-search'),
  overlay:       $('modal-overlay'),
  setupForm:     $('setup-form'),
  fieldUrl:      $('field-url'),
  fieldUser:     $('field-username'),
  fieldPw:       $('field-password'),
  btnCancelSetup:$('btn-cancel-setup'),
  btnConnect:    $('btn-connect'),
  btnConnLabel:  $('btn-connect-label'),
  btnConnSpinner:$('btn-connect-spinner'),
  btnTogglePw:   $('btn-toggle-pw'),
  modalError:    $('modal-error'),
  tooltip:       $('tooltip'),
}

// ── Init ──────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', async () => {
  loadTheme()
  bindEvents()
  await loadConfig()
  if (!cfg) {
    showSetup(false)
    return
  }
  const feedUrl = browserPathToOpdsUrl() || cfg.url
  await loadFeed(feedUrl, false)
})

window.addEventListener('popstate', e => {
  const url = e.state?.url || browserPathToOpdsUrl()
  if (url) loadFeed(url, false)
})

// ── Config ────────────────────────────────────────────────────────────────────
async function loadConfig() {
  try {
    const r = await fetch('/api/config')
    if (r.status === 204) { cfg = null; return }
    cfg = await r.json()
  } catch {
    cfg = null
  }
}

async function saveConfig(url, username, password) {
  const r = await fetch('/api/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, username, password }),
  })
  if (!r.ok) {
    let msg = 'Failed to save configuration'
    try { const d = await r.json(); msg = d.error || msg } catch {}
    throw new Error(msg)
  }
}

// ── Feed loading ──────────────────────────────────────────────────────────────
async function loadFeed(url, push = true) {
  setLoading(true)
  hideError()

  try {
    const r = await fetch(`/api/feed?url=${encodeURIComponent(url)}`)
    if (r.status === 401) { showSetup(!!cfg); return }
    if (!r.ok) {
      let msg = `Server error (${r.status})`
      try { const d = await r.json(); msg = d.error || msg } catch {}
      throw new Error(msg)
    }

    const data = await r.json()
    searchUrl = data.searchUrl || null

    // Update nav stack
    if (push) {
      history.pushState({ url }, data.title || url, opdsUrlToBrowserPath(url))
      const prev = navStack[navStack.length - 1]
      if (!prev || prev.url !== url) {
        navStack.push({ title: data.title || url, url })
      }
    } else {
      // On popstate / direct load: rebuild stack conservatively
      const idx = navStack.findIndex(n => n.url === url)
      if (idx !== -1) {
        navStack = navStack.slice(0, idx + 1)
      } else {
        // Fresh load
        navStack = [{ title: data.title || url, url }]
      }
    }

    renderFeed(data)
    renderBreadcrumb()
    renderSearch()
  } catch (e) {
    showError(e.message)
  } finally {
    setLoading(false)
  }
}

// ── Rendering ─────────────────────────────────────────────────────────────────
function renderFeed(data) {
  dom.content.innerHTML = ''
  dom.pagination.innerHTML = ''

  const entries = data.entries || []
  if (!entries.length) {
    dom.content.innerHTML = '<p class="empty">No entries found.</p>'
    return
  }

  const navEntries  = entries.filter(e => e.kind === 'nav')
  const bookEntries = entries.filter(e => e.kind === 'book')

  if (navEntries.length) {
    const list = document.createElement('div')
    list.className = 'nav-list'
    navEntries.forEach(e => list.appendChild(mkNavEntry(e)))
    dom.content.appendChild(list)
  }

  if (bookEntries.length) {
    const grid = document.createElement('div')
    grid.className = 'book-grid'
    bookEntries.forEach(e => grid.appendChild(mkBookCard(e)))
    dom.content.appendChild(grid)
  }

  renderPagination(data.links || {})
}

function mkNavEntry(entry) {
  const el = document.createElement('div')
  el.className = 'nav-entry'
  el.setAttribute('role', 'button')
  el.setAttribute('tabindex', '0')
  el.innerHTML = `
    <div class="nav-icon">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/>
        <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/>
      </svg>
    </div>
    <div class="nav-info">
      <div class="nav-title">${esc(entry.title)}</div>
      ${entry.summary ? `<div class="nav-desc">${esc(truncate(entry.summary, 80))}</div>` : ''}
    </div>
    <svg class="nav-arrow" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
      <path d="M9 18l6-6-6-6"/>
    </svg>`

  const go = () => entry.navUrl && loadFeed(entry.navUrl)
  el.addEventListener('click', go)
  el.addEventListener('keydown', e => { if (e.key === 'Enter' || e.key === ' ') go() })
  return el
}

function mkBookCard(entry) {
  const card = document.createElement('div')
  card.className = 'book-card'

  const authors = (entry.authors || []).join(', ')
  const coverSrc = entry.thumbUrl || entry.coverUrl

  const formats = (entry.files || []).map(f => {
    const dlUrl = opdsUrlToDownloadPath(f.url)
    const filename = sanitizeFilename(entry.title) + '.' + f.format.toLowerCase()
    return `<a class="fmt-btn" href="${esc(dlUrl)}" download="${esc(filename)}" title="Download ${f.format}">${f.format}</a>`
  }).join('')

  const epubFile = (entry.files || []).find(f => f.format === 'EPUB')
  const readLink = epubFile ? opdsUrlToReadPath(epubFile.url) : null
  const readBtn  = readLink ? `<a class="fmt-btn fmt-read" href="${esc(readLink)}" title="Read online">online</a>` : ''

  card.innerHTML = `
    ${coverSrc ? `<div class="cover-wrap"></div>` : ''}
    <div class="book-info">
      <div class="book-title" title="${esc(entry.title)}">${esc(entry.title)}</div>
      ${authors ? `<div class="book-authors">${esc(authors)}</div>` : ''}
      ${entry.summary ? `<div class="book-desc">${esc(entry.summary)}</div>` : ''}
      ${(formats || readBtn) ? `<div class="book-formats">${formats}${readBtn}</div>` : ''}
    </div>`

  if (coverSrc) {
    const wrap = card.querySelector('.cover-wrap')
    const img = document.createElement('img')
    img.alt = ''
    img.loading = 'lazy'
    img.onload = () => img.classList.add('loaded')
    img.onerror = () => wrap.remove()
    img.src = `/api/proxy?url=${encodeURIComponent(coverSrc)}`
    wrap.appendChild(img)
  }

  if (entry.summary) {
    const desc = card.querySelector('.book-desc')
    if (desc) {
      desc.addEventListener('mouseenter', () => showTooltip(entry.summary))
      desc.addEventListener('mouseleave', hideTooltip)
    }
  }

  return card
}

function renderBreadcrumb() {
  dom.breadcrumb.innerHTML = ''
  const max = 3
  const start = Math.max(0, navStack.length - max)
  const visible = navStack.slice(start)

  visible.forEach((crumb, i) => {
    const isLast = i === visible.length - 1

    if (i > 0) {
      const sep = document.createElement('span')
      sep.className = 'crumb-sep'
      sep.textContent = '/'
      dom.breadcrumb.appendChild(sep)
    }

    const el = document.createElement('span')
    el.className = 'crumb' + (isLast ? ' current' : '')
    el.textContent = crumb.title || crumb.url
    el.title = crumb.title || crumb.url
    if (!isLast) {
      el.addEventListener('click', () => {
        navStack = navStack.slice(0, start + i + 1)
        loadFeed(crumb.url)
      })
    }
    dom.breadcrumb.appendChild(el)
  })
}

function renderSearch() {
  if (searchUrl) {
    dom.searchWrap.classList.remove('hidden')
  } else {
    dom.searchWrap.classList.add('hidden')
  }
}

function renderPagination(links) {
  dom.pagination.innerHTML = ''
  const items = []

  if (links.prev) {
    items.push({ label: 'Previous', url: links.prev, icon: `<path d="M15 18l-6-6 6-6"/>` })
  }
  if (links.next) {
    items.push({ label: 'Next', url: links.next, icon: `<path d="M9 18l6-6-6-6"/>`, right: true })
  }

  items.forEach(item => {
    const btn = document.createElement('button')
    btn.className = 'page-btn'
    const svg = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">${item.icon}</svg>`
    btn.innerHTML = item.right ? `${item.label}${svg}` : `${svg}${item.label}`
    btn.addEventListener('click', () => loadFeed(item.url))
    dom.pagination.appendChild(btn)
  })
}

// ── Setup modal ───────────────────────────────────────────────────────────────
function showSetup(canCancel = true) {
  if (cfg) {
    dom.fieldUrl.value = cfg.url || ''
    dom.fieldUser.value = cfg.username || ''
    dom.fieldPw.placeholder = cfg.hasPassword ? '•••••••• (unchanged)' : 'leave blank if not required'
    dom.fieldPw.value = ''
  } else {
    dom.fieldUrl.value = ''
    dom.fieldUser.value = ''
    dom.fieldPw.value = ''
    dom.fieldPw.placeholder = 'leave blank if not required'
  }

  dom.btnCancelSetup.classList.toggle('hidden', !canCancel)
  dom.overlay.classList.remove('hidden')
  hideModalError()
  setTimeout(() => dom.fieldUrl.focus(), 50)
}

function hideSetup() {
  dom.overlay.classList.add('hidden')
}

function showModalError(msg) {
  dom.modalError.textContent = msg
  dom.modalError.classList.remove('hidden')
}

function hideModalError() {
  dom.modalError.classList.add('hidden')
}

// ── Events ────────────────────────────────────────────────────────────────────
function bindEvents() {
  dom.btnHome.addEventListener('click', () => {
    if (!cfg) return
    navStack = []
    loadFeed(cfg.url)
  })

  dom.btnSettings.addEventListener('click', () => showSetup(true))
  dom.btnCancelSetup.addEventListener('click', hideSetup)

  dom.overlay.addEventListener('click', e => {
    if (e.target === dom.overlay && !dom.btnCancelSetup.classList.contains('hidden')) hideSetup()
  })

  dom.setupForm.addEventListener('submit', async e => {
    e.preventDefault()
    hideModalError()

    const url = dom.fieldUrl.value.trim().replace(/\/$/, '')
    const username = dom.fieldUser.value.trim()
    let password = dom.fieldPw.value

    if (!url) { showModalError('Server URL is required.'); dom.fieldUrl.focus(); return }
    if (!url.startsWith('http://') && !url.startsWith('https://')) {
      showModalError('URL must start with http:// or https://'); dom.fieldUrl.focus(); return
    }

    // Keep existing password if field is blank and config already exists
    if (!password && cfg?.hasPassword) {
      // Re-fetch old cfg to get actual password — we can't, it's httpOnly.
      // Instead send sentinel "" which the server interprets as "keep existing".
      // Actually we must POST again. If field is blank and hasPassword, we
      // omit the password field so the server keeps the old cookie value.
      // Simple approach: if blank and had password, send old cookie as-is by
      // just not including password in the POST (server always replaces cookie).
      // We'll handle by sending empty string and relying on UI note.
    }

    setConnecting(true)
    try {
      await saveConfig(url, username, password)
      await loadConfig()
      hideSetup()
      navStack = []
      await loadFeed(cfg.url, false)
    } catch (e) {
      showModalError(e.message)
    } finally {
      setConnecting(false)
    }
  })

  dom.btnTogglePw.addEventListener('click', () => {
    const isPw = dom.fieldPw.type === 'password'
    dom.fieldPw.type = isPw ? 'text' : 'password'
  })

  dom.btnTheme.addEventListener('click', toggleTheme)

  // Search
  dom.btnSearch.addEventListener('click', doSearch)
  dom.searchInput.addEventListener('keydown', e => { if (e.key === 'Enter') doSearch() })

  // Tooltip positioning
  document.addEventListener('mousemove', e => {
    if (dom.tooltip.classList.contains('hidden')) return
    let x = e.clientX + 16
    let y = e.clientY + 10
    if (x + 340 > window.innerWidth) x = e.clientX - 340
    if (x < 8) x = 8
    dom.tooltip.style.left = x + 'px'
    dom.tooltip.style.top  = y + 'px'
  })
}

function showTooltip(text) {
  if (!text) return
  dom.tooltip.textContent = text
  dom.tooltip.classList.remove('hidden')
}

function hideTooltip() {
  dom.tooltip.classList.add('hidden')
}

function setConnecting(on) {
  dom.btnConnect.disabled = on
  dom.btnConnLabel.textContent = on ? 'Connecting…' : 'Connect'
  dom.btnConnSpinner.classList.toggle('hidden', !on)
}

async function doSearch() {
  const q = dom.searchInput.value.trim()
  if (!q || !searchUrl) return
  const url = searchUrl.replace('{searchTerms}', encodeURIComponent(q))
  await loadFeed(url)
}

// ── Theme ─────────────────────────────────────────────────────────────────────
function loadTheme() {
  const saved = localStorage.getItem('theme') || 'auto'
  applyTheme(saved)
}

function toggleTheme() {
  const current = document.documentElement.dataset.theme || 'auto'
  const next = current === 'dark' ? 'light' : 'dark'
  localStorage.setItem('theme', next)
  applyTheme(next)
}

function applyTheme(theme) {
  document.documentElement.dataset.theme = theme
  const isDark = theme === 'dark' ||
    (theme === 'auto' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  dom.iconSun.classList.toggle('hidden', isDark)
  dom.iconMoon.classList.toggle('hidden', !isDark)
}

window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
  if ((localStorage.getItem('theme') || 'auto') === 'auto') applyTheme('auto')
})

// ── UI helpers ─────────────────────────────────────────────────────────────────
function setLoading(on) {
  dom.loading.classList.toggle('hidden', !on)
  if (on) {
    dom.content.innerHTML = ''
    dom.pagination.innerHTML = ''
  }
}

function showError(msg) {
  dom.errorBar.textContent = msg
  dom.errorBar.classList.remove('hidden')
}

function hideError() {
  dom.errorBar.classList.add('hidden')
}

// ── Utils ─────────────────────────────────────────────────────────────────────
function esc(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function truncate(str, len) {
  return str.length > len ? str.slice(0, len) + '…' : str
}

function sanitizeFilename(title) {
  return (title || 'download')
    .replace(/[<>:"/\\|?*\x00-\x1f]/g, '_')
    .replace(/\s+/g, '_')
    .slice(0, 80)
}

// ── URL scheme: /browse/<path>, /dl/<path>, /read/<path> ─────────────────────

function opdsUrlToLocalPath(prefix, opdsUrl) {
  try {
    const base     = new URL(cfg?.url || '')
    const target   = new URL(opdsUrl)
    const basePath = base.pathname.replace(/\/$/, '')
    let rel = target.pathname
    if (basePath && rel.startsWith(basePath)) rel = rel.slice(basePath.length)
    return prefix + (rel || '') + target.search
  } catch {
    return null
  }
}

function opdsUrlToBrowserPath(opdsUrl) {
  return opdsUrlToLocalPath('/browse', opdsUrl) ?? '/browse'
}

function opdsUrlToDownloadPath(opdsUrl) {
  return opdsUrlToLocalPath('/dl', opdsUrl) ?? ('/api/proxy?url=' + encodeURIComponent(opdsUrl))
}

function opdsUrlToReadPath(opdsUrl) {
  return opdsUrlToLocalPath('/read', opdsUrl)
}

// Reconstruct a full OPDS URL from the current browser location
function browserPathToOpdsUrl() {
  const path   = location.pathname
  const search = location.search
  if (!cfg?.url) return null

  if (path.startsWith('/browse')) {
    try {
      const base     = new URL(cfg.url)
      const basePath = base.pathname.replace(/\/$/, '')
      const rel      = path.slice('/browse'.length) || '/'
      return base.origin + basePath + (rel.startsWith('/') ? rel : '/' + rel) + search
    } catch {
      return cfg.url
    }
  }

  // Legacy ?url= support (bookmarks made before the change)
  const params = new URLSearchParams(search)
  return params.get('url') || cfg.url
}

// Generate a stable hsl color from a string
function hslFromStr(str) {
  let hash = 0
  for (let i = 0; i < str.length; i++) {
    hash = (Math.imul(31, hash) + str.charCodeAt(i)) | 0
  }
  const h = ((hash >>> 0) % 360)
  return `hsl(${h},55%,38%)`
}
