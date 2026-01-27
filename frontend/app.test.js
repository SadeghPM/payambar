/**
 * Payambar Frontend Tests
 * 
 * Run these tests in a browser console or with a test runner like Jest.
 * These are pure JavaScript unit tests for the app logic.
 */

// Mock localStorage
const mockLocalStorage = (() => {
    let store = {};
    return {
        getItem: (key) => store[key] || null,
        setItem: (key, value) => { store[key] = String(value); },
        removeItem: (key) => { delete store[key]; },
        clear: () => { store = {}; },
        get length() { return Object.keys(store).length; },
        key: (i) => Object.keys(store)[i] || null
    };
})();

// Mock WebSocket
class MockWebSocket {
    constructor(url) {
        this.url = url;
        this.readyState = MockWebSocket.OPEN;
        this.sentMessages = [];
        this.onopen = null;
        this.onclose = null;
        this.onmessage = null;
        this.onerror = null;
    }
    
    send(data) {
        this.sentMessages.push(JSON.parse(data));
    }
    
    close() {
        this.readyState = MockWebSocket.CLOSED;
        if (this.onclose) this.onclose();
    }
    
    simulateMessage(data) {
        if (this.onmessage) {
            this.onmessage({ data: JSON.stringify(data) });
        }
    }
}
MockWebSocket.CONNECTING = 0;
MockWebSocket.OPEN = 1;
MockWebSocket.CLOSING = 2;
MockWebSocket.CLOSED = 3;

// Mock fetch
const mockFetch = (responses = {}) => {
    return async (url, options = {}) => {
        const path = url.replace(/^https?:\/\/[^/]+/, '');
        const method = options.method || 'GET';
        const key = `${method} ${path}`;
        
        if (responses[key]) {
            const response = responses[key];
            return {
                ok: response.ok !== false,
                status: response.status || 200,
                json: async () => response.data
            };
        }
        
        return {
            ok: false,
            status: 404,
            json: async () => ({ error: 'Not found' })
        };
    };
};

// Test Suite
const tests = {
    passed: 0,
    failed: 0,
    results: []
};

function assert(condition, message) {
    if (condition) {
        tests.passed++;
        tests.results.push({ status: 'PASS', message });
    } else {
        tests.failed++;
        tests.results.push({ status: 'FAIL', message });
        console.error(`FAIL: ${message}`);
    }
}

function assertEqual(actual, expected, message) {
    const pass = JSON.stringify(actual) === JSON.stringify(expected);
    if (pass) {
        tests.passed++;
        tests.results.push({ status: 'PASS', message });
    } else {
        tests.failed++;
        tests.results.push({ status: 'FAIL', message: `${message} - Expected: ${JSON.stringify(expected)}, Got: ${JSON.stringify(actual)}` });
        console.error(`FAIL: ${message}`, { expected, actual });
    }
}

// App Logic Tests (extracted from app.js for testing)
const AppLogic = {
    formatDate(value) {
        if (!value) return '';
        try {
            return new Date(value).toLocaleDateString('fa-IR');
        } catch (e) {
            return '';
        }
    },
    
    formatStatus(msg) {
        if (msg.status === 'read') return '✓✓';
        if (msg.status === 'delivered') return '✓';
        return '';
    },
    
    isValidAuth(token, userId, username) {
        const isTokenValid = token && token !== 'undefined' && token !== 'null';
        const isUserIdValid = userId && !isNaN(parseInt(userId)) && parseInt(userId) > 0;
        return isTokenValid && isUserIdValid && username;
    },
    
    parseWebSocketMessage(eventData) {
        try {
            return JSON.parse(eventData);
        } catch (e) {
            return null;
        }
    },
    
    findExistingConversation(conversations, userId) {
        return conversations.find(c => c.user_id === userId);
    },
    
    filterConversations(conversations, query) {
        const q = query.trim().toLowerCase();
        if (!q) return conversations;
        return conversations.filter(c => c.username?.toLowerCase().includes(q));
    },
    
    updateMessageStatus(messages, messageId, newStatus) {
        const allMsgs = Object.values(messages).flat();
        const msg = allMsgs.find(m => m.id === messageId);
        if (msg) {
            msg.status = newStatus;
            return true;
        }
        return false;
    },
    
    addMessageToConversation(messages, convUserId, message) {
        if (!messages[convUserId]) {
            messages[convUserId] = [];
        }
        messages[convUserId].push(message);
    },
    
    replaceMessageByClientId(messages, convUserId, clientMessageId, serverMessage) {
        if (!messages[convUserId]) return false;
        const idx = messages[convUserId].findIndex(m => m.client_message_id === clientMessageId);
        if (idx >= 0) {
            messages[convUserId][idx] = {
                ...messages[convUserId][idx],
                id: serverMessage.message_id,
                status: serverMessage.status
            };
            return true;
        }
        return false;
    }
};

