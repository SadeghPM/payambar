const API_URL = (typeof window !== 'undefined' && window.API_URL)
    ? window.API_URL
    : (window.location.origin + '/api');

const WS_URL = (typeof window !== 'undefined' && window.WS_URL)
    ? window.WS_URL
    : (window.location.origin.replace(/^http/, 'ws') + '/ws');

const { createApp } = Vue;
const NEW_CHAT_SEARCH_DEBOUNCE_MS = 300;

const UserSearchItem = {
    props: {
        user: {
            type: Object,
            required: true,
        },
    },
    emits: ['select'],
    computed: {
        normalizedUser() {
            const userId = Number(this.user?.id);
            const username = typeof this.user?.username === 'string' ? this.user.username : '';
            const displayName = typeof this.user?.display_name === 'string' ? this.user.display_name : '';
            const avatarUrl = typeof this.user?.avatar_url === 'string' ? this.user.avatar_url : '';
            const isOnline = !!this.user?.is_online;
            return {
                id: userId,
                username,
                displayName,
                avatarUrl,
                isOnline,
                nameLabel: displayName || username || '?',
            };
        },
    },
    methods: {
        selectUser() {
            this.$emit('select', this.normalizedUser);
        },
    },
    template: `
        <div class="user-item" @click="selectUser">
            <div class="user-avatar-wrapper">
                <img v-if="normalizedUser.avatarUrl" :src="normalizedUser.avatarUrl" class="user-avatar" alt="avatar">
                <span v-else class="user-avatar-placeholder">{{ normalizedUser.nameLabel.charAt(0).toUpperCase() }}</span>
                <span v-if="normalizedUser.isOnline" class="online-indicator"></span>
            </div>
            <div class="user-info">
                <div class="user-display-name">{{ normalizedUser.nameLabel }}</div>
                <div class="user-username">
                    @{{ normalizedUser.username }}
                    <span v-if="normalizedUser.isOnline" class="online-text"> آنلاین</span>
                </div>
            </div>
            <span class="chevron">›</span>
        </div>
    `,
};

