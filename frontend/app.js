const API_URL = (typeof window !== 'undefined' && window.API_URL)
    ? window.API_URL
    : (window.location.origin + '/api');

const WS_URL = (typeof window !== 'undefined' && window.WS_URL)
    ? window.WS_URL
    : (window.location.origin.replace(/^http/, 'ws') + '/ws');

const { createApp } = Vue;

createApp({
    data() {
        return {
            token: null,
            userId: null,
            username: null,
            conversations: [],
            messages: {},
            currentConversationId: null,
            currentConversationUsername: '',
            currentConversationDisplayName: '',
            currentConversationAvatarUrl: null,
            currentConversationIsOnline: false,
            messageText: '',
            searchQuery: '',
            ws: null,
            wsReconnectAttempts: 0,
            wsMaxReconnectAttempts: 5,
            wsReconnectDelay: 3000,
            authTab: 'login',
            login: { username: '', password: '' },
            register: { username: '', password: '', confirm: '' },
            authError: '',
            chatListOpen: true,
            loadingMessages: false,
            loadingOlderMessages: false,
            loadingConversations: false,
            hasMoreMessages: {},
            uploadingFile: false,
            showProfileModal: false,
            profileDisplayName: '',
            myAvatarUrl: null,
            uploadingAvatar: false,
            deleteAccountConfirm: '',
            deletingAccount: false,
            // Context menu state
            contextMenu: {
                show: false,
                x: 0,
                y: 0,
                message: null,
            },
            conversationMenu: {
                show: false,
                x: 0,
                y: 0,
                conversation: null,
            },
            // Offline state
            isOffline: !navigator.onLine,
            serverOffline: false,
            // Pull to refresh state
            pullToRefresh: {
                startY: 0,
                currentY: 0,
                pulling: false,
                refreshing: false,
                threshold: 80,
                ready: false,
            },
            // WebRTC Call state
            iceServers: [],
            localStream: null,
            remoteStream: null,
            peerConnection: null,
            incomingCall: null, // { sender_id, username, displayName, avatar_url, offer }
            outgoingCall: null, // { receiver_id, username, displayName, avatar_url, status }
            activeCall: null,   // { user_id, username, displayName, avatar_url }
            callDuration: '',
            callTimer: null,
            callStartTime: null,
            audioEnabled: true,
        };
    },
    computed: {
        isAuthed() {
            return !!this.token && !!this.userId && this.userId > 0;
        },
        filteredConversations() {
            const convs = [...this.conversations];
            // Sort by latest known message time (newest first)
            convs.sort((a, b) => this.getConversationLastTimestamp(b) - this.getConversationLastTimestamp(a));
            const q = this.searchQuery.trim().toLowerCase();
            if (!q) return convs;
            return convs.filter((c) =>
                c.username?.toLowerCase().includes(q) ||
                c.display_name?.toLowerCase().includes(q)
            );
        },
        messagesForCurrent() {
            return this.messages[this.currentConversationId] || [];
        },
    },
    mounted() {
        console.log('Vue app mounted');
        this.initAuth();
        console.log('Auth state:', { token: !!this.token, userId: this.userId, isAuthed: this.isAuthed });
        if (this.isAuthed) {
            this.loadConversations();
            this.loadMyProfile();
            this.connectWebSocket();
            this.fetchWebRTCConfig();
        }
        // Listen for online/offline events
        window.addEventListener('online', () => {
            this.isOffline = false;
            this.serverOffline = false;
            if (this.isAuthed) {
                this.loadConversations();
                if (this.currentConversationId) {
                    this.refreshCurrentConversation();
                }
                if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
                    this.wsReconnectAttempts = 0;
                    this.connectWebSocket();
                }
            }
        });
        window.addEventListener('offline', () => { this.isOffline = true; });
    },
    methods: {
        initAuth() {
            const storedToken = localStorage.getItem('token');
            const storedUserId = localStorage.getItem('userId');
            const storedUsername = localStorage.getItem('username');
            const storedDisplayName = localStorage.getItem('displayName');

            console.log('initAuth - localStorage:', { storedToken, storedUserId, storedUsername });

            // Validate stored auth data
            const isTokenValid = storedToken && storedToken !== 'undefined' && storedToken !== 'null';
            const isUserIdValid = storedUserId && !isNaN(parseInt(storedUserId)) && parseInt(storedUserId) > 0;

            if (isTokenValid && isUserIdValid && storedUsername) {
                this.token = storedToken;
                this.userId = parseInt(storedUserId);
                this.username = storedUsername;
                this.profileDisplayName = storedDisplayName || '';
                console.log('Auth restored from localStorage');
            } else {
                // Clear invalid data
                localStorage.clear();
                console.log('localStorage cleared - no valid auth data');
            }
        },
        formatDate(value) {
            if (!value) return '';
            try {
                // Handle Go zero time
                if (value === '0001-01-01T00:00:00Z' || value.startsWith('0001-01-01')) {
                    return '';
                }

                const date = new Date(value);
                if (isNaN(date.getTime())) return '';

                // Sanity check - if date is before year 2000, it's likely invalid
                if (date.getFullYear() < 2000) return '';

                const now = new Date();
                const diffMs = now - date;

                // If difference is negative (future date) or more than 10 years, something is wrong
                if (diffMs < 0 || diffMs > 10 * 365 * 24 * 60 * 60 * 1000) {
                    return '';
                }

                const diffSeconds = Math.floor(diffMs / 1000);
                const diffMinutes = Math.floor(diffSeconds / 60);
                const diffHours = Math.floor(diffMinutes / 60);
                const diffDays = Math.floor(diffHours / 24);
                const diffWeeks = Math.floor(diffDays / 7);
                const diffMonths = Math.floor(diffDays / 30);
                const diffYears = Math.floor(diffDays / 365);

                const rtf = new Intl.RelativeTimeFormat('fa', { numeric: 'auto' });

                if (diffSeconds < 60) {
                    return rtf.format(-diffSeconds, 'second');
                } else if (diffMinutes < 60) {
                    return rtf.format(-diffMinutes, 'minute');
                } else if (diffHours < 24) {
                    return rtf.format(-diffHours, 'hour');
                } else if (diffDays < 7) {
                    return rtf.format(-diffDays, 'day');
                } else if (diffWeeks < 4) {
                    return rtf.format(-diffWeeks, 'week');
                } else if (diffMonths < 12) {
                    return rtf.format(-diffMonths, 'month');
                } else {
                    return rtf.format(-diffYears, 'year');
                }
            } catch (e) {
                console.error('formatDate error:', e, value);
                return '';
            }
        },
        formatTime(value) {
            if (!value) return '';
            try {
                const date = new Date(value);
                if (isNaN(date.getTime())) return '';
                const hours = date.getHours().toString().padStart(2, '0');
                const minutes = date.getMinutes().toString().padStart(2, '0');
                // Convert to Persian numerals
                const persianNums = ['۰', '۱', '۲', '۳', '۴', '۵', '۶', '۷', '۸', '۹'];
                const timeStr = `${hours}:${minutes}`;
                return timeStr.replace(/[0-9]/g, d => persianNums[parseInt(d)]);
            } catch (e) {
                return '';
            }
        },
        formatStatus(msg) {
            if (msg.status === 'read') return '✓✓';
            if (msg.status === 'delivered') return '✓';
            return '';
        },
        getPullBottomAllowance(el) {
            if (!el) return 12;
            const style = window.getComputedStyle(el);
            const paddingBottom = parseFloat(style.paddingBottom) || 0;
            return paddingBottom + 24;
        },
        isNearBottom(el) {
            if (!el) return false;
            const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
            const allowance = this.getPullBottomAllowance(el);
            return distanceFromBottom <= (allowance + 160);
        },
        updatePullReady(container) {
            const el = container || document.querySelector('.messages-container');
            if (!el) return;
            this.pullToRefresh.ready = this.isNearBottom(el);
        },
        getConversationLastTimestamp(conv) {
            if (!conv) return 0;
            const localMessages = this.messages[conv.user_id] || [];
            const latestLocal = localMessages.length ? localMessages[localMessages.length - 1]?.created_at : null;
            const source = latestLocal || conv.last_message_at;
            if (!source) return 0;
            const ts = new Date(source).getTime();
            return isNaN(ts) ? 0 : ts;
        },
        updateConversationLastMessage(userId, timestamp) {
            if (!userId || !timestamp) return;
            const idx = this.conversations.findIndex(c => c.user_id === userId);
            if (idx === -1) return;
            this.conversations[idx].last_message_at = timestamp;
        },
        async handleLogin() {
            this.authError = '';
            try {
                const res = await fetch(`${API_URL}/auth/login`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username: this.login.username, password: this.login.password }),
                });
                if (!res.ok) throw new Error((await res.json()).error || 'Login failed');
                const data = await res.json();
                this.setAuth(data);
            } catch (err) {
                this.authError = err.message;
            }
        },
        async handleRegister() {
            this.authError = '';
            if (this.register.password !== this.register.confirm) {
                this.authError = 'رمز‌عبورها مطابقت ندارند';
                return;
            }
            try {
                const res = await fetch(`${API_URL}/auth/register`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username: this.register.username, password: this.register.password }),
                });
                if (!res.ok) throw new Error((await res.json()).error || 'Registration failed');
                const data = await res.json();
                this.setAuth(data);
            } catch (err) {
                this.authError = err.message;
            }
        },
        setAuth(data) {
            this.token = data.token;
            this.userId = data.user_id;
            this.username = data.username;
            localStorage.setItem('token', this.token);
            localStorage.setItem('userId', this.userId);
            localStorage.setItem('username', this.username);
            this.loadConversations();
            this.loadMyProfile();
            this.connectWebSocket();
        },
        clearAuth() {
            this.token = null;
            this.userId = null;
            this.username = null;
            this.conversations = [];
            this.messages = {};
            this.currentConversationId = null;
            this.currentConversationUsername = '';
            this.currentConversationDisplayName = '';
            this.currentConversationAvatarUrl = null;
            this.currentConversationIsOnline = false;
            this.showProfileModal = false;
            this.profileDisplayName = '';
            this.myAvatarUrl = null;
            this.deleteAccountConfirm = '';
            this.deletingAccount = false;
            this.conversationMenu = { show: false, x: 0, y: 0, conversation: null };
            this.serverOffline = false;
            localStorage.clear();
            if (this.ws) {
                try { this.ws.close(); } catch (e) { }
                this.ws = null;
            }
        },
        handleLogout() {
            if (confirm('آیا از خروج اطمینان دارید؟')) {
                this.clearAuth();
            }
        },
        async fetchWebRTCConfig() {
            try {
                const res = await fetch(`${API_URL}/webrtc/config`, {
                    headers: { Authorization: `Bearer ${this.token}` },
                });
                if (res.ok) {
                    const data = await res.json();
                    this.iceServers = data.iceServers || [];
                }
            } catch (err) {
                console.error('Error fetching WebRTC config:', err);
                // Fallback to default Google STUN
                this.iceServers = [{ urls: 'stun:stun.l.google.com:19302' }];
            }
        },
        async loadMyProfile() {
            try {
                const res = await fetch(`${API_URL}/profile`, {
                    headers: { Authorization: `Bearer ${this.token}` },
                });
                if (res.ok) {
                    const data = await res.json();
                    this.profileDisplayName = data.display_name || '';
                    this.myAvatarUrl = data.avatar_url || null;
                }
            } catch (err) {
                console.error('Error loading profile:', err);
            }
        },
        async saveProfile() {
            try {
                const res = await fetch(`${API_URL}/profile`, {
                    method: 'PUT',
                    headers: {
                        'Content-Type': 'application/json',
                        Authorization: `Bearer ${this.token}`
                    },
                    body: JSON.stringify({ display_name: this.profileDisplayName }),
                });
                if (!res.ok) throw new Error('Failed to save profile');
                this.showProfileModal = false;
                alert('پروفایل ذخیره شد');
            } catch (err) {
                console.error('Error saving profile:', err);
                alert('خطا در ذخیره پروفایل');
            }
        },
        async deleteAccount() {
            if (!this.username || this.deleteAccountConfirm.trim() !== this.username) {
                alert('نام کاربری وارد شده صحیح نیست');
                return;
            }

            if (!confirm('این عملیات غیرقابل بازگشت است. آیا از حذف حساب اطمینان دارید؟')) {
                return;
            }

            this.deletingAccount = true;
            try {
                const res = await fetch(`${API_URL}/profile`, {
                    method: 'DELETE',
                    headers: { Authorization: `Bearer ${this.token}` },
                });
                if (!res.ok) {
                    const errData = await res.json();
                    throw new Error(errData.error || 'Delete failed');
                }
                this.clearAuth();
                alert('حساب کاربری حذف شد');
            } catch (err) {
                console.error('Error deleting account:', err);
                alert('خطا در حذف حساب');
            } finally {
                this.deletingAccount = false;
            }
        },
        async handleAvatarUpload(event) {
            const file = event.target.files[0];
            if (!file) return;

            // Validate file type
            if (!file.type.startsWith('image/')) {
                alert('لطفا یک فایل تصویری انتخاب کنید');
                return;
            }

            // Validate file size (2MB max)
            if (file.size > 2 * 1024 * 1024) {
                alert('حجم آواتار باید کمتر از ۲ مگابایت باشد');
                return;
            }

            this.uploadingAvatar = true;
            const formData = new FormData();
            formData.append('avatar', file);

            try {
                const res = await fetch(`${API_URL}/profile/avatar`, {
                    method: 'POST',
                    headers: { Authorization: `Bearer ${this.token}` },
                    body: formData,
                });
                if (!res.ok) throw new Error('Upload failed');
                const data = await res.json();
                this.myAvatarUrl = data.avatar_url;
            } catch (err) {
                console.error('Avatar upload error:', err);
                alert('خطا در آپلود آواتار');
            } finally {
                this.uploadingAvatar = false;
                event.target.value = '';
            }
        },
        async loadConversations() {
            this.loadingConversations = true;
            try {
                const res = await fetch(`${API_URL}/conversations`, {
                    headers: { Authorization: `Bearer ${this.token}` },
                });
                if (!res.ok) {
                    if (res.status === 401) {
                        this.clearAuth();
                    }
                    return;
                }
                this.serverOffline = false;
                const data = await res.json();
                this.conversations = data.conversations || [];
            } catch (err) {
                console.error(err);
                this.serverOffline = true;
            } finally {
                this.loadingConversations = false;
            }
        },
        async selectConversation(conv) {
            this.closeConversationMenu();
            this.currentConversationId = conv.user_id;
            this.currentConversationUsername = conv.username;
            this.currentConversationDisplayName = conv.display_name || '';
            this.currentConversationAvatarUrl = conv.avatar_url || null;
            this.currentConversationIsOnline = conv.is_online || false;
            this.loadingMessages = true;
            this.chatListOpen = false;

            // Reset unread count for this conversation in UI
            const convIndex = this.conversations.findIndex(c => c.user_id === conv.user_id);
            if (convIndex !== -1) {
                this.conversations[convIndex].unread_count = 0;
            }

            try {
                const res = await fetch(`${API_URL}/messages?user_id=${conv.user_id}&limit=50`, {
                    headers: { Authorization: `Bearer ${this.token}` },
                });
                if (!res.ok) {
                    if (res.status === 401) {
                        this.clearAuth();
                        return;
                    }
                    if (res.status === 404) {
                        this.closeConversation();
                        this.loadConversations();
                        return;
                    }
                    throw new Error('Failed to load messages');
                }
                const data = await res.json();
                this.messages[conv.user_id] = data.messages || [];
                // If we got 50 messages, there might be more
                this.hasMoreMessages[conv.user_id] = (data.messages || []).length >= 50;

                const latestMessage = this.messages[conv.user_id].length
                    ? this.messages[conv.user_id][this.messages[conv.user_id].length - 1]
                    : null;
                if (latestMessage?.created_at) {
                    this.updateConversationLastMessage(conv.user_id, latestMessage.created_at);
                }

                // Mark all unread messages as read via WebSocket
                if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                    for (const msg of this.messages[conv.user_id]) {
                        if (Number(msg.sender_id) !== Number(this.userId) && msg.status !== 'read') {
                            this.ws.send(JSON.stringify({ type: 'mark_read', message_id: msg.id }));
                        }
                    }
                }

                // Scroll to bottom after DOM update with delay for rendering
                // Use longer delay for mobile devices
                this.$nextTick(() => {
                    setTimeout(() => this.scrollToBottom(), 100);
                });
            } catch (err) {
                console.error(err);
                this.messages[conv.user_id] = [];
            } finally {
                this.loadingMessages = false;
            }
        },
        async sendMessage() {
            const content = (this.messageText || '').trim();
            if (!content || !this.currentConversationId || !this.ws || this.ws.readyState !== WebSocket.OPEN) return;

            const clientMessageId = `client-${Date.now()}`;
            const msg = {
                id: null,
                client_message_id: clientMessageId,
                sender_id: this.userId,
                receiver_id: this.currentConversationId,
                content,
                status: 'sent',
                created_at: new Date().toISOString(),
            };
            if (!this.messages[this.currentConversationId]) this.messages[this.currentConversationId] = [];
            this.messages[this.currentConversationId].push(msg);
            this.updateConversationLastMessage(this.currentConversationId, msg.created_at);
            this.messageText = '';
            this.chatListOpen = false;

            this.ws.send(JSON.stringify({
                type: 'message',
                receiver_id: this.currentConversationId,
                content,
                client_message_id: clientMessageId,
            }));
            this.$nextTick(() => this.scrollToBottom());
        },
        async handleFileSelect(event) {
            const file = event.target.files[0];
            if (!file || !this.currentConversationId) return;

            this.uploadingFile = true;
            const formData = new FormData();
            formData.append('file', file);
            formData.append('receiver_id', this.currentConversationId);

            try {
                const res = await fetch(`${API_URL}/upload`, {
                    method: 'POST',
                    headers: { Authorization: `Bearer ${this.token}` },
                    body: formData,
                });
                if (!res.ok) throw new Error('Upload failed');
                const data = await res.json();

                // Add file message to local messages
                if (!this.messages[this.currentConversationId]) this.messages[this.currentConversationId] = [];
                const createdAt = new Date().toISOString();
                this.messages[this.currentConversationId].push({
                    id: data.message_id,
                    sender_id: this.userId,
                    receiver_id: this.currentConversationId,
                    content: `📎 ${data.file_name}`,
                    file_url: data.file_url,
                    status: 'sent',
                    created_at: createdAt,
                });
                this.updateConversationLastMessage(this.currentConversationId, createdAt);
                this.$nextTick(() => this.scrollToBottom());
                this.loadConversations();
            } catch (err) {
                console.error('File upload error:', err);
                alert('خطا در آپلود فایل');
            } finally {
                this.uploadingFile = false;
                event.target.value = ''; // Reset file input
            }
        },
        scrollToBottom(attempts = 0) {
            const container = document.querySelector('.messages-container');
            if (container) {
                // Use requestAnimationFrame for smoother scrolling on mobile
                requestAnimationFrame(() => {
                    container.scrollTop = container.scrollHeight;
                    this.updatePullReady(container);

                    // On mobile, sometimes need multiple attempts due to rendering delays
                    if (attempts < 3 && container.scrollTop < container.scrollHeight - container.clientHeight - 50) {
                        setTimeout(() => this.scrollToBottom(attempts + 1), 100);
                    }
                });
            }
        },
        handleMessagesScroll(event) {
            const container = event.target;
            this.updatePullReady(container);
            // Load more when scrolled near top (within 100px)
            if (container.scrollTop < 100 && !this.loadingOlderMessages && this.hasMoreMessages[this.currentConversationId]) {
                this.loadOlderMessages();
            }
        },
        async loadOlderMessages() {
            if (!this.currentConversationId || this.loadingOlderMessages) return;

            const currentMessages = this.messages[this.currentConversationId] || [];
            if (currentMessages.length === 0) return;

            this.loadingOlderMessages = true;
            const container = document.querySelector('.messages-container');
            const oldScrollHeight = container ? container.scrollHeight : 0;

            try {
                const offset = currentMessages.length;
                const res = await fetch(`${API_URL}/messages?user_id=${this.currentConversationId}&limit=50&offset=${offset}`, {
                    headers: { Authorization: `Bearer ${this.token}` },
                });
                if (!res.ok) {
                    if (res.status === 404) {
                        this.closeConversation();
                        this.loadConversations();
                    }
                    return;
                }

                const data = await res.json();
                const olderMessages = data.messages || [];

                if (olderMessages.length > 0) {
                    // Prepend older messages to existing ones
                    this.messages[this.currentConversationId] = [...olderMessages, ...currentMessages];

                    // Maintain scroll position after prepending
                    this.$nextTick(() => {
                        if (container) {
                            const newScrollHeight = container.scrollHeight;
                            container.scrollTop = newScrollHeight - oldScrollHeight;
                        }
                    });
                }

                // If we got less than 50, no more messages
                this.hasMoreMessages[this.currentConversationId] = olderMessages.length >= 50;
            } catch (err) {
                console.error('Error loading older messages:', err);
            } finally {
                this.loadingOlderMessages = false;
            }
        },
        // Pull to refresh methods
        handlePullStart(event) {
            if (!this.currentConversationId || this.pullToRefresh.refreshing) return;
            const container = document.querySelector('.messages-container');
            if (!container) return;
            // Only enable pull-to-refresh when at end of messages
            if (!this.isNearBottom(container)) return;
            this.pullToRefresh.ready = true;

            const touch = event.touches ? event.touches[0] : event;
            this.pullToRefresh.startY = touch.clientY;
            this.pullToRefresh.pulling = true;
        },
        handlePullMove(event) {
            if (!this.pullToRefresh.pulling || this.pullToRefresh.refreshing) return;

            const touch = event.touches ? event.touches[0] : event;
            const deltaY = touch.clientY - this.pullToRefresh.startY;

            // Only pull up when at bottom
            if (deltaY < 0) {
                const magnitude = Math.abs(deltaY);
                this.pullToRefresh.currentY = Math.min(magnitude, this.pullToRefresh.threshold * 1.5);
                // Prevent default scroll when pulling up past the end
                if (magnitude > 10) {
                    event.preventDefault();
                }
            } else {
                this.pullToRefresh.currentY = 0;
            }
        },
        async handlePullEnd() {
            if (!this.pullToRefresh.pulling) return;

            if (this.pullToRefresh.currentY >= this.pullToRefresh.threshold) {
                this.pullToRefresh.refreshing = true;
                await this.refreshCurrentConversation();
                this.pullToRefresh.refreshing = false;
            }

            this.pullToRefresh.pulling = false;
            this.pullToRefresh.startY = 0;
            this.pullToRefresh.currentY = 0;
            this.updatePullReady();
        },
        async refreshCurrentConversation() {
            if (!this.currentConversationId) return;

            try {
                const res = await fetch(`${API_URL}/messages?user_id=${this.currentConversationId}&limit=50`, {
                    headers: { Authorization: `Bearer ${this.token}` },
                });
                if (!res.ok) {
                    if (res.status === 404) {
                        this.closeConversation();
                        this.loadConversations();
                    }
                    return;
                }

                const data = await res.json();
                this.messages[this.currentConversationId] = data.messages || [];
                this.hasMoreMessages[this.currentConversationId] = (data.messages || []).length >= 50;

                const latestMessage = this.messages[this.currentConversationId].length
                    ? this.messages[this.currentConversationId][this.messages[this.currentConversationId].length - 1]
                    : null;
                if (latestMessage?.created_at) {
                    this.updateConversationLastMessage(this.currentConversationId, latestMessage.created_at);
                }

                this.$nextTick(() => this.scrollToBottom());
                this.updatePullReady();

                // Also refresh conversations list
                this.loadConversations();
            } catch (err) {
                console.error('Error refreshing conversation:', err);
            }
        },
        shareProfile() {
            const profileUrl = `${window.location.origin}/u/${this.username}`;
            const text = `پروفایل من: ${profileUrl}`;
            if (navigator.share) {
                navigator.share({ title: 'پروفایل من', text, url: profileUrl });
            } else {
                navigator.clipboard.writeText(profileUrl).then(() => alert('لینک پروفایل کپی شد'));
            }
        },
        toggleChatList(forceState) {
            if (typeof forceState === 'boolean') this.chatListOpen = forceState;
            else this.chatListOpen = !this.chatListOpen;
        },
        goBackToList() {
            this.closeConversation();
        },
        closeConversation() {
            this.currentConversationId = null;
            this.currentConversationUsername = '';
            this.currentConversationDisplayName = '';
            this.currentConversationAvatarUrl = null;
            this.currentConversationIsOnline = false;
            this.chatListOpen = true;
        },
        connectWebSocket() {
            const wsUrlWithToken = `${WS_URL}?token=${encodeURIComponent(this.token)}`;
            this.ws = new WebSocket(wsUrlWithToken);

            this.ws.onopen = () => {
                this.wsReconnectAttempts = 0;
                this.serverOffline = false;
            };

            this.ws.onmessage = (event) => {
                try {
                    const data = JSON.parse(event.data);
                    this.handleWebSocketMessage(data);
                } catch (err) {
                    console.error('WebSocket parse error:', err);
                }
            };

            this.ws.onerror = (err) => {
                console.error('WebSocket error:', err);
                this.serverOffline = true;
            };

            this.ws.onclose = () => {
                this.serverOffline = true;
                if (this.wsReconnectAttempts < this.wsMaxReconnectAttempts) {
                    this.wsReconnectAttempts++;
                    setTimeout(() => this.connectWebSocket(), this.wsReconnectDelay);
                }
            };
        },
        handleWebSocketMessage(data) {
            if (data.type === 'call_offer') {
                if (this.activeCall || this.incomingCall || this.outgoingCall) {
                    this.ws.send(JSON.stringify({ type: 'call_reject', receiver_id: data.sender_id, payload: { reason: 'busy' } }));
                    return;
                }
                // Fetch sender info if not in conversations
                const sender = this.conversations.find(c => c.user_id === data.sender_id) || { username: 'کاربر', user_id: data.sender_id };
                this.incomingCall = {
                    sender_id: data.sender_id,
                    username: sender.username,
                    displayName: sender.display_name,
                    avatar_url: sender.avatar_url,
                    offer: data.payload.offer
                };
            } else if (data.type === 'call_answer') {
                if (this.outgoingCall && this.outgoingCall.receiver_id === data.sender_id) {
                    this.peerConnection.setRemoteDescription(new RTCSessionDescription(data.payload.answer));
                    this.activeCall = { ...this.outgoingCall, user_id: this.outgoingCall.receiver_id };
                    this.outgoingCall = null;
                    this.startCallTimer();
                }
            } else if (data.type === 'ice_candidate') {
                if (this.peerConnection) {
                    this.peerConnection.addIceCandidate(new RTCIceCandidate(data.payload.candidate));
                }
            } else if (data.type === 'call_reject') {
                if (this.outgoingCall && this.outgoingCall.receiver_id === data.sender_id) {
                    alert('تماس رد شد');
                    this.endCall(false);
                }
            } else if (data.type === 'call_hangup') {
                if ((this.activeCall && this.activeCall.user_id === data.sender_id) ||
                    (this.incomingCall && this.incomingCall.sender_id === data.sender_id)) {
                    this.endCall(false);
                }
            } else if (data.type === 'message') {
                const convUser = data.sender_id === this.userId ? data.receiver_id : data.sender_id;
                if (!this.messages[convUser]) this.messages[convUser] = [];

                // Replace temp message by client_message_id if present
                if (data.client_message_id) {
                    const idx = this.messages[convUser].findIndex((m) => m.client_message_id === data.client_message_id);
                    if (idx >= 0) {
                        this.messages[convUser][idx] = {
                            ...this.messages[convUser][idx],
                            id: data.message_id,
                            status: data.status,
                            file_name: data.file_name || this.messages[convUser][idx].file_name,
                            file_url: data.file_url || this.messages[convUser][idx].file_url,
                        };
                    } else {
                        this.messages[convUser].push({
                            id: data.message_id,
                            sender_id: data.sender_id,
                            receiver_id: data.receiver_id,
                            content: data.content,
                            status: data.status,
                            created_at: data.created_at,
                            client_message_id: data.client_message_id,
                            file_name: data.file_name,
                            file_url: data.file_url,
                        });
                    }
                } else {
                    this.messages[convUser].push({
                        id: data.message_id,
                        sender_id: data.sender_id,
                        receiver_id: data.receiver_id,
                        content: data.content,
                        status: data.status,
                        created_at: data.created_at,
                        file_name: data.file_name,
                        file_url: data.file_url,
                    });
                }

                // Update local conversation's last_message_at for immediate sorting
                const convIndex = this.conversations.findIndex(c => c.user_id === convUser);
                if (convIndex !== -1) {
                    this.conversations[convIndex].last_message_at = data.created_at || new Date().toISOString();
                }

                if (this.currentConversationId === convUser) {
                    // Mark as delivered/read
                    this.ws?.send(JSON.stringify({ type: 'mark_delivered', message_id: data.message_id }));
                    this.ws?.send(JSON.stringify({ type: 'mark_read', message_id: data.message_id }));
                    // Scroll to bottom for new messages in current conversation
                    this.$nextTick(() => this.scrollToBottom());
                } else if (data.sender_id !== this.userId) {
                    // Increment unread count for non-active conversation
                    if (convIndex !== -1) {
                        this.conversations[convIndex].unread_count = (this.conversations[convIndex].unread_count || 0) + 1;
                    }
                }

                this.loadConversations();
            } else if (data.type === 'status_update') {
                const allMsgs = Object.values(this.messages).flat();
                const msg = allMsgs.find((m) => m.id === data.message_id);
                if (msg) {
                    msg.status = data.status;
                }
            }
        },
        openNewChat() {
            this.showNewChatModal();
        },
        showNewChatModal() {
            const modal = document.createElement('div');
            modal.className = 'modal';
            modal.innerHTML = `
                <div class="modal-content">
                    <div class="modal-header">
                        <h3>مکالمه جدید</h3>
                        <button class="close-btn" aria-label="بستن">✕</button>
                    </div>
                    <div class="search-user-container">
                        <input type="text" class="search-user-input" placeholder="نام کاربری را جستجو کنید..." autofocus>
                    </div>
                    <div class="users-list"></div>
                </div>`;

            const closeBtn = modal.querySelector('.close-btn');
            const searchInput = modal.querySelector('.search-user-input');
            const usersList = modal.querySelector('.users-list');
            let searchTimeout = null;

            closeBtn.addEventListener('click', () => modal.remove());
            modal.addEventListener('click', (e) => { if (e.target === modal) modal.remove(); });

            const searchUsers = async (query) => {
                if (!query.trim()) {
                    usersList.innerHTML = '<p class="search-hint">نام کاربری را وارد کنید</p>';
                    return;
                }
                usersList.innerHTML = '<p class="searching">در حال جستجو...</p>';
                try {
                    const res = await fetch(`${API_URL}/users?q=${encodeURIComponent(query)}`, {
                        headers: { Authorization: `Bearer ${this.token}` }
                    });
                    if (!res.ok) throw new Error('Search failed');
                    const users = await res.json();
                    if (users.length === 0) {
                        usersList.innerHTML = '<p class="empty">کاربری یافت نشد</p>';
                    } else {
                        usersList.innerHTML = '';
                        users.forEach((u) => {
                            const userId = Number(u.id);
                            const username = typeof u.username === 'string' ? u.username : '';
                            const displayName = typeof u.display_name === 'string' ? u.display_name : '';
                            const avatarUrl = typeof u.avatar_url === 'string' ? u.avatar_url : '';
                            const isOnline = !!u.is_online;
                            const nameLabel = displayName || username || '?';

                            const item = document.createElement('div');
                            item.className = 'user-item';

                            const avatarWrapper = document.createElement('div');
                            avatarWrapper.className = 'user-avatar-wrapper';
                            if (avatarUrl) {
                                const img = document.createElement('img');
                                img.src = avatarUrl;
                                img.className = 'user-avatar';
                                img.alt = 'avatar';
                                avatarWrapper.appendChild(img);
                            } else {
                                const placeholder = document.createElement('span');
                                placeholder.className = 'user-avatar-placeholder';
                                placeholder.textContent = nameLabel.charAt(0).toUpperCase();
                                avatarWrapper.appendChild(placeholder);
                            }
                            if (isOnline) {
                                const onlineIndicator = document.createElement('span');
                                onlineIndicator.className = 'online-indicator';
                                avatarWrapper.appendChild(onlineIndicator);
                            }

                            const info = document.createElement('div');
                            info.className = 'user-info';
                            const displayNameEl = document.createElement('div');
                            displayNameEl.className = 'user-display-name';
                            displayNameEl.textContent = nameLabel;
                            const usernameEl = document.createElement('div');
                            usernameEl.className = 'user-username';
                            usernameEl.textContent = `@${username}`;
                            if (isOnline) {
                                const onlineText = document.createElement('span');
                                onlineText.className = 'online-text';
                                onlineText.textContent = ' آنلاین';
                                usernameEl.appendChild(onlineText);
                            }
                            info.appendChild(displayNameEl);
                            info.appendChild(usernameEl);

                            const chevron = document.createElement('span');
                            chevron.className = 'chevron';
                            chevron.textContent = '›';

                            item.appendChild(avatarWrapper);
                            item.appendChild(info);
                            item.appendChild(chevron);

                            item.addEventListener('click', () => {
                                this.startConversation(userId, username, displayName, avatarUrl, isOnline);
                                modal.remove();
                            });

                            usersList.appendChild(item);
                        });
                    }
                } catch (err) {
                    console.error('Search error:', err);
                    usersList.innerHTML = '<p class="empty">خطا در جستجو</p>';
                }
            };

            searchInput.addEventListener('input', (e) => {
                clearTimeout(searchTimeout);
                searchTimeout = setTimeout(() => searchUsers(e.target.value), 300);
            });

            // Show initial hint
            usersList.innerHTML = '<p class="search-hint">نام کاربری را وارد کنید</p>';

            document.body.appendChild(modal);
            searchInput.focus();
        },
        async startConversation(userId, username, displayName = '', avatarUrl = '', isOnline = false) {
            console.log('Starting conversation with:', userId, username);
            // Check by user_id which is more reliable than participants array
            const existing = this.conversations.find((c) => c.user_id === userId);
            if (existing) {
                console.log('Found existing conversation:', existing);
                // Update online status from search
                existing.is_online = isOnline;
                this.selectConversation(existing);
                return;
            }
            try {
                const res = await fetch(`${API_URL}/conversations`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${this.token}` },
                    body: JSON.stringify({ participant_id: userId }),
                });
                if (!res.ok) throw new Error('Failed to create conversation');
                const conversation = await res.json();
                console.log('Created conversation:', conversation);
                this.conversations.unshift(conversation); // Add to beginning of list
                console.log('Conversations after adding:', this.conversations);
                this.selectConversation(conversation);
            } catch (err) {
                console.error('Error starting conversation:', err);
                alert('خطا در ایجاد مکالمه');
            }
        },
        // Context menu methods
        openContextMenu(event, message) {
            if (Number(message.sender_id) !== Number(this.userId)) return;

            const targetRect = event?.currentTarget?.getBoundingClientRect
                ? event.currentTarget.getBoundingClientRect()
                : null;

            const padding = 12;
            const menuWidth = 160;
            const menuHeight = 60;

            let x = targetRect ? targetRect.left : (event.clientX || event.pageX || 0);
            let y = targetRect ? targetRect.bottom : (event.clientY || event.pageY || 0);

            const viewportWidth = window.innerWidth;
            const viewportHeight = window.innerHeight;

            if (x + menuWidth + padding > viewportWidth) {
                x = viewportWidth - menuWidth - padding;
            }
            if (x < padding) x = padding;

            if (y + menuHeight + padding > viewportHeight) {
                y = targetRect ? targetRect.top - menuHeight : viewportHeight - menuHeight - padding;
            }
            if (y < padding) y = padding;

            this.contextMenu = {
                show: true,
                x,
                y,
                message,
            };
        },
        closeContextMenu() {
            this.contextMenu.show = false;
            this.contextMenu.message = null;
        },
        openConversationMenu(event, conversation) {
            const targetRect = event?.currentTarget?.getBoundingClientRect
                ? event.currentTarget.getBoundingClientRect()
                : null;

            const padding = 12;
            const menuWidth = 160;
            const menuHeight = 56;

            let x = targetRect ? targetRect.left : (event.clientX || event.pageX || 0);
            let y = targetRect ? targetRect.bottom : (event.clientY || event.pageY || 0);

            const viewportWidth = window.innerWidth;
            const viewportHeight = window.innerHeight;

            if (x + menuWidth + padding > viewportWidth) {
                x = viewportWidth - menuWidth - padding;
            }
            if (x < padding) x = padding;

            if (y + menuHeight + padding > viewportHeight) {
                y = targetRect ? targetRect.top - menuHeight : viewportHeight - menuHeight - padding;
            }
            if (y < padding) y = padding;

            this.conversationMenu = {
                show: true,
                x,
                y,
                conversation,
            };
        },
        closeConversationMenu() {
            this.conversationMenu.show = false;
            this.conversationMenu.conversation = null;
        },
        async deleteConversation(conversation) {
            if (!conversation || !conversation.id) {
                this.closeConversationMenu();
                return;
            }

            if (!confirm('آیا از حذف این مکالمه اطمینان دارید؟')) {
                this.closeConversationMenu();
                return;
            }

            try {
                const res = await fetch(`${API_URL}/conversations/${conversation.id}`, {
                    method: 'DELETE',
                    headers: { Authorization: `Bearer ${this.token}` },
                });

                if (!res.ok) {
                    if (res.status === 404) {
                        this.closeConversation();
                        this.loadConversations();
                        return;
                    }
                    const errData = await res.json();
                    throw new Error(errData.error || 'Delete failed');
                }

                this.conversations = this.conversations.filter(c => c.id !== conversation.id);
                delete this.messages[conversation.user_id];

                if (this.currentConversationId === conversation.user_id) {
                    this.closeConversation();
                }

                this.loadConversations();
            } catch (err) {
                console.error('Error deleting conversation:', err);
                alert('خطا در حذف مکالمه');
            } finally {
                this.closeConversationMenu();
            }
        },
        async deleteMessage() {
            const message = this.contextMenu.message;
            if (!message || !message.id) {
                this.closeContextMenu();
                return;
            }

            if (!confirm('آیا از حذف این پیام اطمینان دارید؟')) {
                this.closeContextMenu();
                return;
            }

            try {
                const res = await fetch(`${API_URL}/messages/${message.id}`, {
                    method: 'DELETE',
                    headers: { Authorization: `Bearer ${this.token}` },
                });

                if (!res.ok) {
                    const errData = await res.json();
                    throw new Error(errData.error || 'Delete failed');
                }

                // Remove message from local state
                const convMessages = this.messages[this.currentConversationId];
                if (convMessages) {
                    const idx = convMessages.findIndex(m => m.id === message.id);
                    if (idx !== -1) {
                        convMessages.splice(idx, 1);
                    }
                }
            } catch (err) {
                console.error('Error deleting message:', err);
                alert('خطا در حذف پیام');
            } finally {
                this.closeContextMenu();
            }
        },
        // WebRTC Call Methods
        async startCall() {
            if (this.activeCall || this.outgoingCall || this.incomingCall) return;
            if (this.currentConversationId === this.userId) return;

            const receiverId = this.currentConversationId;
            const username = this.currentConversationUsername;
            const displayName = this.currentConversationDisplayName;
            const avatarUrl = this.currentConversationAvatarUrl;

            this.outgoingCall = { receiver_id: receiverId, username, displayName, avatarUrl, status: 'calling' };

            try {
                console.log('[WebRTC] startCall: Getting user media...');
                this.localStream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
                console.log('[WebRTC] startCall: Got local stream with tracks:', this.localStream.getTracks().map(t => t.kind + ':' + t.enabled));
                this.setupPeerConnection(receiverId);
                this.localStream.getTracks().forEach(track => {
                    console.log('[WebRTC] startCall: Adding track:', track.kind, track.enabled);
                    this.peerConnection.addTrack(track, this.localStream);
                });

                const offer = await this.peerConnection.createOffer();
                await this.peerConnection.setLocalDescription(offer);

                this.ws.send(JSON.stringify({
                    type: 'call_offer',
                    receiver_id: receiverId,
                    payload: { offer }
                }));
            } catch (err) {
                console.error('Failed to start call:', err);
                alert('خطا در دسترسی به میکروفون');
                this.endCall();
            }
        },
        async acceptCall() {
            if (!this.incomingCall) return;
            const senderId = this.incomingCall.sender_id;

            try {
                console.log('[WebRTC] acceptCall: Getting user media...');
                this.localStream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
                console.log('[WebRTC] acceptCall: Got local stream with tracks:', this.localStream.getTracks().map(t => t.kind + ':' + t.enabled));
                this.setupPeerConnection(senderId);
                this.localStream.getTracks().forEach(track => {
                    console.log('[WebRTC] acceptCall: Adding track:', track.kind, track.enabled);
                    this.peerConnection.addTrack(track, this.localStream);
                });

                await this.peerConnection.setRemoteDescription(new RTCSessionDescription(this.incomingCall.offer));
                const answer = await this.peerConnection.createAnswer();
                await this.peerConnection.setLocalDescription(answer);

                this.ws.send(JSON.stringify({
                    type: 'call_answer',
                    receiver_id: senderId,
                    payload: { answer }
                }));

                this.activeCall = {
                    user_id: senderId,
                    username: this.incomingCall.username,
                    displayName: this.incomingCall.displayName,
                    avatar_url: this.incomingCall.avatar_url
                };
                this.incomingCall = null;
                this.startCallTimer();
            } catch (err) {
                console.error('Failed to accept call:', err);
                alert('خطا در دسترسی به میکروفون');
                this.rejectCall();
            }
        },
        rejectCall() {
            if (!this.incomingCall) return;
            this.ws.send(JSON.stringify({
                type: 'call_reject',
                receiver_id: this.incomingCall.sender_id
            }));
            this.saveCallLogMessage(this.incomingCall.sender_id, 'تماس ناموفق');
            this.incomingCall = null;
        },
        endCall(isInitiator = true) {
            if (this.activeCall) {
                if (isInitiator) {
                    this.ws.send(JSON.stringify({
                        type: 'call_hangup',
                        receiver_id: this.activeCall.user_id
                    }));
                    const duration = this.callDuration ? ` (${this.callDuration})` : '';
                    this.saveCallLogMessage(this.activeCall.user_id, `تماس صوتی${duration}`);
                }
            } else if (this.outgoingCall) {
                if (isInitiator) {
                    this.ws.send(JSON.stringify({
                        type: 'call_hangup',
                        receiver_id: this.outgoingCall.receiver_id
                    }));
                    this.saveCallLogMessage(this.outgoingCall.receiver_id, 'تماس ناموفق');
                }
            }

            if (this.peerConnection) {
                this.peerConnection.close();
                this.peerConnection = null;
            }
            if (this.localStream) {
                this.localStream.getTracks().forEach(track => track.stop());
                this.localStream = null;
            }

            const remoteAudio = document.getElementById('remote-audio');
            if (remoteAudio) remoteAudio.srcObject = null;

            this.stopCallTimer();
            this.activeCall = null;
            this.outgoingCall = null;
            this.incomingCall = null;
        },
        setupPeerConnection(otherUserId) {
            console.log('[WebRTC] Setting up peer connection with ICE servers:', this.iceServers);
            this.peerConnection = new RTCPeerConnection({ iceServers: this.iceServers });

            this.peerConnection.onicecandidate = (event) => {
                if (event.candidate) {
                    console.log('[WebRTC] Sending ICE candidate:', event.candidate.type, event.candidate.address);
                    this.ws.send(JSON.stringify({
                        type: 'ice_candidate',
                        receiver_id: otherUserId,
                        payload: { candidate: event.candidate }
                    }));
                }
            };

            this.peerConnection.oniceconnectionstatechange = () => {
                console.log('[WebRTC] ICE connection state:', this.peerConnection.iceConnectionState);
            };

            this.peerConnection.onconnectionstatechange = () => {
                console.log('[WebRTC] Connection state:', this.peerConnection.connectionState);
            };

            this.peerConnection.ontrack = (event) => {
                console.log('[WebRTC] Received remote track:', event.track.kind, event.track.enabled);
                this.remoteStream = event.streams[0];
                let remoteAudio = document.getElementById('remote-audio');
                if (!remoteAudio) {
                    remoteAudio = document.createElement('audio');
                    remoteAudio.id = 'remote-audio';
                    remoteAudio.autoplay = true;
                    document.body.appendChild(remoteAudio);
                }
                remoteAudio.srcObject = this.remoteStream;
                // Explicitly play to handle some browser policies
                remoteAudio.play().catch(err => console.error('Error playing remote audio:', err));
            };
        },
        startCallTimer() {
            this.callStartTime = Date.now();
            this.callTimer = setInterval(() => {
                const now = Date.now();
                const diff = Math.floor((now - this.callStartTime) / 1000);
                const minutes = Math.floor(diff / 60).toString().padStart(2, '0');
                const seconds = (diff % 60).toString().padStart(2, '0');
                this.callDuration = `${minutes}:${seconds}`;
            }, 1000);
        },
        stopCallTimer() {
            if (this.callTimer) {
                clearInterval(this.callTimer);
                this.callTimer = null;
            }
            this.callDuration = '';
            this.callStartTime = null;
        },
        saveCallLogMessage(otherUserId, content) {
            // Send a regular message for call history
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.ws.send(JSON.stringify({
                    type: 'message',
                    receiver_id: otherUserId,
                    content: content
                }));
            }
        },
    },
}).mount('#app');