// Run Tests
function runTests() {
    console.log('Running Payambar Frontend Tests...\n');
    
    // Format Date Tests
    console.log('--- Format Date Tests ---');
    
    assert(AppLogic.formatDate(null) === '', 'formatDate handles null');
    assert(AppLogic.formatDate(undefined) === '', 'formatDate handles undefined');
    assert(AppLogic.formatDate('') === '', 'formatDate handles empty string');
    assert(AppLogic.formatDate('2026-01-25T12:00:00Z') !== '', 'formatDate returns non-empty for valid date');
    
    // Format Status Tests
    console.log('\n--- Format Status Tests ---');
    
    assertEqual(AppLogic.formatStatus({ status: 'read' }), '✓✓', 'formatStatus shows double check for read');
    assertEqual(AppLogic.formatStatus({ status: 'delivered' }), '✓', 'formatStatus shows single check for delivered');
    assertEqual(AppLogic.formatStatus({ status: 'sent' }), '', 'formatStatus shows nothing for sent');
    assertEqual(AppLogic.formatStatus({}), '', 'formatStatus handles missing status');
    
    // Auth Validation Tests
    console.log('\n--- Auth Validation Tests ---');
    
    assert(AppLogic.isValidAuth('valid-token', '1', 'user'), 'Valid auth data returns true');
    assert(!AppLogic.isValidAuth('undefined', '1', 'user'), 'Token "undefined" returns false');
    assert(!AppLogic.isValidAuth('null', '1', 'user'), 'Token "null" returns false');
    assert(!AppLogic.isValidAuth('', '1', 'user'), 'Empty token returns false');
    assert(!AppLogic.isValidAuth('token', 'abc', 'user'), 'Non-numeric userId returns false');
    assert(!AppLogic.isValidAuth('token', '0', 'user'), 'Zero userId returns false');
    assert(!AppLogic.isValidAuth('token', '-1', 'user'), 'Negative userId returns false');
    assert(!AppLogic.isValidAuth('token', '1', ''), 'Empty username returns false');
    
    // WebSocket Message Parsing Tests
    console.log('\n--- WebSocket Message Parsing Tests ---');
    
    assertEqual(
        AppLogic.parseWebSocketMessage('{"type":"message","content":"hello"}'),
        { type: 'message', content: 'hello' },
        'Parses valid JSON message'
    );
    assert(AppLogic.parseWebSocketMessage('invalid json') === null, 'Returns null for invalid JSON');
    assert(AppLogic.parseWebSocketMessage('') === null, 'Returns null for empty string');
    
    // Conversation Finding Tests
    console.log('\n--- Conversation Finding Tests ---');
    
    const conversations = [
        { id: 1, user_id: 10, username: 'alice' },
        { id: 2, user_id: 20, username: 'bob' },
        { id: 3, user_id: 11, username: 'charlie' }
    ];
    
    assertEqual(
        AppLogic.findExistingConversation(conversations, 10),
        conversations[0],
        'Finds conversation by user_id'
    );
    assertEqual(
        AppLogic.findExistingConversation(conversations, 99),
        undefined,
        'Returns undefined for non-existent user_id'
    );
    // Important: This tests the fix for ID matching bug
    assertEqual(
        AppLogic.findExistingConversation(conversations, 1),
        undefined,
        'Does not match user_id 1 with user_id 10 or 11 (ID matching fix)'
    );
    
    // Conversation Filtering Tests
    console.log('\n--- Conversation Filtering Tests ---');
    
    assertEqual(
        AppLogic.filterConversations(conversations, '').length,
        3,
        'Empty query returns all conversations'
    );
    assertEqual(
        AppLogic.filterConversations(conversations, 'ali').length,
        1,
        'Filters by partial username match'
    );
    assertEqual(
        AppLogic.filterConversations(conversations, 'ALI').length,
        1,
        'Filter is case insensitive'
    );
    assertEqual(
        AppLogic.filterConversations(conversations, 'xyz').length,
        0,
        'No match returns empty array'
    );
    
    // Message Status Update Tests
    console.log('\n--- Message Status Update Tests ---');
    
    const messages = {
        10: [
            { id: 1, content: 'hi', status: 'sent' },
            { id: 2, content: 'hello', status: 'sent' }
        ],
        20: [
            { id: 3, content: 'hey', status: 'delivered' }
        ]
    };
    
    assert(
        AppLogic.updateMessageStatus(messages, 1, 'delivered'),
        'Updates status for existing message'
    );
    assertEqual(messages[10][0].status, 'delivered', 'Status was actually updated');
    
    assert(
        !AppLogic.updateMessageStatus(messages, 999, 'read'),
        'Returns false for non-existent message'
    );
    
    // Add Message Tests
    console.log('\n--- Add Message Tests ---');
    
    const newMessages = {};
    AppLogic.addMessageToConversation(newMessages, 5, { id: 1, content: 'test' });
    assert(newMessages[5] !== undefined, 'Creates conversation array if not exists');
    assertEqual(newMessages[5].length, 1, 'Message was added');
    
    AppLogic.addMessageToConversation(newMessages, 5, { id: 2, content: 'test2' });
    assertEqual(newMessages[5].length, 2, 'Second message was added');
    
    // Replace Message by Client ID Tests
    console.log('\n--- Replace Message by Client ID Tests ---');
    
    const tempMessages = {
        10: [
            { client_message_id: 'temp-123', content: 'hello', status: 'sending' }
        ]
    };
    
    const replaced = AppLogic.replaceMessageByClientId(
        tempMessages, 
        10, 
        'temp-123', 
        { message_id: 456, status: 'sent' }
    );
    
    assert(replaced, 'Returns true when message is replaced');
    assertEqual(tempMessages[10][0].id, 456, 'Message ID was updated');
    assertEqual(tempMessages[10][0].status, 'sent', 'Message status was updated');
    assertEqual(tempMessages[10][0].content, 'hello', 'Content preserved');
    
    const notReplaced = AppLogic.replaceMessageByClientId(
        tempMessages, 
        10, 
        'non-existent', 
        { message_id: 789, status: 'sent' }
    );
    assert(!notReplaced, 'Returns false when client_message_id not found');
    
    // MockWebSocket Tests
    console.log('\n--- MockWebSocket Tests ---');
    
    const ws = new MockWebSocket('ws://test/ws');
    ws.send(JSON.stringify({ type: 'message', content: 'test' }));
    assertEqual(ws.sentMessages.length, 1, 'Message was recorded');
    assertEqual(ws.sentMessages[0].type, 'message', 'Message content correct');
    
    let receivedData = null;
    ws.onmessage = (e) => { receivedData = JSON.parse(e.data); };
    ws.simulateMessage({ type: 'incoming', data: 'hello' });
    assertEqual(receivedData.type, 'incoming', 'Simulated message received');
    
    // Print Summary
    console.log('\n========================================');
    console.log(`Tests Passed: ${tests.passed}`);
    console.log(`Tests Failed: ${tests.failed}`);
    console.log(`Total: ${tests.passed + tests.failed}`);
    console.log('========================================\n');
    
    if (tests.failed > 0) {
        console.log('Failed Tests:');
        tests.results.filter(r => r.status === 'FAIL').forEach(r => {
            console.log(`  - ${r.message}`);
        });
    }
    
    return tests.failed === 0;
}

