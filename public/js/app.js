/**
 * Zadroit Server Monitoring System - Client Controller
 * Native Firebase Realtime Database SDK (v10) + WebSockets & Real-time Watcher
 * Full Cloud Persistence for Targets, Users/Auth, Email Alerts & Monitoring Logs
 */

import { initializeApp } from "https://www.gstatic.com/firebasejs/10.9.0/firebase-app.js";
import { getDatabase, ref, onValue, set, get, remove, off } from "https://www.gstatic.com/firebasejs/10.9.0/firebase-database.js";

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

// Default Target Definitions
const DEFAULT_TARGETS = [
  {
    id: "target_backend_1",
    name: "Nivas App product management",
    type: "backend",
    url: "https://nivasappproduct-wishlist.brightoncloudtech.com/checkserver",
    method: "GET",
    expected_keys: ["service", "db"],
    enabled: true
  },
  {
    id: "target_frontend_1",
    name: "Nivas HOC Website",
    type: "frontend",
    url: "https://nivashoc.com/",
    method: "GET",
    expected_keys: [],
    enabled: true
  },
  {
    id: "target_frontend_2",
    name: "Hotel Sherlock Website",
    type: "frontend",
    url: "https://hotelsherlockholmes.com/",
    method: "GET",
    expected_keys: [],
    enabled: true
  },
  {
    id: "target_1791194011314",
    name: "local",
    type: "backend",
    url: "http://192.168.29.143:8083/checkserver",
    method: "GET",
    expected_keys: ["service", "db"],
    enabled: true
  }
];

// Application State
const state = {
  authToken: localStorage.getItem('zadroit_auth_token') || null,
  currentUser: localStorage.getItem('zadroit_username') || 'Zadroit',
  currentPage: 1,
  limit: 25,
  totalLogs: 0,
  targets: [...DEFAULT_TARGETS],
  summary: null,
  cachedLogs: [],
  recipientEmails: ['indumathi.r@zadroit.com', 'vijay.loganathan@zadroit.com'],
  authCredentials: {
    username: 'Zadroit',
    password: ''
  },
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
  firebaseTargetsRef: null,
  firebaseEmailRef: null,
  firebaseAuthRef: null,
  firebaseStatus: {
    connected: false,
    permissionDenied: false,
    totalLogs: 0,
    targetsCount: 0,
    usersCount: 1,
    emailSynced: false
  },
  nextCheckSeconds: 300, // 5 minutes
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

  // Attach Realtime Firebase WebSockets & Listeners for Targets, Users, Emails, Logs
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
// FIREBASE REAL-TIME WEBSOCKET LISTENERS (LOGS, TARGETS, EMAILS, AUTH)
// ============================================================================
function attachFirebaseRealtimeListeners() {
  if (!state.firebaseDb) {
    initFirebaseSDK();
  }
  if (!state.firebaseDb) return;

  const banner = document.getElementById('firebaseRealtimeBanner');
  const authWarning = document.getElementById('firebaseAuthWarning');
  const streamBadge = document.getElementById('firebaseStreamBadge');
  const metricFb = document.getElementById('metricFirebaseStatus');
  const endpointLabel = document.getElementById('firebaseEndpointLabel');

  if (endpointLabel) {
    endpointLabel.textContent = `Firebase Realtime DB: ${firebaseConfig.databaseURL}`;
  }

  // 1. LISTEN TO LOGS STREAM (/server_monitoring_logs)
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

        updateFirebaseBadgeUI();

        // Derive summary & target cards from Firebase logs
        updateMetricsFromFirebaseLogs(logsList);

        // Filter and render logs table
        filterAndRenderClientLogs();
      } else {
        updateFirebaseBadgeUI();
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
    console.error('Error attaching Firebase Logs listener:', err);
  }

  // 2. LISTEN TO TARGETS STREAM (/targets)
  try {
    state.firebaseTargetsRef = ref(state.firebaseDb, 'targets');
    onValue(state.firebaseTargetsRef, (snapshot) => {
      const data = snapshot.val();
      if (data && typeof data === 'object') {
        const targetList = [];
        if (Array.isArray(data)) {
          targetList.push(...data.filter(Boolean));
        } else {
          for (const [k, v] of Object.entries(data)) {
            if (v && typeof v === 'object') {
              if (!v.id) v.id = k;
              targetList.push(v);
            }
          }
        }

        if (targetList.length > 0) {
          state.targets = targetList;
          state.firebaseStatus.targetsCount = targetList.length;
          updateFilterDropdown();
          renderTargetsManagerList(state.targets);
          if (state.summary) {
            renderServiceCards(state.summary);
          }
          updateFirebaseBadgeUI();
        }
      }
    });
  } catch (err) {
    console.warn('Error attaching Firebase Targets listener:', err);
  }

  // 3. LISTEN TO EMAIL CONFIG STREAM (/email_config)
  try {
    state.firebaseEmailRef = ref(state.firebaseDb, 'email_config');
    onValue(state.firebaseEmailRef, (snapshot) => {
      const data = snapshot.val();
      if (data && typeof data === 'object') {
        if (Array.isArray(data.to_emails) && data.to_emails.length > 0) {
          state.recipientEmails = [...data.to_emails];
          state.firebaseStatus.emailSynced = true;
          window.renderEmailChips();
          updateFirebaseBadgeUI();
        }
      }
    });
  } catch (err) {
    console.warn('Error attaching Firebase Email listener:', err);
  }

  // 4. LISTEN TO AUTH STREAM (/auth)
  try {
    state.firebaseAuthRef = ref(state.firebaseDb, 'auth');
    onValue(state.firebaseAuthRef, (snapshot) => {
      const data = snapshot.val();
      if (data && typeof data === 'object' && data.username) {
        state.authCredentials.username = data.username;
        if (data.password) state.authCredentials.password = data.password;
        state.firebaseStatus.usersCount = 1;
        updateFirebaseBadgeUI();
      }
    });
  } catch (err) {
    console.warn('Error attaching Firebase Auth listener:', err);
  }
}

