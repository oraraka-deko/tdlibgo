// Telegram Web App Client Logic

(function () {
  'use strict';

  // Application State
  const state = {
    auth: null,
    user: null,
    connection: 'Disconnected',
    chats: [],
    currentFilter: 'all',
    searchQuery: '',
    selectedChatId: null,
    messages: {}, // chatID -> Message[]
    loadingOlder: false,
    hasMoreOlder: {}, // chatID -> boolean
    replyingTo: null, // { id, sender, text }
  };

  let ws = null;
  let wsReconnectTimer = null;

  // DOM Elements
  const el = {
    // Auth elements
    authScreen: document.getElementById('auth-screen'),
    authTitle: document.getElementById('auth-title'),
    authSubtitle: document.getElementById('auth-subtitle'),
    authErrorBanner: document.getElementById('auth-error-banner'),
    phoneForm: document.getElementById('auth-phone-form'),
    phoneInput: document.getElementById('phone-input'),
    codeForm: document.getElementById('auth-code-form'),
    codeInput: document.getElementById('code-input'),
    codeSentTarget: document.getElementById('code-sent-target'),
    btnEditPhone: document.getElementById('btn-edit-phone'),
    passwordForm: document.getElementById('auth-password-form'),
    passwordInput: document.getElementById('password-input'),
    btnTogglePwd: document.getElementById('btn-toggle-pwd'),
    connBadge: document.getElementById('conn-badge'),

    // App layout elements
    appContainer: document.getElementById('app-container'),
    chatList: document.getElementById('chat-list'),
    chatSearch: document.getElementById('chat-search'),
    btnClearSearch: document.getElementById('btn-clear-search'),
    chatTabs: document.querySelectorAll('.chat-tabs .tab-item'),
    badgeAll: document.getElementById('badge-all'),

    // Side Drawer
    btnMainMenu: document.getElementById('btn-main-menu'),
    menuDrawer: document.getElementById('menu-drawer'),
    menuDrawerBackdrop: document.getElementById('menu-drawer-backdrop'),
    drawerUserName: document.getElementById('drawer-user-name'),
    drawerUserPhone: document.getElementById('drawer-user-phone'),
    drawerUserAvatar: document.getElementById('drawer-user-avatar'),
    btnToggleTheme: document.getElementById('btn-toggle-theme'),
    themeText: document.getElementById('theme-text'),
    btnLogout: document.getElementById('btn-logout'),

    // Active Chat
    noChatState: document.getElementById('no-chat-state'),
    activeChatContent: document.getElementById('active-chat-content'),
    headerAvatar: document.getElementById('header-avatar'),
    headerTitle: document.getElementById('header-title'),
    headerSubtitle: document.getElementById('header-subtitle'),
    btnChatBack: document.getElementById('btn-chat-back'),
    messageStream: document.getElementById('message-stream'),
    btnScrollBottom: document.getElementById('btn-scroll-bottom'),
    messageInput: document.getElementById('message-input'),
    btnSendMessage: document.getElementById('btn-send-message'),

    // Reply Dock Bar
    replyDockBar: document.getElementById('reply-dock-bar'),
    replyDockTitle: document.getElementById('reply-dock-title'),
    replyDockSnippet: document.getElementById('reply-dock-snippet'),
    btnCancelReply: document.getElementById('btn-cancel-reply'),

    // Emoji Drawer
    btnEmoji: document.getElementById('btn-emoji'),
    emojiDrawer: document.getElementById('emoji-drawer'),
    emojiGrid: document.getElementById('emoji-grid'),
    emojiTabs: document.querySelectorAll('.emoji-tab'),

    // Media Lightbox
    mediaLightbox: document.getElementById('media-lightbox'),
    lightboxBackdrop: document.getElementById('lightbox-backdrop'),
    btnCloseLightbox: document.getElementById('btn-close-lightbox'),
    lightboxMediaWrapper: document.getElementById('lightbox-media-wrapper'),
    lightboxDownloadLink: document.getElementById('lightbox-download-link'),

    // Right Info Drawer
    btnInfoDrawer: document.getElementById('btn-info-drawer'),
    infoDrawer: document.getElementById('info-drawer'),
    btnCloseInfo: document.getElementById('btn-close-info'),
    infoAvatar: document.getElementById('info-avatar'),
    infoName: document.getElementById('info-name'),
    infoStatus: document.getElementById('info-status'),
    infoPhone: document.getElementById('info-phone'),
    infoPhoneRow: document.getElementById('info-phone-row'),
    infoUsername: document.getElementById('info-username'),
    infoUsernameRow: document.getElementById('info-username-row'),
  };

  // Helper Functions
  function getInitials(name) {
    if (!name) return 'TG';
    const parts = name.trim().split(/\s+/);
    if (parts.length >= 2) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return name.slice(0, 2).toUpperCase();
  }

  function getAvatarColorClass(id) {
    const abs = Math.abs(id || 0);
    return 'avatar-color-' + (abs % 7);
  }

  function formatTime(dateStr) {
    if (!dateStr) return '';
    const d = new Date(dateStr);
    if (isNaN(d.getTime())) return '';
    const now = new Date();
    const isToday = d.toDateString() === now.toDateString();
    if (isToday) {
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
    }
    return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  }

  function formatDateDivider(dateStr) {
    const d = new Date(dateStr);
    const now = new Date();
    if (d.toDateString() === now.toDateString()) return 'Today';
    const yesterday = new Date(now);
    yesterday.setDate(now.getDate() - 1);
    if (d.toDateString() === yesterday.toDateString()) return 'Yesterday';
    return d.toLocaleDateString([], { month: 'long', day: 'numeric', year: 'numeric' });
  }

  function formatFileSize(bytes) {
    if (!bytes || bytes <= 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  function formatDuration(sec) {
    if (!sec || isNaN(sec)) return '0:00';
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return `${m}:${s < 10 ? '0' : ''}${s}`;
  }

  function isArchive(fileName, mimeType) {
    const fn = (fileName || '').toLowerCase();
    const mime = (mimeType || '').toLowerCase();
    return fn.endsWith('.zip') || fn.endsWith('.rar') || fn.endsWith('.7z') || fn.endsWith('.tar') || fn.endsWith('.gz') || fn.endsWith('.bz2') || mime.includes('zip') || mime.includes('tar') || mime.includes('compressed');
  }

  function setReplyTo(msg) {
    let previewText = msg.text || '';
    if (!previewText && msg.media) {
      previewText = msg.media.type === 'photo' ? '📷 Photo' : (msg.media.type === 'sticker' ? (msg.media.alt_emoji || 'Sticker') : '📁 Media');
    }
    state.replyingTo = {
      id: msg.id,
      sender: msg.sender_name || 'User',
      text: previewText,
    };
    el.replyDockTitle.textContent = `Replying to ${state.replyingTo.sender}`;
    el.replyDockSnippet.textContent = state.replyingTo.text;
    el.replyDockBar.classList.remove('hidden');
    el.messageInput.focus();
  }

  function clearReply() {
    state.replyingTo = null;
    el.replyDockBar.classList.add('hidden');
  }

  function openLightbox(opts) {
    el.lightboxMediaWrapper.innerHTML = '';
    if (opts.type === 'video') {
      const vid = document.createElement('video');
      vid.src = opts.src;
      vid.controls = true;
      vid.autoplay = true;
      el.lightboxMediaWrapper.appendChild(vid);
    } else {
      const img = document.createElement('img');
      img.src = opts.src;
      img.alt = 'Media';
      el.lightboxMediaWrapper.appendChild(img);
    }
    el.lightboxDownloadLink.href = opts.downloadUrl || opts.src;
    el.mediaLightbox.classList.remove('hidden');
  }

  function closeLightbox() {
    const v = el.lightboxMediaWrapper.querySelector('video');
    if (v) v.pause();
    el.lightboxMediaWrapper.innerHTML = '';
    el.mediaLightbox.classList.add('hidden');
  }

  // Emoji Categories Data
  const EMOJI_CATEGORIES = {
    smileys: [
      '😀','😃','😄','😁','😆','😅','😂','🤣','😊','😇','🙂','🙃','😉','😌','😍','🥰','😘','😗','😙','😚','😋','😛','😝','😜','🤪','🤨','🧐','🤓','😎','🤩','🥳','😏','😒','😞','😔','😟','😕','🙁','☹️','😣','😖','😫','😩','🥺','😢','😭','😤','😠','😡','🤬','🤯','😳','🥵','🥶','😱','😨','😰','😥','😓','🤗','🤔','🤭','🤫','🤥','😶','😐','😑','😬','🙄','😯','😦','😧','😮','😲','🥱','😴','🤤','😪','😵','🤐','🥴','🤢','🤮','🤧','😷','🤒','🤕'
    ],
    gestures: [
      '👍','👎','👌','✌️','🤞','🤟','🤘','🤙','👈','👉','👆','🖕','👇','☝️','✋','🤚','🖐','🖖','👋','🤙','💪','🦾','🖕','✍️','🙏','🤝','👐','🙌','🤲','👏','🤜','🤛','✊','👊'
    ],
    hearts: [
      '❤️','🧡','💛','💚','💙','💜','🖤','🤍','🤎','💔','❣️','💕','💞','💓','💗','💖','💘','💝','💟','☮️','✝️','☪️','🕉','☸️','✡️','🔯','🕎','☯️','☦️','🛐','⛎','♈️','♉️','♊️','♋️','♌️','♍️','♎️','♏️','♐️','♑️','♒️','♓️'
    ],
    animals: [
      '🐶','🐱','🐭','🐹','🐰','🦊','🐻','🐼','🐨','🐯','🦁','🐮','🐷','🐽','🐸','🐵','🙈','🙉','🙊','🐒','🐔','🐧','🐦','🐤','🐣','🐥','🦆','🦅','🦉','🦇','🐺','🐗','🐴','🦄','🐝','🐛','🦋','🐌','🐞','🐜','🦟','🐢','🐍','🦎','🐙','🦑','🦐','🦞','🦀','🐡','🐠','🐟','🐬','🐳','🦈'
    ],
    objects: [
      '💡','🔦','🕯','📱','💻','⌨️','🖥','🖨','📷','📹','🎥','📞','📟','📠','📺','📻','⏰','⏱','🧭','🔋','🔌','💎','🔧','🔨','⚙️','📦','✉️','📝','📅','📌','📎','🔑','🔒','🎉','🎊','🎁','🎈','🏆','🥇','🚀','🚗','✈️'
    ],
  };

  // WebSocket Connection
  function initWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;

    if (ws) {
      try { ws.close(); } catch (e) {}
    }

    ws = new WebSocket(wsUrl);

    ws.onopen = function () {
      console.log('[WS] Connected to Telegram server');
      updateConnBadge('connected');
    };

    ws.onmessage = function (evt) {
      try {
        const msg = JSON.parse(evt.data);
        handleWSMessage(msg);
      } catch (e) {
        console.error('[WS] Failed to parse message', e);
      }
    };

    ws.onclose = function () {
      console.log('[WS] Connection closed. Reconnecting in 3s...');
      updateConnBadge('connecting');
      clearTimeout(wsReconnectTimer);
      wsReconnectTimer = setTimeout(initWebSocket, 3000);
    };

    ws.onerror = function (err) {
      console.error('[WS] Error:', err);
    };
  }

  function sendWS(type, payload) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type, payload }));
    }
  }

  function updateConnBadge(status) {
    if (status === 'connected') {
      el.connBadge.className = 'badge badge-connected';
      el.connBadge.textContent = 'Connected';
    } else {
      el.connBadge.className = 'badge badge-connecting';
      el.connBadge.textContent = 'Connecting...';
    }
  }

  // Handle Incoming WS Messages
  function handleWSMessage(msg) {
    switch (msg.type) {
      case 'initial_state':
        if (msg.payload.state) {
          state.auth = msg.payload.state.auth;
          state.connection = msg.payload.state.connection;
          state.user = msg.payload.state.user;
          updateAuthUI();
          updateUserProfileUI();
        }
        if (msg.payload.chats) {
          state.chats = msg.payload.chats;
          renderChatList();
        }
        break;

      case 'auth_state':
        state.auth = msg.payload;
        updateAuthUI();
        break;

      case 'connection_state':
        state.connection = msg.payload;
        if (state.connection === 'Ready') {
          updateConnBadge('connected');
        } else {
          updateConnBadge('connecting');
        }
        break;

      case 'user_profile':
        state.user = msg.payload;
        updateUserProfileUI();
        break;

      case 'chat_updated':
        upsertChatInList(msg.payload);
        break;

      case 'new_message':
        handleNewMessage(msg.payload);
        break;

      case 'edit_message':
        handleEditMessage(msg.payload);
        break;

      case 'delete_messages':
        handleDeleteMessages(msg.payload);
        break;

      case 'user_typing':
        handleUserTyping(msg.payload);
        break;

      case 'user_status':
        handleUserStatus(msg.payload);
        break;
    }
  }

  // Authentication UI Flow
  function updateAuthUI() {
    if (!state.auth) return;

    if (state.auth.error) {
      el.authErrorBanner.textContent = state.auth.error;
      el.authErrorBanner.classList.remove('hidden');
    } else {
      el.authErrorBanner.classList.add('hidden');
    }

    if (state.auth.is_logged_in || state.auth.state === 'Ready') {
      // User is authenticated! Show main app
      el.authScreen.classList.add('hidden');
      el.appContainer.classList.remove('hidden');
      fetchChats();
      return;
    }

    // Still in auth flow
    el.authScreen.classList.remove('hidden');
    el.appContainer.classList.add('hidden');

    el.phoneForm.classList.add('hidden');
    el.codeForm.classList.add('hidden');
    el.passwordForm.classList.add('hidden');

    switch (state.auth.state) {
      case 'Idle':
      case 'SendingCode':
        el.phoneForm.classList.remove('hidden');
        if (state.auth.phone) {
          el.phoneInput.value = state.auth.phone;
        }
        el.authTitle.textContent = 'Sign in to Telegram';
        el.authSubtitle.textContent = 'Please confirm your phone number.';
        break;

      case 'WaitingCode':
        el.codeForm.classList.remove('hidden');
        el.codeSentTarget.textContent = state.auth.phone || el.phoneInput.value;
        el.authTitle.textContent = 'Enter Code';
        el.authSubtitle.textContent = `A verification code was sent to your Telegram app.`;
        el.codeInput.focus();
        break;

      case 'WaitingPassword':
        el.passwordForm.classList.remove('hidden');
        el.authTitle.textContent = 'Two-Step Verification';
        el.authSubtitle.textContent = 'This account is protected with a 2FA cloud password.';
        el.passwordInput.focus();
        break;

      case 'Error':
        el.phoneForm.classList.remove('hidden');
        break;
    }
  }

  function updateUserProfileUI() {
    if (!state.user) return;
    const name = [state.user.first_name, state.user.last_name].filter(Boolean).join(' ') || 'Telegram User';
    el.drawerUserName.textContent = name;
    el.drawerUserPhone.textContent = state.user.phone || '';

    if (state.user.photo_url) {
      el.drawerUserAvatar.innerHTML = `<img src="${state.user.photo_url}" alt="Profile" class="avatar-img" />`;
    } else {
      el.drawerUserAvatar.textContent = getInitials(name);
    }
  }

  // REST API Calls
  async function fetchChats() {
    try {
      const res = await fetch(`/api/chats?type=${state.currentFilter}`);
      const data = await res.json();
      if (data.chats) {
        state.chats = data.chats;
        renderChatList();
      }
    } catch (e) {
      console.error('Failed to fetch chats', e);
    }
  }

  async function fetchMessages(chatId, isInitial = false) {
    try {
      const res = await fetch(`/api/messages?chat_id=${chatId}&limit=50`);
      const data = await res.json();
      if (data.messages) {
        state.messages[chatId] = data.messages;
        if (data.messages.length < 50) {
          state.hasMoreOlder[chatId] = false;
        } else {
          state.hasMoreOlder[chatId] = true;
        }
        renderMessages(chatId);
        if (isInitial) {
          scrollToBottom();
        }
      }
    } catch (e) {
      console.error('Failed to fetch messages', e);
    }
  }

  // Load older messages on upward scroll
  async function loadOlderMessages(chatId) {
    if (state.loadingOlder || state.hasMoreOlder[chatId] === false) return;

    const list = state.messages[chatId] || [];
    const oldest = list.find((m) => !m.pending && m.id > 0);
    if (!oldest) return;

    state.loadingOlder = true;

    // Show loading spinner at top
    let spinner = document.getElementById('history-loading-spinner');
    if (!spinner) {
      spinner = document.createElement('div');
      spinner.id = 'history-loading-spinner';
      spinner.className = 'history-loading-spinner';
      el.messageStream.insertBefore(spinner, el.messageStream.firstChild);
    }

    const prevScrollHeight = el.messageStream.scrollHeight;

    try {
      const res = await fetch(`/api/messages?chat_id=${chatId}&limit=40&offset_id=${oldest.id}`);
      const data = await res.json();
      const olderMessages = data.messages || [];

      if (olderMessages.length < 40) {
        state.hasMoreOlder[chatId] = false;
      }

      if (olderMessages.length > 0) {
        const existingIds = new Set(list.map((m) => m.id));
        const newOnes = olderMessages.filter((m) => !existingIds.has(m.id));

        if (newOnes.length > 0) {
          state.messages[chatId] = [...newOnes, ...list];
          state.messages[chatId].sort((a, b) => a.id - b.id);
          renderMessages(chatId);

          // Preserve exact scroll position
          el.messageStream.scrollTop = el.messageStream.scrollHeight - prevScrollHeight;
        }
      }
    } catch (e) {
      console.error('Failed to load older messages', e);
    } finally {
      const s = document.getElementById('history-loading-spinner');
      if (s) s.remove();
      state.loadingOlder = false;
    }
  }

  // Send Message with robust deduplication and reply support
  async function sendMessage(chatId, text) {
    if (!text.trim()) return;

    const replyTo = state.replyingTo;
    clearReply();

    // Optimistic message with unique local ID
    const tempId = -Date.now();
    const optimisticMsg = {
      id: tempId,
      temp_id: tempId,
      chat_id: chatId,
      sender_id: state.user ? state.user.id : 0,
      sender_name: 'You',
      text: text,
      date: new Date().toISOString(),
      out: true,
      pending: true,
      status: 'sending',
      reply_to_msg_id: replyTo ? replyTo.id : 0,
      reply_to_sender: replyTo ? replyTo.sender : '',
      reply_to_text: replyTo ? replyTo.text : '',
    };

    if (!state.messages[chatId]) {
      state.messages[chatId] = [];
    }
    state.messages[chatId].push(optimisticMsg);
    renderMessages(chatId);
    scrollToBottom();

    try {
      const res = await fetch('/api/messages/send', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          chat_id: chatId,
          text: text,
          reply_to_msg_id: replyTo ? replyTo.id : 0,
        }),
      });
      const sentMsg = await res.json();
      const list = state.messages[chatId] || [];
      const tempIdx = list.findIndex((m) => m.id === tempId || (m.pending && m.text === text));

      if (res.ok && sentMsg.id) {
        const alreadyExists = list.some((m) => m.id === sentMsg.id);
        if (alreadyExists) {
          if (tempIdx >= 0) list.splice(tempIdx, 1);
        } else if (tempIdx >= 0) {
          list[tempIdx] = sentMsg;
        } else {
          list.push(sentMsg);
        }
        renderMessages(chatId);
      } else {
        const errMsg = sentMsg.error || 'Failed to send message';
        console.error('Send error:', errMsg);
        if (tempIdx >= 0) {
          list[tempIdx].pending = false;
          list[tempIdx].status = 'error';
          list[tempIdx].error = errMsg;
          renderMessages(chatId);
        }
        alert('Could not send message: ' + errMsg);
      }
    } catch (e) {
      console.error('Failed to send message', e);
      const list = state.messages[chatId] || [];
      const tempIdx = list.findIndex((m) => m.id === tempId);
      if (tempIdx >= 0) {
        list[tempIdx].pending = false;
        list[tempIdx].status = 'error';
        renderMessages(chatId);
      }
    }
  }

  // Chat List Rendering
  let postSearchTimer = null;

  async function searchGlobalPosts(query) {
    if (!query) return;
    el.chatList.innerHTML = `
      <div class="chat-list-placeholder">
        <div class="spinner-large"></div>
        <p>Searching posts globally...</p>
      </div>`;

    try {
      const res = await fetch(`/api/search/posts?q=${encodeURIComponent(query)}&limit=25`);
      const data = await res.json();
      const posts = data.posts || [];

      if (posts.length === 0) {
        el.chatList.innerHTML = `
          <div class="chat-list-placeholder">
            <p>No posts found for "${escapeHTML(query)}"</p>
          </div>`;
        return;
      }

      el.chatList.innerHTML = '';
      posts.forEach((p) => {
        const card = document.createElement('div');
        card.className = 'post-result-card';
        const initials = getInitials(p.channel_title);
        const avatarHtml = p.photo_url
          ? `<img src="${p.photo_url}" class="avatar-img" />`
          : initials;

        card.innerHTML = `
          <div class="post-header">
            <div class="avatar avatar-color-5" style="width:36px;height:36px;font-size:13px;">
              ${avatarHtml}
            </div>
            <div class="post-channel-info">
              <div class="post-channel-title">${escapeHTML(p.channel_title)}</div>
              ${p.channel_username ? `<div class="post-channel-username">@${escapeHTML(p.channel_username)}</div>` : ''}
            </div>
          </div>
          <div class="post-body">${escapeHTML(p.text)}</div>
          <div class="post-meta">
            <span>${formatTime(p.date)}</span>
            ${p.views ? `<span>👁 ${p.views.toLocaleString()}</span>` : ''}
          </div>
        `;

        card.addEventListener('click', () => {
          // If channel exists in state, select it, otherwise view channel info
          selectChat(p.channel_id);
        });

        el.chatList.appendChild(card);
      });
    } catch (e) {
      console.error('Failed to search posts', e);
      el.chatList.innerHTML = `
        <div class="chat-list-placeholder">
          <p>Error searching posts</p>
        </div>`;
    }
  }

  function renderChatList() {
    const filter = state.currentFilter;
    const query = state.searchQuery.toLowerCase().trim();

    if (filter === 'posts') {
      if (!query) {
        el.chatList.innerHTML = `
          <div class="chat-list-placeholder">
            <div style="font-size:36px;margin-bottom:12px;">✨</div>
            <p style="font-weight:600;color:var(--text-primary);">Global Post Search</p>
            <p style="font-size:13px;max-width:240px;margin-top:6px;text-align:center;">Type keywords above to search public channel posts across Telegram (Premium).</p>
          </div>`;
        return;
      }
      clearTimeout(postSearchTimer);
      postSearchTimer = setTimeout(() => searchGlobalPosts(query), 300);
      return;
    }

    const filtered = state.chats.filter((c) => {
      if (query && !c.title.toLowerCase().includes(query) && !(c.username && c.username.toLowerCase().includes(query))) {
        return false;
      }
      return true;
    });

    el.badgeAll.textContent = state.chats.length;

    if (filtered.length === 0) {
      el.chatList.innerHTML = `
        <div class="chat-list-placeholder">
          <p>No conversations found</p>
        </div>`;
      return;
    }

    el.chatList.innerHTML = '';
    filtered.forEach((chat) => {
      const item = document.createElement('div');
      item.className = 'chat-item' + (chat.id === state.selectedChatId ? ' active' : '');
      item.dataset.id = chat.id;

      const colorClass = getAvatarColorClass(chat.id);
      const initials = getInitials(chat.title);
      const timeStr = formatTime(chat.last_message_date);

      const avatarContent = chat.photo_url
        ? `<img src="${chat.photo_url}" alt="${escapeHTML(chat.title)}" class="avatar-img" />`
        : initials;

      item.innerHTML = `
        <div class="avatar-wrapper">
          <div class="avatar ${colorClass}">${avatarContent}</div>
          ${chat.is_online ? '<div class="avatar-online-dot"></div>' : ''}
        </div>
        <div class="chat-content">
          <div class="chat-row-top">
            <span class="chat-title">${escapeHTML(chat.title)}</span>
            <span class="chat-time">${timeStr}</span>
          </div>
          <div class="chat-row-bottom">
            <span class="chat-preview">
              ${chat.typing_user ? `<em style="color:var(--accent)">${escapeHTML(chat.typing_user)} is typing...</em>` : (chat.top_message_sender ? `<span class="chat-sender">${escapeHTML(chat.top_message_sender)}: </span>` : '') + escapeHTML(chat.top_message_text || '')}
            </span>
            ${chat.unread_count > 0 ? `<span class="chat-badge-unread">${chat.unread_count}</span>` : ''}
          </div>
        </div>
      `;

      item.addEventListener('click', () => selectChat(chat.id));
      el.chatList.appendChild(item);
    });
  }

  function upsertChatInList(chat) {
    const idx = state.chats.findIndex((c) => c.id === chat.id);
    if (idx >= 0) {
      state.chats[idx] = Object.assign({}, state.chats[idx], chat);
    } else {
      state.chats.unshift(chat);
    }
    renderChatList();

    if (chat.id === state.selectedChatId) {
      updateChatHeader(chat);
    }
  }

  function selectChat(chatId) {
    state.selectedChatId = chatId;
    const chat = state.chats.find((c) => c.id === chatId);
    if (!chat) return;

    // Update UI selection
    document.querySelectorAll('.chat-item').forEach((it) => {
      it.classList.toggle('active', it.dataset.id == chatId);
    });

    el.noChatState.classList.add('hidden');
    el.activeChatContent.classList.remove('hidden');

    updateChatHeader(chat);
    renderMessages(chatId);
    fetchMessages(chatId, true);

    // Mark as read
    if (chat.unread_count > 0) {
      chat.unread_count = 0;
      renderChatList();
      fetch('/api/chats/read', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ chat_id: chatId, max_id: chat.top_message_id }),
      });
    }

    el.messageInput.focus();
  }

  function updateChatHeader(chat) {
    el.headerTitle.textContent = chat.title;
    el.headerAvatar.className = 'chat-header-avatar ' + getAvatarColorClass(chat.id);
    el.infoAvatar.className = 'large-avatar ' + getAvatarColorClass(chat.id);

    if (chat.photo_url) {
      el.headerAvatar.innerHTML = `<img src="${chat.photo_url}" alt="${escapeHTML(chat.title)}" class="avatar-img" />`;
      el.infoAvatar.innerHTML = `<img src="${chat.photo_url}" alt="${escapeHTML(chat.title)}" class="avatar-img" />`;
    } else {
      el.headerAvatar.textContent = getInitials(chat.title);
      el.infoAvatar.textContent = getInitials(chat.title);
    }

    if (chat.typing_user) {
      el.headerSubtitle.textContent = `${chat.typing_user} is typing...`;
      el.headerSubtitle.style.color = 'var(--accent)';
    } else if (chat.is_online) {
      el.headerSubtitle.textContent = 'online';
      el.headerSubtitle.style.color = 'var(--online-green)';
    } else {
      el.headerSubtitle.textContent = chat.type === 'channel' ? 'channel' : (chat.type === 'group' ? 'group' : 'last seen recently');
      el.headerSubtitle.style.color = 'var(--text-secondary)';
    }

    // Populate Right Info Drawer
    el.infoName.textContent = chat.title;
    el.infoStatus.textContent = el.headerSubtitle.textContent;

    if (chat.username) {
      el.infoUsernameRow.classList.remove('hidden');
      el.infoUsername.textContent = '@' + chat.username;
    } else {
      el.infoUsernameRow.classList.add('hidden');
    }
  }

  // Render Messages in Active Chat with Rich Media, Replies & Forwards
  function renderMessages(chatId) {
    if (chatId !== state.selectedChatId) return;

    const rawMessages = state.messages[chatId] || [];

    // Deduplicate by message ID
    const seenIds = new Set();
    const messages = [];
    rawMessages.forEach((m) => {
      if (!seenIds.has(m.id)) {
        seenIds.add(m.id);
        messages.push(m);
      }
    });

    el.messageStream.innerHTML = '';

    let lastDate = '';
    messages.forEach((msg) => {
      const msgDate = formatDateDivider(msg.date);
      if (msgDate !== lastDate) {
        lastDate = msgDate;
        const divider = document.createElement('div');
        divider.className = 'date-divider';
        divider.textContent = msgDate;
        el.messageStream.appendChild(divider);
      }

      // Service Notification Message
      if (msg.is_service) {
        const srvRow = document.createElement('div');
        srvRow.className = 'service-message-row';
        srvRow.innerHTML = `<div class="service-message-bubble">${escapeHTML(msg.text)}</div>`;
        el.messageStream.appendChild(srvRow);
        return;
      }

      // Message Row Container
      const row = document.createElement('div');
      row.className = 'message-row ' + (msg.out ? 'outgoing' : 'incoming');

      // Quick Reply Action Button on Hover
      const actions = document.createElement('div');
      actions.className = 'message-actions';
      const btnReply = document.createElement('button');
      btnReply.className = 'btn-msg-action';
      btnReply.title = 'Reply';
      btnReply.innerHTML = `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polyline points="9 17 4 12 9 7"></polyline>
          <path d="M20 18v-2a4 4 0 0 0-4-4H4"></path>
        </svg>
      `;
      btnReply.addEventListener('click', (e) => {
        e.stopPropagation();
        setReplyTo(msg);
      });
      actions.appendChild(btnReply);

      // Message Bubble
      const bubble = document.createElement('div');
      const isSticker = msg.media && msg.media.type === 'sticker';
      bubble.className = 'message-bubble ' + (msg.out ? 'outgoing' : 'incoming') + (isSticker ? ' sticker-bubble' : '');
      bubble.dataset.id = msg.id;

      // Sender Name for Incoming
      if (!msg.out && msg.sender_name && !isSticker) {
        const senderEl = document.createElement('div');
        senderEl.className = 'message-sender-name';
        senderEl.textContent = msg.sender_name;
        bubble.appendChild(senderEl);
      }

      // Forward Header
      if (msg.forward_from) {
        const fwdEl = document.createElement('div');
        fwdEl.className = 'message-forward-header';
        fwdEl.innerHTML = `
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="15 14 20 9 15 4"></polyline>
            <path d="M4 20v-7a4 4 0 0 1 4-4h12"></path>
          </svg>
          <span>Forwarded from <strong class="forward-channel-name">${escapeHTML(msg.forward_from)}</strong></span>
        `;
        bubble.appendChild(fwdEl);
      }

      // Reply Quote
      if (msg.reply_to_msg_id > 0) {
        const replyQuote = document.createElement('div');
        replyQuote.className = 'message-reply-quote';
        replyQuote.innerHTML = `
          <span class="reply-sender">${escapeHTML(msg.reply_to_sender || 'Replied Message')}</span>
          <span class="reply-snippet">${escapeHTML(msg.reply_to_text || 'Click to view original')}</span>
        `;
        replyQuote.addEventListener('click', (e) => {
          e.stopPropagation();
          const targetBubble = document.querySelector(`.message-bubble[data-id="${msg.reply_to_msg_id}"]`);
          if (targetBubble) {
            targetBubble.scrollIntoView({ behavior: 'smooth', block: 'center' });
            targetBubble.classList.remove('highlight-pulse');
            void targetBubble.offsetWidth;
            targetBubble.classList.add('highlight-pulse');
          }
        });
        bubble.appendChild(replyQuote);
      }

      // Rich Media Rendering
      if (msg.media) {
        const m = msg.media;
        switch (m.type) {
          case 'photo': {
            const photoContainer = document.createElement('div');
            photoContainer.className = 'message-media-container message-media-photo';
            const img = document.createElement('img');
            img.src = m.url;
            img.loading = 'lazy';
            img.alt = 'Photo';
            if (m.thumb_url) {
              img.dataset.thumb = m.thumb_url;
              img.onerror = () => { img.src = m.thumb_url; };
            }
            photoContainer.appendChild(img);
            photoContainer.addEventListener('click', () => {
              openLightbox({ type: 'image', src: m.url, downloadUrl: m.url + '&download=1' });
            });
            bubble.appendChild(photoContainer);
            break;
          }

          case 'sticker': {
            const stickerWrapper = document.createElement('div');
            stickerWrapper.className = 'sticker-wrapper';
            stickerWrapper.title = m.alt_emoji || 'Sticker';
            const img = document.createElement('img');
            img.src = m.url;
            img.alt = m.alt_emoji || 'Sticker';
            if (m.thumb_url) {
              img.dataset.thumb = m.thumb_url;
              img.onerror = () => { img.src = m.thumb_url; };
            } else if (m.alt_emoji) {
              img.onerror = () => {
                img.style.display = 'none';
                stickerWrapper.innerHTML = `<span class="sticker-emoji-fallback">${escapeHTML(m.alt_emoji)}</span>`;
              };
            }
            stickerWrapper.appendChild(img);
            if (m.alt_emoji) {
              const badge = document.createElement('span');
              badge.className = 'sticker-emoji-badge';
              badge.textContent = m.alt_emoji;
              stickerWrapper.appendChild(badge);
            }
            bubble.appendChild(stickerWrapper);
            break;
          }

          case 'gif': {
            const gifContainer = document.createElement('div');
            gifContainer.className = 'message-media-container message-media-gif';
            gifContainer.innerHTML = `
              <video autoplay loop muted playsinline src="${m.url}"></video>
              <span class="gif-badge">GIF</span>
            `;
            gifContainer.addEventListener('click', () => {
              openLightbox({ type: 'video', src: m.url, downloadUrl: m.url + '&download=1' });
            });
            bubble.appendChild(gifContainer);
            break;
          }

          case 'video': {
            const vidContainer = document.createElement('div');
            vidContainer.className = 'message-media-container message-media-video';
            const vid = document.createElement('video');
            vid.controls = true;
            vid.playsInline = true;
            vid.src = m.url;
            if (m.thumb_url) vid.poster = m.thumb_url;
            vidContainer.appendChild(vid);
            if (m.duration) {
              const durBadge = document.createElement('span');
              durBadge.className = 'video-duration-badge';
              durBadge.textContent = formatDuration(m.duration);
              vidContainer.appendChild(durBadge);
            }
            bubble.appendChild(vidContainer);
            break;
          }

          case 'document': {
            const fileContainer = document.createElement('div');
            fileContainer.className = 'message-media-file';
            const isArch = isArchive(m.file_name, m.mime_type);
            const iconSvg = isArch
              ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 8v13H3V8"></path><path d="M1 3h22v5H1z"></path><path d="M10 12h4"></path></svg>`
              : `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline><line x1="16" y1="13" x2="8" y2="13"></line><line x1="16" y1="17" x2="8" y2="17"></line></svg>`;

            fileContainer.innerHTML = `
              <div class="file-icon-box ${isArch ? 'archive-icon' : 'doc-icon'}">
                ${iconSvg}
              </div>
              <div class="file-info">
                <div class="file-name" title="${escapeHTML(m.file_name || 'Document')}">${escapeHTML(m.file_name || 'Document')}</div>
                <div class="file-size">${formatFileSize(m.file_size)}</div>
              </div>
              <a href="${m.url}&download=1" class="btn-file-download" title="Download ${escapeHTML(m.file_name || 'file')}" download="${escapeHTML(m.file_name || 'file')}">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path><polyline points="7 10 12 15 17 10"></polyline><line x1="12" y1="15" x2="12" y2="3"></line></svg>
              </a>
            `;
            bubble.appendChild(fileContainer);
            break;
          }

          case 'voice':
          case 'audio': {
            const voiceContainer = document.createElement('div');
            voiceContainer.className = 'message-media-voice';
            voiceContainer.innerHTML = `
              <audio src="${m.url}" preload="none" id="audio-${msg.id}"></audio>
              <button class="voice-play-btn" title="Play audio">
                <svg viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>
              </button>
              <div class="voice-waveform">
                <span class="voice-bar" style="height: 10px;"></span>
                <span class="voice-bar" style="height: 18px;"></span>
                <span class="voice-bar" style="height: 8px;"></span>
                <span class="voice-bar" style="height: 22px;"></span>
                <span class="voice-bar" style="height: 14px;"></span>
                <span class="voice-bar" style="height: 12px;"></span>
                <span class="voice-bar" style="height: 16px;"></span>
              </div>
              <span class="voice-duration">${formatDuration(m.duration)}</span>
            `;
            const audio = voiceContainer.querySelector('audio');
            const playBtn = voiceContainer.querySelector('.voice-play-btn');
            playBtn.addEventListener('click', () => {
              if (audio.paused) {
                audio.play();
                playBtn.innerHTML = `<svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="4" width="4" height="16"></rect><rect x="14" y="4" width="4" height="16"></rect></svg>`;
              } else {
                audio.pause();
                playBtn.innerHTML = `<svg viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>`;
              }
            });
            audio.addEventListener('ended', () => {
              playBtn.innerHTML = `<svg viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>`;
            });
            bubble.appendChild(voiceContainer);
            break;
          }

          case 'poll': {
            if (m.poll) {
              const poll = m.poll;
              const pollContainer = document.createElement('div');
              pollContainer.className = 'message-poll-card';

              let optionsHtml = '';
              (poll.answers || []).forEach((opt) => {
                optionsHtml += `
                  <div class="poll-option ${opt.chosen ? 'chosen' : ''}">
                    <div class="poll-option-bar" style="width: ${opt.percent}%"></div>
                    <div class="poll-option-content">
                      <span class="poll-option-text">${escapeHTML(opt.text)}</span>
                      <span class="poll-option-percent">${opt.percent}%</span>
                    </div>
                  </div>
                `;
              });

              const subText = poll.quiz ? 'Quiz' : (poll.multiple ? 'Multiple Choice Poll' : 'Anonymous Poll');
              pollContainer.innerHTML = `
                <div class="poll-header">
                  <div class="poll-question">${escapeHTML(poll.question)}</div>
                  <div class="poll-subtitle">${subText}</div>
                </div>
                <div class="poll-options">
                  ${optionsHtml}
                </div>
                <div class="poll-footer">
                  <span>${poll.total_voters || 0} votes</span>
                  ${poll.closed ? '<span class="poll-closed-badge">Final Results</span>' : ''}
                </div>
              `;
              bubble.appendChild(pollContainer);
            }
            break;
          }
        }
      }

      // Text Caption / Content
      const isMediaPlaceholder = msg.media && (msg.text === '📷 Photo' || msg.text === 'Sticker' || msg.text === 'GIF' || msg.text === '🎬 Video' || (msg.text && msg.text.startsWith('📊 Poll')));
      if (msg.text && !isMediaPlaceholder && !isSticker) {
        const textEl = document.createElement('div');
        textEl.className = 'message-text';
        textEl.innerHTML = escapeHTML(msg.text);
        bubble.appendChild(textEl);
      }

      // Message Footer (Time & Ticks)
      if (!isSticker) {
        const timeStr = new Date(msg.date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
        let tickHtml = '';
        if (msg.out) {
          if (msg.pending) {
            tickHtml = `<svg class="tick-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"></circle><polyline points="12 7 12 12 15 15"></polyline></svg>`;
          } else {
            tickHtml = `<svg class="tick-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"></polyline></svg>`;
          }
        }

        const footer = document.createElement('div');
        footer.className = 'message-footer';
        footer.innerHTML = `<span>${timeStr}</span>${tickHtml}`;
        bubble.appendChild(footer);
      }

      // Assemble Row based on Outgoing/Incoming
      if (msg.out) {
        row.appendChild(actions);
        row.appendChild(bubble);
      } else {
        row.appendChild(bubble);
        row.appendChild(actions);
      }

      el.messageStream.appendChild(row);
    });
  }

  function handleNewMessage(msg) {
    if (!state.messages[msg.chat_id]) {
      state.messages[msg.chat_id] = [];
    }

    const list = state.messages[msg.chat_id];

    // If outgoing, check if we have a pending optimistic message with matching text
    if (msg.out) {
      const pendingIdx = list.findIndex((m) => m.pending && m.text === msg.text);
      if (pendingIdx >= 0) {
        list[pendingIdx] = msg;
        if (msg.chat_id === state.selectedChatId) {
          renderMessages(msg.chat_id);
        }
        return;
      }
    }

    const exists = list.some((m) => m.id === msg.id);
    if (!exists) {
      list.push(msg);
      if (msg.chat_id === state.selectedChatId) {
        renderMessages(msg.chat_id);
        scrollToBottom();
        // Mark read
        sendWS('read_chat', { chat_id: msg.chat_id, max_id: msg.id });
      }
    }
  }

  function handleEditMessage(msg) {
    const list = state.messages[msg.chat_id];
    if (list) {
      const idx = list.findIndex((m) => m.id === msg.id);
      if (idx >= 0) {
        list[idx] = msg;
        if (msg.chat_id === state.selectedChatId) {
          renderMessages(msg.chat_id);
        }
      }
    }
  }

  function handleDeleteMessages(payload) {
    const { chat_id, message_ids } = payload;
    const idSet = new Set(message_ids);
    if (state.messages[chat_id]) {
      state.messages[chat_id] = state.messages[chat_id].filter((m) => !idSet.has(m.id));
      if (chat_id === state.selectedChatId) {
        renderMessages(chat_id);
      }
    }
  }

  function handleUserTyping(payload) {
    const { chat_id, user_name } = payload;
    const chat = state.chats.find((c) => c.id === chat_id);
    if (chat) {
      chat.typing_user = user_name;
      renderChatList();
      if (chat_id === state.selectedChatId) {
        updateChatHeader(chat);
      }
    }
  }

  function handleUserStatus(payload) {
    const { user_id, is_online } = payload;
    const chat = state.chats.find((c) => c.id === user_id);
    if (chat) {
      chat.is_online = is_online;
      renderChatList();
      if (user_id === state.selectedChatId) {
        updateChatHeader(chat);
      }
    }
  }

  function scrollToBottom() {
    el.messageStream.scrollTop = el.messageStream.scrollHeight;
  }

  function escapeHTML(str) {
    if (!str) return '';
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;')
      .replace(/\n/g, '<br>');
  }

  // Event Listeners Setup
  function setupEventListeners() {
    // Phone Form
    el.phoneForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const phone = el.phoneInput.value.trim();
      if (!phone) return;

      const btn = document.getElementById('btn-send-phone');
      btn.querySelector('.spinner').classList.remove('hidden');
      btn.querySelector('span').classList.add('hidden');

      try {
        await fetch('/api/auth/send-code', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ phone }),
        });
      } catch (err) {
        console.error('Send code error:', err);
      } finally {
        btn.querySelector('.spinner').classList.add('hidden');
        btn.querySelector('span').classList.remove('hidden');
      }
    });

    // Verification Code Form
    el.codeForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const code = el.codeInput.value.trim();
      if (!code) return;

      const btn = document.getElementById('btn-submit-code');
      btn.querySelector('.spinner').classList.remove('hidden');
      btn.querySelector('span').classList.add('hidden');

      try {
        await fetch('/api/auth/submit-code', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ code }),
        });
        sendWS('submit_code', { code });
      } catch (err) {
        console.error('Submit code error:', err);
      } finally {
        btn.querySelector('.spinner').classList.add('hidden');
        btn.querySelector('span').classList.remove('hidden');
      }
    });

    el.btnEditPhone.addEventListener('click', () => {
      el.codeForm.classList.add('hidden');
      el.phoneForm.classList.remove('hidden');
    });

    // 2FA Password Form
    el.passwordForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const password = el.passwordInput.value;
      if (!password) return;

      const btn = document.getElementById('btn-submit-password');
      btn.querySelector('.spinner').classList.remove('hidden');
      btn.querySelector('span').classList.add('hidden');

      try {
        await fetch('/api/auth/submit-password', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ password }),
        });
        sendWS('submit_password', { password });
      } catch (err) {
        console.error('Submit password error:', err);
      } finally {
        btn.querySelector('.spinner').classList.add('hidden');
        btn.querySelector('span').classList.remove('hidden');
      }
    });

    el.btnTogglePwd.addEventListener('click', () => {
      const isPwd = el.passwordInput.type === 'password';
      el.passwordInput.type = isPwd ? 'text' : 'password';
    });

    // Chat search
    el.chatSearch.addEventListener('input', (e) => {
      state.searchQuery = e.target.value;
      el.btnClearSearch.classList.toggle('hidden', !state.searchQuery);
      renderChatList();
    });

    el.btnClearSearch.addEventListener('click', () => {
      el.chatSearch.value = '';
      state.searchQuery = '';
      el.btnClearSearch.classList.add('hidden');
      renderChatList();
    });

    // Filter tabs
    el.chatTabs.forEach((tab) => {
      tab.addEventListener('click', () => {
        el.chatTabs.forEach((t) => t.classList.remove('active'));
        tab.classList.add('active');
        state.currentFilter = tab.dataset.type;
        if (state.currentFilter === 'posts') {
          el.chatSearch.placeholder = 'Search channel posts globally (Premium)...';
          el.chatSearch.focus();
          renderChatList();
        } else {
          el.chatSearch.placeholder = 'Search';
          fetchChats();
        }
      });
    });

    // Menu Drawer
    el.btnMainMenu.addEventListener('click', () => {
      el.menuDrawer.classList.remove('hidden');
      el.menuDrawerBackdrop.classList.remove('hidden');
    });

    const closeDrawer = () => {
      el.menuDrawer.classList.add('hidden');
      el.menuDrawerBackdrop.classList.add('hidden');
    };
    el.menuDrawerBackdrop.addEventListener('click', closeDrawer);

    // Theme Switcher
    el.btnToggleTheme.addEventListener('click', () => {
      const current = document.documentElement.getAttribute('data-theme') || 'dark';
      const next = current === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', next);
      localStorage.setItem('tg_theme', next);
      el.themeText.textContent = next === 'dark' ? 'Night Mode' : 'Day Mode';
    });

    // Logout
    el.btnLogout.addEventListener('click', async () => {
      if (confirm('Log out from Telegram?')) {
        await fetch('/api/auth/logout', { method: 'POST' });
        window.location.reload();
      }
    });

    // Message input & send
    el.messageInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        triggerSend();
      }
    });

    el.btnSendMessage.addEventListener('click', triggerSend);

    function triggerSend() {
      if (!state.selectedChatId) return;
      const text = el.messageInput.value;
      if (!text.trim()) return;
      el.messageInput.value = '';
      el.messageInput.style.height = 'auto';
      sendMessage(state.selectedChatId, text);
    }

    // Auto-resize input
    el.messageInput.addEventListener('input', () => {
      el.messageInput.style.height = 'auto';
      el.messageInput.style.height = Math.min(el.messageInput.scrollHeight, 120) + 'px';
    });

    // Info drawer
    el.btnInfoDrawer.addEventListener('click', () => {
      el.infoDrawer.classList.toggle('hidden');
    });
    el.btnCloseInfo.addEventListener('click', () => {
      el.infoDrawer.classList.add('hidden');
    });

    // Back button for mobile
    el.btnChatBack.addEventListener('click', () => {
      el.activeChatContent.classList.add('hidden');
      el.noChatState.classList.remove('hidden');
      state.selectedChatId = null;
    });

    // Reply Dock
    if (el.btnCancelReply) {
      el.btnCancelReply.addEventListener('click', clearReply);
    }

    // Emoji Drawer
    function renderEmojis(category) {
      if (!el.emojiGrid) return;
      el.emojiGrid.innerHTML = '';
      const list = EMOJI_CATEGORIES[category] || EMOJI_CATEGORIES.smileys;
      list.forEach((emoji) => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'emoji-btn';
        btn.textContent = emoji;
        btn.addEventListener('click', () => {
          insertEmoji(emoji);
        });
        el.emojiGrid.appendChild(btn);
      });
    }

    function insertEmoji(emoji) {
      const input = el.messageInput;
      const start = input.selectionStart || input.value.length;
      const end = input.selectionEnd || input.value.length;
      const before = input.value.substring(0, start);
      const after = input.value.substring(end);
      input.value = before + emoji + after;
      input.selectionStart = input.selectionEnd = start + emoji.length;
      input.focus();
      input.dispatchEvent(new Event('input'));
    }

    if (el.btnEmoji) {
      el.btnEmoji.addEventListener('click', (e) => {
        e.stopPropagation();
        const isHidden = el.emojiDrawer.classList.contains('hidden');
        if (isHidden) {
          renderEmojis('smileys');
          el.emojiDrawer.classList.remove('hidden');
        } else {
          el.emojiDrawer.classList.add('hidden');
        }
      });
    }

    el.emojiTabs.forEach((tab) => {
      tab.addEventListener('click', (e) => {
        e.stopPropagation();
        el.emojiTabs.forEach((t) => t.classList.remove('active'));
        tab.classList.add('active');
        renderEmojis(tab.dataset.category);
      });
    });

    document.addEventListener('click', (e) => {
      if (el.emojiDrawer && !el.emojiDrawer.contains(e.target) && e.target !== el.btnEmoji && !el.btnEmoji.contains(e.target)) {
        el.emojiDrawer.classList.add('hidden');
      }
    });

    // Lightbox Modal
    if (el.btnCloseLightbox) {
      el.btnCloseLightbox.addEventListener('click', closeLightbox);
    }
    if (el.lightboxBackdrop) {
      el.lightboxBackdrop.addEventListener('click', closeLightbox);
    }

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        if (el.mediaLightbox && !el.mediaLightbox.classList.contains('hidden')) {
          closeLightbox();
        }
        if (state.replyingTo) {
          clearReply();
        }
      }
    });

    // Infinite scroll up for older messages & scroll to bottom button
    el.messageStream.addEventListener('scroll', () => {
      if (el.messageStream.scrollTop < 60 && state.selectedChatId) {
        loadOlderMessages(state.selectedChatId);
      }

      const distFromBottom = el.messageStream.scrollHeight - el.messageStream.scrollTop - el.messageStream.clientHeight;
      el.btnScrollBottom.classList.toggle('hidden', distFromBottom < 150);
    });

    el.btnScrollBottom.addEventListener('click', scrollToBottom);
  }

  // Initialize
  function init() {
    const savedTheme = localStorage.getItem('tg_theme') || 'dark';
    document.documentElement.setAttribute('data-theme', savedTheme);
    el.themeText.textContent = savedTheme === 'dark' ? 'Night Mode' : 'Day Mode';

    setupEventListeners();
    initWebSocket();
  }

  window.addEventListener('DOMContentLoaded', init);
})();
