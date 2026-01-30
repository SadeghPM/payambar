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
            // Context menu state
            contextMenu: {
                show: false,
                x: 0,
                y: 0,
                message: null,
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
        updatePullReady(container) {
            const el = container || document.querySelector('.messages-container');
            if (!el) return;
            const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
            this.pullToRefresh.ready = distanceFromBottom <= 12;
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
            this.serverOffline = false;
            localStorage.clear();
            if (this.ws) {
                try { this.ws.close(); } catch (e) {}
                this.ws = null;
            }
        },
        handleLogout() {
            if (confirm('آیا از خروج اطمینان دارید؟')) {
                this.clearAuth();
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
                    this.clearAuth();
                    return;
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
                if (!res.ok) return;
                
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
            const distanceFromBottom = container.scrollHeight - container.scrollTop - container.clientHeight;
            // Only enable pull-to-refresh when at end of messages
            if (distanceFromBottom > 10) return;
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
                if (!res.ok) return;
                
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
            this.currentConversationId = null;
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
            if (data.type === 'message') {
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
                        usersList.innerHTML = users.map(u => `
                            <div class="user-item" data-id="${u.id}" data-username="${u.username}" data-display-name="${u.display_name || ''}" data-avatar="${u.avatar_url || ''}" data-online="${u.is_online || false}">
                                <div class="user-avatar-wrapper">
                                    ${u.avatar_url 
                                        ? `<img src="${u.avatar_url}" class="user-avatar" alt="avatar">` 
                                        : `<span class="user-avatar-placeholder">${(u.display_name || u.username || '?').charAt(0).toUpperCase()}</span>`
                                    }
                                    ${u.is_online ? '<span class="online-indicator"></span>' : ''}
                                </div>
                                <div class="user-info">
                                    <div class="user-display-name">${u.display_name || u.username}</div>
                                    <div class="user-username">@${u.username}${u.is_online ? ' <span class="online-text">آنلاین</span>' : ''}</div>
                                </div>
                                <span class="chevron">›</span>
                            </div>
                        `).join('');
                        usersList.querySelectorAll('.user-item').forEach((el) => {
                            el.addEventListener('click', () => {
                                const id = parseInt(el.getAttribute('data-id'));
                                const username = el.getAttribute('data-username');
                                const displayName = el.getAttribute('data-display-name');
                                const avatarUrl = el.getAttribute('data-avatar');
                                const isOnline = el.getAttribute('data-online') === 'true';
                                this.startConversation(id, username, displayName, avatarUrl, isOnline);
                                modal.remove();
                            });
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
    },
}).mount('#app');