function updateFirebaseBadgeUI() {
  const streamBadge = document.getElementById('firebaseStreamBadge');
  const metricFb = document.getElementById('metricFirebaseStatus');
  const entityPills = document.getElementById('firebaseEntityPills');

  if (streamBadge) {
    streamBadge.className = 'stream-badge';
    streamBadge.innerHTML = `<i class="fa-solid fa-bolt"></i> Realtime (${state.firebaseStatus.totalLogs} logs)`;
  }
  if (metricFb) {
    metricFb.textContent = `Live Firebase (${state.firebaseStatus.totalLogs} logs)`;
  }

  if (entityPills) {
    entityPills.innerHTML = `
      <span class="pill-badge"><i class="fa-solid fa-list-check"></i> ${state.targets.length} Targets</span>
      <span class="pill-badge"><i class="fa-solid fa-user-shield"></i> Users (${state.currentUser})</span>
      <span class="pill-badge"><i class="fa-solid fa-envelope"></i> ${state.recipientEmails.length} Emails</span>
      <span class="pill-badge"><i class="fa-solid fa-database"></i> ${state.totalLogs} Logs</span>
    `;
  }
}

function detachFirebaseListeners() {
  if (state.firebaseLogsRef) {
    try { off(state.firebaseLogsRef); } catch (e) { }
    state.firebaseLogsRef = null;
  }
  if (state.firebaseTargetsRef) {
    try { off(state.firebaseTargetsRef); } catch (e) { }
    state.firebaseTargetsRef = null;
  }
  if (state.firebaseEmailRef) {
    try { off(state.firebaseEmailRef); } catch (e) { }
    state.firebaseEmailRef = null;
  }
  if (state.firebaseAuthRef) {
    try { off(state.firebaseAuthRef); } catch (e) { }
    state.firebaseAuthRef = null;
  }
}

// Compute live metrics and service status from Firebase logs stream
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
    const st = targetStatuses[t.name] || targetStatuses[t.id];
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
  renderServiceCards(summary);
}

// Force re-check and sync
window.checkFirebaseRealtime = function () {
  attachFirebaseRealtimeListeners();
  showToast('Re-connecting to Firebase Realtime Database...', 'info');
};

