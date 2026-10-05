/**
 * Zadroit Server Monitoring System - Client Controller
 * Native Firebase Realtime Database SDK (v10) + WebSockets & Real-time Watcher
 */

import { initializeApp } from "https://www.gstatic.com/firebasejs/10.9.0/firebase-app.js";
import { getDatabase, ref, onValue, onChildAdded, off } from "https://www.gstatic.com/firebasejs/10.9.0/firebase-database.js";

// User's Firebase Configuration
const firebaseConfig = {
  apiKey: "AIzaSyA2sTTwDtuWcWF9Xg2sPfrrYuDLbjJCMUc",
  authDomain: "server-monitor-8ffb0.firebaseapp.com",
  databaseURL: "https://server-monitor-8ffb0-default-rtdb.firebaseio.com",
  projectId: "server-monitor-8ffb0",
  storageBucket: "server-monitor-8ffb0.firebasestorage.app",
  messagingSenderId: "274069716120",
  appId: "1:274069716120:web:9585c80e368ac8b9e624ed",
  measurementId: "G-Q7R2VPN390"
};

// Application State
const state = {
  authToken: localStorage.getItem('zadroit_auth_token') || null,
  currentUser: localStorage.getItem('zadroit_username') || 'Zadroit',
  currentPage: 1,
  limit: 25,
  totalLogs: 0,
  targets: [],
  summary: null,
  cachedLogs: [],
  filters: {
    serviceName: 'all',
    type: 'all',
    status: 'all',
    fromDate: '',
    toDate: '',
    search: '',
  },
  firebaseApp: null,
  firebaseDb: null,
  firebaseLogsRef: null,
  firebaseStatus: {
    connected: false,
    permissionDenied: false,
    totalLogs: 0
  },
  nextCheckSeconds: 300, // 5 minutes (300s)
  searchTimer: null,
  countdownTimer: null,
  pollTimer: null,
};

// ============================================================================
// INITIALIZATION
// ============================================================================
document.addEventListener('DOMContentLoaded', () => {
  initApp();
});

function initApp() {
  initFirebaseSDK();

  if (state.authToken) {
    showDashboard();
  } else {
    showLogin();
  }
}

// Initialize Native Firebase JS SDK
function initFirebaseSDK() {
  try {
    state.firebaseApp = initializeApp(firebaseConfig);
    state.firebaseDb = getDatabase(state.firebaseApp);
    console.log('[FIREBASE SDK] Initialized successfully for project:', firebaseConfig.projectId);
  } catch (err) {
    console.warn('[FIREBASE SDK] Initialization note:', err);
  }
}

function showLogin() {
  document.getElementById('loginView').classList.remove('hidden');
  document.getElementById('dashboardView').classList.add('hidden');
  clearIntervals();
  detachFirebaseListeners();
}

function showDashboard() {
  document.getElementById('loginView').classList.add('hidden');
  document.getElementById('dashboardView').classList.remove('hidden');

  // Load initial data
  loadTargets();
  fetchStatus();
  fetchLogs();

  // Attach Realtime Firebase WebSockets & Listeners
  attachFirebaseRealtimeListeners();

  // Start 5-minute countdown and background poll
  startTimers();
}

function startTimers() {
  clearIntervals();

  // 1-second interval for countdown timer
  state.countdownTimer = setInterval(() => {
    if (state.nextCheckSeconds > 0) {
      state.nextCheckSeconds--;
    } else {
      state.nextCheckSeconds = (state.summary?.interval_minutes || 5) * 60;
      fetchStatus();
      if (!state.firebaseStatus.connected) {
        fetchLogs();
      }
    }
    updateTimerDisplay();
  }, 1000);

  // Periodic UI refresh every 15 seconds
  state.pollTimer = setInterval(() => {
    fetchStatus();
    if (!state.firebaseStatus.connected) {
      fetchLogs(false);
    }
  }, 15000);
}

function clearIntervals() {
  if (state.countdownTimer) clearInterval(state.countdownTimer);
  if (state.pollTimer) clearInterval(state.pollTimer);
}

