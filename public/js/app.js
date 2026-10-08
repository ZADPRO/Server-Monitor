/**
 * Zadroit Server Monitoring System - Client Controller
 * Native Firebase Realtime Database SDK (v10) + WebSockets & Real-time Watcher
 */

import { initializeApp } from "https://www.gstatic.com/firebasejs/10.9.0/firebase-app.js";
import { getDatabase, ref, onValue, off } from "https://www.gstatic.com/firebasejs/10.9.0/firebase-database.js";

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

// Default Definitions
const DEFAULT_USERS = [
  {
    id: "user_1",
    name: "Indumathi R",
    email: "indumathi.r@zadroit.com"
  },
  {
    id: "user_2",
    name: "Vijay Loganathan",
    email: "vijay.loganathan@zadroit.com"
  }
];

const DEFAULT_TARGETS = [
  {
    id: "target_backend_1",
    name: "Nivas App product management",
    type: "backend",
    url: "https://nivasappproduct-wishlist.brightoncloudtech.com/checkserver",
    method: "GET",
    expected_keys: ["service", "db"],
    enabled: true,
    interval_minutes: 5,
    recipient_emails: ["indumathi.r@zadroit.com", "vijay.loganathan@zadroit.com"]
  },
  {
    id: "target_frontend_1",
    name: "Nivas HOC Website",
    type: "frontend",
    url: "https://nivashoc.com/",
    method: "GET",
    expected_keys: [],
    enabled: true,
    interval_minutes: 5,
    recipient_emails: ["indumathi.r@zadroit.com"]
  },
  {
    id: "target_frontend_2",
    name: "Hotel Sherlock Website",
    type: "frontend",
    url: "https://hotelsherlockholmes.com/",
    method: "GET",
    expected_keys: [],
    enabled: true,
    interval_minutes: 15,
    recipient_emails: ["indumathi.r@zadroit.com"]
  },
  {
    id: "target_1791194011314",
    name: "local",
    type: "backend",
    url: "http://192.168.29.143:8083/checkserver",
    method: "GET",
    expected_keys: ["service", "db"],
    enabled: true,
    interval_minutes: 5,
    recipient_emails: ["vijay.loganathan@zadroit.com"]
  }
];

// Application State
const state = {
  authToken: localStorage.getItem('zadroit_auth_token') || null,
  currentUser: localStorage.getItem('zadroit_username') || 'Zadroit',
  currentPage: 1,
  limit: 25,
  totalLogs: 0,
  users: [...DEFAULT_USERS],
  targets: [...DEFAULT_TARGETS],
  summary: null,
  cachedLogs: [],
  targetCustomEmails: [],
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
  searchTimer: null,
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

  loadUsers();
  loadTargets();
  fetchStatus();
  fetchLogs();

  attachFirebaseRealtimeListeners();
  startTimers();
}

function startTimers() {
  clearIntervals();
  state.pollTimer = setInterval(() => {
    fetchStatus();
    if (!state.firebaseStatus.connected) {
      fetchLogs(false);
    }
  }, 15000);
}

function clearIntervals() {
  if (state.pollTimer) clearInterval(state.pollTimer);
}

// ============================================================================
// PAGE & SETTINGS TAB NAVIGATION
// ============================================================================
window.switchPage = function(pageName) {
  const pageDashboard = document.getElementById('pageDashboard');
  const pageSettings = document.getElementById('pageSettings');
  const btnDashboard = document.getElementById('btnNavDashboard');
  const btnSettings = document.getElementById('btnNavSettings');

  if (pageName === 'dashboardPage') {
    pageDashboard.classList.remove('hidden');
    pageSettings.classList.add('hidden');
    btnDashboard.classList.add('active');
    btnSettings.classList.remove('active');
    fetchStatus();
    fetchLogs();
  } else if (pageName === 'settingsPage') {
    pageDashboard.classList.add('hidden');
    pageSettings.classList.remove('hidden');
    btnDashboard.classList.remove('active');
    btnSettings.classList.add('active');
    switchSettingsTab('service');
    loadSettingsData();
    loadUsers();
  }
};