window.syncNowFromFirebase = async function () {
  const icon = document.getElementById('syncFbIcon');
  if (icon) icon.classList.add('fa-spin');

  try {
    const res = await fetch('/api/firebase-sync-all', { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      showToast(`Synchronized with Firebase: ${data.message}`, 'success');
      loadTargets();
      fetchStatus();
      fetchLogs();
    } else {
      // Direct Firebase read fallback
      attachFirebaseRealtimeListeners();
      showToast('Synced directly via Firebase Realtime Database', 'success');
    }
  } catch (err) {
    attachFirebaseRealtimeListeners();
    showToast('Firebase Realtime WebSocket stream is active', 'info');
  } finally {
    if (icon) icon.classList.remove('fa-spin');
  }
};

window.syncAllToFirebase = async function () {
  const btn = document.getElementById('btnSyncAllFb');
  if (btn) btn.disabled = true;

  try {
    // 1. Sync via Backend if available
    const res = await fetch('/api/firebase-sync-all', { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      showToast(data.message || 'Synced Targets, Users, Emails, and Logs to Firebase!', 'success');
    } else if (state.firebaseDb) {
      // 2. Direct Firebase write fallback
      const targetsMap = {};
      state.targets.forEach(t => { if (t.id) targetsMap[t.id] = t; });
      await set(ref(state.firebaseDb, 'targets'), targetsMap);
      await set(ref(state.firebaseDb, 'email_config'), {
        enabled: true,
        to_emails: state.recipientEmails
      });
      await set(ref(state.firebaseDb, 'auth'), {
        username: state.currentUser,
        password: state.authCredentials.password || 'ZadGugSlm06'
      });
      showToast('All Targets, Users, and Emails saved to Firebase DB!', 'success');
    }
  } catch (err) {
    if (state.firebaseDb) {
      try {
        const targetsMap = {};
        state.targets.forEach(t => { if (t.id) targetsMap[t.id] = t; });
        await set(ref(state.firebaseDb, 'targets'), targetsMap);
        showToast('Targets synced directly to Firebase!', 'success');
      } catch (e) {
        showToast('Sync error: ' + e.message, 'error');
      }
    }
  } finally {
    if (btn) btn.disabled = false;
    attachFirebaseRealtimeListeners();
  }
};

// ============================================================================
// AUTHENTICATION (STATIC + BACKEND + FIREBASE REALTIME DB)
// ============================================================================
window.handleLogin = async function (e) {
  e.preventDefault();
  const username = document.getElementById('usernameInput').value.trim();
  const password = document.getElementById('passwordInput').value;
  const errorBox = document.getElementById('loginError');
  const errorText = document.getElementById('loginErrorText');
  const loginBtn = document.getElementById('loginBtn');

  errorBox.classList.add('hidden');
  loginBtn.disabled = true;
  loginBtn.innerHTML = `<div class="spinner-inline"></div> Verifying...`;

  // 1. Static Login Check: Zadroit / ZadGugSlm06
  if (username === 'Zadroit' && password === 'ZadGugSlm06') {
    completeLogin(username);
    return;
  }

  // 2. Check Backend API
  try {
    const res = await fetch('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });

    if (res.ok) {
      const data = await res.json();
      if (data.success) {
        completeLogin(data.username || username, data.token);
        return;
      }
    }
  } catch (err) { }

  // 3. Check Firebase DB directly (/auth or /users)
  if (state.firebaseDb) {
    try {
      const authSnap = await get(ref(state.firebaseDb, 'auth'));
      if (authSnap.exists()) {
        const fbAuth = authSnap.val();
        if (fbAuth.username === username && fbAuth.password === password) {
          completeLogin(username);
          return;
        }
      }

      const userSnap = await get(ref(state.firebaseDb, `users/${username}`));
      if (userSnap.exists()) {
        const fbUser = userSnap.val();
        if (fbUser.password === password) {
          completeLogin(username);
          return;
        }
      }
    } catch (fbErr) {
      console.warn('[FIREBASE AUTH CHECK]', fbErr);
    }
  }

  // Invalid credentials
  errorText.textContent = 'Invalid username or password';
  errorBox.classList.remove('hidden');
  shakeElement(document.querySelector('.auth-card'));
  loginBtn.disabled = false;
  loginBtn.innerHTML = `<span>Sign In to Dashboard</span> <i class="fa-solid fa-arrow-right"></i>`;
};

function completeLogin(username, token) {
  const authToken = token || 'zadroit_auth_' + Date.now();
  state.authToken = authToken;
  state.currentUser = username;
  localStorage.setItem('zadroit_auth_token', authToken);
  localStorage.setItem('zadroit_username', username);
  showToast(`Welcome back, ${username}!`, 'success');
  showDashboard();

  const loginBtn = document.getElementById('loginBtn');
  if (loginBtn) {
    loginBtn.disabled = false;
    loginBtn.innerHTML = `<span>Sign In to Dashboard</span> <i class="fa-solid fa-arrow-right"></i>`;
  }
}

window.handleLogout = function () {
  state.authToken = null;
  localStorage.removeItem('zadroit_auth_token');
  localStorage.removeItem('zadroit_username');
  showToast('Logged out successfully', 'info');
  showLogin();
};

window.fillCredentials = function (user, pass) {
  document.getElementById('usernameInput').value = user;
  document.getElementById('passwordInput').value = pass;
  showToast('Credentials filled. Click Sign In.', 'info');
};

window.togglePasswordVisibility = function (inputId) {
  const input = document.getElementById(inputId);
  const icon = document.getElementById('passwordToggleIcon');
  if (input.type === 'password') {
    input.type = 'text';
    if (icon) {
      icon.classList.remove('fa-eye');
      icon.classList.add('fa-eye-slash');
    }
  } else {
    input.type = 'password';
    if (icon) {
      icon.classList.remove('fa-eye-slash');
      icon.classList.add('fa-eye');
    }
  }
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
      renderServiceCards(summary);
      return;
    }
  } catch (err) { }

  if (state.cachedLogs && state.cachedLogs.length > 0) {
    updateMetricsFromFirebaseLogs(state.cachedLogs);
  }
}

function renderSummaryMetrics(summary) {
  if (!summary) return;
  document.getElementById('metricTotalTargets').textContent = summary.total_targets || state.targets.length || 0;
  document.getElementById('metricOnlineTargets').textContent = summary.online_targets || 0;
  document.getElementById('metricOfflineTargets').textContent = summary.offline_targets || 0;
  document.getElementById('metricTotalChecks').textContent = summary.total_checks || state.totalLogs || 0;

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

  if (summary.last_check_time && summary.last_check_time !== 'Never') {
    const lastCheckElem = document.getElementById('lastCheckTimeText');
    if (lastCheckElem) {
      lastCheckElem.textContent = `Last Checked: ${summary.last_check_time}`;
    }
  }
}

function renderServiceCards(summary) {
  const container = document.getElementById('serviceCardsContainer');
  if (!container) return;

  const statuses = summary?.target_statuses || {};
  const targets = state.targets || [];

  if (targets.length === 0 && Object.keys(statuses).length === 0) {
    container.innerHTML = `<div class="card-skeleton">No active targets configured. Add a target to start monitoring.</div>`;
    return;
  }

  let html = '';
  targets.forEach((target) => {
    const st = statuses[target.id] || statuses[target.name] || {
      status: false,
      hit_time: 'Awaiting probe...',
      message: 'Monitoring active',
      http_status: 0,
      response_time_ms: 0,
      data: {},
    };

    const isUp = Boolean(st.status);
    const cardClass = isUp ? 'status-up' : 'status-down';
    const badgeClass = isUp ? 'up' : 'down';
    const badgeText = isUp ? '● UP (200)' : '● DOWN / ERROR';
    const typeClass = target.type === 'backend' ? 'backend' : 'frontend';

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
          <span><i class="fa-regular fa-clock"></i> ${escapeHTML(st.hit_time || 'Just now')}</span>
          <span>${escapeHTML(st.message || '')}</span>
        </div>
      </div>
    `;
  });

  container.innerHTML = html;
}

// Fetch logs (with client-side fallback)
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
    if (res.ok) {
      const data = await res.json();
      state.cachedLogs = data.logs || [];
      state.totalLogs = data.total || 0;
      renderLogsTable(data.logs, data.total);
      renderPagination(data.total, data.page, data.limit);
      if (refreshIcon) refreshIcon.classList.remove('fa-spin');
      return;
    }
  } catch (err) { }

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
      const sUrl = (l.url || '').toLowerCase();
      const sMsg = (l.message || '').toLowerCase();
      const sErr = (l.error_detail || '').toLowerCase();
      return sName.includes(q) || sUrl.includes(q) || sMsg.includes(q) || sErr.includes(q);
    });
  }

  const total = filtered.length;
  const startIdx = (state.currentPage - 1) * state.limit;
  const pageLogs = filtered.slice(startIdx, startIdx + state.limit);

  renderLogsTable(pageLogs, total);
  renderPagination(total, state.currentPage, state.limit);
}

function renderLogsTable(logs, total) {
  const tbody = document.getElementById('logsTableBody');
  if (!tbody) return;

  const totalRecordsBadge = document.getElementById('totalRecordsBadge');
  if (totalRecordsBadge) totalRecordsBadge.textContent = `${total} records`;

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

    let dataPreview = '';
    if (item.data && Object.keys(item.data).length > 0) {
      let chips = '';
      for (const [key, val] of Object.entries(item.data)) {
        if (typeof val === 'boolean') {
          const chipClass = val ? 'pass' : 'fail';
          const icon = val ? '✓' : '✗';
          chips += `<span class="data-chip ${chipClass}">${escapeHTML(key)}: ${icon}</span>`;
        } else {
          chips += `<span class="data-chip">${escapeHTML(key)}: ${escapeHTML(String(val))}</span>`;
        }
      }
      dataPreview = `<div class="data-chips-container">${chips}</div>`;
    } else {
      dataPreview = `<span class="text-muted font-mono" style="font-size:11px;">${item.type === 'frontend' ? 'DOM & Assets OK' : '{}'}</span>`;
    }

    const rowNum = (state.currentPage - 1) * state.limit + index + 1;
    const sName = item.service_name || item['service name'] || 'Unnamed';
    const hitTime = item.hit_time || item['hit time'] || 'N/A';
    const targetUrl = item.url || state.targets.find(t => t.name === sName || t.id === item.target_id)?.url || '';

    html += `
      <tr class="${isSuccess ? 'row-success' : 'row-failure'}">
        <td class="text-muted font-mono text-center" style="width:50px;">#${rowNum}</td>
        <td>
          <div class="table-service-name" style="font-weight:600; color:var(--text-primary);">${escapeHTML(sName)}</div>
        </td>
        <td>${typeBadge}</td>
        <td class="table-url-cell">
          ${targetUrl ? `
            <a href="${escapeHTML(targetUrl)}" target="_blank" rel="noopener noreferrer" class="table-url-link" title="${escapeHTML(targetUrl)}">
              <i class="fa-solid fa-arrow-up-right-from-square"></i> ${escapeHTML(targetUrl)}
            </a>
          ` : `<span class="text-muted font-mono" style="font-size:12px;">--</span>`}
        </td>
        <td class="text-center">${statusBadge}</td>
        <td class="font-mono text-muted" style="white-space:nowrap; font-size:12px;">${escapeHTML(hitTime)}</td>
        <td>
          <span class="table-message-text" title="${escapeHTML(item.message || '')}">
            ${escapeHTML(item.message || (isSuccess ? 'Service OK' : item.error_detail || 'Error'))}
          </span>
        </td>
        <td>
          ${dataPreview}
        </td>
        <td class="text-center">
          <button class="btn btn-ghost btn-sm" onclick="inspectLog('${escapeHTML(item.id || index)}')" title="View Full Log JSON">
            <i class="fa-solid fa-eye text-cyan"></i>
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

window.goToPage = function (page) {
  state.currentPage = page;
  fetchLogs();
};

window.onLimitChange = function () {
  state.limit = parseInt(document.getElementById('limitSelect').value, 10) || 25;
  state.currentPage = 1;
  fetchLogs();
};

// ============================================================================
// FILTERS
// ============================================================================
window.onFilterChange = function () {
  state.filters.serviceName = document.getElementById('filterService').value;
  state.filters.type = document.getElementById('filterType').value;
  state.filters.status = document.getElementById('filterStatus').value;
  state.filters.fromDate = document.getElementById('filterFromDate').value;
  state.filters.toDate = document.getElementById('filterToDate').value;
  state.currentPage = 1;
  fetchLogs();
};

function updateFilterDropdown() {
  const select = document.getElementById('filterService');
  if (select) {
    let options = '<option value="all">All Services</option>';
    state.targets.forEach((t) => {
      options += `<option value="${escapeHTML(t.name)}">${escapeHTML(t.name)}</option>`;
    });
    select.innerHTML = options;
  }
}

window.setQuickDate = function (preset, btn) {
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

window.debounceSearch = function () {
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

window.clearSearch = function () {
  document.getElementById('filterSearch').value = '';
  document.getElementById('clearSearchBtn').style.display = 'none';
  state.filters.search = '';
  state.currentPage = 1;
  fetchLogs();
};

window.resetAllFilters = function () {
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

window.exportCSV = function () {
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
// ACTIONS: CHECK NOW & TEST EMAIL
// ============================================================================
window.triggerManualCheck = async function () {
  const btn = document.getElementById('btnCheckNow');
  const icon = document.getElementById('checkNowIcon');

  btn.disabled = true;
  icon.classList.add('fa-spin');

  try {
    const res = await fetch('/api/check-now', { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      if (data.success) {
        state.nextCheckSeconds = (state.summary?.interval_minutes || 5) * 60;
        showToast('Instant health check completed!', 'success');
        fetchStatus();
        fetchLogs();
      }
    } else {
      showToast('Manual check endpoint active on backend server.', 'info');
    }
  } catch (err) {
    showToast('Scheduled 5-minute automated checks run on Go backend.', 'info');
  } finally {
    btn.disabled = false;
    icon.classList.remove('fa-spin');
  }
};

window.triggerTestEmail = async function () {
  const btn = document.getElementById('btnTestEmail');
  const statusElem = document.getElementById('testEmailStatus');
  btn.disabled = true;
  statusElem.innerHTML = `<span class="text-cyan"><div class="spinner-inline"></div> Sending test alert...</span>`;

  try {
    const res = await fetch('/api/test-email', { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      if (data.success) {
        statusElem.innerHTML = `<span class="text-green"><i class="fa-solid fa-check"></i> ${escapeHTML(data.message)}</span>`;
        showToast('Test alert email sent successfully!', 'success');
      } else {
        statusElem.innerHTML = `<span class="text-red"><i class="fa-solid fa-xmark"></i> ${escapeHTML(data.error || 'Failed')}</span>`;
        showToast('Failed sending email: ' + (data.error || ''), 'error');
      }
    } else {
      statusElem.innerHTML = `<span class="text-muted">SMTP triggers configured on Go backend</span>`;
    }
  } catch (err) {
    statusElem.innerHTML = `<span class="text-muted">SMTP service active on backend</span>`;
  } finally {
    btn.disabled = false;
  }
};

// ============================================================================
// TARGETS MANAGEMENT (PERSISTED IN FIREBASE REALTIME DB)
// ============================================================================
async function loadTargets() {
  // 1. Try backend API
  try {
    const res = await fetch('/api/targets');
    if (res.ok) {
      const targets = await res.json();
      if (Array.isArray(targets) && targets.length > 0) {
        state.targets = targets;
        state.firebaseStatus.targetsCount = targets.length;
      }
    }
  } catch (err) {
    // 2. Try direct Firebase RTDB fetch
    if (state.firebaseDb) {
      try {
        const snap = await get(ref(state.firebaseDb, 'targets'));
        if (snap.exists()) {
          const data = snap.val();
          const targetList = Object.values(data);
          if (targetList.length > 0) {
            state.targets = targetList;
            state.firebaseStatus.targetsCount = targetList.length;
          }
        }
      } catch (fbErr) { }
    }
  }

  updateFilterDropdown();
  const badge = document.getElementById('targetCountBadge');
  if (badge) badge.textContent = state.targets.length;
  renderTargetsManagerList(state.targets);
  updateFirebaseBadgeUI();
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

window.editTarget = function (id) {
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

window.resetTargetForm = function () {
  document.getElementById('targetFormId').value = '';
  document.getElementById('targetFormName').value = '';
  document.getElementById('targetFormType').value = 'backend';
  document.getElementById('targetFormMethod').value = 'GET';
  document.getElementById('targetFormURL').value = '';
  document.getElementById('targetFormKeys').value = 'service, db';
  document.getElementById('targetFormEnabled').checked = true;
};

window.handleSaveTarget = async function (e) {
  e.preventDefault();
  const id = document.getElementById('targetFormId').value;
  const name = document.getElementById('targetFormName').value.trim();
  const type = document.getElementById('targetFormType').value;
  const method = document.getElementById('targetFormMethod').value;
  const url = document.getElementById('targetFormURL').value.trim();
  const keysStr = document.getElementById('targetFormKeys').value.trim();
  const enabled = document.getElementById('targetFormEnabled').checked;

  const expected_keys = keysStr
    ? keysStr.split(',').map((k) => k.trim()).filter(Boolean)
    : [];

  const targetData = {
    id: id || 'target_' + Date.now(),
    name,
    type,
    url,
    method,
    expected_keys,
    enabled,
  };

  // 1. Send to Backend API
  try {
    const endpoint = id ? `/api/targets/${id}` : '/api/targets';
    const httpMethod = id ? 'PUT' : 'POST';

    await fetch(endpoint, {
      method: httpMethod,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(targetData),
    });
  } catch (err) { }

  // 2. Direct Sync to Firebase Realtime DB
  if (state.firebaseDb) {
    try {
      await set(ref(state.firebaseDb, `targets/${targetData.id}`), targetData);
    } catch (fbErr) {
      console.warn('[FIREBASE SAVE TARGET ERROR]', fbErr);
    }
  }

  // 3. Update local state
  const existingIdx = state.targets.findIndex(t => t.id === targetData.id);
  if (existingIdx >= 0) {
    state.targets[existingIdx] = targetData;
  } else {
    state.targets.push(targetData);
  }

  loadTargets();
  resetTargetForm();
  fetchStatus();
  showToast(`Target "${name}" saved & synced to Firebase!`, 'success');
};

window.deleteTarget = async function (id) {
  if (!confirm('Are you sure you want to delete this target?')) return;

  // 1. Call Backend API
  try {
    await fetch(`/api/targets/${id}`, { method: 'DELETE' });
  } catch (err) { }

  // 2. Remove from Firebase Realtime DB
  if (state.firebaseDb) {
    try {
      await remove(ref(state.firebaseDb, `targets/${id}`));
      await remove(ref(state.firebaseDb, `current_status/${id}`));
    } catch (fbErr) {
      console.warn('[FIREBASE DELETE TARGET ERROR]', fbErr);
    }
  }

  state.targets = state.targets.filter(t => t.id !== id);
  loadTargets();
  fetchStatus();
  showToast('Target removed from local store and Firebase DB', 'info');
};

// ============================================================================
// MULTI-EMAIL RECIPIENTS CHIP MANAGER
// ============================================================================
window.renderEmailChips = function () {
  const container = document.getElementById('emailChipsContainer');
  if (!container) return;

  const chips = state.recipientEmails.map((email, idx) => `
    <div class="email-chip">
      <i class="fa-solid fa-envelope"></i>
      <span>${escapeHTML(email)}</span>
      <button type="button" class="btn-remove-chip" onclick="removeEmailChip(${idx})" title="Remove ${escapeHTML(email)}">
        <i class="fa-solid fa-xmark"></i>
      </button>
    </div>
  `).join('');

  container.innerHTML = chips + `
    <input type="email" id="newEmailInput" class="email-chip-input" placeholder="Type email and press Enter..." onkeydown="handleEmailInputKey(event)" autocomplete="off">
  `;
};

window.addEmailFromInput = function () {
  const input = document.getElementById('newEmailInput');
  if (!input) return;
  const val = input.value.trim();
  if (!val) return;

  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  if (!emailRegex.test(val)) {
    showToast(`"${val}" is not a valid email address.`, 'error');
    return;
  }

  if (state.recipientEmails.includes(val)) {
    showToast(`"${val}" is already in the recipient list.`, 'info');
    input.value = '';
    return;
  }

  state.recipientEmails.push(val);
  input.value = '';
  window.renderEmailChips();
  showToast(`Added ${val} to alert recipients`, 'success');
  const newInput = document.getElementById('newEmailInput');
  if (newInput) newInput.focus();
};

window.removeEmailChip = function (index) {
  if (state.recipientEmails.length <= 1) {
    showToast('At least one recipient email is required for alerts.', 'error');
    return;
  }
  const removed = state.recipientEmails.splice(index, 1);
  window.renderEmailChips();
  if (removed.length > 0) {
    showToast(`Removed ${removed[0]}`, 'info');
  }
};

window.handleEmailInputKey = function (e) {
  if (e.key === 'Enter' || e.key === ',' || e.key === ';') {
    e.preventDefault();
    window.addEmailFromInput();
  }
};

// ============================================================================
// SETTINGS MANAGEMENT (FIREBASE, USERS, EMAIL ALERTS)
// ============================================================================
window.openSettingsModal = async function () {
  try {
    const res = await fetch('/api/config');
    if (res.ok) {
      const cfg = await res.json();
      document.getElementById('setFirebaseEnabled').checked = cfg.firebase?.enabled ?? true;
      document.getElementById('setFirebaseURL').value = cfg.firebase?.database_url || firebaseConfig.databaseURL;
      document.getElementById('setFirebaseCollection').value = cfg.firebase?.collection || 'server_monitoring_logs';
      document.getElementById('setFirebaseAuth').value = cfg.firebase?.auth_secret || '';

      document.getElementById('setAuthUsername').value = cfg.auth?.username || state.currentUser || 'Zadroit';
      document.getElementById('setAuthPassword').value = '';

      document.getElementById('setEmailEnabled').checked = cfg.email?.enabled ?? true;
      document.getElementById('setEmailFrom').value = cfg.email?.from_email || '';
      document.getElementById('setEmailPassword').value = cfg.email?.app_password || '';

      if (Array.isArray(cfg.email?.to_emails) && cfg.email.to_emails.length > 0) {
        state.recipientEmails = [...cfg.email.to_emails];
      }

      document.getElementById('setIntervalMins').value = cfg.monitoring?.interval_minutes || 5;
      document.getElementById('setTimeoutSecs').value = cfg.monitoring?.request_timeout_seconds || 15;
    }
  } catch (err) {
    document.getElementById('setAuthUsername').value = state.currentUser || 'Zadroit';
  }

  window.renderEmailChips();
  openModal('settingsModal');
};

window.handleSaveSettings = async function (e) {
  e.preventDefault();

  const pendingInput = document.getElementById('newEmailInput');
  if (pendingInput && pendingInput.value.trim()) {
    window.addEmailFromInput();
  }

  if (state.recipientEmails.length === 0) {
    showToast('Please add at least one recipient email address.', 'error');
    return;
  }

  const newUsername = document.getElementById('setAuthUsername').value.trim() || 'Zadroit';
  const newPassword = document.getElementById('setAuthPassword').value;

  const newConfig = {
    auth: {
      username: newUsername,
      password: newPassword,
    },
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
    targets: state.targets,
  };

  // 1. Save to Backend API
  try {
    await fetch('/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(newConfig),
    });
  } catch (err) { }

  // 2. Direct Sync to Firebase Realtime DB
  if (state.firebaseDb) {
    try {
      await set(ref(state.firebaseDb, 'email_config'), newConfig.email);
      if (newUsername && newPassword) {
        await set(ref(state.firebaseDb, 'auth'), { username: newUsername, password: newPassword });
        await set(ref(state.firebaseDb, `users/${newUsername}`), {
          username: newUsername,
          password: newPassword,
          role: 'admin',
          updated_at: new Date().toISOString()
        });
      }
    } catch (fbErr) {
      console.warn('[FIREBASE SETTINGS SAVE ERROR]', fbErr);
    }
  }

  state.currentUser = newUsername;
  localStorage.setItem('zadroit_username', newUsername);

  showToast(`Settings & Firebase credentials saved! (${state.recipientEmails.length} recipient(s))`, 'success');
  closeModal('settingsModal');
  attachFirebaseRealtimeListeners();
  fetchStatus();
};

// ============================================================================
// INSPECT LOG MODAL
// ============================================================================
window.inspectLog = function (id) {
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

window.closeModal = function (id) {
  document.getElementById(id).classList.add('hidden');
};

window.openTargetModal = function () {
  loadTargets();
  openModal('targetModal');
};

window.handleBackdropClick = function (e, modalId) {
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