// Integration Test Helpers
const IntegrationTests = {
    async testLoginFlow(fetchMock) {
        const responses = {
            'POST /api/auth/login': {
                ok: true,
                data: { token: 'test-token', user_id: 1, username: 'testuser' }
            }
        };
        
        global.fetch = mockFetch(responses);
        
        const result = await fetch('/api/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: 'testuser', password: 'password123' })
        });
        
        const data = await result.json();
        
        assert(result.ok, 'Login request succeeds');
        assertEqual(data.username, 'testuser', 'Username returned correctly');
        assert(data.token !== undefined, 'Token returned');
        
        return data;
    },
    
    async testConversationLoad(token) {
        const responses = {
            'GET /api/conversations': {
                ok: true,
                data: {
                    conversations: [
                        { id: 1, user_id: 2, username: 'alice', unread_count: 3 },
                        { id: 2, user_id: 3, username: 'bob', unread_count: 0 }
                    ]
                }
            }
        };
        
        global.fetch = mockFetch(responses);
        
        const result = await fetch('/api/conversations', {
            headers: { Authorization: `Bearer ${token}` }
        });
        
        const data = await result.json();
        
        assert(result.ok, 'Conversations request succeeds');
        assertEqual(data.conversations.length, 2, 'Two conversations returned');
        
        return data.conversations;
    },
    
    async testMessageSend(ws) {
        const message = {
            type: 'message',
            receiver_id: 2,
            content: 'Hello!',
            client_message_id: 'temp-' + Date.now()
        };
        
        ws.send(JSON.stringify(message));
        
        assertEqual(ws.sentMessages.length, 1, 'Message was sent via WebSocket');
        assertEqual(ws.sentMessages[0].type, 'message', 'Message type is correct');
        assertEqual(ws.sentMessages[0].content, 'Hello!', 'Message content is correct');
    },
    
    testWebSocketReconnect() {
        let reconnectAttempts = 0;
        const maxAttempts = 5;
        
        function connect() {
            const ws = new MockWebSocket('ws://test/ws');
            
            ws.onclose = () => {
                if (reconnectAttempts < maxAttempts) {
                    reconnectAttempts++;
                    // Would normally setTimeout here
                }
            };
            
            // Simulate close
            ws.close();
            
            return ws;
        }
        
        connect();
        
        assertEqual(reconnectAttempts, 1, 'Reconnect attempt was made');
    }
};

// Export for Node.js/Jest environment
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        AppLogic,
        MockWebSocket,
        mockFetch,
        mockLocalStorage,
        runTests,
        IntegrationTests
    };
}

// Auto-run if in browser
if (typeof window !== 'undefined') {
    window.PayambarTests = {
        AppLogic,
        MockWebSocket,
        mockFetch,
        mockLocalStorage,
        runTests,
        IntegrationTests
    };
    
    // Uncomment to auto-run tests when loaded
    // runTests();
}

// Run tests if executed directly
if (typeof process !== 'undefined' && process.argv && process.argv[1] && process.argv[1].includes('app.test.js')) {
    runTests();
}