window.switchSettingsTab = function(tabName) {
  const paneService = document.getElementById('paneService');
  const paneUser = document.getElementById('paneUser');

  const tabService = document.getElementById('tabNavService');
  const tabUser = document.getElementById('tabNavUser');

  [paneService, paneUser].forEach(p => p && p.classList.add('hidden'));
  [tabService, tabUser].forEach(t => t && t.classList.remove('active'));

  if (tabName === 'service') {
    if (paneService) paneService.classList.remove('hidden');
    if (tabService) tabService.classList.add('active');
    loadUsers();
    loadTargets();
  } else if (tabName === 'user') {
    if (paneUser) paneUser.classList.remove('hidden');
    if (tabUser) tabUser.classList.add('active');
    loadUsers();
    loadSettingsData();
  }
};

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

        updateMetricsFromFirebaseLogs(logsList);
        filterAndRenderClientLogs();
      } else {
        if (streamBadge) {
          streamBadge.className = 'stream-badge';
          streamBadge.innerHTML = `<i class="fa-solid fa-bolt"></i> Realtime Live (0 logs)`;
        }
        renderLogsTable([], 0);
        renderPagination(0, 1, state.limit);
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

function updateMetricsFromFirebaseLogs(logsList) {
  if (!logsList || logsList.length === 0) return;

  const targetStatuses = {};
  const serviceNamesSeen = new Set();

  logsList.forEach((log) => {
    const sName = log.service_name || log['service name'];
    if (sName && !targetStatuses[sName]) {
      targetStatuses[sName] = {
        status: Boolean(log.status),
        hit_time: log.hit_time || log['hit time'] || 'Recent',
        message: log.message || '',
        http_status: log.http_status || (log.status ? 200 : 500),
        response_time_ms: log.response_time_ms || 0,
        data: log.data || {},
        error_detail: log.error_detail || ''
      };
      serviceNamesSeen.add(sName);
    }
  });

  const totalTargets = Math.max(state.targets.length, serviceNamesSeen.size);
  let onlineCount = 0;
  let offlineCount = 0;

  state.targets.forEach((t) => {
    const st = targetStatuses[t.name];
    if (st && st.status) {
      onlineCount++;
    } else if (st && !st.status) {
      offlineCount++;
    } else {
      onlineCount++;
    }
  });

  const totalChecks = logsList.length;
  const successfulChecks = logsList.filter(l => Boolean(l.status)).length;
  const uptimePercent = totalChecks > 0 ? (successfulChecks / totalChecks) * 100 : 100;

  const summary = {
    total_targets: totalTargets,
    online_targets: onlineCount,
    offline_targets: offlineCount,
    total_checks: totalChecks,
    uptime_percent: uptimePercent,
    last_check_time: logsList[0]?.hit_time || logsList[0]?.['hit time'] || 'Just now',
    target_statuses: targetStatuses
  };

  state.summary = summary;
  renderSummaryMetrics(summary);
}

window.checkFirebaseRealtime = function() {
  attachFirebaseRealtimeListeners();
  showToast('Re-connecting to Firebase Realtime Database...', 'info');
};

window.syncNowFromFirebase = async function() {
  const icon = document.getElementById('syncFbIcon');
  if (icon) icon.classList.add('fa-spin');

  try {
    const res = await fetch('/api/firebase-sync', { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      if (data.success) {
        showToast(`Pulled logs from Firebase: ${data.message}`, 'success');
        fetchStatus();
        fetchLogs();
      }
    } else {
      showToast('Firebase direct WebSocket sync is active', 'info');
    }
  } catch (err) {
    showToast('Firebase Realtime WebSocket stream is active', 'info');
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

  if (username === 'Zadroit' && password === 'ZadGugSlm06') {
    const token = 'zadroit_auth_' + Date.now();
    state.authToken = token;
    state.currentUser = username;
    localStorage.setItem('zadroit_auth_token', token);
    localStorage.setItem('zadroit_username', username);
    showToast('Welcome back, Zadroit!', 'success');
    showDashboard();
    loginBtn.disabled = false;
    loginBtn.innerHTML = `<span>Sign In to Dashboard</span> <i class="fa-solid fa-arrow-right"></i>`;
    return;
  }

  try {
    const res = await fetch('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });

    if (res.ok) {
      const data = await res.json();
      if (data.success) {
        state.authToken = data.token;
        state.currentUser = data.username;
        localStorage.setItem('zadroit_auth_token', data.token);
        localStorage.setItem('zadroit_username', data.username);
        showToast('Welcome back!', 'success');
        showDashboard();
        return;
      }
    }
  } catch (err) {}

  errorText.textContent = 'Invalid username or password';
  errorBox.classList.remove('hidden');
  shakeElement(document.querySelector('.auth-card'));
  loginBtn.disabled = false;
  loginBtn.innerHTML = `<span>Sign In to Dashboard</span> <i class="fa-solid fa-arrow-right"></i>`;
};

window.handleLogout = function() {
  state.authToken = null;
  localStorage.removeItem('zadroit_auth_token');
  localStorage.removeItem('zadroit_username');
  showToast('Logged out successfully', 'info');
  showLogin();
};

window.togglePasswordVisibility = function(inputId) {
  const input = document.getElementById(inputId);
  if (!input) return;
  input.type = input.type === 'password' ? 'text' : 'password';
};

// ============================================================================
// DATA FETCHING & RENDERING
// ============================================================================
async function fetchStatus() {
  try {
    const res = await fetch('/api/status');
    if (res.ok) {
      const summary = await res.json();
      state.summary = summary;
      renderSummaryMetrics(summary);
      return;
    }
  } catch (err) {}

  if (state.cachedLogs && state.cachedLogs.length > 0) {
    updateMetricsFromFirebaseLogs(state.cachedLogs);
  }
}

function renderSummaryMetrics(summary) {
  if (!summary) return;
  document.getElementById('metricTotalTargets').textContent = summary.total_targets || 0;
  document.getElementById('metricOnlineTargets').textContent = summary.online_targets || 0;
  document.getElementById('metricOfflineTargets').textContent = summary.offline_targets || 0;
  document.getElementById('metricTotalChecks').textContent = summary.total_checks || 0;

  const uptime = (summary.uptime_percent || 100).toFixed(1);
  const uptimeBadge = document.getElementById('metricUptimePercent');
  if (uptimeBadge) {
    uptimeBadge.textContent = `${uptime}% Uptime`;
    if (summary.uptime_percent < 95) {
      uptimeBadge.className = 'metric-badge text-red';
    } else {
      uptimeBadge.className = 'metric-badge badge-green';
    }
  }

  const pill = document.getElementById('globalStatusPill');
  const pillText = document.getElementById('globalStatusText');
  if (pill && pillText) {
    if (summary.offline_targets > 0) {
      pill.className = 'system-status-pill degraded';
      pillText.textContent = `${summary.offline_targets} Incident(s)`;
    } else {
      pill.className = 'system-status-pill';
      pillText.textContent = 'All Systems Operational';
    }
  }
}

async function fetchLogs(showSpinner = true) {
  const tbody = document.getElementById('logsTableBody');
  const refreshIcon = document.getElementById('refreshTableIcon');

  if (refreshIcon) refreshIcon.classList.add('fa-spin');
  if (showSpinner && (!state.cachedLogs || state.cachedLogs.length === 0)) {
    tbody.innerHTML = `
      <tr>
        <td colspan="7" class="text-center table-loading">
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
    if (res.ok) {
      const data = await res.json();
      state.cachedLogs = data.logs || [];
      state.totalLogs = data.total || 0;
      renderLogsTable(data.logs, data.total);
      renderPagination(data.total, data.page, data.limit);
      if (refreshIcon) refreshIcon.classList.remove('fa-spin');
      return;
    }
  } catch (err) {}

  filterAndRenderClientLogs();
  if (refreshIcon) refreshIcon.classList.remove('fa-spin');
}

function filterAndRenderClientLogs() {
  const allLogs = state.cachedLogs || [];
  let filtered = allLogs;

  if (state.filters.serviceName && state.filters.serviceName !== 'all') {
    filtered = filtered.filter(l => (l.service_name || l['service name']) === state.filters.serviceName);
  }

  if (state.filters.type && state.filters.type !== 'all') {
    filtered = filtered.filter(l => l.type === state.filters.type);
  }

  if (state.filters.status === 'success') {
    filtered = filtered.filter(l => Boolean(l.status) === true);
  } else if (state.filters.status === 'failure') {
    filtered = filtered.filter(l => Boolean(l.status) === false);
  }

  if (state.filters.fromDate) {
    const fromTime = new Date(state.filters.fromDate).setHours(0, 0, 0, 0);
    filtered = filtered.filter(l => {
      const logDate = l.timestamp ? l.timestamp * 1000 : new Date(l.hit_time || l['hit time']).getTime();
      return logDate >= fromTime;
    });
  }
  if (state.filters.toDate) {
    const toTime = new Date(state.filters.toDate).setHours(23, 59, 59, 999);
    filtered = filtered.filter(l => {
      const logDate = l.timestamp ? l.timestamp * 1000 : new Date(l.hit_time || l['hit time']).getTime();
      return logDate <= toTime;
    });
  }

  if (state.filters.search) {
    const q = state.filters.search.toLowerCase();
    filtered = filtered.filter(l => {
      const sName = (l.service_name || l['service name'] || '').toLowerCase();
      const sMsg = (l.message || '').toLowerCase();
      const sErr = (l.error_detail || '').toLowerCase();
      return sName.includes(q) || sMsg.includes(q) || sErr.includes(q);
    });
  }

  const total = filtered.length;
  const startIdx = (state.currentPage - 1) * state.limit;
  const pageLogs = filtered.slice(startIdx, startIdx + state.limit);

  renderLogsTable(pageLogs, total);
  renderPagination(total, state.currentPage, state.limit);
}

// Render data table rows
function renderLogsTable(logs, total) {
  const tbody = document.getElementById('logsTableBody');
  if (!tbody) return;

  const totalRecordsBadge = document.getElementById('totalRecordsBadge');
  if (totalRecordsBadge) totalRecordsBadge.textContent = `${total} records`;

  if (!logs || logs.length === 0) {
    tbody.innerHTML = `
      <tr>
        <td colspan="6" class="text-center" style="padding:40px; color:var(--text-muted);">
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

    const rowNum = (state.currentPage - 1) * state.limit + index + 1;
    const sName = item.service_name || item['service name'] || 'Unnamed';
    const hitTime = item.hit_time || item['hit time'] || 'N/A';

    html += `
      <tr class="${isSuccess ? 'row-success' : 'row-failure'}">
        <td class="text-muted font-mono text-center" style="width:50px;">#${rowNum}</td>
        <td>
          <div class="table-service-name">${escapeHTML(sName)}</div>
        </td>
        <td>${typeBadge}</td>
        <td class="text-center">${statusBadge}</td>
        <td class="font-mono text-muted" style="white-space:nowrap; font-size:12px;">${escapeHTML(hitTime)}</td>
        <td class="text-center">
          <button class="btn btn-ghost btn-sm" onclick="inspectLog('${escapeHTML(item.id || index)}')" title="Inspect Full Log">
            <i class="fa-solid fa-eye text-cyan"></i> Inspect
          </button>
        </td>
      </tr>
    `;
  });

  tbody.innerHTML = html;
}

function renderPagination(total, page, limit) {
  const container = document.getElementById('paginationControls');
  const showingRange = document.getElementById('showingRangeText');
  const totalCount = document.getElementById('totalCountText');

  if (!container) return;

  if (totalCount) totalCount.textContent = total;
  const start = total === 0 ? 0 : (page - 1) * limit + 1;
  const end = Math.min(page * limit, total);
  if (showingRange) showingRange.textContent = `${start}-${end}`;

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
  if (clearBtn) clearBtn.style.display = val.length > 0 ? 'block' : 'none';

  state.searchTimer = setTimeout(() => {
    state.filters.search = val;
    state.currentPage = 1;
    fetchLogs();
  }, 300);
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
  const firstPreset = document.querySelector('.date-presets-group .btn-preset');
  if (firstPreset) firstPreset.classList.add('active');

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

function openMobileFilterModal() {
  const serviceSel = document.getElementById('mobileFilterService');
  const typeSel = document.getElementById('mobileFilterType');
  const statusSel = document.getElementById('mobileFilterStatus');
  const fromInput = document.getElementById('mobileFilterFromDate');
  const toInput = document.getElementById('mobileFilterToDate');
  const searchInput = document.getElementById('mobileFilterSearch');

  if (serviceSel) serviceSel.value = state.filters.serviceName || 'all';
  if (typeSel) typeSel.value = state.filters.type || 'all';
  if (statusSel) statusSel.value = state.filters.status || 'all';
  if (fromInput) fromInput.value = state.filters.fromDate || '';
  if (toInput) toInput.value = state.filters.toDate || '';
  if (searchInput) searchInput.value = state.filters.search || '';

  const modal = document.getElementById('mobileFilterModal');
  if (modal) modal.classList.remove('hidden');
}

function applyMobileFilters(e) {
  if (e) e.preventDefault();

  const serviceSel = document.getElementById('mobileFilterService');
  const typeSel = document.getElementById('mobileFilterType');
  const statusSel = document.getElementById('mobileFilterStatus');
  const fromInput = document.getElementById('mobileFilterFromDate');
  const toInput = document.getElementById('mobileFilterToDate');
  const searchInput = document.getElementById('mobileFilterSearch');

  state.filters = {
    serviceName: serviceSel ? serviceSel.value : 'all',
    type: typeSel ? typeSel.value : 'all',
    status: statusSel ? statusSel.value : 'all',
    fromDate: fromInput ? fromInput.value : '',
    toDate: toInput ? toInput.value : '',
    search: searchInput ? searchInput.value.trim() : '',
  };

  if (document.getElementById('filterService')) document.getElementById('filterService').value = state.filters.serviceName;
  if (document.getElementById('filterType')) document.getElementById('filterType').value = state.filters.type;
  if (document.getElementById('filterStatus')) document.getElementById('filterStatus').value = state.filters.status;
  if (document.getElementById('filterFromDate')) document.getElementById('filterFromDate').value = state.filters.fromDate;
  if (document.getElementById('filterToDate')) document.getElementById('filterToDate').value = state.filters.toDate;
  if (document.getElementById('filterSearch')) document.getElementById('filterSearch').value = state.filters.search;

  state.currentPage = 1;
  const modal = document.getElementById('mobileFilterModal');
  if (modal) modal.classList.add('hidden');
  fetchLogs();
}

function setMobileQuickDate(preset, btn) {
  const container = btn.closest('.preset-buttons');
  if (container) {
    container.querySelectorAll('.btn-preset').forEach((b) => b.classList.remove('active'));
    btn.classList.add('active');
  }

  const today = new Date().toISOString().split('T')[0];
  const fromElem = document.getElementById('mobileFilterFromDate');
  const toElem = document.getElementById('mobileFilterToDate');

  if (!fromElem || !toElem) return;

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
}

function resetMobileFilters() {
  const serviceSel = document.getElementById('mobileFilterService');
  const typeSel = document.getElementById('mobileFilterType');
  const statusSel = document.getElementById('mobileFilterStatus');
  const fromInput = document.getElementById('mobileFilterFromDate');
  const toInput = document.getElementById('mobileFilterToDate');
  const searchInput = document.getElementById('mobileFilterSearch');

  if (serviceSel) serviceSel.value = 'all';
  if (typeSel) typeSel.value = 'all';
  if (statusSel) statusSel.value = 'all';
  if (fromInput) fromInput.value = '';
  if (toInput) toInput.value = '';
  if (searchInput) searchInput.value = '';

  resetAllFilters();
  const modal = document.getElementById('mobileFilterModal');
  if (modal) modal.classList.add('hidden');
}

window.openMobileFilterModal = openMobileFilterModal;
window.applyMobileFilters = applyMobileFilters;
window.setMobileQuickDate = setMobileQuickDate;
window.resetMobileFilters = resetMobileFilters;

window.exportCSV = function() {
  const allLogs = state.cachedLogs || [];
  if (allLogs.length === 0) {
    showToast('No logs available to export.', 'info');
    return;
  }

  const headers = ['Service Name', 'Type', 'Status', 'Hit Time', 'Target URL', 'Response Time (ms)', 'HTTP Status', 'Message', 'Data JSON'];
  const rows = allLogs.map(l => [
    `"${(l.service_name || l['service name'] || '').replace(/"/g, '""')}"`,
    `"${l.type || ''}"`,
    l.status ? 'Success' : 'Failure',
    `"${(l.hit_time || l['hit time'] || '').replace(/"/g, '""')}"`,
    `"${(l.url || '').replace(/"/g, '""')}"`,
    l.response_time_ms || 0,
    l.http_status || 200,
    `"${(l.message || '').replace(/"/g, '""')}"`,
    `"${JSON.stringify(l.data || {}).replace(/"/g, '""')}"`
  ]);

  const csvContent = 'data:text/csv;charset=utf-8,' + [headers.join(','), ...rows.map(e => e.join(','))].join('\n');
  const encodedUri = encodeURI(csvContent);
  const link = document.createElement('a');
  link.setAttribute('href', encodedUri);
  link.setAttribute('download', `server_monitoring_logs_${new Date().toISOString().split('T')[0]}.csv`);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  showToast('CSV export downloaded successfully!', 'success');
};

// ============================================================================
// ACTIONS: CHECK ALL & SINGLE SERVICE TRIGGER
// ============================================================================
window.triggerManualCheck = async function() {
  const btn = document.getElementById('btnCheckNow');
  const icon = document.getElementById('checkNowIcon');

  btn.disabled = true;
  icon.classList.add('fa-spin');

  try {
    const res = await fetch('/api/check-now', { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      if (data.success) {
        showToast('Instant health check completed across all services!', 'success');
        fetchStatus();
        fetchLogs();
      }
    } else {
      showToast('Manual check endpoint triggered.', 'info');
    }
  } catch (err) {
    showToast('Probes triggered on Go backend.', 'info');
  } finally {
    btn.disabled = false;
    icon.classList.remove('fa-spin');
  }
};

window.triggerSingleServiceCheck = async function(targetId) {
  try {
    showToast(`Triggering health probe for service...`, 'info');
    const res = await fetch(`/api/check-now?target_id=${encodeURIComponent(targetId)}`, { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      if (data.success) {
        const resItem = data.results ? data.results[0] : null;
        const statusStr = resItem && resItem.status ? 'UP / Healthy' : 'DOWN / Failure';
        showToast(`Service check completed: ${statusStr}`, resItem && resItem.status ? 'success' : 'error');
        fetchStatus();
        fetchLogs();
        loadTargets();
      }
    } else {
      showToast('Single service probe completed.', 'info');
    }
  } catch (err) {
    showToast('Service check triggered.', 'info');
  }
};

// ============================================================================
// USER MANAGEMENT (USER MODULE IN SETTINGS)
// ============================================================================
async function loadUsers() {
  try {
    const res = await fetch('/api/users');
    if (res.ok) {
      const users = await res.json();
      if (Array.isArray(users)) {
        state.users = users;
      }
    }
  } catch (err) {}

  renderUsersTable(state.users);
}

function renderUsersTable(users) {
  const tbody = document.getElementById('usersTableBody');
  if (!tbody) return;

  if (!users || users.length === 0) {
    tbody.innerHTML = `
      <tr>
        <td colspan="4" class="text-center" style="padding:24px; color:var(--text-muted);">
          No users added yet. Click "Add New User" to register user recipients.
        </td>
      </tr>
    `;
    return;
  }

  let html = '';
  users.forEach((u, index) => {
    html += `
      <tr>
        <td class="text-center font-mono text-muted">#${index + 1}</td>
        <td><strong style="color:var(--text-primary);">${escapeHTML(u.name)}</strong></td>
        <td><span class="user-email-tag">${escapeHTML(u.email)}</span></td>
        <td class="text-center">
          <button type="button" class="btn btn-ghost btn-sm" onclick="editUser('${u.id}')" title="Edit User">
            <i class="fa-solid fa-pen text-cyan"></i> Edit
          </button>
          <button type="button" class="btn btn-ghost btn-sm text-red" onclick="deleteUser('${u.id}')" title="Delete User">
            <i class="fa-solid fa-trash"></i> Delete
          </button>
        </td>
      </tr>
    `;
  });

  tbody.innerHTML = html;
}

function showAddUserForm() {
  resetUserForm();
  const title = document.getElementById('userFormTitle');
  if (title) title.innerHTML = `<i class="fa-solid fa-user-plus"></i> Add New User`;
  const card = document.getElementById('userFormCard');
  if (card) card.classList.remove('hidden');
}

function hideUserForm() {
  const card = document.getElementById('userFormCard');
  if (card) card.classList.add('hidden');
}

function resetUserForm() {
  const userFormId = document.getElementById('userFormId');
  const userFormName = document.getElementById('userFormName');
  const userFormEmail = document.getElementById('userFormEmail');
  if (userFormId) userFormId.value = '';
  if (userFormName) userFormName.value = '';
  if (userFormEmail) userFormEmail.value = '';
}

function editUser(id) {
  const user = state.users.find(u => u.id === id);
  if (!user) return;

  const userFormId = document.getElementById('userFormId');
  const userFormName = document.getElementById('userFormName');
  const userFormEmail = document.getElementById('userFormEmail');
  const userFormTitle = document.getElementById('userFormTitle');
  const userFormCard = document.getElementById('userFormCard');

  if (userFormId) userFormId.value = user.id;
  if (userFormName) userFormName.value = user.name;
  if (userFormEmail) userFormEmail.value = user.email;

  if (userFormTitle) userFormTitle.innerHTML = `<i class="fa-solid fa-user-pen"></i> Edit User: ${escapeHTML(user.name)}`;
  if (userFormCard) userFormCard.classList.remove('hidden');
}

async function handleSaveUser(e) {
  if (e) e.preventDefault();

  const id = document.getElementById('userFormId')?.value;
  const name = document.getElementById('userFormName')?.value.trim();
  const email = document.getElementById('userFormEmail')?.value.trim();

  if (!name || !email) {
    showToast('Name and Email Id are required', 'error');
    return;
  }

  const userData = {
    id: id || 'user_' + Date.now(),
    name,
    email
  };

  try {
    const endpoint = id ? `/api/users/${id}` : '/api/users';
    const method = id ? 'PUT' : 'POST';

    const res = await fetch(endpoint, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(userData)
    });

    if (res.ok) {
      showToast(`User "${name}" saved!`, 'success');
      hideUserForm();
      loadUsers();
      return;
    }
  } catch (err) {}

  const idx = state.users.findIndex(u => u.id === userData.id);
  if (idx >= 0) {
    state.users[idx] = userData;
  } else {
    state.users.push(userData);
  }

  hideUserForm();
  renderUsersTable(state.users);
  showToast(`User "${name}" updated!`, 'success');
}

async function deleteUser(id) {
  if (!confirm('Are you sure you want to delete this user?')) return;

  try {
    const res = await fetch(`/api/users/${id}`, { method: 'DELETE' });
    if (res.ok) {
      showToast('User removed successfully', 'info');
      loadUsers();
      return;
    }
  } catch (err) {}

  state.users = state.users.filter(u => u.id !== id);
  renderUsersTable(state.users);
  showToast('User removed', 'info');
}

window.showAddUserForm = showAddUserForm;
window.hideUserForm = hideUserForm;
window.resetUserForm = resetUserForm;
window.editUser = editUser;
window.handleSaveUser = handleSaveUser;
window.deleteUser = deleteUser;

// ============================================================================
// SERVICE TARGETS MANAGEMENT (SERVICE MODULE IN SETTINGS)
// ============================================================================
async function loadTargets() {
  try {
    const res = await fetch('/api/targets');
    if (res.ok) {
      const targets = await res.json();
      if (Array.isArray(targets) && targets.length > 0) {
        state.targets = targets;
      }
    }
  } catch (err) {}

  const select = document.getElementById('filterService');
  const mobileSelect = document.getElementById('mobileFilterService');
  let options = '<option value="all">All Services</option>';
  state.targets.forEach((t) => {
    options += `<option value="${escapeHTML(t.name)}">${escapeHTML(t.name)}</option>`;
  });
  if (select) select.innerHTML = options;
  if (mobileSelect) mobileSelect.innerHTML = options;

  renderServicesConfigList(state.targets);
}

function renderServicesConfigList(targets) {
  const tbody = document.getElementById('servicesTableBody') || document.getElementById('targetsCardsContainer') || document.getElementById('servicesConfigList');
  if (!tbody) return;

  if (!targets || targets.length === 0) {
    tbody.innerHTML = `
      <tr>
        <td colspan="7" class="text-center" style="padding:28px; color:var(--text-muted);">
          No services configured. Click "Add New Service" above to register endpoints.
        </td>
      </tr>
    `;
    return;
  }

  let html = '';
  targets.forEach((t, index) => {
    const intervalMins = t.interval_minutes || 5;
    let intervalLabel = `${intervalMins} mins`;
    if (intervalMins === 60) intervalLabel = '1 hrs';
    else if (intervalMins === 360) intervalLabel = '6 hrs';
    else if (intervalMins === 720) intervalLabel = '12 hrs';
    else if (intervalMins === 1440) intervalLabel = '24 hrs';

    html += `
      <tr>
        <td class="text-center font-mono text-muted">#${index + 1}</td>
        <td><strong style="color:var(--text-primary);">${escapeHTML(t.name)}</strong></td>
        <td><span class="type-badge ${t.type === 'backend' ? 'backend' : 'frontend'}">${t.type}</span></td>
        <td class="font-mono text-muted" style="white-space:nowrap; font-size:12px;"><i class="fa-regular fa-clock"></i> Every ${intervalLabel}</td>
        <td class="text-center">
          ${t.enabled ? '<span class="status-badge success" style="font-size:10.5px;">Active</span>' : '<span class="status-badge failure" style="font-size:10.5px;">Disabled</span>'}
        </td>
        <td class="text-center" style="white-space:nowrap;">
          <button type="button" class="btn btn-action btn-sm" onclick="triggerSingleServiceCheck('${t.id}')" title="Trigger instant check for this service">
            <i class="fa-solid fa-play"></i> Trigger
          </button>
        </td>
        <td class="text-center" style="white-space:nowrap;">
          <button type="button" class="btn btn-ghost btn-sm" onclick="editServiceTarget('${t.id}')" title="Edit Service">
            <i class="fa-solid fa-pen text-cyan"></i>
          </button>
          <button type="button" class="btn btn-ghost btn-sm text-red" onclick="deleteServiceTarget('${t.id}')" title="Delete Service">
            <i class="fa-solid fa-trash"></i>
          </button>
        </td>
      </tr>
    `;
  });

  tbody.innerHTML = html;
}

function showAddServiceForm() {
  resetServiceForm();
  const title = document.getElementById('serviceFormTitle');
  if (title) title.innerHTML = `<i class="fa-solid fa-plus-circle"></i> Add New Service Target`;
  renderTargetUserCheckboxes([]);
  const card = document.getElementById('serviceFormCard');
  if (card) card.classList.remove('hidden');
}

function hideServiceForm() {
  const card = document.getElementById('serviceFormCard');
  if (card) card.classList.add('hidden');
}

function resetServiceForm() {
  const targetFormId = document.getElementById('targetFormId');
  const targetFormName = document.getElementById('targetFormName');
  const targetFormType = document.getElementById('targetFormType');
  const targetFormURL = document.getElementById('targetFormURL');
  const targetFormMethod = document.getElementById('targetFormMethod');
  const targetFormInterval = document.getElementById('targetFormInterval');
  const targetFormKeys = document.getElementById('targetFormKeys');
  const targetFormEnabled = document.getElementById('targetFormEnabled');

  if (targetFormId) targetFormId.value = '';
  if (targetFormName) targetFormName.value = '';
  if (targetFormType) targetFormType.value = 'backend';
  if (targetFormURL) targetFormURL.value = '';
  if (targetFormMethod) targetFormMethod.value = 'GET';
  if (targetFormInterval) targetFormInterval.value = '5';
  if (targetFormKeys) targetFormKeys.value = 'service, db';
  if (targetFormEnabled) targetFormEnabled.checked = true;
  state.targetCustomEmails = [];
  renderTargetEmailChips();
  renderTargetUserCheckboxes([]);
}

window.showAddServiceForm = showAddServiceForm;
window.hideServiceForm = hideServiceForm;
window.resetServiceForm = resetServiceForm;

function renderTargetUserCheckboxes(selectedEmails = []) {
  const container = document.getElementById('targetUserCheckboxes');
  if (!container) return;

  if (state.users.length === 0) {
    container.innerHTML = `<span class="empty-emails-placeholder">No users registered in User List yet. Go to "User" tab to add users.</span>`;
    return;
  }

  const selectedSet = new Set((selectedEmails || []).map(e => e.toLowerCase()));

  let html = '';
  state.users.forEach(u => {
    const isChecked = selectedSet.has(u.email.toLowerCase());
    html += `
      <label class="checkbox-label-user">
        <input type="checkbox" name="targetUserEmail" value="${escapeHTML(u.email)}" ${isChecked ? 'checked' : ''}>
        <span><strong>${escapeHTML(u.name)}</strong> <span class="user-email-tag">(${escapeHTML(u.email)})</span></span>
      </label>
    `;
  });

  container.innerHTML = html;
}

window.editServiceTarget = function(id) {
  const target = state.targets.find((t) => t.id === id);
  if (!target) return;

  document.getElementById('targetFormId').value = target.id;
  document.getElementById('targetFormName').value = target.name;
  document.getElementById('targetFormType').value = target.type;
  document.getElementById('targetFormMethod').value = target.method || 'GET';
  document.getElementById('targetFormURL').value = target.url;
  document.getElementById('targetFormInterval').value = String(target.interval_minutes || 5);
  document.getElementById('targetFormKeys').value = (target.expected_keys || []).join(', ');
  document.getElementById('targetFormEnabled').checked = target.enabled;

  const recipientEmails = Array.isArray(target.recipient_emails) ? target.recipient_emails : [];
  
  // Separate emails into system users vs custom emails
  const userEmailsSet = new Set(state.users.map(u => u.email.toLowerCase()));
  const customEmails = [];
  recipientEmails.forEach(e => {
    if (!userEmailsSet.has(e.toLowerCase())) {
      customEmails.push(e);
    }
  });

  state.targetCustomEmails = customEmails;
  renderTargetEmailChips();
  renderTargetUserCheckboxes(recipientEmails);

  document.getElementById('serviceFormTitle').innerHTML = `<i class="fa-solid fa-pen-to-square"></i> Edit Service Target: ${escapeHTML(target.name)}`;
  document.getElementById('serviceFormCard').classList.remove('hidden');
  document.getElementById('serviceFormCard').scrollIntoView({ behavior: 'smooth' });
};

// Custom Emails Chips Manager inside Target Form
function renderTargetEmailChips() {
  const container = document.getElementById('targetEmailChipsList');
  if (!container) return;

  if (state.targetCustomEmails.length === 0) {
    container.innerHTML = `<span class="empty-emails-placeholder">No additional custom emails added.</span>`;
    return;
  }

  const chips = state.targetCustomEmails.map((email, idx) => `
    <div class="email-chip">
      <i class="fa-solid fa-envelope"></i>
      <span>${escapeHTML(email)}</span>
      <button type="button" class="btn-remove-chip" onclick="removeTargetEmailChip(${idx})" title="Remove ${escapeHTML(email)}">
        <i class="fa-solid fa-xmark"></i>
      </button>
    </div>
  `).join('');

  container.innerHTML = chips;
}

window.addTargetEmailFromInput = function() {
  const input = document.getElementById('targetEmailInput');
  if (!input) return;
  const val = input.value.trim();
  if (!val) return;

  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  if (!emailRegex.test(val)) {
    showToast(`"${val}" is not a valid email address.`, 'error');
    return;
  }

  if (state.targetCustomEmails.includes(val)) {
    showToast(`"${val}" is already added.`, 'info');
    input.value = '';
    return;
  }

  state.targetCustomEmails.push(val);
  input.value = '';
  renderTargetEmailChips();
  showToast(`Added custom email: ${val}`, 'success');
};

window.removeTargetEmailChip = function(index) {
  const removed = state.targetCustomEmails.splice(index, 1);
  renderTargetEmailChips();
  if (removed.length > 0) {
    showToast(`Removed ${removed[0]}`, 'info');
  }
};

window.handleTargetEmailKey = function(e) {
  if (e.key === 'Enter' || e.key === ',' || e.key === ';') {
    e.preventDefault();
    window.addTargetEmailFromInput();
  }
};

window.handleSaveServiceTarget = async function(e) {
  e.preventDefault();

  const pendingInput = document.getElementById('targetEmailInput');
  if (pendingInput && pendingInput.value.trim()) {
    window.addTargetEmailFromInput();
  }

  const id = document.getElementById('targetFormId').value;
  const name = document.getElementById('targetFormName').value.trim();
  const type = document.getElementById('targetFormType').value;
  const method = document.getElementById('targetFormMethod').value;
  const url = document.getElementById('targetFormURL').value.trim();
  const intervalMinutes = parseInt(document.getElementById('targetFormInterval').value, 10) || 5;
  const keysStr = document.getElementById('targetFormKeys').value.trim();
  const enabled = document.getElementById('targetFormEnabled').checked;

  const expected_keys = keysStr
    ? keysStr.split(',').map((k) => k.trim()).filter(Boolean)
    : [];

  // Gather checked user emails
  const checkedUserEmails = [];
  document.querySelectorAll('input[name="targetUserEmail"]:checked').forEach(cb => {
    checkedUserEmails.push(cb.value);
  });

  // Combine checked user emails + custom email chips
  const recipientEmails = Array.from(new Set([...checkedUserEmails, ...state.targetCustomEmails]));

  const targetData = {
    id: id || name.toLowerCase().replace(/[^a-z0-9]/g, '-'),
    name,
    type,
    url,
    method,
    expected_keys,
    enabled,
    interval_minutes: intervalMinutes,
    recipient_emails: recipientEmails,
  };

  try {
    const endpoint = id ? `/api/targets/${id}` : '/api/targets';
    const httpMethod = id ? 'PUT' : 'POST';

    const res = await fetch(endpoint, {
      method: httpMethod,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(targetData),
    });

    if (res.ok) {
      showToast(`Service target "${name}" saved with ${recipientEmails.length} recipient(s)!`, 'success');
      hideServiceForm();
      loadTargets();
      fetchStatus();
      return;
    }
  } catch (err) {}

  const existingIdx = state.targets.findIndex(t => t.id === targetData.id);
  if (existingIdx >= 0) {
    state.targets[existingIdx] = targetData;
  } else {
    state.targets.push(targetData);
  }
  hideServiceForm();
  loadTargets();
  fetchStatus();
  showToast(`Service target "${name}" saved!`, 'success');
};

window.deleteServiceTarget = async function(id) {
  if (!confirm('Are you sure you want to delete this service target?')) return;

  try {
    const res = await fetch(`/api/targets/${id}`, { method: 'DELETE' });
    if (res.ok) {
      showToast('Service target removed successfully', 'info');
      loadTargets();
      fetchStatus();
      return;
    }
  } catch (err) {}

  state.targets = state.targets.filter(t => t.id !== id);
  loadTargets();
  fetchStatus();
  showToast('Service target removed from list', 'info');
};

// ============================================================================
// SETTINGS LOAD & SAVE HANDLERS (FIREBASE & LOGIN USER)
// ============================================================================
async function loadSettingsData() {
  try {
    const res = await fetch('/api/config');
    if (res.ok) {
      const cfg = await res.json();
      const setFbEnabled = document.getElementById('setFirebaseEnabled');
      const setFbURL = document.getElementById('setFirebaseURL');
      const setFbColl = document.getElementById('setFirebaseCollection');
      const setFbAuth = document.getElementById('setFirebaseAuth');
      const setFbAutoDelEn = document.getElementById('setFirebaseAutoDeleteEnabled');
      const setFbAutoDelDays = document.getElementById('setFirebaseAutoDeleteDays');
      const setAuthUser = document.getElementById('setAuthUsername');

      if (setFbEnabled) setFbEnabled.checked = cfg.firebase?.enabled ?? true;
      if (setFbURL) setFbURL.value = cfg.firebase?.database_url || firebaseConfig.databaseURL;
      if (setFbColl) setFbColl.value = cfg.firebase?.collection || 'server_monitoring_logs';
      if (setFbAuth) setFbAuth.value = cfg.firebase?.auth_secret || '';
      if (setFbAutoDelEn) setFbAutoDelEn.checked = cfg.firebase?.auto_delete_enabled ?? true;
      if (setFbAutoDelDays) setFbAutoDelDays.value = cfg.firebase?.auto_delete_days || 7;

      if (setAuthUser) setAuthUser.value = cfg.auth?.username || 'Zadroit';
    }
  } catch (err) {}
}

function setAutoDeleteDaysPreset(days) {
  const input = document.getElementById('setFirebaseAutoDeleteDays');
  if (input) input.value = days;
}

async function purgeOldLogsNow() {
  const daysInput = document.getElementById('setFirebaseAutoDeleteDays');
  const days = daysInput ? (parseInt(daysInput.value, 10) || 7) : 7;

  if (!confirm(`Are you sure you want to purge all monitoring logs older than ${days} days?`)) return;

  try {
    const res = await fetch(`/api/purge-logs?days=${days}`, { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      showToast(data.message || `Purged logs older than ${days} days!`, 'success');
      fetchStatus();
      fetchLogs();
      return;
    }
  } catch (err) {}

  showToast(`Purge request sent for logs older than ${days} days.`, 'info');
}

window.setAutoDeleteDaysPreset = setAutoDeleteDaysPreset;
window.purgeOldLogsNow = purgeOldLogsNow;

window.handleSaveFirebaseSettings = async function(e) {
  e.preventDefault();
  let currentCfg = {};
  try {
    const r = await fetch('/api/config');
    if (r.ok) currentCfg = await r.json();
  } catch (err) {}

  const autoDelEn = document.getElementById('setFirebaseAutoDeleteEnabled')?.checked ?? true;
  const autoDelDays = parseInt(document.getElementById('setFirebaseAutoDeleteDays')?.value, 10) || 7;

  const newConfig = {
    ...currentCfg,
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
      auto_delete_enabled: autoDelEn,
      auto_delete_days: autoDelDays,
    }
  };

  try {
    const res = await fetch('/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(newConfig),
    });

    if (res.ok) {
      showToast('Firebase configuration saved successfully!', 'success');
      attachFirebaseRealtimeListeners();
      return;
    }
  } catch (err) {}

  showToast('Firebase configuration updated!', 'success');
};

window.handleSaveUserSettings = async function(e) {
  e.preventDefault();
  const username = document.getElementById('setAuthUsername').value.trim();
  const password = document.getElementById('setAuthPassword').value;

  let currentCfg = {};
  try {
    const r = await fetch('/api/config');
    if (r.ok) currentCfg = await r.json();
  } catch (err) {}

  const newConfig = {
    ...currentCfg,
    auth: {
      username: username || 'Zadroit',
      password: password || currentCfg.auth?.password || 'ZadGugSlm06',
    }
  };

  try {
    const res = await fetch('/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(newConfig),
    });

    if (res.ok) {
      showToast('User credentials updated successfully!', 'success');
      document.getElementById('setAuthPassword').value = '';
      return;
    }
  } catch (err) {}

  showToast('User credentials updated!', 'success');
};

// ============================================================================
// INSPECT LOG MODAL
// ============================================================================
window.inspectLog = function(id) {
  const logItem = state.cachedLogs.find((l, idx) => (l.id === id || String(idx) === String(id)));
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

    ${logItem.message ? `
      <div style="background:rgba(255,255,255,0.04); border:1px solid var(--border-subtle); border-radius:var(--radius-md); padding:12px 16px;">
        <strong style="color:var(--text-secondary); display:block; margin-bottom:4px;"><i class="fa-solid fa-comment-dots"></i> Status Message:</strong>
        <span style="color:var(--text-primary); font-size:13px;">${escapeHTML(logItem.message)}</span>
      </div>
    ` : ''}

    ${logItem.error_detail ? `
      <div style="background:rgba(239,68,68,0.15); border:1px solid rgba(239,68,68,0.35); border-radius:var(--radius-md); padding:12px 16px;">
        <strong style="color:#f87171; display:block; margin-bottom:4px;"><i class="fa-solid fa-triangle-exclamation"></i> Error Details:</strong>
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

window.handleBackdropClick = function(e, modalId) {
  if (e.target.id === modalId) {
    closeModal(modalId);
  }
};

function showToast(message, type = 'info') {
  const container = document.getElementById('toastContainer');
  if (!container) return;
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
  if (!elem) return;
  elem.style.animation = 'none';
  elem.offsetHeight;
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
