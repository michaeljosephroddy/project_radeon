const baseUrl = process.env.BASE_URL ?? 'http://localhost:8080';
const email = process.env.PROBE_EMAIL ?? 'test@radeon.dev';
const password = process.env.PROBE_PASSWORD ?? 'password123';

async function request(path, init = {}) {
    const response = await fetch(`${baseUrl}${path}`, init);
    const text = await response.text();
    let body = {};
    try {
        body = JSON.parse(text);
    } catch {
        body = { raw: text };
    }
    if (!response.ok) {
        throw new Error(`${path} -> ${response.status} ${text}`);
    }
    return body.data ?? body;
}

async function login() {
    const data = await request('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
    });
    return data.token;
}

async function getObservability() {
    return request('/debug/observability');
}

function counterDelta(before, after, key) {
    return (after.counters?.[key] ?? 0) - (before.counters?.[key] ?? 0);
}

function timerDelta(before, after, key) {
    const previous = before.timers?.[key];
    const current = after.timers?.[key];
    if (!current) return null;

    return {
        count_delta: (current.count ?? 0) - (previous?.count ?? 0),
        error_delta: (current.error_count ?? 0) - (previous?.error_count ?? 0),
        total_ms_delta: Number(((current.total_ms ?? 0) - (previous?.total_ms ?? 0)).toFixed(3)),
        avg_ms_after: current.avg_ms ?? 0,
        max_ms_after: current.max_ms ?? 0,
    };
}

async function main() {
    const token = await login();
    const authHeaders = { Authorization: `Bearer ${token}` };
    const before = await getObservability();

    const chats = await request('/chats?limit=20', { headers: authHeaders });
    const firstChatId = chats.items?.[0]?.id ?? null;
    let probeFeedItem = null;

    for (let index = 0; index < 3; index += 1) {
        const feedPage = await request('/feed/home?limit=20', { headers: authHeaders });
        if (!probeFeedItem) {
            probeFeedItem = feedPage.items?.[0] ?? null;
        }
    }

    for (let index = 0; index < 2; index += 1) {
        await request('/users/discover?limit=10&page=1', { headers: authHeaders });
        await request('/users/discover?limit=10&page=2', { headers: authHeaders });
        await request('/users/discover?limit=10&page=3', { headers: authHeaders });
    }

    const recommended = await request('/meetups?sort=recommended&limit=5', { headers: authHeaders });
    if (recommended.next_cursor) {
        await request(`/meetups?sort=recommended&limit=5&cursor=${encodeURIComponent(recommended.next_cursor)}`, { headers: authHeaders });
        await request(`/meetups?sort=recommended&limit=5&cursor=${encodeURIComponent(recommended.next_cursor)}`, { headers: authHeaders });
    }

    if (firstChatId) {
        await request(`/chats/${firstChatId}`, { headers: authHeaders });
        await request(`/chats/${firstChatId}/messages?limit=50`, { headers: authHeaders });
    }

    if (probeFeedItem) {
        await request('/feed/impressions', {
            method: 'POST',
            headers: {
                ...authHeaders,
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                impressions: [{
                    item_id: probeFeedItem.id,
                    item_kind: probeFeedItem.kind,
                    feed_mode: 'home',
                    session_id: 'probe-session',
                    position: 0,
                    served_at: new Date().toISOString(),
                    viewed_at: new Date().toISOString(),
                    view_ms: 1500,
                    was_clicked: false,
                    was_liked: false,
                    was_commented: false,
                }],
            }),
        });

        await request('/feed/events', {
            method: 'POST',
            headers: {
                ...authHeaders,
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                events: [{
                    item_id: probeFeedItem.id,
                    item_kind: probeFeedItem.kind,
                    feed_mode: 'home',
                    event_type: 'open_comments',
                    position: 0,
                    payload: { source: 'observability_probe' },
                }],
            }),
        });
    }

    const after = await getObservability();
    const timers = [
        'http.GET /feed/home',
        'http.GET /users/discover',
        'http.GET /meetups',
        'http.GET /chats',
        'http.GET /chats/{id}',
        'http.GET /chats/{id}/messages',
        'http.POST /feed/impressions',
        'http.POST /feed/events',
        'cache.read_through',
        'cache.get_json',
    ];

    const result = {
        base_url: baseUrl,
        first_chat_id: firstChatId,
        counters: {
            cache_read_through_hit: counterDelta(before, after, 'cache.read_through.hit'),
            cache_read_through_miss: counterDelta(before, after, 'cache.read_through.miss'),
            cache_get_json_hit: counterDelta(before, after, 'cache.get_json.hit'),
            cache_get_json_miss: counterDelta(before, after, 'cache.get_json.miss'),
            db_with_cte: counterDelta(before, after, 'db.query.with.cte.count'),
        },
        timers: Object.fromEntries(
            timers
                .map((key) => [key, timerDelta(before, after, key)])
                .filter(([, value]) => value != null),
        ),
    };

    console.log(JSON.stringify(result, null, 2));
}

main().catch((error) => {
    console.error(error);
    process.exitCode = 1;
});