const app = createApp({
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
            wsReconnectTimer: null,
            wsIntentionalClose: false,
            wsConnected: false,
            authTab: 'login',
            login: { username: '', password: '' },
            register: { username: '', password: '', confirm: '' },
            authPassword: '',
            suppressBackupWarningOnce: false,
            showRulesModal: false,
            acceptRules: false,
            authError: '',
            chatListOpen: true,
            loadingMessages: false,
            loadingOlderMessages: false,
            loadingConversations: false,
            hasMoreMessages: {},
            uploadingFile: false,
            recordingVoice: false,
            recordingElapsedSec: 0,
            recordingTimer: null,
            recordingStream: null,
            mediaRecorder: null,
            recordedChunks: [],
            sendingVoice: false,
            showNewChatModal: false,
            newChatSearchQuery: '',
            newChatSearchResults: [],
            newChatSearchLoading: false,
            newChatSearchError: '',
            newChatSearchTimeout: null,
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
            // Push notification state
            pushNotificationsEnabled: false,
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
            e2ee: {
                enabled: true,
                ready: false,
                ownerUserId: null,
                deviceId: '',
                keyId: '',
                privateJwk: null,
                publicJwk: null,
                recipientKeys: {},
                recipientKeyPromises: {},
                recipientKeyMeta: {}, // { [userId]: { fetchedAt: number } }
                noKeyWarnedRecipients: {},
            },
        };
    },
    computed: {
        isAuthed() {
            return !!this.token && !!this.userId && this.userId > 0;
        },
        userProfileStatusText() {
            if (!this.isAuthed) return '';
            if (this.wsConnected) {
                return 'آنلاین';
            }
            return 'در حال اتصال...';
        },
        filteredConversations() {
            const convs = this.getSortedConversations();
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
            this.ensureE2EEReady().catch((err) => console.warn('E2EE init skipped:', err));
            this.connectWebSocket();
            this.fetchWebRTCConfig();
            this.restorePushSubscription();
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
    beforeUnmount() {
        this.cleanupVoiceRecorder();
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

        utf8ToBase64Url(value) {
            const bytes = new TextEncoder().encode(value);
            return this.bytesToBase64Url(bytes);
        },
        base64UrlToUtf8(value) {
            const bytes = this.base64UrlToBytes(value);
            return new TextDecoder().decode(bytes);
        },
        bytesToBase64Url(bytes) {
            let binary = '';
            bytes.forEach((b) => { binary += String.fromCharCode(b); });
            return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
        },
        base64UrlToBytes(value) {
            const base64 = value.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((value.length + 3) % 4);
            const binary = atob(base64);
            const out = new Uint8Array(binary.length);
            for (let i = 0; i < binary.length; i += 1) out[i] = binary.charCodeAt(i);
            return out;
        },
        async ensureE2EEReady() {
            if (!this.e2ee.enabled || !window.crypto?.subtle || !this.token || !this.userId) return false;
            if (
                this.e2ee.ready &&
                this.e2ee.privateJwk &&
                this.e2ee.publicJwk &&
                Number(this.e2ee.ownerUserId) === Number(this.userId)
            ) return true;

            if (Number(this.e2ee.ownerUserId) !== Number(this.userId)) {
                this.resetE2EEState();
            }

            const storagePrefix = `payambar:e2ee:${this.userId}`;
            const storedPrivate = localStorage.getItem(`${storagePrefix}:private_jwk`);
            const storedPublic = localStorage.getItem(`${storagePrefix}:public_jwk`);
            const storedDeviceId = localStorage.getItem(`${storagePrefix}:device_id`);
            const storedKeyId = localStorage.getItem(`${storagePrefix}:key_id`);

            const passwordForBackup = this.authPassword || '';
            let keysFromExistingSource = false;

            if (storedPrivate && storedPublic && storedDeviceId && storedKeyId) {
                this.e2ee.privateJwk = JSON.parse(storedPrivate);
                this.e2ee.publicJwk = JSON.parse(storedPublic);
                this.e2ee.deviceId = storedDeviceId;
                this.e2ee.keyId = storedKeyId;
                this.e2ee.ownerUserId = this.userId;
                keysFromExistingSource = true;
            } else if (passwordForBackup) {
                // Try restoring from server backup
                const myDevices = await this.getMyDeviceKeys();
                const backupDevice = (myDevices || []).find((d) => d.enc_private_key);
                if (backupDevice) {
                    try {
                        const { privateJwk, publicJwk } = await this.decryptPrivateKeyBackup(backupDevice, passwordForBackup);
                        this.e2ee.privateJwk = privateJwk;
                        this.e2ee.publicJwk = publicJwk;
                        this.e2ee.deviceId = backupDevice.device_id;
                        this.e2ee.keyId = backupDevice.key_id;
                        this.e2ee.ownerUserId = this.userId;
                        keysFromExistingSource = true;
                        localStorage.setItem(`${storagePrefix}:private_jwk`, JSON.stringify(privateJwk));
                        localStorage.setItem(`${storagePrefix}:public_jwk`, JSON.stringify(publicJwk));
                        localStorage.setItem(`${storagePrefix}:device_id`, backupDevice.device_id);
                        localStorage.setItem(`${storagePrefix}:key_id`, backupDevice.key_id);
                    } catch (err) {
                        console.warn('Failed to decrypt backed up key', err);
                        alert('بازیابی کلید امن با رمز عبور فعلی ممکن نیست. پیام‌های قدیمی ممکن است قابل خواندن نباشند.');
                    }
                } else {
                    if (!this.suppressBackupWarningOnce) {
                        alert('پشتیبان کلید امنی روی سرور پیدا نشد. کلید جدید ساخته می‌شود و پیام‌های رمزنگاری‌شده قبلی در این دستگاه قابل خواندن نیست.');
                    }
                }
            }

            if (!this.e2ee.privateJwk || !this.e2ee.publicJwk) {
                const keyPair = await window.crypto.subtle.generateKey(
                    { name: 'ECDH', namedCurve: 'P-256' },
                    true,
                    ['deriveBits']
                );
                const privateJwk = await window.crypto.subtle.exportKey('jwk', keyPair.privateKey);
                const publicJwk = await window.crypto.subtle.exportKey('jwk', keyPair.publicKey);
                const deviceId = (window.crypto.randomUUID ? window.crypto.randomUUID() : `web-${Date.now()}`);
                const keyId = `k-${Date.now()}`;
                localStorage.setItem(`${storagePrefix}:private_jwk`, JSON.stringify(privateJwk));
                localStorage.setItem(`${storagePrefix}:public_jwk`, JSON.stringify(publicJwk));
                localStorage.setItem(`${storagePrefix}:device_id`, deviceId);
                localStorage.setItem(`${storagePrefix}:key_id`, keyId);
                this.e2ee.privateJwk = privateJwk;
                this.e2ee.publicJwk = publicJwk;
                this.e2ee.deviceId = deviceId;
                this.e2ee.keyId = keyId;
                this.e2ee.ownerUserId = this.userId;
            }

            // Backup (and publish) device key
            let backupPayload = {};
            if (passwordForBackup) {
                try {
                    backupPayload = await this.encryptPrivateKeyForBackup(this.e2ee.privateJwk, passwordForBackup);
                } catch (err) {
                    console.warn('Encrypt private key for backup failed', err);
                }
            }

            try {
                const res = await fetch(`${API_URL}/keys/devices`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${this.token}` },
                    body: JSON.stringify({
                        device_id: this.e2ee.deviceId,
                        algorithm: 'ECDH-P256',
                        public_key: this.utf8ToBase64Url(JSON.stringify(this.e2ee.publicJwk)),
                        key_id: this.e2ee.keyId,
                        ...backupPayload,
                    }),
                });
                if (!res.ok) throw new Error('Device key publish failed');
            } catch (err) {
                console.warn('Failed to publish device key:', err);
                if (!keysFromExistingSource) {
                    // New keys that were never published — recipient can't decrypt
                    this.authPassword = '';
                    return false;
                }
                // Keys were previously published, local encryption still works
            }
            this.e2ee.ready = true;
            this.authPassword = '';
            this.suppressBackupWarningOnce = false;
            return true;
        },

        async getMyDeviceKeys() {
            const res = await fetch(`${API_URL}/keys/devices/self`, {
                headers: { Authorization: `Bearer ${this.token}` },
            });
            if (!res.ok) return [];
            const data = await res.json();
            return data.devices || [];
        },

        async encryptPrivateKeyForBackup(privateJwk, password) {
            const saltBytes = window.crypto.getRandomValues(new Uint8Array(16));
            const ivBytes = window.crypto.getRandomValues(new Uint8Array(12));
            const derivedKey = await this.derivePasswordKey(password, saltBytes, 150000);
            const encoded = new TextEncoder().encode(JSON.stringify(privateJwk));
            const encrypted = await window.crypto.subtle.encrypt({ name: 'AES-GCM', iv: ivBytes }, derivedKey, encoded);
            return {
                enc_private_key: this.bytesToBase64Url(new Uint8Array(encrypted)),
                enc_private_key_iv: this.bytesToBase64Url(ivBytes),
                kdf_salt: this.bytesToBase64Url(saltBytes),
                kdf_iterations: 150000,
                kdf_alg: 'PBKDF2-SHA256',
                key_wrap_version: 1,
            };
        },

        async decryptPrivateKeyBackup(device, password) {
            if (!device?.enc_private_key || !device?.enc_private_key_iv || !device?.kdf_salt || !device?.kdf_iterations) {
                throw new Error('missing backup fields');
            }
            const saltBytes = this.base64UrlToBytes(device.kdf_salt);
            const ivBytes = this.base64UrlToBytes(device.enc_private_key_iv);
            const derivedKey = await this.derivePasswordKey(password, saltBytes, device.kdf_iterations);
            const decrypted = await window.crypto.subtle.decrypt(
                { name: 'AES-GCM', iv: ivBytes },
                derivedKey,
                this.base64UrlToBytes(device.enc_private_key)
            );
            const privateJwk = JSON.parse(new TextDecoder().decode(decrypted));
            return {
                privateJwk,
                publicJwk: JSON.parse(this.base64UrlToUtf8(device.public_key)),
            };
        },

        async derivePasswordKey(password, saltBytes, iterations) {
            const enc = new TextEncoder().encode(password);
            const baseKey = await window.crypto.subtle.importKey('raw', enc, 'PBKDF2', false, ['deriveKey']);
            return window.crypto.subtle.deriveKey(
                {
                    name: 'PBKDF2',
                    salt: saltBytes,
                    iterations,
                    hash: 'SHA-256',
                },
                baseKey,
                { name: 'AES-GCM', length: 256 },
                false,
                ['encrypt', 'decrypt']
            );
        },
        async getUserDeviceKeys(userId) {
            const TTL_POPULATED_MS = 30000;
            const TTL_EMPTY_MS = 3000;
            const meta = this.e2ee.recipientKeyMeta[userId];
            if (this.e2ee.recipientKeys[userId] && meta && Date.now() - meta.fetchedAt < meta.ttl) {
                return this.e2ee.recipientKeys[userId];
            }
            if (this.e2ee.recipientKeyPromises[userId]) return this.e2ee.recipientKeyPromises[userId];

            const fetchPromise = (async () => {
                const res = await fetch(`${API_URL}/keys/users/${userId}/devices`, {
                    headers: { Authorization: `Bearer ${this.token}` },
                });
                if (!res.ok) throw new Error('failed to fetch device keys');
                const data = await res.json();
                const devices = (data.devices || []).filter((d) =>
                    (d.algorithm || '').toUpperCase() === 'ECDH-P256' && !!d.public_key
                );
                this.e2ee.recipientKeys[userId] = devices;
                this.e2ee.recipientKeyMeta[userId] = {
                    fetchedAt: Date.now(),
                    ttl: devices.length ? TTL_POPULATED_MS : TTL_EMPTY_MS,
                };
                return devices;
            })();

            this.e2ee.recipientKeyPromises[userId] = fetchPromise.finally(() => {
                delete this.e2ee.recipientKeyPromises[userId];
            });

            return fetchPromise;
        },
        async getRecipientDeviceKey(userId, { keyId = null, deviceId = null } = {}) {
            const devices = await this.getUserDeviceKeys(userId);
            if (!devices.length) return null;

            if (keyId || deviceId) {
                const matched = devices.find((d) =>
                    (!keyId || d.key_id === keyId) && (!deviceId || d.device_id === deviceId)
                );
                if (matched) return matched;
            }

            return devices[0] || null;
        },
        async deriveAesKeyFromDevice(device) {
            const privateKey = await window.crypto.subtle.importKey(
                'jwk',
                this.e2ee.privateJwk,
                { name: 'ECDH', namedCurve: 'P-256' },
                false,
                ['deriveBits']
            );
            const publicJwk = JSON.parse(this.base64UrlToUtf8(device.public_key));
            const recipientPublicKey = await window.crypto.subtle.importKey(
                'jwk',
                publicJwk,
                { name: 'ECDH', namedCurve: 'P-256' },
                false,
                []
            );
            const bits = await window.crypto.subtle.deriveBits(
                { name: 'ECDH', public: recipientPublicKey },
                privateKey,
                256
            );
            return window.crypto.subtle.importKey('raw', bits, { name: 'AES-GCM' }, false, ['encrypt', 'decrypt']);
        },
        async encryptTextMessage(receiverId, plainText) {
            try {
                const ready = await this.ensureE2EEReady();
                if (!ready) return null;
                const device = await this.getRecipientDeviceKey(receiverId);
                if (!device) return null;
                const aesKey = await this.deriveAesKeyFromDevice(device);
                const ivBytes = window.crypto.getRandomValues(new Uint8Array(12));
                const encoded = new TextEncoder().encode(plainText);
                const encryptedBuffer = await window.crypto.subtle.encrypt({ name: 'AES-GCM', iv: ivBytes }, aesKey, encoded);
                return {
                    encrypted: true,
                    e2ee_v: 1,
                    alg: 'AES-256-GCM',
                    sender_device_id: this.e2ee.deviceId,
                    key_id: this.e2ee.keyId,
                    iv: this.bytesToBase64Url(ivBytes),
                    ciphertext: this.bytesToBase64Url(new Uint8Array(encryptedBuffer)),
                };
            } catch (err) {
                console.warn('E2EE encryption failed, will send plaintext:', err);
                return null;
            }
        },
        async maybeDecryptMessage(msg) {
            if (!msg?.encrypted || !msg?.ciphertext || !msg?.iv) return msg;
            try {
                const isOutgoing = Number(msg.sender_id) === Number(this.userId);
                const peerId = isOutgoing ? Number(msg.receiver_id) : Number(msg.sender_id);
                const device = await this.getRecipientDeviceKey(
                    peerId,
                    isOutgoing ? {} : { keyId: msg.key_id, deviceId: msg.sender_device_id }
                );
                if (!device) return { ...msg, content: '🔒 پیام رمزنگاری شده' };
                const aesKey = await this.deriveAesKeyFromDevice(device);
                const plaintextBuffer = await window.crypto.subtle.decrypt(
                    { name: 'AES-GCM', iv: this.base64UrlToBytes(msg.iv) },
                    aesKey,
                    this.base64UrlToBytes(msg.ciphertext)
                );
                const content = new TextDecoder().decode(plaintextBuffer);
                return { ...msg, content };
            } catch (err) {
                console.warn('Decrypt failed', err);
                return { ...msg, content: '🔒 پیام رمزنگاری شده (قابل خواندن نیست)' };
            }
        },
        async decryptMessageList(messages) {
            if (!Array.isArray(messages) || messages.length === 0) return [];
            return Promise.all(messages.map((m) => this.maybeDecryptMessage(m)));
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
        shouldShowMessageStatus(msg, index) {
            if (!msg) return false;
            if (Number(msg.sender_id) !== Number(this.userId)) return false;
            const list = this.messagesForCurrent || [];
            for (let i = list.length - 1; i >= 0; i--) {
                if (Number(list[i]?.sender_id) === Number(this.userId)) {
                    return i === index;
                }
            }
            return false;
        },
        formatRecordingDuration(seconds) {
            const mins = Math.floor(seconds / 60).toString().padStart(2, '0');
            const secs = (seconds % 60).toString().padStart(2, '0');
            return `${mins}:${secs}`;
        },
        isAudioMessage(msg) {
            if (!msg || !msg.file_url) return false;
            const fileName = this.getMessageFileName(msg);
            if (fileName.startsWith('voice-')) return true;
            const contentType = typeof msg.file_content_type === 'string' ? msg.file_content_type.toLowerCase() : '';
            if (contentType.startsWith('audio/')) return true;
            return (
                fileName.endsWith('.webm') ||
                fileName.endsWith('.ogg') ||
                fileName.endsWith('.mp3') ||
                fileName.endsWith('.wav') ||
                fileName.endsWith('.m4a')
            );
        },
        isImageMessage(msg) {
            if (!msg || !msg.file_url) return false;
            const contentType = typeof msg.file_content_type === 'string' ? msg.file_content_type.toLowerCase() : '';
            if (contentType.startsWith('image/')) return true;
            const fileName = this.getMessageFileName(msg);
            return (
                fileName.endsWith('.jpg') ||
                fileName.endsWith('.jpeg') ||
                fileName.endsWith('.png') ||
                fileName.endsWith('.gif') ||
                fileName.endsWith('.webp') ||
                fileName.endsWith('.bmp') ||
                fileName.endsWith('.svg')
            );
        },
        isVideoMessage(msg) {
            if (!msg || !msg.file_url) return false;
            if (this.isAudioMessage(msg)) return false;
            const contentType = typeof msg.file_content_type === 'string' ? msg.file_content_type.toLowerCase() : '';
            if (contentType.startsWith('video/')) return true;
            const fileName = this.getMessageFileName(msg);
            return (
                fileName.endsWith('.mp4') ||
                fileName.endsWith('.webm') ||
                fileName.endsWith('.mov') ||
                fileName.endsWith('.mkv') ||
                fileName.endsWith('.m4v')
            );
        },
        getMessageFileName(msg) {
            const fromName = (msg?.file_name || '').toLowerCase();
            if (fromName) return fromName;
            try {
                const url = String(msg?.file_url || '').split('?')[0];
                return url.toLowerCase();
            } catch (e) {
                return '';
            }
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
        parseTimestamp(value) {
            if (!value) return 0;
            const ts = new Date(value).getTime();
            return Number.isFinite(ts) ? ts : 0;
        },
        getConversationLastTimestamp(conv) {
            if (!conv) return 0;
            const fromConversation = this.parseTimestamp(conv.last_message_at);
            const localMessages = this.messages[conv.user_id] || [];
            let localMax = 0;
            for (const msg of localMessages) {
                const ts = this.parseTimestamp(msg?.created_at);
                if (ts > localMax) localMax = ts;
            }
            return Math.max(fromConversation, localMax);
        },
        getSortedConversations() {
            return [...this.conversations].sort((a, b) => {
                return this.getConversationLastTimestamp(b) - this.getConversationLastTimestamp(a);
            });
        },
        sortConversationsInPlace() {
            this.conversations.sort((a, b) => {
                return this.getConversationLastTimestamp(b) - this.getConversationLastTimestamp(a);
            });
        },
        updateConversationLastMessage(userId, timestamp) {
            if (!userId || !timestamp) return;
            const idx = this.conversations.findIndex(c => c.user_id === userId);
            if (idx === -1) return;
            this.conversations[idx].last_message_at = timestamp;
            this.sortConversationsInPlace();
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
                this.authPassword = this.login.password;
                this.suppressBackupWarningOnce = false;
                this.setAuth(data);
            } catch (err) {
                this.authError = err.message;
            }
        },
        async handleRegister() {
            this.authError = '';
            if (!this.acceptRules) {
                this.authError = 'لطفاً قوانین را بپذیرید.';
                return;
            }
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
                this.authPassword = this.register.password;
                this.suppressBackupWarningOnce = true; // first device has no backup; avoid warning
                this.setAuth(data);
            } catch (err) {
                this.authError = err.message;
            }
        },
        setAuth(data) {
            this.closeWebSocket(true);
            if (Number(this.userId) !== Number(data.user_id)) {
                this.resetE2EEState();
            }
            this.token = data.token;
            this.userId = data.user_id;
            this.username = data.username;
            localStorage.setItem('token', this.token);
            localStorage.setItem('userId', this.userId);
            localStorage.setItem('username', this.username);
            this.loadConversations();
            this.loadMyProfile();
            // Ensure device key is registered as soon as the user is authenticated
            this.ensureE2EEReady().catch((err) => console.warn('E2EE init after auth failed', err));
            this.connectWebSocket();
        },
        resetE2EEState() {
            this.e2ee.ready = false;
            this.e2ee.ownerUserId = null;
            this.e2ee.deviceId = '';
            this.e2ee.keyId = '';
            this.e2ee.privateJwk = null;
            this.e2ee.publicJwk = null;
            this.e2ee.recipientKeys = {};
            this.e2ee.recipientKeyPromises = {};
            this.e2ee.recipientKeyMeta = {};
            this.e2ee.noKeyWarnedRecipients = {};
        },
        clearAuth() {
            this.resetE2EEState();
            this.token = null;
            this.userId = null;
            this.username = null;
            this.acceptRules = false;
            this.authPassword = '';
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
            this.showNewChatModal = false;
            this.newChatSearchQuery = '';
            this.newChatSearchResults = [];
            this.newChatSearchLoading = false;
            this.newChatSearchError = '';
            this.cleanupVoiceRecorder();
            this.conversationMenu = { show: false, x: 0, y: 0, conversation: null };
            this.serverOffline = false;
            this.wsReconnectAttempts = 0;
            if (this.newChatSearchTimeout) {
                clearTimeout(this.newChatSearchTimeout);
                this.newChatSearchTimeout = null;
            }
            localStorage.clear();
            this.closeWebSocket(true);
        },
        handleLogout() {
            if (confirm('آیا از خروج اطمینان دارید؟')) {
                this.clearAuth();
            }
        },
        openRulesModal() {
            this.showRulesModal = true;
        },
        closeRulesModal() {
            this.showRulesModal = false;
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
        // ── Push Notifications ─────────────────────────────────────────────
        async restorePushSubscription() {
            const stored = localStorage.getItem('pushNotificationsEnabled');
            if (stored === 'true') {
                this.pushNotificationsEnabled = true;
                // Re-subscribe silently to keep subscription fresh
                try {
                    await this.subscribePush();
                } catch (err) {
                    console.warn('Failed to restore push subscription:', err);
                }
            }
        },
        async togglePushNotifications() {
            if (this.pushNotificationsEnabled) {
                try {
                    await this.subscribePush();
                    localStorage.setItem('pushNotificationsEnabled', 'true');
                } catch (err) {
                    console.error('Push subscribe failed:', err);
                    this.pushNotificationsEnabled = false;
                    localStorage.removeItem('pushNotificationsEnabled');
                    alert('فعال‌سازی اعلان‌ها ناموفق بود');
                }
            } else {
                try {
                    await this.unsubscribePush();
                } catch (err) {
                    console.error('Push unsubscribe failed:', err);
                }
                localStorage.removeItem('pushNotificationsEnabled');
            }
        },
        async subscribePush() {
            if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
                throw new Error('Push notifications not supported');
            }

            const permission = await Notification.requestPermission();
            if (permission !== 'granted') {
                throw new Error('Notification permission denied');
            }

            // Get VAPID public key from server
            const vapidRes = await fetch(`${API_URL}/push/vapid-key`);
            if (!vapidRes.ok) throw new Error('Push not configured on server');
            const { vapid_public_key } = await vapidRes.json();

            // Convert VAPID key to Uint8Array
            const urlBase64ToUint8Array = (base64String) => {
                const padding = '='.repeat((4 - base64String.length % 4) % 4);
                const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
                const raw = atob(base64);
                const arr = new Uint8Array(raw.length);
                for (let i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
                return arr;
            };

            const reg = await navigator.serviceWorker.ready;
            const subscription = await reg.pushManager.subscribe({
                userVisibleOnly: true,
                applicationServerKey: urlBase64ToUint8Array(vapid_public_key),
            });

            const subJSON = subscription.toJSON();
            const res = await fetch(`${API_URL}/push/subscribe`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    Authorization: `Bearer ${this.token}`,
                },
                body: JSON.stringify({
                    endpoint: subJSON.endpoint,
                    keys: {
                        p256dh: subJSON.keys.p256dh,
                        auth: subJSON.keys.auth,
                    },
                }),
            });
            if (!res.ok) throw new Error('Server rejected subscription');
        },
        async unsubscribePush() {
            try {
                const reg = await navigator.serviceWorker.ready;
                const subscription = await reg.pushManager.getSubscription();
                if (subscription) {
                    const subJSON = subscription.toJSON();
                    await fetch(`${API_URL}/push/subscribe`, {
                        method: 'DELETE',
                        headers: {
                            'Content-Type': 'application/json',
                            Authorization: `Bearer ${this.token}`,
                        },
                        body: JSON.stringify({ endpoint: subJSON.endpoint }),
                    });
                    await subscription.unsubscribe();
                }
            } catch (err) {
                console.warn('Unsubscribe error:', err);
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
                this.sortConversationsInPlace();
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
                this.messages[conv.user_id] = await this.decryptMessageList(data.messages || []);
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

            let encryptedPayload = null;
            try {
                encryptedPayload = await this.encryptTextMessage(this.currentConversationId, content);
            } catch (err) {
                console.warn('Encryption error, sending plaintext:', err);
            }
            if (this.e2ee.enabled && !encryptedPayload && !this.e2ee.noKeyWarnedRecipients[this.currentConversationId]) {
                alert('ارسال امن ممکن نیست؛ کلید مخاطب در دسترس نیست. پیام به صورت غیر رمزنگاری‌شده ارسال می‌شود.');
                this.e2ee.noKeyWarnedRecipients[this.currentConversationId] = true;
            }

            const payload = {
                type: 'message',
                receiver_id: this.currentConversationId,
                content: encryptedPayload ? '' : content,
                client_message_id: clientMessageId,
            };
            if (encryptedPayload) Object.assign(payload, encryptedPayload);

            this.ws.send(JSON.stringify(payload));
            this.$nextTick(() => this.scrollToBottom());
        },
        async sendFileMessage(file) {
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
                const messageID = Number(data.message_id);
                const wsOpen = this.ws && this.ws.readyState === WebSocket.OPEN;

                // Single source of truth to prevent duplicates:
                // when WS is connected, wait for WS echo and do not append locally.
                if (!wsOpen) {
                    if (!this.messages[this.currentConversationId]) this.messages[this.currentConversationId] = [];
                    const existingIdx = this.messages[this.currentConversationId]
                        .findIndex((m) => Number(m.id) === messageID);
                    const createdAt = new Date().toISOString();
                    const msg = {
                        id: messageID,
                        sender_id: this.userId,
                        receiver_id: this.currentConversationId,
                        content: `📎 ${data.file_name}`,
                        file_name: data.file_name,
                        file_url: data.file_url,
                        file_content_type: data.file_content_type || file.type || '',
                        status: 'sent',
                        created_at: createdAt,
                    };
                    if (existingIdx >= 0) {
                        this.messages[this.currentConversationId][existingIdx] = {
                            ...this.messages[this.currentConversationId][existingIdx],
                            ...msg,
                        };
                    } else {
                        this.messages[this.currentConversationId].push(msg);
                    }
                    this.updateConversationLastMessage(this.currentConversationId, createdAt);
                    this.$nextTick(() => this.scrollToBottom());
                }
                this.loadConversations();
            } catch (err) {
                console.error('File upload error:', err);
                alert('خطا در آپلود فایل');
            } finally {
                this.uploadingFile = false;
            }
        },
        async handleFileSelect(event) {
            const file = event.target.files[0];
            if (!file || !this.currentConversationId) return;
            await this.sendFileMessage(file);
            event.target.value = ''; // Reset file input
        },
        cleanupVoiceRecorder() {
            if (this.recordingTimer) {
                clearInterval(this.recordingTimer);
                this.recordingTimer = null;
            }
            if (this.mediaRecorder) {
                this.mediaRecorder.ondataavailable = null;
                this.mediaRecorder.onstop = null;
                this.mediaRecorder = null;
            }
            if (this.recordingStream) {
                this.recordingStream.getTracks().forEach((track) => track.stop());
                this.recordingStream = null;
            }
            this.recordedChunks = [];
            this.recordingVoice = false;
            this.recordingElapsedSec = 0;
        },
        async toggleVoiceRecording() {
            if (!this.currentConversationId || this.uploadingFile || this.sendingVoice) return;
            if (this.recordingVoice) {
                this.stopVoiceRecordingAndSend();
                return;
            }
            await this.startVoiceRecording();
        },
        async startVoiceRecording() {
            try {
                const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
                const preferredType = MediaRecorder.isTypeSupported('audio/webm;codecs=opus')
                    ? 'audio/webm;codecs=opus'
                    : (MediaRecorder.isTypeSupported('audio/webm') ? 'audio/webm' : '');
                const recorder = preferredType
                    ? new MediaRecorder(stream, { mimeType: preferredType })
                    : new MediaRecorder(stream);

                this.recordedChunks = [];
                this.recordingStream = stream;
                this.mediaRecorder = recorder;
                this.recordingVoice = true;
                this.recordingElapsedSec = 0;

                recorder.ondataavailable = (event) => {
                    if (event.data && event.data.size > 0) {
                        this.recordedChunks.push(event.data);
                    }
                };

                recorder.onstop = async () => {
                    const mimeType = recorder.mimeType || 'audio/webm';
                    const blob = new Blob(this.recordedChunks, { type: mimeType });
                    this.cleanupVoiceRecorder();

                    if (blob.size === 0) return;
                    this.sendingVoice = true;
                    const extension = mimeType.includes('ogg')
                        ? 'ogg'
                        : (mimeType.includes('mp4') || mimeType.includes('m4a') ? 'm4a' : 'webm');
                    const file = new File([blob], `voice-${Date.now()}.${extension}`, { type: mimeType });
                    await this.sendFileMessage(file);
                    this.sendingVoice = false;
                };

                recorder.start(250);
                this.recordingTimer = setInterval(() => {
                    this.recordingElapsedSec += 1;
                }, 1000);
            } catch (err) {
                console.error('Voice recording error:', err);
                alert('دسترسی میکروفون لازم است');
                this.cleanupVoiceRecorder();
            }
        },
        stopVoiceRecordingAndSend() {
            if (!this.mediaRecorder || this.mediaRecorder.state === 'inactive') return;
            if (this.recordingTimer) {
                clearInterval(this.recordingTimer);
                this.recordingTimer = null;
            }
            this.recordingVoice = false;
            this.mediaRecorder.stop();
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
                const olderMessages = await this.decryptMessageList(data.messages || []);

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
                this.messages[this.currentConversationId] = await this.decryptMessageList(data.messages || []);
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
        closeWebSocket(intentional = true) {
            this.wsIntentionalClose = intentional;
            this.wsConnected = false;
            if (this.wsReconnectTimer) {
                clearTimeout(this.wsReconnectTimer);
                this.wsReconnectTimer = null;
            }
            if (this.ws) {
                try { this.ws.close(); } catch (e) { }
                this.ws = null;
            }
            if (intentional) {
                this.wsReconnectAttempts = 0;
                this.serverOffline = false;
            }
        },
        connectWebSocket() {
            const token = this.token;
            const isTokenValid = typeof token === 'string' && token && token !== 'undefined' && token !== 'null';
            if (!this.isAuthed || !isTokenValid) {
                return;
            }
            if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
                return;
            }
            if (this.wsReconnectTimer) {
                clearTimeout(this.wsReconnectTimer);
                this.wsReconnectTimer = null;
            }
            this.wsIntentionalClose = false;
            this.wsConnected = false;
            const wsUrlWithToken = `${WS_URL}?token=${encodeURIComponent(token)}`;
            this.ws = new WebSocket(wsUrlWithToken);

            this.ws.onopen = () => {
                this.wsReconnectAttempts = 0;
                this.serverOffline = false;
                this.wsConnected = true;
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
                if (!this.isAuthed || this.wsIntentionalClose) {
                    return;
                }
                console.error('WebSocket error:', err);
                this.serverOffline = true;
                this.wsConnected = false;
            };

            this.ws.onclose = () => {
                const isIntentional = this.wsIntentionalClose || !this.isAuthed;
                this.ws = null;
                this.wsConnected = false;
                if (isIntentional) {
                    this.wsIntentionalClose = false;
                    return;
                }
                this.serverOffline = true;
                if (this.wsReconnectAttempts < this.wsMaxReconnectAttempts && this.isAuthed) {
                    this.wsReconnectAttempts++;
                    this.wsReconnectTimer = setTimeout(() => {
                        this.wsReconnectTimer = null;
                        this.connectWebSocket();
                    }, this.wsReconnectDelay);
                }
            };
        },
        async handleWebSocketMessage(data) {
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
                const normalizedMessage = await this.maybeDecryptMessage(data);
                const incomingContent = normalizedMessage.content;
                const convUser = data.sender_id === this.userId ? data.receiver_id : data.sender_id;
                if (!this.messages[convUser]) this.messages[convUser] = [];
                const incomingID = Number(data.message_id);
                const existingByID = this.messages[convUser].findIndex((m) => Number(m.id) === incomingID);

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
                            file_content_type: data.file_content_type || this.messages[convUser][idx].file_content_type,
                        };
                    } else if (existingByID >= 0) {
                        this.messages[convUser][existingByID] = {
                            ...this.messages[convUser][existingByID],
                            status: data.status,
                            file_name: data.file_name || this.messages[convUser][existingByID].file_name,
                            file_url: data.file_url || this.messages[convUser][existingByID].file_url,
                            file_content_type: data.file_content_type || this.messages[convUser][existingByID].file_content_type,
                        };
                    } else {
                        this.messages[convUser].push({
                            id: data.message_id,
                            sender_id: data.sender_id,
                            receiver_id: data.receiver_id,
                            content: incomingContent,
                            status: data.status,
                            created_at: data.created_at,
                            client_message_id: data.client_message_id,
                            file_name: data.file_name,
                            file_url: data.file_url,
                            file_content_type: data.file_content_type,
                            encrypted: !!data.encrypted,
                            e2ee_v: data.e2ee_v,
                            alg: data.alg,
                            sender_device_id: data.sender_device_id,
                            key_id: data.key_id,
                            iv: data.iv,
                            ciphertext: data.ciphertext,
                            aad: data.aad,
                        });
                    }
                } else {
                    if (existingByID >= 0) {
                        this.messages[convUser][existingByID] = {
                            ...this.messages[convUser][existingByID],
                            status: data.status,
                            file_name: data.file_name || this.messages[convUser][existingByID].file_name,
                            file_url: data.file_url || this.messages[convUser][existingByID].file_url,
                            file_content_type: data.file_content_type || this.messages[convUser][existingByID].file_content_type,
                        };
                    } else {
                        this.messages[convUser].push({
                            id: data.message_id,
                            sender_id: data.sender_id,
                            receiver_id: data.receiver_id,
                            content: incomingContent,
                            status: data.status,
                            created_at: data.created_at,
                            file_name: data.file_name,
                            file_url: data.file_url,
                            file_content_type: data.file_content_type,
                            encrypted: !!data.encrypted,
                            e2ee_v: data.e2ee_v,
                            alg: data.alg,
                            sender_device_id: data.sender_device_id,
                            key_id: data.key_id,
                            iv: data.iv,
                            ciphertext: data.ciphertext,
                            aad: data.aad,
                        });
                    }
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
            this.showNewChatModal = true;
            this.newChatSearchQuery = '';
            this.newChatSearchResults = [];
            this.newChatSearchError = '';
            this.newChatSearchLoading = false;
            this.$nextTick(() => {
                const input = this.$refs.newChatSearchInput;
                if (input && typeof input.focus === 'function') {
                    input.focus();
                }
            });
        },
        closeNewChatModal() {
            this.showNewChatModal = false;
            this.newChatSearchQuery = '';
            this.newChatSearchResults = [];
            this.newChatSearchError = '';
            this.newChatSearchLoading = false;
            if (this.newChatSearchTimeout) {
                clearTimeout(this.newChatSearchTimeout);
                this.newChatSearchTimeout = null;
            }
        },
        onNewChatSearchInput() {
            const query = this.newChatSearchQuery.trim();
            this.newChatSearchError = '';
            if (this.newChatSearchTimeout) {
                clearTimeout(this.newChatSearchTimeout);
                this.newChatSearchTimeout = null;
            }
            if (!query) {
                this.newChatSearchLoading = false;
                this.newChatSearchResults = [];
                return;
            }
            this.newChatSearchLoading = true;
            this.newChatSearchTimeout = setTimeout(() => {
                this.searchUsersForNewChat(query);
            }, NEW_CHAT_SEARCH_DEBOUNCE_MS);
        },
        async searchUsersForNewChat(query) {
            try {
                const res = await fetch(`${API_URL}/users?q=${encodeURIComponent(query)}`, {
                    headers: { Authorization: `Bearer ${this.token}` }
                });
                if (!res.ok) throw new Error('Search failed');
                const users = await res.json();
                this.newChatSearchResults = Array.isArray(users) ? users : [];
            } catch (err) {
                console.error('Search error:', err);
                this.newChatSearchResults = [];
                this.newChatSearchError = 'خطا در جستجو';
            } finally {
                this.newChatSearchLoading = false;
                this.newChatSearchTimeout = null;
            }
        },
        async handleSelectSearchedUser(user) {
            await this.startConversation(
                user.id,
                user.username,
                user.displayName,
                user.avatarUrl,
                user.isOnline
            );
            this.closeNewChatModal();
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
});

app.component('user-search-item', UserSearchItem);
app.mount('#app');