function updateTimerDisplay() {
  const mins = Math.floor(state.nextCheckSeconds / 60);
  const secs = state.nextCheckSeconds % 60;
  const timerElem = document.getElementById('nextCheckTimer');
  if (timerElem) {
    timerElem.textContent = `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
  }
}

// ============================================================================
// FIREBASE REAL-TIME WEBSOCKET LISTENERS
// ============================================================================
function attachFirebaseRealtimeListeners() {
  if (!state.firebaseDb) {
    initFirebaseSDK();
  }

  const banner = document.getElementById('firebaseRealtimeBanner');
  const authWarning = document.getElementById('firebaseAuthWarning');
  const streamBadge = document.getElementById('firebaseStreamBadge');
  const metricFb = document.getElementById('metricFirebaseStatus');
  const endpointLabel = document.getElementById('firebaseEndpointLabel');

  if (endpointLabel) {
    endpointLabel.textContent = `Firebase Realtime DB: ${firebaseConfig.databaseURL}`;
  }

  try {
    state.firebaseLogsRef = ref(state.firebaseDb, 'server_monitoring_logs');

    // 1. Listen for full dataset and changes
    onValue(state.firebaseLogsRef, (snapshot) => {
      const data = snapshot.val();
      state.firebaseStatus.connected = true;
      state.firebaseStatus.permissionDenied = false;

      if (banner) banner.className = 'firebase-live-banner connected';
      if (authWarning) authWarning.classList.add('hidden');

      if (data) {
        const logsList = [];
        for (const [key, item] of Object.entries(data)) {
          if (item && typeof item === 'object') {
            if (!item.id) item.id = key;
            logsList.push(item);
          }
        }

        logsList.sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));
        state.cachedLogs = logsList;
        state.totalLogs = logsList.length;
        state.firebaseStatus.totalLogs = logsList.length;

        if (streamBadge) {
          streamBadge.className = 'stream-badge';
          streamBadge.innerHTML = `<i class="fa-solid fa-bolt"></i> Realtime Live (${logsList.length} logs)`;
        }
        if (metricFb) {
          metricFb.textContent = `Live Firebase (${logsList.length} logs)`;
        }

        renderLogsTable(logsList.slice(0, state.limit), logsList.length);
        renderPagination(logsList.length, 1, state.limit);
      } else {
        if (streamBadge) {
          streamBadge.className = 'stream-badge';
          streamBadge.innerHTML = `<i class="fa-solid fa-bolt"></i> Realtime Live (0 logs)`;
        }
      }
    }, (error) => {
      console.warn('[FIREBASE REALTIME ERROR]', error);
      if (error.code === 'PERMISSION_DENIED' || String(error).includes('Permission denied')) {
        state.firebaseStatus.permissionDenied = true;
        state.firebaseStatus.connected = false;
        if (banner) banner.className = 'firebase-live-banner warning';
        if (authWarning) authWarning.classList.remove('hidden');
        if (streamBadge) {
          streamBadge.className = 'stream-badge offline';
          streamBadge.innerHTML = `<i class="fa-solid fa-lock"></i> Rules Locked (401)`;
        }
        if (metricFb) {
          metricFb.textContent = `Permission Denied (401)`;
        }
      }
    });

  } catch (err) {
    console.error('Error attaching Firebase Realtime listeners:', err);
  }
}

function detachFirebaseListeners() {
  if (state.firebaseLogsRef) {
    try {
      off(state.firebaseLogsRef);
    } catch (e) {}
    state.firebaseLogsRef = null;
  }
}

// Force re-check and sync
window.checkFirebaseRealtime = function() {
  attachFirebaseRealtimeListeners();
  showToast('Re-connecting to Firebase Realtime Database...', 'info');
};

window.syncNowFromFirebase = async function() {
  const icon = document.getElementById('syncFbIcon');
  if (icon) icon.classList.add('fa-spin');

  try {
    const res = await fetch('/api/firebase-sync', { method: 'POST' });
    const data = await res.json();
    if (res.ok && data.success) {
      showToast(`Pulled logs from Firebase: ${data.message}`, 'success');
      fetchStatus();
      fetchLogs();
    } else {
      showToast(`Firebase note: ${data.error || 'Check database rules in console'}`, 'info');
    }
  } catch (err) {
    showToast(`Sync error: ${err.message}`, 'error');
  } finally {
    if (icon) icon.classList.remove('fa-spin');
    attachFirebaseRealtimeListeners();
  }
};

// ============================================================================
// AUTHENTICATION
// ============================================================================
window.handleLogin = async function(e) {
  e.preventDefault();
  const username = document.getElementById('usernameInput').value.trim();
  const password = document.getElementById('passwordInput').value;
  const errorBox = document.getElementById('loginError');
  const errorText = document.getElementById('loginErrorText');
  const loginBtn = document.getElementById('loginBtn');

  errorBox.classList.add('hidden');
  loginBtn.disabled = true;
  loginBtn.innerHTML = `<div class="spinner-inline"></div> Verifying...`;

  try {
    const res = await fetch('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });

    const data = await res.json();
    if (res.ok && data.success) {
      state.authToken = data.token;
      state.currentUser = data.username;
      localStorage.setItem('zadroit_auth_token', data.token);
      localStorage.setItem('zadroit_username', data.username);
      showToast('Welcome back, Zadroit!', 'success');
      showDashboard();
    } else {
      errorText.textContent = data.message || 'Invalid username or password';
      errorBox.classList.remove('hidden');
      shakeElement(document.querySelector('.auth-card'));
    }
  } catch (err) {
    errorText.textContent = 'Server connection error. Please verify backend is running.';
    errorBox.classList.remove('hidden');
  } finally {
    loginBtn.disabled = false;
    loginBtn.innerHTML = `<span>Sign In to Dashboard</span> <i class="fa-solid fa-arrow-right"></i>`;
  }
};

window.handleLogout = function() {
  state.authToken = null;
  localStorage.removeItem('zadroit_auth_token');
  localStorage.removeItem('zadroit_username');
  showToast('Logged out successfully', 'info');
  showLogin();
};

window.fillCredentials = function(user, pass) {
  document.getElementById('usernameInput').value = user;
  document.getElementById('passwordInput').value = pass;
  showToast('Credentials filled. Click Sign In.', 'info');
};

window.togglePasswordVisibility = function(inputId) {
  const input = document.getElementById(inputId);
  const icon = document.getElementById('passwordToggleIcon');
  if (input.type === 'password') {
    input.type = 'text';
    icon.classList.remove('fa-eye');
    icon.classList.add('fa-eye-slash');
  } else {
    input.type = 'password';
    icon.classList.remove('fa-eye-slash');
    icon.classList.add('fa-eye');
  }
};

// ============================================================================
// DATA FETCHING & RENDERING
// ============================================================================

// Fetch overview summary
async function fetchStatus() {
  try {
    const res = await fetch('/api/status');
    if (!res.ok) return;
    const summary = await res.json();
    state.summary = summary;
    renderSummaryMetrics(summary);
    renderServiceCards(summary);
  } catch (err) {
    console.error('Failed to fetch status summary:', err);
  }
}

function renderSummaryMetrics(summary) {
  document.getElementById('metricTotalTargets').textContent = summary.total_targets || 0;
  document.getElementById('metricOnlineTargets').textContent = summary.online_targets || 0;
  document.getElementById('metricOfflineTargets').textContent = summary.offline_targets || 0;
  document.getElementById('metricTotalChecks').textContent = summary.total_checks || 0;

  const uptime = (summary.uptime_percent || 100).toFixed(1);
  const uptimeBadge = document.getElementById('metricUptimePercent');
  uptimeBadge.textContent = `${uptime}% Uptime`;
  if (summary.uptime_percent < 95) {
    uptimeBadge.className = 'metric-badge text-red';
  } else {
    uptimeBadge.className = 'metric-badge badge-green';
  }

  // Global Header status pill
  const pill = document.getElementById('globalStatusPill');
  const pillText = document.getElementById('globalStatusText');
  if (summary.offline_targets > 0) {
    pill.className = 'system-status-pill degraded';
    pillText.textContent = `${summary.offline_targets} Incident(s)`;
  } else {
    pill.className = 'system-status-pill';
    pillText.textContent = 'All Systems Operational';
  }

  // Last check time
  if (summary.last_check_time && summary.last_check_time !== 'Never') {
    document.getElementById('lastCheckTimeText').textContent = `Last Checked: ${summary.last_check_time}`;
  }
}

// Render service cards in the top overview ribbon
function renderServiceCards(summary) {
  const container = document.getElementById('serviceCardsContainer');
  const statuses = summary.target_statuses || {};
  const targets = state.targets || [];

  if (targets.length === 0 && Object.keys(statuses).length === 0) {
    container.innerHTML = `<div class="card-skeleton">No active targets configured. Add a target to start monitoring.</div>`;
    return;
  }

  let html = '';
  targets.forEach((target) => {
    const st = statuses[target.id] || {
      status: false,
      hit_time: 'Awaiting first check...',
      message: 'Pending initial health probe',
      http_status: 0,
      response_time_ms: 0,
      data: {},
    };

    const isUp = st.status;
    const cardClass = isUp ? 'status-up' : 'status-down';
    const badgeClass = isUp ? 'up' : 'down';
    const badgeText = isUp ? '● UP (200)' : '● DOWN / ERROR';
    const typeClass = target.type === 'backend' ? 'backend' : 'frontend';

    // Sub-data tags
    let subDataHtml = '';
    if (st.data && typeof st.data === 'object') {
      for (const [k, v] of Object.entries(st.data)) {
        if (typeof v === 'boolean') {
          const passClass = v ? 'pass' : 'fail';
          const icon = v ? '✓' : '✗';
          subDataHtml += `<span class="data-chip ${passClass}">${k}: ${icon}</span>`;
        }
      }
    }

    html += `
      <div class="target-card ${cardClass}">
        <div class="target-card-header">
          <div class="target-title-col">
            <div class="target-name">${escapeHTML(target.name)}</div>
            <a href="${escapeHTML(target.url)}" target="_blank" rel="noopener noreferrer" class="target-url-link">
              <i class="fa-solid fa-arrow-up-right-from-square"></i> ${escapeHTML(target.url)}
            </a>
          </div>
          <span class="target-status-badge ${badgeClass}">${badgeText}</span>
        </div>

        <div class="target-card-details">
          <div class="detail-item">
            <span class="label">Type</span>
            <span class="type-badge ${typeClass}">${target.type}</span>
          </div>
          <div class="detail-item">
            <span class="label">Latency</span>
            <span class="val">${st.response_time_ms || 0}ms</span>
          </div>
          <div class="detail-item">
            <span class="label">HTTP Code</span>
            <span class="val">${st.http_status || (isUp ? 200 : 'ERR')}</span>
          </div>
        </div>

        ${subDataHtml ? `<div class="data-chips-container">${subDataHtml}</div>` : ''}

        <div class="target-card-footer">
          <span><i class="fa-regular fa-clock"></i> ${st.hit_time || 'Just now'}</span>
          <span>${escapeHTML(st.message || '')}</span>
        </div>
      </div>
    `;
  });

  container.innerHTML = html;
}

// Fetch logs with current filters and pagination
async function fetchLogs(showSpinner = true) {
  const tbody = document.getElementById('logsTableBody');
  const refreshIcon = document.getElementById('refreshTableIcon');

  if (refreshIcon) refreshIcon.classList.add('fa-spin');
  if (showSpinner && (!state.cachedLogs || state.cachedLogs.length === 0)) {
    tbody.innerHTML = `
      <tr>
        <td colspan="9" class="text-center table-loading">
          <div class="spinner-inline"></div>
          <span>Loading server monitoring logs...</span>
        </td>
      </tr>
    `;
  }

  const params = new URLSearchParams({
    page: state.currentPage,
    limit: state.limit,
    service_name: state.filters.serviceName,
    type: state.filters.type,
    status: state.filters.status,
    from_date: state.filters.fromDate,
    to_date: state.filters.toDate,
    search: state.filters.search,
  });

  try {
    const res = await fetch(`/api/logs?${params.toString()}`);
    if (!res.ok) throw new Error('Failed to retrieve logs');
    const data = await res.json();
    state.cachedLogs = data.logs || [];
    state.totalLogs = data.total || 0;
    renderLogsTable(data.logs, data.total);
    renderPagination(data.total, data.page, data.limit);
  } catch (err) {
    tbody.innerHTML = `
      <tr>
        <td colspan="9" class="text-center text-red" style="padding:30px;">
          <i class="fa-solid fa-triangle-exclamation"></i> Error loading logs: ${escapeHTML(err.message)}
        </td>
      </tr>
    `;
  } finally {
    if (refreshIcon) refreshIcon.classList.remove('fa-spin');
  }
}

// Render data table rows
function renderLogsTable(logs, total) {
  const tbody = document.getElementById('logsTableBody');
  document.getElementById('totalRecordsBadge').textContent = `${total} records`;

  if (!logs || logs.length === 0) {
    tbody.innerHTML = `
      <tr>
        <td colspan="9" class="text-center" style="padding:40px; color:var(--text-muted);">
          <i class="fa-solid fa-inbox" style="font-size:24px; margin-bottom:8px; display:block;"></i>
          No monitoring logs match the specified criteria.
        </td>
      </tr>
    `;
    return;
  }

  let html = '';
  logs.forEach((item, index) => {
    const isSuccess = Boolean(item.status);
    const statusBadge = isSuccess
      ? `<span class="status-badge success"><i class="fa-solid fa-circle-check"></i> Success</span>`
      : `<span class="status-badge failure"><i class="fa-solid fa-circle-xmark"></i> Failure</span>`;

    const typeBadge = item.type === 'backend'
      ? `<span class="type-badge backend"><i class="fa-solid fa-microchip"></i> backend</span>`
      : `<span class="type-badge frontend"><i class="fa-solid fa-globe"></i> frontend</span>`;

    // Render Data chips / preview
    let dataPreview = '';
    if (item.data && Object.keys(item.data).length > 0) {
      let chips = '';
      for (const [key, val] of Object.entries(item.data)) {
        if (typeof val === 'boolean') {
          const chipClass = val ? 'pass' : 'fail';
          const icon = val ? '✓' : '✗';
          chips += `<span class="data-chip ${chipClass}">${escapeHTML(key)}: ${icon}</span>`;
        } else if (typeof val === 'string' || typeof val === 'number') {
          chips += `<span class="data-chip">${escapeHTML(key)}: ${escapeHTML(String(val))}</span>`;
        }
      }
      dataPreview = `<div class="data-chips-container">${chips}</div>`;
    } else {
      dataPreview = `<span style="color:var(--text-muted); font-size:12px;">--</span>`;
    }

    html += `
      <tr>
        <td class="text-center" style="font-family:var(--font-mono); color:var(--text-muted); font-weight:600;">${item.s_no || index + 1}</td>
        <td><strong>${escapeHTML(item.service_name || item['service name'] || '')}</strong></td>
        <td>${typeBadge}</td>
        <td class="table-url-cell">
          <a href="${escapeHTML(item.url || '')}" target="_blank" rel="noopener noreferrer" title="${escapeHTML(item.url || '')}">
            <i class="fa-solid fa-arrow-up-right-from-square"></i> ${escapeHTML(item.url || '')}
          </a>
        </td>
        <td class="text-center">${statusBadge}</td>
        <td class="time-cell">${escapeHTML(item.hit_time || item['hit time'] || '')}</td>
        <td class="message-cell" title="${escapeHTML(item.message || '')}">
          ${escapeHTML(item.message || '')}
        </td>
        <td>${dataPreview}</td>
        <td class="text-center">
          <button class="btn btn-ghost btn-sm" onclick="inspectLog('${escapeHTML(item.id)}')" title="Inspect Full JSON & Error Detail">
            <i class="fa-solid fa-magnifying-glass-plus text-cyan"></i>
          </button>
        </td>
      </tr>
    `;
  });

  tbody.innerHTML = html;
}

// Render pagination
function renderPagination(total, page, limit) {
  const container = document.getElementById('paginationControls');
  const showingRange = document.getElementById('showingRangeText');
  const totalCount = document.getElementById('totalCountText');

  totalCount.textContent = total;
  const start = total === 0 ? 0 : (page - 1) * limit + 1;
  const end = Math.min(page * limit, total);
  showingRange.textContent = `${start}-${end}`;

  const totalPages = Math.ceil(total / limit) || 1;
  let html = '';

  html += `<button class="page-btn" ${page <= 1 ? 'disabled' : ''} onclick="goToPage(${page - 1})"><i class="fa-solid fa-chevron-left"></i></button>`;

  const maxButtons = 5;
  let startPage = Math.max(1, page - 2);
  let endPage = Math.min(totalPages, startPage + maxButtons - 1);
  if (endPage - startPage < maxButtons - 1) {
    startPage = Math.max(1, endPage - maxButtons + 1);
  }

  for (let p = startPage; p <= endPage; p++) {
    html += `<button class="page-btn ${p === page ? 'active' : ''}" onclick="goToPage(${p})">${p}</button>`;
  }

  html += `<button class="page-btn" ${page >= totalPages ? 'disabled' : ''} onclick="goToPage(${page + 1})"><i class="fa-solid fa-chevron-right"></i></button>`;

  container.innerHTML = html;
}

window.goToPage = function(page) {
  state.currentPage = page;
  fetchLogs();
};

window.onLimitChange = function() {
  state.limit = parseInt(document.getElementById('limitSelect').value, 10) || 25;
  state.currentPage = 1;
  fetchLogs();
};

// ============================================================================
// FILTERS
// ============================================================================
window.onFilterChange = function() {
  state.filters.serviceName = document.getElementById('filterService').value;
  state.filters.type = document.getElementById('filterType').value;
  state.filters.status = document.getElementById('filterStatus').value;
  state.filters.fromDate = document.getElementById('filterFromDate').value;
  state.filters.toDate = document.getElementById('filterToDate').value;
  state.currentPage = 1;
  fetchLogs();
};

window.setQuickDate = function(preset, btn) {
  document.querySelectorAll('.date-presets-group .btn-preset').forEach((b) => b.classList.remove('active'));
  btn.classList.add('active');

  const today = new Date().toISOString().split('T')[0];
  const fromElem = document.getElementById('filterFromDate');
  const toElem = document.getElementById('filterToDate');

  if (preset === 'all') {
    fromElem.value = '';
    toElem.value = '';
  } else if (preset === 'today') {
    fromElem.value = today;
    toElem.value = today;
  } else if (preset === '7days') {
    const d = new Date();
    d.setDate(d.getDate() - 7);
    fromElem.value = d.toISOString().split('T')[0];
    toElem.value = today;
  }

  window.onFilterChange();
};

window.debounceSearch = function() {
  clearTimeout(state.searchTimer);
  const val = document.getElementById('filterSearch').value.trim();
  const clearBtn = document.getElementById('clearSearchBtn');
  clearBtn.style.display = val.length > 0 ? 'block' : 'none';

  state.searchTimer = setTimeout(() => {
    state.filters.search = val;
    state.currentPage = 1;
    fetchLogs();
  }, 350);
};

window.clearSearch = function() {
  document.getElementById('filterSearch').value = '';
  document.getElementById('clearSearchBtn').style.display = 'none';
  state.filters.search = '';
  state.currentPage = 1;
  fetchLogs();
};

window.resetAllFilters = function() {
  document.getElementById('filterService').value = 'all';
  document.getElementById('filterType').value = 'all';
  document.getElementById('filterStatus').value = 'all';
  document.getElementById('filterFromDate').value = '';
  document.getElementById('filterToDate').value = '';
  document.getElementById('filterSearch').value = '';
  document.getElementById('clearSearchBtn').style.display = 'none';

  document.querySelectorAll('.date-presets-group .btn-preset').forEach((b) => b.classList.remove('active'));
  document.querySelector('.date-presets-group .btn-preset').classList.add('active');

  state.filters = {
    serviceName: 'all',
    type: 'all',
    status: 'all',
    fromDate: '',
    toDate: '',
    search: '',
  };
  state.currentPage = 1;
  fetchLogs();
  showToast('Filters reset to default', 'info');
};

window.exportCSV = function() {
  const params = new URLSearchParams({
    service_name: state.filters.serviceName,
    type: state.filters.type,
    status: state.filters.status,
    from_date: state.filters.fromDate,
    to_date: state.filters.toDate,
    search: state.filters.search,
  });
  window.location.href = `/api/export-logs?${params.toString()}`;
  showToast('Downloading CSV report...', 'success');
};

// ============================================================================
// ACTIONS: CHECK NOW & TEST EMAIL
// ============================================================================
window.triggerManualCheck = async function() {
  const btn = document.getElementById('btnCheckNow');
  const icon = document.getElementById('checkNowIcon');

  btn.disabled = true;
  icon.classList.add('fa-spin');

  try {
    const res = await fetch('/api/check-now', { method: 'POST' });
    const data = await res.json();
    if (res.ok && data.success) {
      state.nextCheckSeconds = (state.summary?.interval_minutes || 5) * 60;
      showToast('Instant health check completed!', 'success');
      fetchStatus();
      fetchLogs();
    } else {
      showToast('Check failed: ' + (data.error || 'Unknown error'), 'error');
    }
  } catch (err) {
    showToast('Failed to trigger health check: ' + err.message, 'error');
  } finally {
    btn.disabled = false;
    icon.classList.remove('fa-spin');
  }
};

window.triggerTestEmail = async function() {
  const btn = document.getElementById('btnTestEmail');
  const statusElem = document.getElementById('testEmailStatus');
  btn.disabled = true;
  statusElem.innerHTML = `<span class="text-cyan"><div class="spinner-inline"></div> Sending test alert...</span>`;

  try {
    const res = await fetch('/api/test-email', { method: 'POST' });
    const data = await res.json();
    if (res.ok && data.success) {
      statusElem.innerHTML = `<span class="text-green"><i class="fa-solid fa-check"></i> ${escapeHTML(data.message)}</span>`;
      showToast('Test alert email sent successfully!', 'success');
    } else {
      statusElem.innerHTML = `<span class="text-red"><i class="fa-solid fa-xmark"></i> ${escapeHTML(data.error || 'Failed')}</span>`;
      showToast('Failed sending email: ' + (data.error || ''), 'error');
    }
  } catch (err) {
    statusElem.innerHTML = `<span class="text-red">Error: ${escapeHTML(err.message)}</span>`;
  } finally {
    btn.disabled = false;
  }
};

// ============================================================================
// TARGETS MANAGEMENT
// ============================================================================
async function loadTargets() {
  try {
    const res = await fetch('/api/targets');
    if (!res.ok) return;
    const targets = await res.json();
    state.targets = targets;

    // Update filter dropdown
    const select = document.getElementById('filterService');
    let options = '<option value="all">All Services</option>';
    targets.forEach((t) => {
      options += `<option value="${escapeHTML(t.name)}">${escapeHTML(t.name)}</option>`;
    });
    select.innerHTML = options;

    // Update badge count
    document.getElementById('targetCountBadge').textContent = targets.length;
    renderTargetsManagerList(targets);
  } catch (err) {
    console.error('Failed to load targets:', err);
  }
}

function renderTargetsManagerList(targets) {
  const container = document.getElementById('targetsConfigList');
  if (!container) return;

  if (targets.length === 0) {
    container.innerHTML = `<div class="card-skeleton">No targets configured.</div>`;
    return;
  }

  let html = '';
  targets.forEach((t) => {
    html += `
      <div class="target-config-item">
        <div class="target-config-info">
          <div class="target-config-name">${escapeHTML(t.name)}</div>
          <span class="type-badge ${t.type === 'backend' ? 'backend' : 'frontend'}">${t.type}</span>
          <span class="target-config-url">${escapeHTML(t.url)}</span>
        </div>
        <div class="target-config-actions">
          <button type="button" class="btn btn-ghost btn-sm" onclick="editTarget('${t.id}')" title="Edit">
            <i class="fa-solid fa-pen-to-square"></i>
          </button>
          <button type="button" class="btn btn-ghost btn-sm text-red" onclick="deleteTarget('${t.id}')" title="Delete">
            <i class="fa-solid fa-trash"></i>
          </button>
        </div>
      </div>
    `;
  });
  container.innerHTML = html;
}

window.editTarget = function(id) {
  const target = state.targets.find((t) => t.id === id);
  if (!target) return;

  document.getElementById('targetFormId').value = target.id;
  document.getElementById('targetFormName').value = target.name;
  document.getElementById('targetFormType').value = target.type;
  document.getElementById('targetFormMethod').value = target.method || 'GET';
  document.getElementById('targetFormURL').value = target.url;
  document.getElementById('targetFormKeys').value = (target.expected_keys || []).join(', ');
  document.getElementById('targetFormEnabled').checked = target.enabled;
};

window.resetTargetForm = function() {
  document.getElementById('targetFormId').value = '';
  document.getElementById('targetFormName').value = '';
  document.getElementById('targetFormType').value = 'backend';
  document.getElementById('targetFormMethod').value = 'GET';
  document.getElementById('targetFormURL').value = '';
  document.getElementById('targetFormKeys').value = '';
  document.getElementById('targetFormEnabled').checked = true;
};

window.handleSaveTarget = async function(e) {
  e.preventDefault();
  const id = document.getElementById('targetFormId').value.trim();
  const name = document.getElementById('targetFormName').value.trim();
  const type = document.getElementById('targetFormType').value;
  const method = document.getElementById('targetFormMethod').value;
  const url = document.getElementById('targetFormURL').value.trim();
  const keysStr = document.getElementById('targetFormKeys').value.trim();
  const enabled = document.getElementById('targetFormEnabled').checked;

  const expected_keys = keysStr ? keysStr.split(',').map((k) => k.trim()).filter((k) => k) : [];

  const targetData = {
    id: id || `target_${Date.now()}`,
    name,
    type,
    method,
    url,
    expected_keys,
    enabled,
  };

  const isEdit = Boolean(id);
  const endpoint = isEdit ? `/api/targets/${id}` : '/api/targets';
  const httpMethod = isEdit ? 'PUT' : 'POST';

  try {
    const res = await fetch(endpoint, {
      method: httpMethod,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(targetData),
    });

    if (res.ok) {
      showToast(`Target "${name}" ${isEdit ? 'updated' : 'added'} successfully!`, 'success');
      window.resetTargetForm();
      loadTargets();
      fetchStatus();
    } else {
      const err = await res.json();
      showToast('Failed to save target: ' + (err.error || 'Unknown'), 'error');
    }
  } catch (err) {
    showToast('Network error saving target: ' + err.message, 'error');
  }
};

window.deleteTarget = async function(id) {
  if (!confirm('Are you sure you want to delete this monitored target?')) return;
  try {
    const res = await fetch(`/api/targets/${id}`, { method: 'DELETE' });
    if (res.ok) {
      showToast('Target deleted', 'info');
      loadTargets();
      fetchStatus();
    }
  } catch (err) {
    showToast('Failed to delete target: ' + err.message, 'error');
  }
};

// ============================================================================
// MULTI-EMAIL CHIPS MANAGER
// ============================================================================
state.recipientEmails = [];

window.renderEmailChips = function() {
  const container = document.getElementById('emailChipsList');
  if (!container) return;

  if (!state.recipientEmails || state.recipientEmails.length === 0) {
    container.innerHTML = `<span class="empty-emails-placeholder">No recipient emails configured yet. Add at least one email below.</span>`;
    return;
  }

  let html = '';
  state.recipientEmails.forEach((email, idx) => {
    html += `
      <span class="email-chip">
        <i class="fa-regular fa-envelope"></i>
        <span>${escapeHTML(email)}</span>
        <button type="button" class="chip-remove-btn" onclick="removeEmailChip(${idx})" title="Remove ${escapeHTML(email)}">
          <i class="fa-solid fa-xmark"></i>
        </button>
      </span>
    `;
  });
  container.innerHTML = html;
};

window.addEmailFromInput = function() {
  const input = document.getElementById('newEmailInput');
  if (!input) return;
  const rawValue = input.value.trim();
  if (!rawValue) return;

  // Split on commas, semicolons, or spaces
  const candidates = rawValue.split(/[,;\s]+/).map((s) => s.trim().toLowerCase()).filter((s) => s);
  let addedCount = 0;

  candidates.forEach((cand) => {
    // Basic email validation
    if (cand.includes('@') && cand.includes('.') && !state.recipientEmails.includes(cand)) {
      state.recipientEmails.push(cand);
      addedCount++;
    }
  });

  input.value = '';
  window.renderEmailChips();

  if (addedCount > 0) {
    showToast(`Added ${addedCount} recipient email(s)`, 'info');
  }
};

window.removeEmailChip = function(idx) {
  if (idx >= 0 && idx < state.recipientEmails.length) {
    const removed = state.recipientEmails.splice(idx, 1);
    window.renderEmailChips();
    showToast(`Removed ${removed[0]}`, 'info');
  }
};

window.handleEmailInputKey = function(e) {
  if (e.key === 'Enter' || e.key === ',' || e.key === ';') {
    e.preventDefault();
    window.addEmailFromInput();
  }
};

// ============================================================================
// SETTINGS MANAGEMENT (FIREBASE & EMAIL)
// ============================================================================
window.openSettingsModal = async function() {
  try {
    const res = await fetch('/api/config');
    if (res.ok) {
      const cfg = await res.json();
      // Populate Firebase
      document.getElementById('setFirebaseEnabled').checked = cfg.firebase?.enabled ?? true;
      document.getElementById('setFirebaseURL').value = cfg.firebase?.database_url || firebaseConfig.databaseURL;
      document.getElementById('setFirebaseCollection').value = cfg.firebase?.collection || 'server_monitoring_logs';
      document.getElementById('setFirebaseAuth').value = cfg.firebase?.auth_secret || '';

      // Populate Email
      document.getElementById('setEmailEnabled').checked = cfg.email?.enabled ?? true;
      document.getElementById('setEmailFrom').value = cfg.email?.from_email || '';
      document.getElementById('setEmailPassword').value = cfg.email?.app_password || '';
      
      // Multi-email list
      state.recipientEmails = Array.isArray(cfg.email?.to_emails) ? [...cfg.email.to_emails] : [];
      if (state.recipientEmails.length === 0 && cfg.email?.to_emails) {
        state.recipientEmails = [cfg.email.to_emails];
      }
      window.renderEmailChips();

      // Populate Intervals
      document.getElementById('setIntervalMins').value = cfg.monitoring?.interval_minutes || 5;
      document.getElementById('setTimeoutSecs').value = cfg.monitoring?.request_timeout_seconds || 15;
    }
  } catch (err) {
    console.error('Error fetching config for settings modal:', err);
  }
  openModal('settingsModal');
};

window.handleSaveSettings = async function(e) {
  e.preventDefault();
  
  // Also grab any text currently typed in the email input
  const pendingInput = document.getElementById('newEmailInput');
  if (pendingInput && pendingInput.value.trim()) {
    window.addEmailFromInput();
  }

  if (state.recipientEmails.length === 0) {
    showToast('Please add at least one recipient email address.', 'error');
    return;
  }

  const newConfig = {
    firebase: {
      enabled: document.getElementById('setFirebaseEnabled').checked,
      type: 'realtime',
      api_key: firebaseConfig.apiKey,
      auth_domain: firebaseConfig.authDomain,
      database_url: document.getElementById('setFirebaseURL').value.trim() || firebaseConfig.databaseURL,
      project_id: firebaseConfig.projectId,
      storage_bucket: firebaseConfig.storageBucket,
      messaging_sender_id: firebaseConfig.messagingSenderId,
      app_id: firebaseConfig.appId,
      measurement_id: firebaseConfig.measurementId,
      collection: document.getElementById('setFirebaseCollection').value.trim() || 'server_monitoring_logs',
      auth_secret: document.getElementById('setFirebaseAuth').value.trim(),
    },
    email: {
      enabled: document.getElementById('setEmailEnabled').checked,
      smtp_host: 'smtp.gmail.com',
      smtp_port: 587,
      from_email: document.getElementById('setEmailFrom').value.trim(),
      app_password: document.getElementById('setEmailPassword').value,
      to_emails: state.recipientEmails,
    },
    monitoring: {
      interval_minutes: parseInt(document.getElementById('setIntervalMins').value, 10) || 5,
      request_timeout_seconds: parseInt(document.getElementById('setTimeoutSecs').value, 10) || 15,
    },
  };

  try {
    const res = await fetch('/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(newConfig),
    });

    if (res.ok) {
      showToast(`System settings saved! (${state.recipientEmails.length} alert recipient(s))`, 'success');
      closeModal('settingsModal');
      attachFirebaseRealtimeListeners();
      fetchStatus();
    } else {
      const err = await res.json();
      showToast('Failed to save settings: ' + (err.error || 'Unknown'), 'error');
    }
  } catch (err) {
    showToast('Network error saving settings: ' + err.message, 'error');
  }
};

// ============================================================================
// INSPECT LOG MODAL
// ============================================================================
window.inspectLog = function(id) {
  const logItem = state.cachedLogs.find((l) => l.id === id);
  if (!logItem) return;

  document.getElementById('modalLogTitle').textContent = `Inspect Log: ${logItem.service_name || logItem['service name'] || 'Log'}`;
  const modalBody = document.getElementById('modalLogBody');

  const isSuccess = Boolean(logItem.status);
  const statusBadge = isSuccess
    ? `<span class="status-badge success"><i class="fa-solid fa-circle-check"></i> Success (200 OK)</span>`
    : `<span class="status-badge failure"><i class="fa-solid fa-circle-xmark"></i> Failure</span>`;

  modalBody.innerHTML = `
    <div class="inspect-meta-grid">
      <div class="inspect-meta-item">
        <span class="key">Service Name</span>
        <span class="value">${escapeHTML(logItem.service_name || logItem['service name'] || '')}</span>
      </div>
      <div class="inspect-meta-item">
        <span class="key">Status</span>
        <span class="value">${statusBadge}</span>
      </div>
      <div class="inspect-meta-item">
        <span class="key">Service Type</span>
        <span class="value type-badge ${logItem.type === 'backend' ? 'backend' : 'frontend'}">${logItem.type}</span>
      </div>
      <div class="inspect-meta-item">
        <span class="key">Hit Timestamp</span>
        <span class="value font-mono">${escapeHTML(logItem.hit_time || logItem['hit time'] || '')}</span>
      </div>
      <div class="inspect-meta-item">
        <span class="key">Target URL</span>
        <span class="value"><a href="${escapeHTML(logItem.url)}" target="_blank" style="color:var(--accent-cyan); word-break:break-all;">${escapeHTML(logItem.url)}</a></span>
      </div>
      <div class="inspect-meta-item">
        <span class="key">Latency / HTTP Status</span>
        <span class="value font-mono">${logItem.response_time_ms || 0}ms • HTTP ${logItem.http_status || 'ERR'}</span>
      </div>
    </div>

    ${logItem.error_detail ? `
      <div style="background:rgba(239,68,68,0.15); border:1px solid rgba(239,68,68,0.35); border-radius:var(--radius-md); padding:12px 16px;">
        <strong style="color:#f87171; display:block; margin-bottom:4px;"><i class="fa-solid fa-triangle-exclamation"></i> Error Detail:</strong>
        <span style="color:#fca5a5; font-size:13px;">${escapeHTML(logItem.error_detail)}</span>
      </div>
    ` : ''}

    <div>
      <h4 style="font-size:13px; font-weight:700; color:var(--text-secondary); margin-bottom:8px; text-transform:uppercase; letter-spacing:0.5px;">
        <i class="fa-solid fa-code"></i> API / Response Data (JSON)
      </h4>
      <div class="json-viewer">${escapeHTML(JSON.stringify(logItem.data || {}, null, 2))}</div>
    </div>
  `;

  openModal('logDetailModal');
};

// ============================================================================
// MODAL CONTROLS & UI UTILITIES
// ============================================================================
function openModal(id) {
  document.getElementById(id).classList.remove('hidden');
}

window.closeModal = function(id) {
  document.getElementById(id).classList.add('hidden');
};

window.openTargetModal = function() {
  loadTargets();
  openModal('targetModal');
};

window.handleBackdropClick = function(e, modalId) {
  if (e.target.id === modalId) {
    closeModal(modalId);
  }
};

function showToast(message, type = 'info') {
  const container = document.getElementById('toastContainer');
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;

  let icon = 'fa-info-circle';
  if (type === 'success') icon = 'fa-circle-check';
  if (type === 'error') icon = 'fa-circle-exclamation';

  toast.innerHTML = `<i class="fa-solid ${icon}"></i> <span>${escapeHTML(message)}</span>`;
  container.appendChild(toast);

  setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transform = 'translateX(100%)';
    toast.style.transition = 'all 0.3s ease';
    setTimeout(() => toast.remove(), 300);
  }, 4000);
}

function shakeElement(elem) {
  elem.style.animation = 'none';
  elem.offsetHeight; // trigger reflow
  elem.style.animation = 'shake 0.4s ease';
}

function escapeHTML(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}
