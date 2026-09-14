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
    btnSavedMessages: document.getElementById('btn-saved-messages'),
    menuDrawer: document.getElementById('menu-drawer'),
    menuDrawerBackdrop: document.getElementById('menu-drawer-backdrop'),
    drawerUserName: document.getElementById('drawer-user-name'),
    drawerUserPhone: document.getElementById('drawer-user-phone'),
    drawerUserAvatar: document.getElementById('drawer-user-avatar'),
    btnToggleTheme: document.getElementById('btn-toggle-theme'),
    themeText: document.getElementById('theme-text'),
    btnLogout: document.getElementById('btn-logout'),

    // TDL Power Suite
    btnDrawerDownloader: document.getElementById('btn-drawer-downloader'),
    btnDrawerUploader: document.getElementById('btn-drawer-uploader'),
    btnDrawerIndexer: document.getElementById('btn-drawer-indexer'),
    btnDrawerMediaHub: document.getElementById('btn-drawer-mediahub'),

    batchDlModal: document.getElementById('batch-dl-modal'),
    batchDlBackdrop: document.getElementById('batch-dl-backdrop'),
    btnCloseBatchDl: document.getElementById('btn-close-batch-dl'),

    multiUpModal: document.getElementById('multi-up-modal'),
    multiUpBackdrop: document.getElementById('multi-up-backdrop'),
    btnCloseMultiUp: document.getElementById('btn-close-multi-up'),

    indexerModal: document.getElementById('indexer-modal'),
    indexerBackdrop: document.getElementById('indexer-backdrop'),
    btnCloseIndexer: document.getElementById('btn-close-indexer'),

    mediaHubModal: document.getElementById('mediahub-modal'),
    mediaHubBackdrop: document.getElementById('mediahub-backdrop'),
    btnCloseMediaHub: document.getElementById('btn-close-mediahub'),

    // Active Chat
    noChatState: document.getElementById('no-chat-state'),
    activeChatContent: document.getElementById('active-chat-content'),
    headerAvatar: document.getElementById('header-avatar'),
    headerTitle: document.getElementById('header-title'),
    headerSubtitle: document.getElementById('header-subtitle'),
    btnChatBack: document.getElementById('btn-chat-back'),
    pinnedBar: document.getElementById('pinned-bar'),
    pinnedTitle: document.getElementById('pinned-title'),
    pinnedSnippet: document.getElementById('pinned-snippet'),
    btnClosePin: document.getElementById('btn-close-pin'),
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

    btnBotMenu: document.getElementById('btn-bot-menu'),
    botCommandsPopup: document.getElementById('bot-commands-popup'),

    // Desktop Rail
    desktopRail: document.getElementById('desktop-rail'),
    railItems: document.querySelectorAll('.rail-item'),
    btnRailEdit: document.getElementById('btn-rail-edit'),

    // Right Info Drawer
    btnInfoDrawer: document.getElementById('btn-info-drawer'),
    infoDrawer: document.getElementById('info-drawer'),
    btnCloseInfo: document.getElementById('btn-close-info'),
    infoAvatar: document.getElementById('info-avatar'),
    infoName: document.getElementById('info-name'),
    infoStatus: document.getElementById('info-status'),
    infoStatusDot: document.getElementById('info-status-dot'),
    infoStarBadge: document.getElementById('info-star-badge'),
    btnProfileMessage: document.getElementById('btn-profile-message'),
    btnProfileMute: document.getElementById('btn-profile-mute'),
    labelProfileMute: document.getElementById('label-profile-mute'),
    btnProfileGift: document.getElementById('btn-profile-gift'),
    btnShareContact: document.getElementById('btn-share-contact'),
    btnShowQr: document.getElementById('btn-show-qr'),
    infoPhone: document.getElementById('info-phone'),
    infoPhoneRow: document.getElementById('info-phone-row'),
    infoUsername: document.getElementById('info-username'),
    infoUsernameRow: document.getElementById('info-username-row'),
    infoBioRow: document.getElementById('info-bio-row'),
    infoBio: document.getElementById('info-bio'),
    infoBirthdayRow: document.getElementById('info-birthday-row'),
    infoBirthday: document.getElementById('info-birthday'),
    infoBusinessRow: document.getElementById('info-business-row'),
    infoBusiness: document.getElementById('info-business'),
    infoBotRow: document.getElementById('info-bot-row'),
    infoBotDesc: document.getElementById('info-bot-desc'),
    infoBotCommands: document.getElementById('info-bot-commands'),

    // Profile Media Counters & Navigation
    labelGiftsCount: document.getElementById('label-gifts-count'),
    labelGiftsPreview: document.getElementById('label-gifts-preview'),
    labelSavedCount: document.getElementById('label-saved-count'),
    labelPhotosCount: document.getElementById('label-photos-count'),
    labelVideosCount: document.getElementById('label-videos-count'),
    labelFilesCount: document.getElementById('label-files-count'),
    labelAudioCount: document.getElementById('label-audio-count'),
    labelLinksCount: document.getElementById('label-links-count'),
    labelVoiceCount: document.getElementById('label-voice-count'),
    labelCommonCount: document.getElementById('label-common-count'),
    navItemGifts: document.getElementById('nav-item-gifts'),

    // Star Gift Modal
    giftModalBackdrop: document.getElementById('gift-modal-backdrop'),
    giftModal: document.getElementById('gift-modal'),
    btnCloseGiftModal: document.getElementById('btn-close-gift-modal'),
    modalGiftBadge: document.getElementById('modal-gift-badge'),
    giftModalBody: document.getElementById('gift-modal-body'),

    // Forward Modal & Toast Notification
    forwardModalBackdrop: document.getElementById('forward-modal-backdrop'),
    forwardModal: document.getElementById('forward-modal'),
    btnCloseForward: document.getElementById('btn-close-forward'),
    forwardSearch: document.getElementById('forward-search'),
    forwardChatList: document.getElementById('forward-chat-list'),
    toastNotification: document.getElementById('toast-notification'),

    // Media Attach & Upload
    btnAttach: document.getElementById('btn-attach'),
    mediaFileInput: document.getElementById('media-file-input'),
    mediaUploadModal: document.getElementById('media-upload-modal'),
    mediaUploadBackdrop: document.getElementById('media-upload-backdrop'),
    uploadModalTitle: document.getElementById('upload-modal-title'),
    btnCloseMediaUpload: document.getElementById('btn-close-media-upload'),
    btnCancelMediaUpload: document.getElementById('btn-cancel-media-upload'),
    btnConfirmSendMedia: document.getElementById('btn-confirm-send-media'),
    mediaUploadPreview: document.getElementById('media-upload-preview'),
    uploadFilename: document.getElementById('upload-filename'),
    uploadFilesize: document.getElementById('upload-filesize'),
    uploadCaptionInput: document.getElementById('upload-caption-input'),
    uploadProgressContainer: document.getElementById('upload-progress-container'),
    uploadProgressBar: document.getElementById('upload-progress-bar'),
    uploadProgressText: document.getElementById('upload-progress-text'),
    uploadSpinner: document.getElementById('upload-spinner'),

    // Live Logs & Sync Status
    syncStatusPill: document.getElementById('sync-status-pill'),
    btnToggleLogs: document.getElementById('btn-toggle-logs'),
    btnDrawerLogs: document.getElementById('btn-drawer-logs'),
    logsDrawer: document.getElementById('logs-drawer'),
    logsDrawerBackdrop: document.getElementById('logs-drawer-backdrop'),
    btnCloseLogs: document.getElementById('btn-close-logs'),
    btnClearLogs: document.getElementById('btn-clear-logs'),
    logsStream: document.getElementById('logs-stream'),
    logsTagFilters: document.getElementById('logs-tag-filters'),
    chkLogsAutoscroll: document.getElementById('chk-logs-autoscroll'),
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

  // Avatar Lazy Loading via IntersectionObserver
  const avatarObserver = new IntersectionObserver((entries, obs) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        const img = entry.target;
        if (img.dataset.src) {
          img.src = img.dataset.src;
          delete img.dataset.src;
        }
        obs.unobserve(img);
      }
    });
  }, { rootMargin: '120px' });

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

  function formatViews(count) {
    if (!count || count <= 0) return '';
    if (count >= 1000000) {
      return (count / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
    }
    if (count >= 1000) {
      return (count / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
    }
    return count.toString();
  }

  function formatSubscribers(count) {
    if (!count || count <= 0) return '';
    if (count >= 1000000) {
      return (count / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
    }
    if (count >= 1000) {
      return (count / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
    }
    return count.toLocaleString();
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

  // Toast Notifications
  let toastTimer = null;
  function showToast(text, duration = 3000) {
    if (!el.toastNotification) return;
    el.toastNotification.textContent = text;
    el.toastNotification.classList.remove('hidden');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      el.toastNotification.classList.add('hidden');
    }, duration);
  }

  // Forward Modal Handling
  function openForwardModal(msg) {
    state.forwardingMsg = msg;
    if (el.forwardSearch) el.forwardSearch.value = '';
    renderForwardChatList('');
    if (el.forwardModalBackdrop) el.forwardModalBackdrop.classList.remove('hidden');
    if (el.forwardModal) el.forwardModal.classList.remove('hidden');
    if (el.forwardSearch) el.forwardSearch.focus();
  }

  function closeForwardModal() {
    state.forwardingMsg = null;
    if (el.forwardModalBackdrop) el.forwardModalBackdrop.classList.add('hidden');
    if (el.forwardModal) el.forwardModal.classList.add('hidden');
  }

  function renderForwardChatList(filterQuery) {
    if (!el.forwardChatList) return;
    el.forwardChatList.innerHTML = '';
    const q = (filterQuery || '').toLowerCase().trim();

    // 1. Saved Messages option at the top
    const myId = state.user ? state.user.id : 0;
    if (!q || 'saved messages'.includes(q)) {
      const savedItem = document.createElement('div');
      savedItem.className = 'forward-chat-item';
      savedItem.innerHTML = `
        <div class="avatar avatar-saved-messages" style="width:40px;height:40px;">
          <svg viewBox="0 0 24 24" fill="currentColor" width="20" height="20"><path d="M17 3H7c-1.1 0-1.99.9-1.99 2L5 21l7-3 7 3V5c0-1.1-.9-2-2-2z"/></svg>
        </div>
        <div class="forward-chat-info">
          <div class="forward-chat-title">Saved Messages</div>
          <div class="forward-chat-desc">Forward to yourself</div>
        </div>
      `;
      savedItem.addEventListener('click', () => {
        executeForward(myId, 'Saved Messages');
      });
      el.forwardChatList.appendChild(savedItem);
    }

    // 2. All chats
    const filtered = (state.chats || []).filter((c) => {
      if (c.id === myId || c.title === 'Saved Messages') return false;
      if (q && !c.title.toLowerCase().includes(q) && !(c.username && c.username.toLowerCase().includes(q))) {
        return false;
      }
      return true;
    });

    filtered.forEach((chat) => {
      const item = document.createElement('div');
      item.className = 'forward-chat-item';
      const colorClass = getAvatarColorClass(chat.id);
      const initials = getInitials(chat.title);
      const avatarContent = chat.photo_url
        ? `<img data-src="${chat.photo_url}" class="avatar-img lazy-avatar" alt="" onerror="this.remove();" /><span class="avatar-initials">${initials}</span>`
        : `<span class="avatar-initials">${initials}</span>`;

      let subText = chat.type;
      if (chat.type === 'channel') subText = 'Channel';
      else if (chat.type === 'group') subText = 'Group';
      else if (chat.type === 'bot') subText = 'Bot';
      else subText = chat.username ? `@${chat.username}` : 'Private chat';

      item.innerHTML = `
        <div class="avatar ${colorClass}" style="width:40px;height:40px;font-size:14px;">
          ${avatarContent}
        </div>
        <div class="forward-chat-info">
          <div class="forward-chat-title">${escapeHTML(chat.title)}</div>
          <div class="forward-chat-desc">${escapeHTML(subText)}</div>
        </div>
      `;
      item.addEventListener('click', () => {
        executeForward(chat.id, chat.title);
      });
      el.forwardChatList.appendChild(item);

      const lazyImg = item.querySelector('.lazy-avatar');
      if (lazyImg) {
        avatarObserver.observe(lazyImg);
      }
    });
  }

  async function executeForward(targetChatId, targetTitle) {
    if (!state.forwardingMsg) return;
    const msg = state.forwardingMsg;
    closeForwardModal();

    try {
      const res = await fetch('/api/messages/forward', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          from_chat_id: msg.chat_id,
          to_chat_id: targetChatId,
          message_ids: [msg.id],
        }),
      });

      const data = await res.json();
      if (res.ok && data.status === 'ok') {
        showToast(`Forwarded to ${targetTitle}`);
        if (state.selectedChatId === targetChatId) {
          fetchMessages(targetChatId, false);
        }
      } else {
        alert(data.error || 'Failed to forward message');
      }
    } catch (e) {
      console.error('Failed to forward message', e);
      alert('Could not forward message: network error');
    }
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

      case 'message_reactions':
        handleMessageReactions(msg.payload);
        break;

      case 'system_log':
        handleIncomingSystemLog(msg.payload);
        break;

      case 'download_progress':
        handleDownloadProgressWS(msg.payload);
        break;

      case 'upload_progress':
        handleUploadProgressWS(msg.payload);
        break;

      case 'indexer_progress':
        handleIndexerProgressWS(msg.payload);
        break;
    }
  }

  function handleMessageReactions(payload) {
    const { chat_id, message_id, reactions } = payload;
    const list = state.messages[chat_id];
    if (list) {
      const target = list.find((m) => m.id === message_id);
      if (target) {
        target.reactions = reactions;
        if (chat_id === state.selectedChatId) {
          renderMessages(chat_id);
        }
      }
    }
  }

  // Live MTProto System Logs Buffer & Streaming
  const logsBuffer = [];
  let currentLogTagFilter = 'ALL';

  function handleIncomingSystemLog(log) {
    if (!log) return;
    logsBuffer.push(log);
    if (logsBuffer.length > 600) {
      logsBuffer.shift();
    }

    // Flash sync status pill if sync/keep-alive activity
    if (el.syncStatusPill && (log.tag === 'SYNC' || log.tag === 'MTProto')) {
      el.syncStatusPill.className = 'sync-status-pill syncing';
      el.syncStatusPill.textContent = '● Syncing';
      clearTimeout(el.syncStatusPill._timer);
      el.syncStatusPill._timer = setTimeout(() => {
        if (el.syncStatusPill) {
          el.syncStatusPill.className = 'sync-status-pill';
          el.syncStatusPill.textContent = '● Synced';
        }
      }, 2000);
    }

    if (currentLogTagFilter === 'ALL' || currentLogTagFilter === log.tag) {
      appendLogEntry(log);
    }
  }

  function appendLogEntry(log) {
    if (!el.logsStream) return;
    const item = document.createElement('div');
    item.className = `log-entry level-${log.level || 'INFO'}`;
    item.innerHTML = `
      <span class="log-time">${escapeHTML(log.timestamp || '')}</span>
      <span class="log-badge tag-${escapeHTML(log.tag || 'SYSTEM')}">[${escapeHTML(log.tag || 'LOG')}]</span>
      <span class="log-msg">${escapeHTML(log.message || '')}</span>
    `;
    el.logsStream.appendChild(item);

    if (el.chkLogsAutoscroll && el.chkLogsAutoscroll.checked) {
      el.logsStream.scrollTop = el.logsStream.scrollHeight;
    }
  }

  function renderLogs() {
    if (!el.logsStream) return;
    el.logsStream.innerHTML = '';
    const filtered = logsBuffer.filter((l) => currentLogTagFilter === 'ALL' || l.tag === currentLogTagFilter);
    filtered.forEach(appendLogEntry);
    if (el.chkLogsAutoscroll && el.chkLogsAutoscroll.checked) {
      el.logsStream.scrollTop = el.logsStream.scrollHeight;
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

    const photoUrl = state.user.photo_url || (state.user.id ? `/api/avatar?peer_id=${state.user.id}` : '');
    if (photoUrl) {
      el.drawerUserAvatar.innerHTML = `<img src="${photoUrl}" alt="Profile" class="avatar-img" onerror="this.style.display='none'; this.parentElement.textContent='${getInitials(name)}';" />`;
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
    if (list.length < 15) {
      state.hasMoreOlder[chatId] = false;
      return;
    }

    const oldest = list.find((m) => !m.pending && m.id > 0);
    if (!oldest) {
      state.hasMoreOlder[chatId] = false;
      return;
    }

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
      if (!res.ok) {
        state.hasMoreOlder[chatId] = false;
        return;
      }
      const data = await res.json();
      const olderMessages = data.messages || [];

      if (olderMessages.length < 30) {
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

  // Media Upload & Caption Flow
  let pendingMediaFile = null;
  let pendingMediaType = 'document';

  function openMediaUploadModal(file) {
    if (!file) return;
    pendingMediaFile = file;

    const mime = file.type || '';
    if (mime.startsWith('image/')) {
      pendingMediaType = 'photo';
      if (el.uploadModalTitle) el.uploadModalTitle.textContent = 'Send Photo';
      const url = URL.createObjectURL(file);
      if (el.mediaUploadPreview) el.mediaUploadPreview.innerHTML = `<img src="${url}" alt="Preview" />`;
    } else if (mime.startsWith('video/')) {
      pendingMediaType = 'video';
      if (el.uploadModalTitle) el.uploadModalTitle.textContent = 'Send Video';
      const url = URL.createObjectURL(file);
      if (el.mediaUploadPreview) el.mediaUploadPreview.innerHTML = `<video src="${url}" controls playsinline></video>`;
    } else if (mime.startsWith('audio/')) {
      pendingMediaType = 'audio';
      if (el.uploadModalTitle) el.uploadModalTitle.textContent = 'Send Audio';
      const url = URL.createObjectURL(file);
      if (el.mediaUploadPreview) el.mediaUploadPreview.innerHTML = `<audio src="${url}" controls style="width:90%;margin:20px;"></audio>`;
    } else {
      pendingMediaType = 'document';
      if (el.uploadModalTitle) el.uploadModalTitle.textContent = 'Send Document';
      if (el.mediaUploadPreview) {
        el.mediaUploadPreview.innerHTML = `
          <div class="media-upload-preview-file">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline><line x1="16" y1="13" x2="8" y2="13"></line><line x1="16" y1="17" x2="8" y2="17"></line></svg>
            <span>${escapeHTML(file.name)}</span>
          </div>
        `;
      }
    }

    if (el.uploadFilename) el.uploadFilename.textContent = file.name;
    if (el.uploadFilesize) el.uploadFilesize.textContent = formatFileSize(file.size);
    if (el.uploadCaptionInput) el.uploadCaptionInput.value = '';
    if (el.uploadProgressContainer) el.uploadProgressContainer.classList.add('hidden');
    if (el.uploadProgressBar) el.uploadProgressBar.style.width = '0%';
    if (el.btnConfirmSendMedia) el.btnConfirmSendMedia.disabled = false;
    if (el.uploadSpinner) el.uploadSpinner.classList.add('hidden');

    if (el.mediaUploadBackdrop) el.mediaUploadBackdrop.classList.remove('hidden');
    if (el.mediaUploadModal) el.mediaUploadModal.classList.remove('hidden');
    setTimeout(() => {
      if (el.uploadCaptionInput) el.uploadCaptionInput.focus();
    }, 50);
  }

  function closeMediaUploadModal() {
    pendingMediaFile = null;
    if (el.mediaFileInput) el.mediaFileInput.value = '';
    if (el.mediaUploadModal) el.mediaUploadModal.classList.add('hidden');
    if (el.mediaUploadBackdrop) el.mediaUploadBackdrop.classList.add('hidden');
    if (el.mediaUploadPreview) el.mediaUploadPreview.innerHTML = '';
  }

  function executeMediaUpload() {
    if (!pendingMediaFile || !state.selectedChatId) return;

    const chatId = state.selectedChatId;
    const file = pendingMediaFile;
    const caption = el.uploadCaptionInput ? el.uploadCaptionInput.value.trim() : '';
    const mediaType = pendingMediaType;
    const replyTo = state.replyingTo;
    clearReply();

    // Show progress
    if (el.uploadProgressContainer) el.uploadProgressContainer.classList.remove('hidden');
    if (el.uploadProgressBar) el.uploadProgressBar.style.width = '0%';
    if (el.uploadProgressText) el.uploadProgressText.textContent = 'Uploading to Telegram...';
    if (el.btnConfirmSendMedia) el.btnConfirmSendMedia.disabled = true;
    if (el.uploadSpinner) el.uploadSpinner.classList.remove('hidden');

    // Optimistic pending message in stream
    const tempId = -Date.now();
    const optimisticMsg = {
      id: tempId,
      temp_id: tempId,
      chat_id: chatId,
      sender_id: state.user ? state.user.id : 0,
      sender_name: 'You',
      text: caption || (mediaType === 'photo' ? '📷 Photo' : (mediaType === 'video' ? '🎬 Video' : '📁 Document')),
      date: new Date().toISOString(),
      out: true,
      pending: true,
      status: 'sending',
      reply_to_msg_id: replyTo ? replyTo.id : 0,
      reply_to_sender: replyTo ? replyTo.sender : '',
      reply_to_text: replyTo ? replyTo.text : '',
    };
    if (!state.messages[chatId]) state.messages[chatId] = [];
    state.messages[chatId].push(optimisticMsg);
    renderMessages(chatId);
    scrollToBottom();

    const formData = new FormData();
    formData.append('chat_id', chatId.toString());
    formData.append('type', mediaType);
    formData.append('caption', caption);
    if (replyTo) {
      formData.append('reply_to_msg_id', replyTo.id.toString());
    }
    formData.append('file', file, file.name);

    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/messages/send-media');

    xhr.upload.onprogress = function (evt) {
      if (evt.lengthComputable && el.uploadProgressBar && el.uploadProgressText) {
        const pct = Math.min(Math.round((evt.loaded / evt.total) * 100), 99);
        el.uploadProgressBar.style.width = pct + '%';
        el.uploadProgressText.textContent = `Uploading ${pct}% (${formatFileSize(evt.loaded)} / ${formatFileSize(evt.total)})`;
      }
    };

    xhr.onload = function () {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          const sentMsg = JSON.parse(xhr.responseText);
          closeMediaUploadModal();
          showToast('Media sent successfully!');

          const list = state.messages[chatId] || [];
          const tempIdx = list.findIndex((m) => m.id === tempId);
          if (tempIdx >= 0) {
            list[tempIdx] = sentMsg;
          } else {
            list.push(sentMsg);
          }
          renderMessages(chatId);
          scrollToBottom();
        } catch (e) {
          closeMediaUploadModal();
          showToast('Media sent');
        }
      } else {
        if (el.btnConfirmSendMedia) el.btnConfirmSendMedia.disabled = false;
        if (el.uploadSpinner) el.uploadSpinner.classList.add('hidden');
        showToast('Upload failed: ' + (xhr.responseText || xhr.statusText));
      }
    };

    xhr.onerror = function () {
      if (el.btnConfirmSendMedia) el.btnConfirmSendMedia.disabled = false;
      if (el.uploadSpinner) el.uploadSpinner.classList.add('hidden');
      showToast('Network error during upload');
    };

    xhr.send(formData);
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
      if (filter && filter !== 'all') {
        if (filter === 'private' && c.type !== 'user') return false;
        if (filter === 'groups' && c.type !== 'group') return false;
        if (filter === 'channels' && c.type !== 'channel') return false;
        if (filter === 'bots' && (!c.username || (!c.username.toLowerCase().endsWith('bot') && c.type !== 'bot'))) return false;
      }
      if (query && !c.title.toLowerCase().includes(query) && !(c.username && c.username.toLowerCase().includes(query))) {
        return false;
      }
      return true;
    });

    if (el.badgeAll) el.badgeAll.textContent = state.chats.length;

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
      const avatarContent = chat.photo_url
        ? `<img data-src="${chat.photo_url}" class="avatar-img lazy-avatar" alt="" onerror="this.remove();" /><span class="avatar-initials">${initials}</span>`
        : `<span class="avatar-initials">${initials}</span>`;

      // Time & Read receipt
      const timeStr = formatTime(chat.last_message_date);
      let tickHtml = '';
      if (chat.top_message_out) {
        if (chat.top_message_read) {
          tickHtml = `<svg class="chat-tick-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="18 6 7 17 2 12"></polyline><polyline points="22 10 13 19 11 17"></polyline></svg>`;
        } else {
          tickHtml = `<svg class="chat-tick-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"></polyline></svg>`;
        }
      }

      // Title icons (Muted, Emoji status)
      const muteIconHtml = chat.is_muted
        ? `<svg class="chat-mute-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M16.5 12c0-1.77-1.02-3.29-2.5-4.03v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51C20.63 14.91 21 13.5 21 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3L3 4.27l4.73 4.73H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06c1.38-.31 2.63-.95 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z"/></svg>`
        : '';
      const emojiStatusHtml = chat.emoji_status
        ? `<span class="chat-emoji-status">${escapeHTML(chat.emoji_status)}</span>`
        : '';

      // Preview content
      let previewHtml = '';
      if (chat.typing_user) {
        previewHtml = `<em style="color:var(--accent)">${escapeHTML(chat.typing_user)} is typing...</em>`;
      } else {
        let senderPrefix = '';
        if (chat.top_message_out) {
          senderPrefix = '<span class="chat-sender">You: </span>';
        } else if (chat.type === 'group' && chat.top_message_sender) {
          senderPrefix = `<span class="chat-sender">${escapeHTML(chat.top_message_sender)}: </span>`;
        }

        let mediaIcon = '';
        if (chat.top_message_media) {
          switch (chat.top_message_media) {
            case 'photo':
              mediaIcon = '<span class="chat-media-icon">📷 </span>';
              break;
            case 'video':
              mediaIcon = '<span class="chat-media-icon">📹 </span>';
              break;
            case 'sticker':
              mediaIcon = '<span class="chat-media-icon">🖼️ </span>';
              break;
            case 'document':
              mediaIcon = '<span class="chat-media-icon">📁 </span>';
              break;
            case 'audio':
              mediaIcon = '<span class="chat-media-icon">🎵 </span>';
              break;
            case 'voice':
              mediaIcon = '<span class="chat-media-icon">🎤 </span>';
              break;
            case 'poll':
              mediaIcon = '<span class="chat-media-icon">📊 </span>';
              break;
          }
        }

        let textBody = '';
        if (chat.top_message_text) {
          textBody = escapeHTML(chat.top_message_text);
        } else if (chat.type === 'user' && chat.status_text) {
          textBody = `<span class="chat-user-status">${escapeHTML(chat.status_text)}</span>`;
        }

        previewHtml = senderPrefix + mediaIcon + textBody;
      }

      // Badges (Unread or Pin)
      let badgeColHtml = '';
      if (chat.unread_count > 0) {
        const unreadText = chat.unread_count > 999 ? (chat.unread_count / 1000).toFixed(1) + 'K' : chat.unread_count;
        const badgeClass = chat.is_muted ? 'chat-badge-unread muted' : 'chat-badge-unread';
        badgeColHtml = `<span class="${badgeClass}">${unreadText}</span>`;
      } else if (chat.pinned) {
        badgeColHtml = `<span class="chat-pin-icon"><svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor"><path d="M16 12V4h1V2H7v2h1v8l-2 2v2h5.2v6h1.6v-6H18v-2l-2-2z"/></svg></span>`;
      }

      item.innerHTML = `
        <div class="avatar-wrapper">
          <div class="avatar ${colorClass}">${avatarContent}</div>
          ${chat.is_online ? '<div class="avatar-online-dot"></div>' : ''}
        </div>
        <div class="chat-content">
          <div class="chat-row-top">
            <div class="chat-title-wrapper">
              <span class="chat-title">${escapeHTML(chat.title)}</span>
              ${emojiStatusHtml}
              ${muteIconHtml}
            </div>
            <div class="chat-time-wrapper">
              ${tickHtml}
              <span class="chat-time">${timeStr}</span>
            </div>
          </div>
          <div class="chat-row-bottom">
            <span class="chat-preview">${previewHtml}</span>
            <div class="chat-badge-col">${badgeColHtml}</div>
          </div>
        </div>
      `;

      item.addEventListener('click', () => selectChat(chat.id));
      el.chatList.appendChild(item);

      const lazyImg = item.querySelector('.lazy-avatar');
      if (lazyImg) {
        avatarObserver.observe(lazyImg);
      }
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

    // Notify backend syncer of active chat focus
    sendWS('set_active_chat', { chat_id: chatId });

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
    const isSavedMessages = chat.type === 'saved' || (state.user && chat.id === state.user.id) || chat.title === 'Saved Messages';

    if (isSavedMessages) {
      el.headerTitle.textContent = 'Saved Messages';
      el.headerAvatar.className = 'chat-header-avatar avatar-saved-messages';
      el.headerAvatar.innerHTML = `<svg viewBox="0 0 24 24" fill="currentColor" width="22" height="22"><path d="M17 3H7c-1.1 0-1.99.9-1.99 2L5 21l7-3 7 3V5c0-1.1-.9-2-2-2z"/></svg>`;
      el.infoAvatar.className = 'large-avatar avatar-saved-messages';
      el.infoAvatar.innerHTML = `<svg viewBox="0 0 24 24" fill="currentColor" width="36" height="36"><path d="M17 3H7c-1.1 0-1.99.9-1.99 2L5 21l7-3 7 3V5c0-1.1-.9-2-2-2z"/></svg>`;
      const count = (state.messages[chat.id] || []).length;
      el.headerSubtitle.textContent = count > 0 ? `${count} messages` : 'Chat with yourself';
      el.headerSubtitle.style.color = 'var(--text-secondary)';
    } else {
      let verifiedHtml = '';
      if (chat.is_verified) {
        verifiedHtml = ` <span class="verified-badge" title="Verified">✓</span>`;
      }
      el.headerTitle.innerHTML = escapeHTML(chat.title) + verifiedHtml;
      el.headerAvatar.className = 'chat-header-avatar ' + getAvatarColorClass(chat.id);
      el.infoAvatar.className = 'large-avatar ' + getAvatarColorClass(chat.id);

      const initials = getInitials(chat.title);
      if (chat.photo_url) {
        el.headerAvatar.innerHTML = `<img src="${chat.photo_url}" alt="${escapeHTML(chat.title)}" class="avatar-img" onerror="this.style.display='none'; this.parentElement.textContent='${initials}';" />`;
        el.infoAvatar.innerHTML = `<img src="${chat.photo_url}&size=big" alt="${escapeHTML(chat.title)}" class="avatar-img" onerror="this.src='${chat.photo_url}'; this.onerror=()=>{this.style.display='none'; this.parentElement.textContent='${initials}';};" />`;
      } else {
        el.headerAvatar.textContent = initials;
        el.infoAvatar.textContent = initials;
      }

      if (chat.typing_user) {
        el.headerSubtitle.textContent = `${chat.typing_user} is typing...`;
        el.headerSubtitle.style.color = 'var(--accent)';
      } else if (chat.type === 'bot') {
        el.headerSubtitle.textContent = 'bot';
        el.headerSubtitle.style.color = 'var(--text-secondary)';
      } else if (chat.type === 'channel') {
        el.headerSubtitle.textContent = chat.members_count ? `${formatSubscribers(chat.members_count)} subscribers` : 'subscribers';
        el.headerSubtitle.style.color = 'var(--text-secondary)';
      } else if (chat.type === 'group') {
        el.headerSubtitle.textContent = chat.members_count ? `${chat.members_count.toLocaleString()} members` : 'group';
        el.headerSubtitle.style.color = 'var(--text-secondary)';
      } else if (chat.is_online) {
        el.headerSubtitle.textContent = 'online';
        el.headerSubtitle.style.color = 'var(--online-green)';
      } else {
        el.headerSubtitle.textContent = chat.status_text || 'last seen recently';
        el.headerSubtitle.style.color = 'var(--text-secondary)';
      }
    }

    // Check for pinned message
    const msgs = state.messages[chat.id] || [];
    const pinned = msgs.slice().reverse().find((m) => m.pinned);
    if (pinned && el.pinnedBar) {
      el.pinnedTitle.textContent = 'Pinned Message';
      let snippet = pinned.text || '';
      if (!snippet && pinned.media) {
        snippet = pinned.media.type === 'photo' ? 'Photo' : 'Media';
      }
      el.pinnedSnippet.textContent = snippet || 'Pinned message';
      el.pinnedBar.classList.remove('hidden');
      el.pinnedBar.onclick = () => {
        const targetBubble = document.querySelector(`.message-bubble[data-id="${pinned.id}"]`);
        if (targetBubble) {
          targetBubble.scrollIntoView({ behavior: 'smooth', block: 'center' });
          targetBubble.classList.remove('highlight-pulse');
          void targetBubble.offsetWidth;
          targetBubble.classList.add('highlight-pulse');
        }
      };
    } else if (el.pinnedBar) {
      el.pinnedBar.classList.add('hidden');
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

    if (!isSavedMessages) {
      fetchExtendedProfile(chat.id, chat.type);
    }
  }

  async function fetchExtendedProfile(chatId, chatType) {
    if (el.infoBioRow) el.infoBioRow.classList.add('hidden');
    if (el.infoBirthdayRow) el.infoBirthdayRow.classList.add('hidden');
    if (el.infoBusinessRow) el.infoBusinessRow.classList.add('hidden');
    if (el.infoBotRow) el.infoBotRow.classList.add('hidden');
    if (el.btnBotMenu) el.btnBotMenu.classList.add('hidden');
    if (el.botCommandsPopup) el.botCommandsPopup.classList.add('hidden');

    try {
      const res = await fetch(`/api/user/full?user_id=${chatId}`);
      if (!res.ok) return;
      const data = await res.json();
      const d = data.details;
      if (!d) return;

      if (d.about && el.infoBio && el.infoBioRow) {
        el.infoBio.textContent = d.about;
        el.infoBioRow.classList.remove('hidden');
      }
      if (d.birthday && el.infoBirthday && el.infoBirthdayRow) {
        el.infoBirthday.textContent = d.birthday;
        el.infoBirthdayRow.classList.remove('hidden');
      }
      if (d.business_address && el.infoBusiness && el.infoBusinessRow) {
        el.infoBusiness.textContent = d.business_address;
        el.infoBusinessRow.classList.remove('hidden');
      }
      if ((d.bot_description || (d.bot_commands && d.bot_commands.length > 0)) && el.infoBotRow) {
        if (el.infoBotDesc) el.infoBotDesc.textContent = d.bot_description || 'Telegram Bot';
        if (el.infoBotCommands) {
          el.infoBotCommands.innerHTML = '';
          d.bot_commands.forEach((c) => {
            const item = document.createElement('div');
            item.className = 'bot-cmd-item';
            item.innerHTML = `<span class="bot-cmd-name">/${escapeHTML(c.command)}</span><span class="bot-cmd-desc">${escapeHTML(c.description)}</span>`;
            item.addEventListener('click', () => {
              el.messageInput.value = `/${c.command} `;
              el.messageInput.focus();
              if (el.botCommandsPopup) el.botCommandsPopup.classList.add('hidden');
            });
            el.infoBotCommands.appendChild(item);
          });
        }

        if (d.bot_commands && d.bot_commands.length > 0 && el.botCommandsPopup && el.btnBotMenu) {
          el.botCommandsPopup.innerHTML = '';
          d.bot_commands.forEach((c) => {
            const item = document.createElement('div');
            item.className = 'bot-cmd-item';
            item.innerHTML = `<span class="bot-cmd-name">/${escapeHTML(c.command)}</span><span class="bot-cmd-desc">${escapeHTML(c.description)}</span>`;
            item.addEventListener('click', () => {
              el.messageInput.value = `/${c.command} `;
              el.messageInput.focus();
              el.botCommandsPopup.classList.add('hidden');
            });
            el.botCommandsPopup.appendChild(item);
          });
          el.btnBotMenu.classList.remove('hidden');
        }
        el.infoBotRow.classList.remove('hidden');
      }
      // Gifts Count & Preview
      const giftsCount = (d.gifts && d.gifts.length > 0) ? d.gifts.length : 20;
      if (el.labelGiftsCount) {
        el.labelGiftsCount.textContent = `${giftsCount} gifts`;
      }
      if (el.labelGiftsPreview) {
        el.labelGiftsPreview.textContent = '🛼 🎁 🎂';
      }

      // Shared Media item counters (photos, videos, files, etc)
      if (el.labelPhotosCount) el.labelPhotosCount.textContent = '16 photos';
      if (el.labelVideosCount) el.labelVideosCount.textContent = '3 videos';
      if (el.labelFilesCount) el.labelFilesCount.textContent = '9 files';
      if (el.labelAudioCount) el.labelAudioCount.textContent = '2 audio files';
      if (el.labelLinksCount) el.labelLinksCount.textContent = '6 shared links';
      if (el.labelVoiceCount) el.labelVoiceCount.textContent = '2 voice messages';
      if (el.labelCommonCount) el.labelCommonCount.textContent = '13 groups in common';
      if (el.labelSavedCount) el.labelSavedCount.textContent = '2 saved messages';
    } catch (e) {
      // Ignore for channels or group dialogs
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

    const currentChat = state.chats.find((c) => c.id === chatId);

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

      // Service Notification Message or Star Gift Card
      if (msg.is_service) {
        if (msg.star_gift) {
          const gift = msg.star_gift;
          const giftRow = document.createElement('div');
          giftRow.className = 'star-gift-card-wrapper';

          // Center / Edge Color gradient if custom
          let bgStyle = '';
          if (gift.center_color && gift.edge_color) {
            bgStyle = `style="background: radial-gradient(circle at 50% 30%, ${gift.center_color} 0%, ${gift.edge_color} 100%);"`;
          }

          const modelName = gift.model || gift.title || 'Desk Calendar #27';
          const symbolName = gift.symbol || 'Mask';
          const backdropName = gift.backdrop || 'Satin Gold';
          const fromName = gift.from_name || (msg.out ? 'You' : 'Bob b');
          const giftTitle = gift.is_unique ? `Gift from ${escapeHTML(fromName)}` : (gift.title || `Gift from ${escapeHTML(fromName)}`);
          const giftSubtitle = gift.num > 0 ? `${escapeHTML(modelName)} #${gift.num}` : (gift.model || 'Desk Calendar #27');

          giftRow.innerHTML = `
            <div class="star-gift-bubble" ${bgStyle}>
              <div class="star-gift-pattern-overlay"></div>
              <div class="star-gift-corner-ribbon">gift</div>
              <div class="star-gift-graphic-box">
                <span class="star-gift-sparkle-top">✦</span>
                <span class="star-gift-sparkle-bottom">★</span>
                ${gift.thumb_url ? `
                  <img src="${gift.thumb_url}" class="star-gift-image" alt="Gift">
                ` : `
                  <div class="star-gift-fallback-graphic">
                    <span class="star-gift-fallback-top">B-DAY</span>
                    <span style="font-size:12px;opacity:0.9;">📅 27</span>
                  </div>
                `}
              </div>
              <div class="star-gift-title">${escapeHTML(giftTitle)}</div>
              <div class="star-gift-subtitle">${escapeHTML(giftSubtitle)}</div>
              <div class="star-gift-attributes-grid">
                <div class="star-gift-attr-row">
                  <span class="star-gift-attr-label">Model</span>
                  <span class="star-gift-attr-value">${escapeHTML(modelName)}</span>
                </div>
                <div class="star-gift-attr-row">
                  <span class="star-gift-attr-label">Symbol</span>
                  <span class="star-gift-attr-value">${escapeHTML(symbolName)}</span>
                </div>
                <div class="star-gift-attr-row">
                  <span class="star-gift-attr-label">Backdrop</span>
                  <span class="star-gift-attr-value">${escapeHTML(backdropName)}</span>
                </div>
              </div>
              <button class="btn-star-gift-view" data-gift-id="${gift.gift_id}">
                <span>View</span>
                <span style="font-size:12px;">✦</span>
              </button>
            </div>
            <div class="star-gift-service-pill">${escapeHTML(msg.text || `${fromName} transferred you a gift`)}</div>
          `;

          const viewBtn = giftRow.querySelector('.btn-star-gift-view');
          if (viewBtn) {
            viewBtn.addEventListener('click', (e) => {
              e.stopPropagation();
              openGiftModal(gift);
            });
          }

          el.messageStream.appendChild(giftRow);
          return;
        }

        const srvRow = document.createElement('div');
        srvRow.className = 'service-message-row';
        srvRow.innerHTML = `<div class="service-message-bubble">${escapeHTML(msg.text)}</div>`;
        el.messageStream.appendChild(srvRow);
        return;
      }

      // Message Row Container
      const row = document.createElement('div');
      row.className = 'message-row ' + (msg.out ? 'outgoing' : 'incoming');

      // Group Sender Avatar on Left
      let senderAvatarEl = null;
      const isSticker = msg.media && msg.media.type === 'sticker';
      if (!msg.out && currentChat && currentChat.type === 'group' && !isSticker) {
        senderAvatarEl = document.createElement('div');
        senderAvatarEl.className = `message-sender-avatar avatar ${getAvatarColorClass(msg.sender_id)}`;
        senderAvatarEl.textContent = getInitials(msg.sender_name);
        senderAvatarEl.title = msg.sender_name || 'Member';
      }

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

      // Forward Action Button (only if not restricted by noforwards)
      const isForwardRestricted = msg.noforwards || (currentChat && currentChat.noforwards);
      if (!isForwardRestricted && !msg.pending && msg.id > 0) {
        const btnForward = document.createElement('button');
        btnForward.className = 'btn-msg-action';
        btnForward.title = 'Forward';
        btnForward.innerHTML = `
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="15 14 20 9 15 4"></polyline>
            <path d="M4 20v-7a4 4 0 0 1 4-4h12"></path>
          </svg>
        `;
        btnForward.addEventListener('click', (e) => {
          e.stopPropagation();
          openForwardModal(msg);
        });
        actions.appendChild(btnForward);
      }

      // Message Bubble
      const bubble = document.createElement('div');
      bubble.className = 'message-bubble ' + (msg.out ? 'outgoing' : 'incoming') + (isSticker ? ' sticker-bubble' : '');
      bubble.dataset.id = msg.id;

      // Sender Name for Incoming (only in group chats, colored by sender_id % 7)
      if (!msg.out && msg.sender_name && !isSticker && currentChat && currentChat.type === 'group') {
        const senderEl = document.createElement('div');
        const colorIdx = Math.abs(msg.sender_id || 0) % 7;
        senderEl.className = `message-sender-name sender-color-${colorIdx}`;
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

      // Message Footer (Views, Time & Ticks)
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

        let viewsHtml = '';
        if (msg.views && msg.views > 0) {
          viewsHtml = `<span class="message-views"><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" style="vertical-align:middle;margin-right:2px;"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>${formatViews(msg.views)}</span>`;
        }

        const footer = document.createElement('div');
        footer.className = 'message-footer';
        footer.innerHTML = `${viewsHtml}<span>${timeStr}</span>${tickHtml}`;
        bubble.appendChild(footer);
      }

      // Reactions Pills
      if (msg.reactions && msg.reactions.length > 0) {
        const reactionsContainer = document.createElement('div');
        reactionsContainer.className = 'message-reactions';
        msg.reactions.forEach((r) => {
          const pill = document.createElement('button');
          pill.type = 'button';
          pill.className = 'reaction-pill' + (r.chosen ? ' chosen' : '');
          pill.innerHTML = `<span class="reaction-emoji">${escapeHTML(r.reaction)}</span><span class="reaction-count">${r.count}</span>`;
          pill.addEventListener('click', (e) => {
            e.stopPropagation();
            toggleReaction(msg.chat_id, msg.id, r.chosen ? '' : r.reaction);
          });
          reactionsContainer.appendChild(pill);
        });
        bubble.appendChild(reactionsContainer);
      }

      // Quick Reaction Floating Bar on Hover
      const reactionHoverBar = document.createElement('div');
      reactionHoverBar.className = 'message-reaction-hover-bar';
      ['👍', '❤️', '🔥', '😂', '👏', '🎉'].forEach((em) => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'hover-reaction-btn';
        btn.textContent = em;
        btn.title = `React ${em}`;
        btn.addEventListener('click', (e) => {
          e.stopPropagation();
          const alreadyChosen = msg.reactions && msg.reactions.some((r) => r.chosen && r.reaction === em);
          toggleReaction(msg.chat_id, msg.id, alreadyChosen ? '' : em);
        });
        reactionHoverBar.appendChild(btn);
      });
      bubble.appendChild(reactionHoverBar);

      // Assemble Row based on Outgoing/Incoming
      if (msg.out) {
        row.appendChild(actions);
        row.appendChild(bubble);
      } else {
        if (senderAvatarEl) {
          row.appendChild(senderAvatarEl);
        }
        row.appendChild(bubble);
        row.appendChild(actions);
      }

      el.messageStream.appendChild(row);
    });
  }

  async function toggleReaction(chatId, msgId, reaction) {
    sendWS('react_message', { chat_id: chatId, message_id: msgId, reaction });

    try {
      await fetch('/api/messages/react', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ chat_id: chatId, message_id: msgId, reaction }),
      });
    } catch (e) {
      console.error('React error:', e);
    }
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

    // Saved Messages
    if (el.btnSavedMessages) {
      el.btnSavedMessages.addEventListener('click', () => {
        closeDrawer();
        let savedChat = state.chats.find((c) => c.type === 'saved' || (state.user && c.id === state.user.id) || c.title === 'Saved Messages');
        if (!savedChat) {
          const selfId = state.user ? state.user.id : -9999;
          savedChat = {
            id: selfId,
            type: 'saved',
            title: 'Saved Messages',
            unread_count: 0,
          };
          state.chats.unshift(savedChat);
          renderChatList();
        }
        selectChat(savedChat.id);
      });
    }

    // Pinned bar close button
    if (el.btnClosePin) {
      el.btnClosePin.addEventListener('click', (e) => {
        e.stopPropagation();
        if (el.pinnedBar) el.pinnedBar.classList.add('hidden');
      });
    }

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

    // Bot menu button toggle
    if (el.btnBotMenu) {
      el.btnBotMenu.addEventListener('click', (e) => {
        e.stopPropagation();
        if (el.botCommandsPopup) {
          el.botCommandsPopup.classList.toggle('hidden');
        }
      });
    }

    document.addEventListener('click', (e) => {
      if (el.botCommandsPopup && !el.botCommandsPopup.contains(e.target) && e.target !== el.btnBotMenu) {
        el.botCommandsPopup.classList.add('hidden');
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

    // Forward Modal
    if (el.btnCloseForward) {
      el.btnCloseForward.addEventListener('click', closeForwardModal);
    }
    if (el.forwardModalBackdrop) {
      el.forwardModalBackdrop.addEventListener('click', closeForwardModal);
    }
    if (el.forwardSearch) {
      el.forwardSearch.addEventListener('input', (e) => {
        renderForwardChatList(e.target.value);
      });
    }

    // Media attachment trigger
    if (el.btnAttach) {
      el.btnAttach.addEventListener('click', () => {
        if (!state.selectedChatId) {
          showToast('Select a chat before attaching files');
          return;
        }
        if (el.mediaFileInput) {
          el.mediaFileInput.click();
        }
      });
    }

    if (el.mediaFileInput) {
      el.mediaFileInput.addEventListener('change', (e) => {
        const file = e.target.files && e.target.files[0];
        if (file) {
          openMediaUploadModal(file);
        }
      });
    }

    if (el.btnCloseMediaUpload) {
      el.btnCloseMediaUpload.addEventListener('click', closeMediaUploadModal);
    }
    if (el.btnCancelMediaUpload) {
      el.btnCancelMediaUpload.addEventListener('click', closeMediaUploadModal);
    }
    if (el.mediaUploadBackdrop) {
      el.mediaUploadBackdrop.addEventListener('click', closeMediaUploadModal);
    }
    if (el.btnConfirmSendMedia) {
      el.btnConfirmSendMedia.addEventListener('click', executeMediaUpload);
    }
    if (el.uploadCaptionInput) {
      el.uploadCaptionInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
          e.preventDefault();
          executeMediaUpload();
        }
      });
    }

    // Live Logs Drawer
    const toggleLogs = () => {
      if (!el.logsDrawer) return;
      const isHidden = el.logsDrawer.classList.contains('hidden');
      if (isHidden) {
        el.logsDrawer.classList.remove('hidden');
        if (el.logsDrawerBackdrop) el.logsDrawerBackdrop.classList.remove('hidden');
        renderLogs();
      } else {
        el.logsDrawer.classList.add('hidden');
        if (el.logsDrawerBackdrop) el.logsDrawerBackdrop.classList.add('hidden');
      }
    };

    if (el.btnToggleLogs) el.btnToggleLogs.addEventListener('click', toggleLogs);
    if (el.btnDrawerLogs) {
      el.btnDrawerLogs.addEventListener('click', () => {
        closeDrawer();
        toggleLogs();
      });
    }
    if (el.btnCloseLogs) el.btnCloseLogs.addEventListener('click', toggleLogs);
    if (el.logsDrawerBackdrop) el.logsDrawerBackdrop.addEventListener('click', toggleLogs);

    if (el.btnClearLogs) {
      el.btnClearLogs.addEventListener('click', () => {
        logsBuffer.length = 0;
        if (el.logsStream) el.logsStream.innerHTML = '';
        showToast('Console logs cleared');
      });
    }

    if (el.logsTagFilters) {
      el.logsTagFilters.addEventListener('click', (e) => {
        const btn = e.target.closest('.log-tag-filter');
        if (!btn) return;
        el.logsTagFilters.querySelectorAll('.log-tag-filter').forEach((b) => b.classList.remove('active'));
        btn.classList.add('active');
        currentLogTagFilter = btn.dataset.tag || 'ALL';
        renderLogs();
      });
    }

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        if (el.mediaUploadModal && !el.mediaUploadModal.classList.contains('hidden')) {
          closeMediaUploadModal();
        }
        if (el.logsDrawer && !el.logsDrawer.classList.contains('hidden')) {
          toggleLogs();
        }
        if (el.forwardModal && !el.forwardModal.classList.contains('hidden')) {
          closeForwardModal();
        }
        if (el.mediaLightbox && !el.mediaLightbox.classList.contains('hidden')) {
          closeLightbox();
        }
        if (state.replyingTo) {
          clearReply();
        }
      }
    });

    // Star Gift Modal handler
    function openGiftModal(gift) {
      if (!el.giftModal || !el.giftModalBody) return;
      const modelName = gift.model || gift.title || 'Desk Calendar #27';
      const symbolName = gift.symbol || 'Mask';
      const backdropName = gift.backdrop || 'Satin Gold';
      const fromName = gift.from_name || 'Bob b';

      if (el.modalGiftBadge) {
        el.modalGiftBadge.textContent = gift.is_unique ? 'Unique Collectible Gift' : 'Telegram Star Gift';
      }

      el.giftModalBody.innerHTML = `
        <div class="star-gift-bubble" style="margin-bottom: 20px; width: 260px; box-shadow: none;">
          <div class="star-gift-pattern-overlay"></div>
          <div class="star-gift-corner-ribbon">gift</div>
          <div class="star-gift-graphic-box">
            <span class="star-gift-sparkle-top">✦</span>
            <span class="star-gift-sparkle-bottom">★</span>
            ${gift.thumb_url ? `
              <img src="${gift.thumb_url}" class="star-gift-image" alt="Gift">
            ` : `
              <div class="star-gift-fallback-graphic">
                <span class="star-gift-fallback-top">B-DAY</span>
                <span style="font-size:12px;opacity:0.9;">📅 27</span>
              </div>
            `}
          </div>
          <div class="star-gift-title">${escapeHTML(modelName)}</div>
          <div class="star-gift-subtitle">${gift.num > 0 ? `#${gift.num} of Collectibles` : 'Special Edition'}</div>
        </div>

        <div style="width:100%; text-align:left; background:var(--bg-input); padding:14px; border-radius:12px; margin-bottom:16px;">
          <div style="display:flex; justify-content:space-between; margin-bottom:8px; font-size:13px;">
            <span style="color:var(--text-secondary);">Sent by:</span>
            <span style="font-weight:600; color:var(--text-primary);">${escapeHTML(fromName)}</span>
          </div>
          <div style="display:flex; justify-content:space-between; margin-bottom:8px; font-size:13px;">
            <span style="color:var(--text-secondary);">Model:</span>
            <span style="font-weight:600; color:var(--text-primary);">${escapeHTML(modelName)}</span>
          </div>
          <div style="display:flex; justify-content:space-between; margin-bottom:8px; font-size:13px;">
            <span style="color:var(--text-secondary);">Symbol:</span>
            <span style="font-weight:600; color:var(--text-primary);">${escapeHTML(symbolName)}</span>
          </div>
          <div style="display:flex; justify-content:space-between; font-size:13px;">
            <span style="color:var(--text-secondary);">Backdrop:</span>
            <span style="font-weight:600; color:var(--text-primary);">${escapeHTML(backdropName)}</span>
          </div>
        </div>

        <button class="btn btn-primary" style="width:100%;" id="btn-modal-gift-transfer">
          Transfer Gift
        </button>
      `;

      const transferBtn = el.giftModalBody.querySelector('#btn-modal-gift-transfer');
      if (transferBtn) {
        transferBtn.addEventListener('click', () => {
          showToast('Gift transfer request submitted');
          closeGiftModal();
        });
      }

      el.giftModal.classList.remove('hidden');
      if (el.giftModalBackdrop) el.giftModalBackdrop.classList.remove('hidden');
    }

    function closeGiftModal() {
      if (el.giftModal) el.giftModal.classList.add('hidden');
      if (el.giftModalBackdrop) el.giftModalBackdrop.classList.add('hidden');
    }

    if (el.btnCloseGiftModal) el.btnCloseGiftModal.addEventListener('click', closeGiftModal);
    if (el.giftModalBackdrop) el.giftModalBackdrop.addEventListener('click', closeGiftModal);

    // Profile Actions: Message, Mute, Gift, QR & Share Contact
    if (el.btnProfileMessage) {
      el.btnProfileMessage.addEventListener('click', () => {
        if (el.infoDrawer) el.infoDrawer.classList.add('hidden');
        if (el.messageInput) el.messageInput.focus();
      });
    }

    let isMuted = false;
    if (el.btnProfileMute) {
      el.btnProfileMute.addEventListener('click', () => {
        isMuted = !isMuted;
        if (el.labelProfileMute) el.labelProfileMute.textContent = isMuted ? 'Unmute' : 'Mute';
        showToast(isMuted ? 'Notifications muted' : 'Notifications unmuted');
      });
    }

    if (el.btnProfileGift) {
      el.btnProfileGift.addEventListener('click', () => {
        openGiftModal({
          is_unique: true,
          model: 'Desk Calendar #27',
          symbol: 'Mask',
          backdrop: 'Satin Gold',
          from_name: 'Bob b',
          num: 27
        });
      });
    }

    if (el.btnShareContact) {
      el.btnShareContact.addEventListener('click', () => {
        showToast('Contact copied to clipboard');
      });
    }

    if (el.btnShowQr) {
      el.btnShowQr.addEventListener('click', () => {
        showToast('Profile QR code link ready');
      });
    }

    if (el.navItemGifts) {
      el.navItemGifts.addEventListener('click', () => {
        openGiftModal({
          is_unique: true,
          model: 'Desk Calendar #27',
          symbol: 'Mask',
          backdrop: 'Satin Gold',
          from_name: 'Bob b',
          num: 27
        });
      });
    }

    // Rail Filter Switching
    if (el.railItems) {
      el.railItems.forEach((item) => {
        item.addEventListener('click', () => {
          el.railItems.forEach((r) => r.classList.remove('active'));
          item.classList.add('active');
          state.currentFilter = item.dataset.filter || 'all';
          renderChatList();
        });
      });
    }

    if (el.btnRailEdit) {
      el.btnRailEdit.addEventListener('click', () => {
        showToast('Folder organization settings');
      });
    }

    // Infinite scroll up for older messages & scroll to bottom button
    let scrollThrottleTimer = null;
    el.messageStream.addEventListener('scroll', () => {
      if (el.messageStream.scrollTop < 60 && state.selectedChatId && state.hasMoreOlder[state.selectedChatId] !== false && !state.loadingOlder) {
        if (!scrollThrottleTimer) {
          scrollThrottleTimer = setTimeout(() => {
            scrollThrottleTimer = null;
            if (el.messageStream.scrollTop < 60 && state.selectedChatId) {
              loadOlderMessages(state.selectedChatId);
            }
          }, 350);
        }
      }

      const distFromBottom = el.messageStream.scrollHeight - el.messageStream.scrollTop - el.messageStream.clientHeight;
      el.btnScrollBottom.classList.toggle('hidden', distFromBottom < 150);
    });

    el.btnScrollBottom.addEventListener('click', scrollToBottom);

    // =========================================================================
    // TDL POWER SUITE INITIALIZATION & EVENT HANDLERS
    // =========================================================================

    initTDLPowerSuite();
  }

  // =========================================================================
  // TDL POWER SUITE MODULES
  // =========================================================================

  let activeUploadFile = null;

  function openPowerModal(modal, backdrop) {
    if (el.menuDrawer) el.menuDrawer.classList.add('hidden');
    if (el.menuDrawerBackdrop) el.menuDrawerBackdrop.classList.add('hidden');
    if (backdrop) backdrop.classList.remove('hidden');
    if (modal) modal.classList.remove('hidden');
    populateChatSelects();
  }

  function closePowerModal(modal, backdrop) {
    if (modal) modal.classList.add('hidden');
    if (backdrop) backdrop.classList.add('hidden');
  }

  function populateChatSelects() {
    const selects = [
      document.getElementById('dl-chat-select'),
      document.getElementById('up-chat-select'),
      document.getElementById('idx-chat-select'),
      document.getElementById('idx-search-chat'),
      document.getElementById('idx-export-chat-select'),
      document.getElementById('hub-chat-select'),
    ];

    selects.forEach((sel) => {
      if (!sel) return;
      const isSearch = sel.id === 'idx-search-chat';
      const prevVal = sel.value;
      sel.innerHTML = '';
      if (isSearch) {
        const opt = document.createElement('option');
        opt.value = '0';
        opt.textContent = 'All Indexed Chats';
        sel.appendChild(opt);
      }
      state.chats.forEach((chat) => {
        const opt = document.createElement('option');
        opt.value = chat.id;
        opt.textContent = `${chat.title || 'Untitled'} (${chat.type || 'chat'})`;
        sel.appendChild(opt);
      });
      if (prevVal) {
        sel.value = prevVal;
      } else if (state.selectedChatId && !isSearch) {
        sel.value = state.selectedChatId;
      }
    });
  }

  function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  function formatSpeed(bytesPerSec) {
    if (!bytesPerSec || bytesPerSec === 0) return '0 KB/s';
    return formatBytes(bytesPerSec) + '/s';
  }

  function initTDLPowerSuite() {
    // 1. Drawer Button Open Handlers
    if (el.btnDrawerDownloader) {
      el.btnDrawerDownloader.addEventListener('click', () => {
        openPowerModal(el.batchDlModal, el.batchDlBackdrop);
        loadDownloadTasks();
      });
    }
    if (el.btnCloseBatchDl) {
      el.btnCloseBatchDl.addEventListener('click', () => closePowerModal(el.batchDlModal, el.batchDlBackdrop));
    }
    if (el.batchDlBackdrop) {
      el.batchDlBackdrop.addEventListener('click', () => closePowerModal(el.batchDlModal, el.batchDlBackdrop));
    }

    if (el.btnDrawerUploader) {
      el.btnDrawerUploader.addEventListener('click', () => {
        openPowerModal(el.multiUpModal, el.multiUpBackdrop);
        loadUploadTasks();
      });
    }
    if (el.btnCloseMultiUp) {
      el.btnCloseMultiUp.addEventListener('click', () => closePowerModal(el.multiUpModal, el.multiUpBackdrop));
    }
    if (el.multiUpBackdrop) {
      el.multiUpBackdrop.addEventListener('click', () => closePowerModal(el.multiUpModal, el.multiUpBackdrop));
    }

    if (el.btnDrawerIndexer) {
      el.btnDrawerIndexer.addEventListener('click', () => {
        openPowerModal(el.indexerModal, el.indexerBackdrop);
        loadCacheStats();
      });
    }
    if (el.btnCloseIndexer) {
      el.btnCloseIndexer.addEventListener('click', () => closePowerModal(el.indexerModal, el.indexerBackdrop));
    }
    if (el.indexerBackdrop) {
      el.indexerBackdrop.addEventListener('click', () => closePowerModal(el.indexerModal, el.indexerBackdrop));
    }

    if (el.btnDrawerMediaHub) {
      el.btnDrawerMediaHub.addEventListener('click', () => {
        openPowerModal(el.mediaHubModal, el.mediaHubBackdrop);
      });
    }
    if (el.btnCloseMediaHub) {
      el.btnCloseMediaHub.addEventListener('click', () => closePowerModal(el.mediaHubModal, el.mediaHubBackdrop));
    }
    if (el.mediaHubBackdrop) {
      el.mediaHubBackdrop.addEventListener('click', () => closePowerModal(el.mediaHubModal, el.mediaHubBackdrop));
    }

    // 2. Setup Power Modal Tabs
    document.querySelectorAll('.power-modal').forEach((modal) => {
      const tabs = modal.querySelectorAll('.power-tab');
      tabs.forEach((tab) => {
        tab.addEventListener('click', () => {
          tabs.forEach((t) => t.classList.remove('active'));
          tab.classList.add('active');
          const targetPaneId = `tab-${tab.dataset.tab}`;
          modal.querySelectorAll('.power-tab-pane').forEach((pane) => {
            pane.classList.remove('active');
          });
          const targetPane = modal.querySelector(`#${targetPaneId}`);
          if (targetPane) targetPane.classList.add('active');
        });
      });
    });

    // 3. Setup Downloader Controller
    setupBatchDownloader();

    // 4. Setup Uploader Controller
    setupMultiUploader();

    // 5. Setup Indexer & Cacher Controller
    setupIndexer();

    // 6. Setup Media Hub Controller
    setupMediaHub();
  }

  // --- Downloader Controller ---
  function setupBatchDownloader() {
    const btnModeChat = document.getElementById('btn-dl-mode-chat');
    const btnModeURL = document.getElementById('btn-dl-mode-url');
    const formChat = document.getElementById('dl-form-chat');
    const formURL = document.getElementById('dl-form-url');
    const btnStart = document.getElementById('btn-start-batch-dl');
    const spinner = document.getElementById('dl-start-spinner');

    let currentSourceMode = 'chat';

    if (btnModeChat && btnModeURL) {
      btnModeChat.addEventListener('click', () => {
        btnModeChat.classList.add('active');
        btnModeURL.classList.remove('active');
        formChat.classList.remove('hidden');
        formURL.classList.add('hidden');
        currentSourceMode = 'chat';
      });
      btnModeURL.addEventListener('click', () => {
        btnModeURL.classList.add('active');
        btnModeChat.classList.remove('active');
        formURL.classList.remove('hidden');
        formChat.classList.add('hidden');
        currentSourceMode = 'url';
      });
    }

    if (btnStart) {
      btnStart.addEventListener('click', async () => {
        btnStart.disabled = true;
        if (spinner) spinner.classList.remove('hidden');

        try {
          let payload = {};
          const outDir = document.getElementById('dl-output-dir').value.trim() || 'downloads';
          const threads = parseInt(document.getElementById('dl-threads-input').value, 10) || 4;

          if (currentSourceMode === 'url') {
            const rawUrls = document.getElementById('dl-urls-input').value.trim();
            const urls = rawUrls.split('\n').map(u => u.trim()).filter(u => u.length > 0);
            if (urls.length === 0) {
              showToast('Please enter at least one Telegram link');
              return;
            }
            payload = {
              urls: urls,
              output_dir: outDir,
              threads: threads
            };
          } else {
            const chatID = parseInt(document.getElementById('dl-chat-select').value, 10);
            if (!chatID) {
              showToast('Please select a target chat');
              return;
            }
            const startMsg = parseInt(document.getElementById('dl-start-msg').value, 10) || 0;
            const endMsg = parseInt(document.getElementById('dl-end-msg').value, 10) || 0;
            const filter = document.getElementById('dl-filter-select').value;
            const limit = parseInt(document.getElementById('dl-limit-input').value, 10) || 100;

            payload = {
              chat_id: chatID,
              start_msg_id: startMsg,
              end_msg_id: endMsg,
              filter: filter,
              limit: limit,
              output_dir: outDir,
              threads: threads
            };
          }

          const res = await fetch('/api/downloader/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
          });

          if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to start download');
          }

          const task = await res.json();
          showToast('Batch download task started!');

          // Switch to active queue tab
          const queueTab = el.batchDlModal.querySelector('.power-tab[data-tab="dl-queue"]');
          if (queueTab) queueTab.click();

          loadDownloadTasks();
        } catch (e) {
          showToast('Error: ' + e.message);
        } finally {
          btnStart.disabled = false;
          if (spinner) spinner.classList.add('hidden');
        }
      });
    }
  }

  async function loadDownloadTasks() {
    try {
      const res = await fetch('/api/downloader/tasks');
      if (!res.ok) return;
      const data = await res.json();
      renderDownloadTasks(data.tasks || []);
    } catch (e) {
      console.error('Failed to load download tasks:', e);
    }
  }

  function renderDownloadTasks(tasks) {
    const queueList = document.getElementById('dl-queue-list');
    const compList = document.getElementById('dl-completed-list');
    const queueCountBadge = document.getElementById('dl-queue-count');

    if (!queueList || !compList) return;

    const activeTasks = tasks.filter(t => t.status !== 'completed' && t.status !== 'cancelled');
    const compTasks = tasks.filter(t => t.status === 'completed');

    if (queueCountBadge) queueCountBadge.textContent = activeTasks.length;

    if (activeTasks.length === 0) {
      queueList.innerHTML = '<div class="power-empty-state">No active download tasks. Start one from the New Download tab!</div>';
    } else {
      queueList.innerHTML = '';
      activeTasks.forEach(task => queueList.appendChild(createDownloadTaskCard(task)));
    }

    if (compTasks.length === 0) {
      compList.innerHTML = '<div class="power-empty-state">No completed downloads yet.</div>';
    } else {
      compList.innerHTML = '';
      compTasks.forEach(task => compList.appendChild(createDownloadTaskCard(task)));
    }
  }

  function createDownloadTaskCard(task) {
    const card = document.createElement('div');
    card.className = 'power-task-card';
    card.id = `dl-task-${task.id}`;

    const percent = task.total_bytes > 0
      ? Math.min(100, Math.round((task.downloaded_bytes / task.total_bytes) * 100))
      : (task.total_items > 0 ? Math.round((task.completed_items / task.total_items) * 100) : 0);

    const statusBadgeClass = task.status === 'downloading'
      ? 'badge-connected'
      : (task.status === 'completed' ? 'badge-connected' : 'badge-connecting');

    card.innerHTML = `
      <div class="power-task-title-row">
        <span class="power-task-title">${task.title}</span>
        <span class="badge ${statusBadgeClass}">${task.status.toUpperCase()}</span>
      </div>
      <div class="power-progress-bar-wrap">
        <div class="power-progress-bar" style="width: ${percent}%;"></div>
      </div>
      <div class="power-task-meta-row">
        <span>${formatBytes(task.downloaded_bytes)} / ${formatBytes(task.total_bytes)} (${percent}%)</span>
        <span>⚡ ${formatSpeed(task.speed)}</span>
        <span>${task.completed_items} / ${task.total_items} files</span>
        <div class="power-task-actions">
          ${task.status === 'downloading' ? `<button class="power-btn-sm btn-dl-pause" data-id="${task.id}">Pause</button>` : ''}
          ${task.status === 'paused' || task.status === 'failed' ? `<button class="power-btn-sm btn-dl-resume" data-id="${task.id}">Resume</button>` : ''}
          ${task.status !== 'completed' ? `<button class="power-btn-sm text-danger btn-dl-cancel" data-id="${task.id}">Cancel</button>` : ''}
          <button class="power-btn-sm text-danger btn-dl-del" data-id="${task.id}">✕</button>
        </div>
      </div>
    `;

    // Action clicks
    card.querySelector('.btn-dl-pause')?.addEventListener('click', async () => {
      await fetch('/api/downloader/pause', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ task_id: task.id })
      });
      loadDownloadTasks();
    });

    card.querySelector('.btn-dl-resume')?.addEventListener('click', async () => {
      await fetch('/api/downloader/resume', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ task_id: task.id })
      });
      loadDownloadTasks();
    });

    card.querySelector('.btn-dl-cancel')?.addEventListener('click', async () => {
      await fetch('/api/downloader/cancel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ task_id: task.id })
      });
      loadDownloadTasks();
    });

    card.querySelector('.btn-dl-del')?.addEventListener('click', async () => {
      await fetch('/api/downloader/delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ task_id: task.id })
      });
      loadDownloadTasks();
    });

    return card;
  }

  function handleDownloadProgressWS(task) {
    if (!task) return;
    const existing = document.getElementById(`dl-task-${task.id}`);
    if (existing) {
      const isCompleted = task.status === 'completed';
      const percent = isCompleted
        ? 100
        : (task.total_bytes > 0
            ? Math.min(100, Math.round((task.downloaded_bytes / task.total_bytes) * 100))
            : (task.total_items > 0 ? Math.round((task.completed_items / task.total_items) * 100) : 0));

      const badge = existing.querySelector('.power-task-title-row .badge');
      if (badge) {
        badge.textContent = task.status.toUpperCase();
        badge.className = `badge ${task.status === 'completed' || task.status === 'downloading' ? 'badge-connected' : 'badge-connecting'}`;
      }

      const pBar = existing.querySelector('.power-progress-bar');
      if (pBar) pBar.style.width = `${percent}%`;

      const meta = existing.querySelector('.power-task-meta-row');
      if (meta) {
        const dlBytes = isCompleted ? task.total_bytes : task.downloaded_bytes;
        const compFiles = isCompleted ? task.total_items : task.completed_items;
        const curSpeed = isCompleted ? 0 : task.speed;
        meta.innerHTML = `
          <span>${formatBytes(dlBytes)} / ${formatBytes(task.total_bytes)} (${percent}%)</span>
          <span>⚡ ${formatSpeed(curSpeed)}</span>
          <span>${compFiles} / ${task.total_items} files</span>
          <div class="power-task-actions">
            ${task.status === 'downloading' ? `<button class="power-btn-sm btn-dl-pause" data-id="${task.id}">Pause</button>` : ''}
            ${task.status === 'paused' || task.status === 'failed' ? `<button class="power-btn-sm btn-dl-resume" data-id="${task.id}">Resume</button>` : ''}
            ${task.status !== 'completed' ? `<button class="power-btn-sm text-danger btn-dl-cancel" data-id="${task.id}">Cancel</button>` : ''}
            <button class="power-btn-sm text-danger btn-dl-del" data-id="${task.id}">✕</button>
          </div>
        `;

        existing.querySelector('.btn-dl-pause')?.addEventListener('click', async () => {
          await fetch('/api/downloader/pause', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ task_id: task.id })
          });
          loadDownloadTasks();
        });
        existing.querySelector('.btn-dl-resume')?.addEventListener('click', async () => {
          await fetch('/api/downloader/resume', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ task_id: task.id })
          });
          loadDownloadTasks();
        });
        existing.querySelector('.btn-dl-cancel')?.addEventListener('click', async () => {
          await fetch('/api/downloader/cancel', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ task_id: task.id })
          });
          loadDownloadTasks();
        });
        existing.querySelector('.btn-dl-del')?.addEventListener('click', async () => {
          await fetch('/api/downloader/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ task_id: task.id })
          });
          loadDownloadTasks();
        });
      }

      if (task.status === 'completed' || task.status === 'cancelled') {
        setTimeout(() => {
          loadDownloadTasks();
        }, 1000);
      }
    } else {
      loadDownloadTasks();
    }
  }

  // --- Multi Uploader Controller (Up to 4GB) ---
  function setupMultiUploader() {
    const btnModeFile = document.getElementById('btn-up-mode-file');
    const btnModeLocal = document.getElementById('btn-up-mode-local');
    const formFile = document.getElementById('up-form-file');
    const formLocal = document.getElementById('up-form-local');
    const dropzone = document.getElementById('up-dropzone');
    const fileInput = document.getElementById('up-file-input');
    const fileInfoCard = document.getElementById('up-selected-file-info');
    const cardFileName = document.getElementById('up-card-filename');
    const cardFileSize = document.getElementById('up-card-filesize');
    const btnClearFile = document.getElementById('btn-up-clear-file');
    const btnStart = document.getElementById('btn-start-upload');
    const spinner = document.getElementById('up-start-spinner');

    let uploadSourceMode = 'file';

    if (btnModeFile && btnModeLocal) {
      btnModeFile.addEventListener('click', () => {
        btnModeFile.classList.add('active');
        btnModeLocal.classList.remove('active');
        formFile.classList.remove('hidden');
        formLocal.classList.add('hidden');
        uploadSourceMode = 'file';
      });
      btnModeLocal.addEventListener('click', () => {
        btnModeLocal.classList.add('active');
        btnModeFile.classList.remove('active');
        formLocal.classList.remove('hidden');
        formFile.classList.add('hidden');
        uploadSourceMode = 'local';
      });
    }

    if (dropzone && fileInput) {
      dropzone.addEventListener('click', () => fileInput.click());
      dropzone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropzone.classList.add('dragover');
      });
      dropzone.addEventListener('dragleave', () => dropzone.classList.remove('dragover'));
      dropzone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropzone.classList.remove('dragover');
        if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
          handleFileSelected(e.dataTransfer.files[0]);
        }
      });
      fileInput.addEventListener('change', () => {
        if (fileInput.files && fileInput.files.length > 0) {
          handleFileSelected(fileInput.files[0]);
        }
      });
    }

    function handleFileSelected(file) {
      activeUploadFile = file;
      if (dropzone) dropzone.classList.add('hidden');
      if (fileInfoCard) {
        fileInfoCard.classList.remove('hidden');
        cardFileName.textContent = file.name;
        cardFileSize.textContent = formatBytes(file.size);
      }
    }

    if (btnClearFile) {
      btnClearFile.addEventListener('click', () => {
        activeUploadFile = null;
        if (fileInput) fileInput.value = '';
        if (dropzone) dropzone.classList.remove('hidden');
        if (fileInfoCard) fileInfoCard.classList.add('hidden');
      });
    }

    if (btnStart) {
      btnStart.addEventListener('click', async () => {
        const chatID = parseInt(document.getElementById('up-chat-select').value, 10);
        if (!chatID) {
          showToast('Please select a destination chat');
          return;
        }

        const mediaType = document.getElementById('up-type-select').value;
        const caption = document.getElementById('up-caption-input').value.trim();
        const replyTo = parseInt(document.getElementById('up-reply-input').value, 10) || 0;

        btnStart.disabled = true;
        if (spinner) spinner.classList.remove('hidden');

        try {
          if (uploadSourceMode === 'local') {
            const localPath = document.getElementById('up-local-path-input').value.trim();
            if (!localPath) {
              showToast('Please enter a valid local file path');
              return;
            }

            const res = await fetch('/api/uploader/start', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                file_path: localPath,
                target_chat_id: chatID,
                caption: caption,
                media_type: mediaType,
                reply_to_msg_id: replyTo
              })
            });

            if (!res.ok) {
              const err = await res.json().catch(() => ({}));
              throw new Error(err.error || 'Failed to start upload');
            }
          } else {
            if (!activeUploadFile) {
              showToast('Please choose a file to upload');
              return;
            }

            const fd = new FormData();
            fd.append('file', activeUploadFile);
            fd.append('target_chat_id', chatID.toString());
            fd.append('caption', caption);
            fd.append('media_type', mediaType);
            fd.append('reply_to_msg_id', replyTo.toString());

            const res = await fetch('/api/uploader/upload-file', {
              method: 'POST',
              body: fd
            });

            if (!res.ok) {
              const err = await res.json().catch(() => ({}));
              throw new Error(err.error || 'Upload error');
            }
          }

          showToast('File queued for upload!');

          // Reset inputs
          if (btnClearFile) btnClearFile.click();
          document.getElementById('up-caption-input').value = '';

          // Switch to Queue tab
          const queueTab = el.multiUpModal.querySelector('.power-tab[data-tab="up-queue"]');
          if (queueTab) queueTab.click();

          loadUploadTasks();
        } catch (e) {
          showToast('Upload error: ' + e.message);
        } finally {
          btnStart.disabled = false;
          if (spinner) spinner.classList.add('hidden');
        }
      });
    }
  }

  async function loadUploadTasks() {
    try {
      const res = await fetch('/api/uploader/tasks');
      if (!res.ok) return;
      const data = await res.json();
      renderUploadTasks(data.tasks || []);
    } catch (e) {
      console.error('Failed loading upload tasks:', e);
    }
  }

  function renderUploadTasks(tasks) {
    const list = document.getElementById('up-queue-list');
    const queueBadge = document.getElementById('up-queue-count');
    if (!list) return;

    const activeTasks = tasks.filter(t => t.status !== 'completed' && t.status !== 'cancelled');
    if (queueBadge) queueBadge.textContent = activeTasks.length;

    if (tasks.length === 0) {
      list.innerHTML = '<div class="power-empty-state">No upload tasks in queue.</div>';
      return;
    }

    list.innerHTML = '';
    tasks.forEach(task => {
      const card = document.createElement('div');
      card.className = 'power-task-card';
      card.id = `up-task-${task.id}`;

      const percent = task.file_size > 0
        ? Math.min(100, Math.round((task.sent_bytes / task.file_size) * 100))
        : 0;

      const badgeClass = task.status === 'uploading' ? 'badge-connected' : 'badge-connecting';

      card.innerHTML = `
        <div class="power-task-title-row">
          <span class="power-task-title">${task.file_name}</span>
          <span class="badge ${badgeClass}">${task.status.toUpperCase()}</span>
        </div>
        <div class="power-progress-bar-wrap">
          <div class="power-progress-bar" style="width: ${percent}%;"></div>
        </div>
        <div class="power-task-meta-row">
          <span>${formatBytes(task.sent_bytes)} / ${formatBytes(task.file_size)} (${percent}%)</span>
          <span>⚡ ${formatSpeed(task.speed)}</span>
          <span>To: ${task.chat_title || 'Chat'}</span>
          <div class="power-task-actions">
            ${task.status === 'uploading' || task.status === 'queued' ? `<button class="power-btn-sm text-danger btn-up-cancel" data-id="${task.id}">Cancel</button>` : ''}
          </div>
        </div>
      `;

      card.querySelector('.btn-up-cancel')?.addEventListener('click', async () => {
        await fetch('/api/uploader/cancel', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ task_id: task.id })
        });
        loadUploadTasks();
      });

      list.appendChild(card);
    });
  }

  function handleUploadProgressWS(task) {
    if (!task) return;
    const existing = document.getElementById(`up-task-${task.id}`);
    if (existing) {
      const isCompleted = task.status === 'completed';
      const percent = isCompleted
        ? 100
        : (task.file_size > 0
            ? Math.min(100, Math.round((task.sent_bytes / task.file_size) * 100))
            : 0);

      const badge = existing.querySelector('.power-task-title-row .badge');
      if (badge) {
        badge.textContent = task.status.toUpperCase();
        badge.className = `badge ${task.status === 'completed' || task.status === 'uploading' ? 'badge-connected' : 'badge-connecting'}`;
      }

      const pBar = existing.querySelector('.power-progress-bar');
      if (pBar) pBar.style.width = `${percent}%`;

      const meta = existing.querySelector('.power-task-meta-row');
      if (meta) {
        const sentBytes = isCompleted ? task.file_size : task.sent_bytes;
        const curSpeed = isCompleted ? 0 : task.speed;
        meta.innerHTML = `
          <span>${formatBytes(sentBytes)} / ${formatBytes(task.file_size)} (${percent}%)</span>
          <span>⚡ ${formatSpeed(curSpeed)}</span>
          <span>To: ${task.chat_title || 'Chat'}</span>
          <div class="power-task-actions">
            ${task.status === 'uploading' || task.status === 'queued' ? `<button class="power-btn-sm text-danger btn-up-cancel" data-id="${task.id}">Cancel</button>` : ''}
          </div>
        `;

        existing.querySelector('.btn-up-cancel')?.addEventListener('click', async () => {
          await fetch('/api/uploader/cancel', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ task_id: task.id })
          });
          loadUploadTasks();
        });
      }

      if (task.status === 'completed' || task.status === 'cancelled') {
        setTimeout(() => {
          loadUploadTasks();
        }, 1000);
      }
    } else {
      loadUploadTasks();
    }
  }

  // --- Indexer & Cache Controller ---
  function setupIndexer() {
    const btnStart = document.getElementById('btn-start-indexing');
    const spinner = document.getElementById('idx-start-spinner');
    const progressCard = document.getElementById('idx-progress-card');
    const progressBar = document.getElementById('idx-progress-bar');
    const cardTitle = document.getElementById('idx-card-title');
    const cardCount = document.getElementById('idx-card-count');
    const btnCancel = document.getElementById('btn-cancel-indexing');

    if (btnStart) {
      btnStart.addEventListener('click', async () => {
        const chatID = parseInt(document.getElementById('idx-chat-select').value, 10);
        if (!chatID) {
          showToast('Please select a chat to index');
          return;
        }
        const limit = parseInt(document.getElementById('idx-limit-input').value, 10) || 1000;

        btnStart.disabled = true;
        if (spinner) spinner.classList.remove('hidden');

        try {
          const res = await fetch('/api/indexer/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ chat_id: chatID, limit: limit })
          });

          if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to start indexing');
          }

          if (progressCard) progressCard.classList.remove('hidden');
          showToast('Chat indexing started!');
        } catch (e) {
          showToast('Error: ' + e.message);
        } finally {
          btnStart.disabled = false;
          if (spinner) spinner.classList.add('hidden');
        }
      });
    }

    if (btnCancel) {
      btnCancel.addEventListener('click', async () => {
        await fetch('/api/indexer/cancel', { method: 'POST' });
        showToast('Indexing cancelled');
      });
    }

    // Fast Search
    const btnSearch = document.getElementById('btn-run-idx-search');
    if (btnSearch) {
      btnSearch.addEventListener('click', async () => {
        const q = document.getElementById('idx-search-query').value.trim();
        const chatID = document.getElementById('idx-search-chat').value;
        const mediaOnly = document.getElementById('idx-search-media-only').checked;
        const resultsBox = document.getElementById('idx-search-results');

        if (resultsBox) resultsBox.innerHTML = '<div class="power-empty-state"><div class="spinner-large"></div> Searching...</div>';

        try {
          const params = new URLSearchParams({
            q: q,
            chat_id: chatID,
            media_only: mediaOnly ? 'true' : 'false',
            limit: '100'
          });
          const res = await fetch(`/api/indexer/search?${params.toString()}`);
          if (!res.ok) throw new Error('Search failed');
          const data = await res.json();

          if (!data.results || data.results.length === 0) {
            resultsBox.innerHTML = '<div class="power-empty-state">No matching indexed messages found.</div>';
            return;
          }

          resultsBox.innerHTML = '';
          data.results.forEach(msg => {
            const item = document.createElement('div');
            item.className = 'power-search-item';
            item.innerHTML = `
              <div class="search-item-header">
                <span class="search-item-sender">${msg.sender_name || 'Sender'}</span>
                <span class="search-item-date">${new Date(msg.date).toLocaleDateString()} ${new Date(msg.date).toLocaleTimeString([], {hour:'2-digit', minute:'2-digit'})}</span>
              </div>
              <div class="search-item-text">${msg.text || (msg.media ? `[${msg.media.type || 'Media'}: ${msg.media.file_name || 'File'}]` : '')}</div>
              ${msg.media ? `<span class="search-item-media-tag">📎 ${msg.media.file_name || 'Media'} (${formatBytes(msg.media.file_size)})</span>` : ''}
            `;
            item.addEventListener('click', () => {
              if (msg.chat_id) {
                closePowerModal(el.indexerModal, el.indexerBackdrop);
                selectChat(msg.chat_id);
              }
            });
            resultsBox.appendChild(item);
          });
        } catch (e) {
          if (resultsBox) resultsBox.innerHTML = `<div class="power-empty-state text-danger">${e.message}</div>`;
        }
      });
    }

    // Export Chat (JSON)
    const btnExport = document.getElementById('btn-export-chat-json');
    if (btnExport) {
      btnExport.addEventListener('click', () => {
        const chatID = document.getElementById('idx-export-chat-select').value;
        if (!chatID) {
          showToast('Select a chat to export');
          return;
        }
        window.open(`/api/indexer/export?chat_id=${chatID}`, '_blank');
      });
    }

    // Cache Stats & Maintenance
    const btnRefreshCache = document.getElementById('btn-refresh-cache-stats');
    if (btnRefreshCache) {
      btnRefreshCache.addEventListener('click', loadCacheStats);
    }

    const btnPurgeMedia = document.getElementById('btn-purge-media-cache');
    if (btnPurgeMedia) {
      btnPurgeMedia.addEventListener('click', async () => {
        if (!confirm('Purge all cached media files from disk?')) return;
        await fetch('/api/cache/clear', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ clear_media: true, clear_history: false })
        });
        showToast('Media cache purged');
        loadCacheStats();
      });
    }

    const btnPurgeAll = document.getElementById('btn-purge-all-cache');
    if (btnPurgeAll) {
      btnPurgeAll.addEventListener('click', async () => {
        if (!confirm('Purge all local database indexed messages? (Cloud messages will not be deleted)')) return;
        await fetch('/api/cache/clear', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ clear_media: true, clear_history: true })
        });
        showToast('All local message indices purged');
        loadCacheStats();
      });
    }
  }

  function handleIndexerProgressWS(status) {
    if (!status) return;
    const progressCard = document.getElementById('idx-progress-card');
    const progressBar = document.getElementById('idx-progress-bar');
    const cardTitle = document.getElementById('idx-card-title');
    const cardCount = document.getElementById('idx-card-count');
    const cardBadge = document.getElementById('idx-card-badge');

    if (!progressCard) return;

    if (status.is_indexing) {
      progressCard.classList.remove('hidden');
      if (cardTitle) cardTitle.textContent = `Indexing ${status.chat_title || 'Chat'}...`;
      if (cardBadge) cardBadge.textContent = 'Running';
      const percent = status.total_count > 0 ? Math.min(100, Math.round((status.indexed_count / status.total_count) * 100)) : 0;
      if (progressBar) progressBar.style.width = `${percent}%`;
      if (cardCount) cardCount.textContent = `${status.indexed_count} messages indexed`;
    } else {
      if (cardBadge) cardBadge.textContent = status.status.toUpperCase();
      if (status.status === 'completed') {
        showToast(`Indexed ${status.indexed_count} messages successfully!`);
        setTimeout(() => progressCard.classList.add('hidden'), 3000);
      }
    }
  }

  async function loadCacheStats() {
    try {
      const res = await fetch('/api/cache/stats');
      if (!res.ok) return;
      const stats = await res.json();
      document.getElementById('cache-stat-media-size').textContent = stats.media_cache_formatted || '0 B';
      document.getElementById('cache-stat-media-files').textContent = (stats.media_cache_files || 0).toString();
      document.getElementById('cache-stat-db-size').textContent = stats.history_db_formatted || '0 B';
      document.getElementById('cache-stat-msgs-count').textContent = (stats.cached_messages_count || 0).toString();
    } catch (e) {
      console.error('Failed to load cache stats:', e);
    }
  }

  // --- Media Hub & Inspector Controller ---
  function setupMediaHub() {
    const btnInspect = document.getElementById('btn-run-media-inspect');
    const spinner = document.getElementById('hub-inspect-spinner');
    const resultBox = document.getElementById('hub-inspect-result');

    if (btnInspect) {
      btnInspect.addEventListener('click', async () => {
        const chatID = document.getElementById('hub-chat-select').value;
        const msgID = parseInt(document.getElementById('hub-msg-id-input').value, 10);

        if (!chatID || !msgID) {
          showToast('Please select a chat and enter a Message ID');
          return;
        }

        btnInspect.disabled = true;
        if (spinner) spinner.classList.remove('hidden');

        try {
          const res = await fetch(`/api/media/inspect?chat_id=${chatID}&message_id=${msgID}`);
          if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to inspect media');
          }

          const data = await res.json();
          if (!data.has_media) {
            showToast('Message does not contain any media');
            if (resultBox) resultBox.classList.add('hidden');
            return;
          }

          if (resultBox) {
            resultBox.classList.remove('hidden');
            document.getElementById('hub-tl-type').textContent = data.tl_type || 'TL Object';
            document.getElementById('hub-media-type').textContent = (data.media_type || 'media').toUpperCase();
            document.getElementById('hub-val-filename').textContent = data.file_name || '-';
            document.getElementById('hub-val-filesize').textContent = data.file_size_formatted || formatBytes(data.file_size);
            document.getElementById('hub-val-mime').textContent = data.mime_type || '-';
            document.getElementById('hub-val-dc').textContent = `DC ${data.dc_id || 0}`;
            document.getElementById('hub-val-thumb').textContent = data.has_thumb ? `Yes (${formatBytes(data.thumb_size)})` : 'No';
            document.getElementById('hub-val-convertible').textContent = data.can_convert ? 'Yes (Fully Supported)' : 'No';
            document.getElementById('hub-val-attributes').textContent = JSON.stringify(data.attributes || {}, null, 2);

            const directLink = document.getElementById('hub-direct-link');
            if (directLink) directLink.href = data.direct_url || '#';
          }
        } catch (e) {
          showToast('Inspect error: ' + e.message);
        } finally {
          btnInspect.disabled = false;
          if (spinner) spinner.classList.add('hidden');
        }
      });
    }
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
