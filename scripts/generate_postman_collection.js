const fs = require('fs');
const path = require('path');

const outputPath = path.join(__dirname, '..', 'postman', 'project_radeon_backend.postman_collection.json');
const mainPath = path.join(__dirname, '..', 'cmd', 'api', 'main.go');

function queryParam(key, value, disabled = false, description) {
  return {
    key,
    value,
    ...(disabled ? { disabled: true } : {}),
    ...(description ? { description } : {}),
  };
}

function makeUrl(routePath, query = []) {
  const normalized = routePath.startsWith('/') ? routePath : `/${routePath}`;
  const cleanQuery = query.filter(Boolean);
  const rawQuery = cleanQuery.length
    ? `?${cleanQuery
        .filter((entry) => !entry.disabled)
        .map((entry) => `${entry.key}=${entry.value}`)
        .join('&')}`
    : '';

  return {
    raw: `{{base_url}}${normalized}${rawQuery}`,
    host: ['{{base_url}}'],
    path: normalized.split('/').filter(Boolean),
    ...(cleanQuery.length ? { query: cleanQuery } : {}),
  };
}

function makeEvent(testLines) {
  return [
    {
      listen: 'test',
      script: {
        type: 'text/javascript',
        exec: testLines,
      },
    },
  ];
}

function asJsonBody(body) {
  return {
    mode: 'raw',
    raw: JSON.stringify(body, null, 2),
    options: {
      raw: {
        language: 'json',
      },
    },
  };
}

function requestItem(name, method, routePath, options = {}) {
  const request = {
    method,
    header: options.formdata
      ? []
      : [{ key: 'Content-Type', value: 'application/json' }],
    url: makeUrl(routePath, options.query),
    ...(options.description ? { description: options.description } : {}),
    ...(options.noauth ? { auth: { type: 'noauth' } } : {}),
  };

  if (method === 'GET' || method === 'DELETE') {
    if (!options.body && !options.formdata) {
      delete request.header;
    }
  }

  if (options.body) {
    request.body = asJsonBody(options.body);
  }

  if (options.formdata) {
    request.body = {
      mode: 'formdata',
      formdata: options.formdata,
    };
    delete request.header;
  }

  return {
    name,
    ...(options.tests ? { event: makeEvent(options.tests) } : {}),
    request,
  };
}

function folder(name, items) {
  return { name, item: items };
}

const loginRegisterTests = [
  "if (pm.response.code === 200 || pm.response.code === 201) {",
  "  const json = pm.response.json();",
  "  pm.collectionVariables.set('token', json.data.token);",
  "  pm.collectionVariables.set('user_id', json.data.user_id);",
  "}",
];

function setIdVarOnSuccess(code, variableName, accessPath) {
  return [
    `if (pm.response.code === ${code}) {`,
    '  const json = pm.response.json();',
    `  const value = ${accessPath};`,
    `  if (value) pm.collectionVariables.set('${variableName}', value);`,
    '}',
  ];
}

const collection = {
  info: {
    _postman_id: '3fdb5c52-8dfc-4d74-b34c-9fbc0d5d7f7d',
    name: 'Project Radeon Backend API',
    description:
      'Route-complete Postman collection for the Project Radeon backend mounted in cmd/api/main.go. Set {{base_url}}, call Register or Login to populate {{token}}, then use the protected requests. The chats WebSocket entry is included as an endpoint reference and expects the access token in the query string.',
    schema: 'https://schema.getpostman.com/json/collection/v2.1.0/collection.json',
  },
  auth: {
    type: 'bearer',
    bearer: [{ key: 'token', value: '{{token}}', type: 'string' }],
  },
  variable: [
    { key: 'base_url', value: 'http://localhost:8080', type: 'string' },
    { key: 'token', value: '', type: 'string' },
    { key: 'username', value: 'postman_user', type: 'string' },
    { key: 'email', value: 'postman_user@example.com', type: 'string' },
    { key: 'password', value: 'Password123!', type: 'string' },
    { key: 'user_id', value: '', type: 'string' },
    { key: 'other_user_id', value: '00000000-0000-0000-0000-000000000002', type: 'string' },
    { key: 'post_id', value: '00000000-0000-0000-0000-000000000003', type: 'string' },
    { key: 'feed_item_id', value: '00000000-0000-0000-0000-000000000004', type: 'string' },
    { key: 'meetup_id', value: '00000000-0000-0000-0000-000000000005', type: 'string' },
    { key: 'support_request_id', value: '00000000-0000-0000-0000-000000000006', type: 'string' },
    { key: 'chat_id', value: '00000000-0000-0000-0000-000000000007', type: 'string' },
    { key: 'message_id', value: '00000000-0000-0000-0000-000000000008', type: 'string' },
    { key: 'notification_id', value: '00000000-0000-0000-0000-000000000009', type: 'string' },
    { key: 'device_id', value: '00000000-0000-0000-0000-000000000010', type: 'string' },
    { key: 'expo_push_token', value: 'ExponentPushToken[example-token]', type: 'string' },
  ],
  item: [
    folder('Health', [
      requestItem('Health Check', 'GET', '/health', {
        noauth: true,
        description: 'Public health endpoint.',
      }),
    ]),
    folder('Auth & Public', [
      requestItem('Register', 'POST', '/auth/register', {
        noauth: true,
        body: {
          username: '{{username}}',
          email: '{{email}}',
          password: '{{password}}',
          city: 'Dublin',
          country: 'Ireland',
          sober_since: '2024-01-01',
        },
        tests: loginRegisterTests,
      }),
      requestItem('Login', 'POST', '/auth/login', {
        noauth: true,
        body: {
          email: '{{email}}',
          password: '{{password}}',
        },
        tests: loginRegisterTests,
      }),
      requestItem('List Interests', 'GET', '/interests', {
        noauth: true,
      }),
    ]),
    folder('Feed', [
      requestItem('Get Home Feed', 'GET', '/feed/home', {
        query: [queryParam('limit', '20'), queryParam('before', '', true)],
      }),
      requestItem('Get Hidden Feed Items', 'GET', '/feed/hidden', {
        query: [queryParam('limit', '20'), queryParam('before', '', true)],
      }),
      requestItem('React To Feed Item', 'POST', '/feed/items/{{feed_item_id}}/react', {
        body: {
          type: 'like',
          item_kind: 'post',
        },
      }),
      requestItem('Add Feed Item Comment', 'POST', '/feed/items/{{feed_item_id}}/comments', {
        body: {
          body: 'Nice update from Postman.',
          item_kind: 'post',
          mention_user_ids: ['{{other_user_id}}'],
        },
      }),
      requestItem('Get Feed Item Comments', 'GET', '/feed/items/{{feed_item_id}}/comments', {
        query: [
          queryParam('item_kind', 'post'),
          queryParam('limit', '20'),
          queryParam('after', '', true),
        ],
      }),
      requestItem('Hide Feed Item', 'POST', '/feed/items/{{feed_item_id}}/hide', {
        body: {
          item_kind: 'post',
        },
      }),
      requestItem('Unhide Feed Item', 'DELETE', '/feed/items/{{feed_item_id}}/hide', {
        query: [queryParam('item_kind', 'post')],
      }),
      requestItem('Mute Feed Author', 'POST', '/feed/authors/{{other_user_id}}/mute'),
      requestItem('Log Feed Impressions', 'POST', '/feed/impressions', {
        body: {
          impressions: [
            {
              item_id: '{{feed_item_id}}',
              item_kind: 'post',
              feed_mode: 'home',
              session_id: 'postman-session',
              position: 0,
              served_at: '2026-04-28T10:00:00Z',
              viewed_at: '2026-04-28T10:00:05Z',
              view_ms: 5000,
              was_clicked: true,
              was_liked: false,
              was_commented: false,
            },
          ],
        },
      }),
      requestItem('Log Feed Events', 'POST', '/feed/events', {
        body: {
          events: [
            {
              item_id: '{{feed_item_id}}',
              item_kind: 'post',
              feed_mode: 'home',
              event_type: 'open_post',
              position: 0,
              event_at: '2026-04-28T10:01:00Z',
              payload: {
                source: 'postman',
              },
            },
          ],
        },
      }),
    ]),
    folder('Posts', [
      requestItem('Create Post', 'POST', '/posts', {
        body: {
          body: 'Checking in from Postman.',
        },
        tests: setIdVarOnSuccess(201, 'post_id', 'json.data.id'),
      }),
      requestItem('Upload Post Image', 'POST', '/posts/images', {
        formdata: [{ key: 'image', type: 'file', src: '' }],
      }),
      requestItem('Delete Post', 'DELETE', '/posts/{{post_id}}'),
      requestItem('Share Post', 'POST', '/posts/{{post_id}}/share', {
        body: {
          commentary: 'Worth resharing.',
        },
      }),
      requestItem('React To Post', 'POST', '/posts/{{post_id}}/react', {
        body: {
          type: 'like',
        },
      }),
      requestItem('Get Reactions', 'GET', '/posts/{{post_id}}/reactions', {
        query: [queryParam('page', '1'), queryParam('limit', '50')],
      }),
      requestItem('Add Comment', 'POST', '/posts/{{post_id}}/comments', {
        body: {
          body: 'Great post @postman_friend',
          mention_user_ids: ['{{other_user_id}}'],
        },
      }),
      requestItem('Get Comments', 'GET', '/posts/{{post_id}}/comments', {
        query: [queryParam('limit', '20'), queryParam('after', '', true)],
      }),
      requestItem('Get User Posts', 'GET', '/users/{{other_user_id}}/posts', {
        query: [queryParam('limit', '20'), queryParam('before', '', true)],
      }),
    ]),
    folder('Users', [
      requestItem('Get Me', 'GET', '/users/me'),
      requestItem('Update Me', 'PATCH', '/users/me', {
        body: {
          city: 'Dublin',
          country: 'Ireland',
          bio: 'One day at a time.',
          interests: ['fitness', 'coffee'],
          sober_since: '2024-01-01',
        },
      }),
      requestItem('Update My Current Location', 'PATCH', '/users/me/location', {
        body: {
          lat: 53.3498,
          lng: -6.2603,
          city: 'Dublin',
        },
      }),
      requestItem('Upload Avatar', 'POST', '/users/me/avatar', {
        formdata: [{ key: 'avatar', type: 'file', src: '' }],
      }),
      requestItem('Upload Banner', 'POST', '/users/me/banner', {
        formdata: [{ key: 'banner', type: 'file', src: '' }],
      }),
      requestItem('Get Discover Preview', 'GET', '/users/discover/preview', {
        query: [
          queryParam('q', 'postman'),
          queryParam('city', 'Dublin'),
          queryParam('gender', 'woman', true),
          queryParam('sobriety', 'days_90', true),
          queryParam('age_min', '25', true),
          queryParam('age_max', '40', true),
          queryParam('distance_km', '50', true),
          queryParam('interest', 'fitness', true),
        ],
      }),
      requestItem('Discover Users', 'GET', '/users/discover', {
        query: [
          queryParam('q', 'postman'),
          queryParam('city', 'Dublin'),
          queryParam('page', '1'),
          queryParam('limit', '20'),
          queryParam('gender', 'woman', true),
          queryParam('sobriety', 'days_90', true),
          queryParam('age_min', '25', true),
          queryParam('age_max', '40', true),
          queryParam('distance_km', '50', true),
          queryParam('interest', 'fitness', true),
        ],
      }),
      requestItem('Get User', 'GET', '/users/{{other_user_id}}'),
    ]),
    folder('Friends', [
      requestItem('Send Friend Request', 'POST', '/users/{{other_user_id}}/friend-request'),
      requestItem('Accept Friend Request', 'PATCH', '/users/{{other_user_id}}/friend-request', {
        body: {
          action: 'accept',
        },
      }),
      requestItem('Decline Friend Request', 'PATCH', '/users/{{other_user_id}}/friend-request', {
        body: {
          action: 'decline',
        },
      }),
      requestItem('Cancel Friend Request', 'DELETE', '/users/{{other_user_id}}/friend-request'),
      requestItem('Remove Friend', 'DELETE', '/users/{{other_user_id}}/friend'),
      requestItem('List Friends', 'GET', '/users/me/friends', {
        query: [queryParam('limit', '25'), queryParam('before', '', true)],
      }),
      requestItem('List Incoming Friend Requests', 'GET', '/users/me/friend-requests/incoming', {
        query: [queryParam('limit', '25'), queryParam('before', '', true)],
      }),
      requestItem('List Outgoing Friend Requests', 'GET', '/users/me/friend-requests/outgoing', {
        query: [queryParam('limit', '25'), queryParam('before', '', true)],
      }),
    ]),
    folder('Meetups', [
      requestItem('List Meetup Categories', 'GET', '/meetups/categories'),
      requestItem('Discover Meetups', 'GET', '/meetups', {
        query: [
          queryParam('q', 'coffee'),
          queryParam('city', 'Dublin'),
          queryParam('limit', '20'),
          queryParam('cursor', '', true),
          queryParam('category', 'coffee-chat', true),
          queryParam('distance_km', '25', true),
          queryParam('event_type', 'in_person', true),
          queryParam('date_preset', 'this_week', true),
          queryParam('date_from', '2026-05-01', true),
          queryParam('date_to', '2026-05-07', true),
          queryParam('day_of_week', '2,4', true),
          queryParam('time_of_day', 'morning,evening', true),
          queryParam('open_spots_only', 'true', true),
          queryParam('sort', 'recommended', true),
        ],
      }),
      requestItem('Create Meetup', 'POST', '/meetups', {
        body: {
          title: 'Morning coffee check-in',
          description: 'Low-pressure sober meetup.',
          category_slug: 'coffee-chat',
          co_host_ids: [],
          event_type: 'in_person',
          status: 'published',
          visibility: 'public',
          city: 'Dublin',
          country: 'Ireland',
          venue_name: 'Third Place',
          address_line_1: '12 Example Street',
          address_line_2: '',
          how_to_find_us: 'Upstairs table near the window.',
          online_url: '',
          cover_image_url: '',
          starts_at: '2026-05-03T10:00:00Z',
          ends_at: '2026-05-03T11:30:00Z',
          timezone: 'Europe/Dublin',
          lat: 53.3498,
          lng: -6.2603,
          capacity: 12,
          waitlist_enabled: true,
        },
        tests: setIdVarOnSuccess(201, 'meetup_id', 'json.data.id'),
      }),
      requestItem('Upload Meetup Cover Image', 'POST', '/meetups/images', {
        formdata: [{ key: 'cover', type: 'file', src: '' }],
      }),
      requestItem('Get Meetup', 'GET', '/meetups/{{meetup_id}}'),
      requestItem('Update Meetup', 'PATCH', '/meetups/{{meetup_id}}', {
        body: {
          title: 'Morning coffee check-in',
          description: 'Updated details from Postman.',
          category_slug: 'coffee-chat',
          co_host_ids: [],
          event_type: 'in_person',
          status: 'published',
          visibility: 'public',
          city: 'Dublin',
          country: 'Ireland',
          venue_name: 'Third Place',
          address_line_1: '12 Example Street',
          address_line_2: '',
          how_to_find_us: 'Ask for the sober social table.',
          online_url: '',
          cover_image_url: '',
          starts_at: '2026-05-03T10:00:00Z',
          ends_at: '2026-05-03T11:30:00Z',
          timezone: 'Europe/Dublin',
          lat: 53.3498,
          lng: -6.2603,
          capacity: 12,
          waitlist_enabled: true,
        },
      }),
      requestItem('Delete Meetup', 'DELETE', '/meetups/{{meetup_id}}'),
      requestItem('Publish Meetup', 'POST', '/meetups/{{meetup_id}}/publish'),
      requestItem('Cancel Meetup', 'POST', '/meetups/{{meetup_id}}/cancel'),
      requestItem('RSVP Meetup', 'POST', '/meetups/{{meetup_id}}/rsvp'),
      requestItem('Get Meetup Attendees', 'GET', '/meetups/{{meetup_id}}/attendees', {
        query: [queryParam('limit', '50'), queryParam('cursor', '', true)],
      }),
      requestItem('Get Meetup Waitlist', 'GET', '/meetups/{{meetup_id}}/waitlist', {
        query: [queryParam('limit', '50'), queryParam('cursor', '', true)],
      }),
      requestItem('List My Meetups', 'GET', '/users/me/meetups', {
        query: [
          queryParam('scope', 'upcoming'),
          queryParam('limit', '20'),
          queryParam('cursor', '', true),
        ],
      }),
    ]),
    folder('Support', [
      requestItem('Create Immediate Support Request', 'POST', '/support/requests/immediate', {
        body: {
          type: 'need_to_talk',
          message: 'Need support right now.',
          urgency: 'right_now',
          privacy_level: 'standard',
        },
        tests: setIdVarOnSuccess(201, 'support_request_id', 'json.data.id'),
      }),
      requestItem('Create Community Support Request', 'POST', '/support/requests/community', {
        body: {
          type: 'need_encouragement',
          message: 'Could use some encouragement today.',
          urgency: 'when_you_can',
          privacy_level: 'standard',
        },
        tests: setIdVarOnSuccess(201, 'support_request_id', 'json.data.id'),
      }),
      requestItem('List Visible Support Requests', 'GET', '/support/requests', {
        query: [queryParam('channel', 'community'), queryParam('limit', '20'), queryParam('cursor', '', true)],
      }),
      requestItem('List My Support Requests', 'GET', '/support/requests/mine', {
        query: [queryParam('limit', '20'), queryParam('before', '', true)],
      }),
      requestItem('Get Support Request', 'GET', '/support/requests/{{support_request_id}}'),
      requestItem('Close Support Request', 'PATCH', '/support/requests/{{support_request_id}}', {
        body: {
          status: 'closed',
        },
      }),
      requestItem('Create Support Response', 'POST', '/support/requests/{{support_request_id}}/responses', {
        body: {
          response_type: 'can_chat',
          message: "I'm here and can talk now.",
        },
        tests: setIdVarOnSuccess(201, 'support_response_id', 'json.data.response.id').concat(
          setIdVarOnSuccess(201, 'support_response_id', 'json.data.id')
        ),
      }),
      requestItem('List Support Responses', 'GET', '/support/requests/{{support_request_id}}/responses', {
        query: [queryParam('page', '1'), queryParam('limit', '50')],
      }),
      requestItem('Accept Support Response', 'POST', '/support/requests/{{support_request_id}}/responses/{{support_response_id}}/accept', {
        tests: [
          "if (pm.response.code === 200) {",
          '  const json = pm.response.json();',
          "  const request = json.data && json.data.request ? json.data.request : json.data;",
          "  if (request && request.id) pm.collectionVariables.set('support_request_id', request.id);",
          "  if (request && request.chat_id) pm.collectionVariables.set('chat_id', request.chat_id);",
          '}',
        ],
      }),
    ]),
    folder('Chats', [
      requestItem('Realtime Chat WebSocket Reference', 'GET', '/chats/ws', {
        noauth: true,
        query: [queryParam('access_token', '{{token}}')],
        description:
          'Reference entry for the authenticated WebSocket endpoint. Use a WebSocket client against this URL rather than a normal HTTP request.',
      }),
      requestItem('List Chats', 'GET', '/chats', {
        query: [
          queryParam('q', ''),
          queryParam('limit', '20'),
          queryParam('before', '', true),
        ],
      }),
      requestItem('List Chat Requests', 'GET', '/chats/requests'),
      requestItem('Create Direct Chat', 'POST', '/chats', {
        body: {
          member_ids: ['{{other_user_id}}'],
        },
        tests: setIdVarOnSuccess(200, 'chat_id', 'json.data.id').concat(
          setIdVarOnSuccess(201, 'chat_id', 'json.data.id')
        ),
      }),
      requestItem('Create Group Chat', 'POST', '/chats', {
        body: {
          member_ids: ['{{other_user_id}}', '{{user_id}}'],
          name: 'Postman Group',
        },
        tests: setIdVarOnSuccess(201, 'chat_id', 'json.data.id'),
      }),
      requestItem('Get Chat', 'GET', '/chats/{{chat_id}}'),
      requestItem('Get Messages', 'GET', '/chats/{{chat_id}}/messages', {
        query: [queryParam('limit', '50'), queryParam('before', '', true)],
      }),
      requestItem('Send Message', 'POST', '/chats/{{chat_id}}/messages', {
        body: {
          body: 'Hello from Postman.',
        },
        tests: setIdVarOnSuccess(201, 'message_id', 'json.data.id'),
      }),
      requestItem('Mark Chat Read', 'POST', '/chats/{{chat_id}}/read', {
        body: {
          last_read_message_id: '{{message_id}}',
        },
      }),
      requestItem('Delete Or Leave Chat', 'DELETE', '/chats/{{chat_id}}'),
      requestItem('Accept Chat Request', 'PATCH', '/chats/{{chat_id}}/status', {
        body: {
          status: 'active',
        },
      }),
      requestItem('Decline Chat Request', 'PATCH', '/chats/{{chat_id}}/status', {
        body: {
          status: 'declined',
        },
      }),
    ]),
    folder('Notifications', [
      requestItem('Register Device', 'POST', '/notifications/devices', {
        body: {
          push_token: '{{expo_push_token}}',
          platform: 'ios',
          device_name: 'Postman Simulator',
          app_version: '1.0.0',
        },
        tests: setIdVarOnSuccess(201, 'device_id', 'json.data.id'),
      }),
      requestItem('Delete Device', 'DELETE', '/notifications/devices/{{device_id}}'),
      requestItem('List Notifications', 'GET', '/notifications', {
        query: [queryParam('limit', '20'), queryParam('before', '', true)],
      }),
      requestItem('Mark Notification Read', 'POST', '/notifications/{{notification_id}}/read'),
      requestItem('Get Notification Preferences', 'GET', '/notifications/preferences'),
      requestItem('Update Notification Preferences', 'PATCH', '/notifications/preferences', {
        body: {
          chat_messages: true,
          comment_mentions: true,
        },
      }),
    ]),
  ],
};

function collectCollectionRoutes(items, bucket = []) {
  for (const item of items) {
    if (item.request) {
      bucket.push(`${item.request.method.toUpperCase()} ${item.request.url.raw}`);
    }
    if (item.item) {
      collectCollectionRoutes(item.item, bucket);
    }
  }
  return bucket;
}

function normalizeGeneratedRoute(route) {
  const [method, rawUrl] = route.split(' ');
  const pathOnly = rawUrl.replace('{{base_url}}', '').split('?')[0];
  return `${method} ${pathOnly.replace(/{{[^}]+}}/g, '{param}')}`;
}

function normalizeActualRoute(route) {
  return route.replace(/\{[^}]+\}/g, '{param}');
}

function extractActualRoutes() {
  const source = fs.readFileSync(mainPath, 'utf8');
  return [...source.matchAll(/\b[a-zA-Z_][a-zA-Z0-9_]*\.(Get|Post|Patch|Delete)\("([^"]+)"/g)].map(
    (match) => `${match[1].toUpperCase()} ${match[2]}`
  );
}

function validateCoverage() {
  const actualRoutes = extractActualRoutes().map(normalizeActualRoute).sort();
  const generatedRoutes = collectCollectionRoutes(collection.item).map(normalizeGeneratedRoute).sort();

  const missing = actualRoutes.filter((route) => !generatedRoutes.includes(route));
  const extra = generatedRoutes.filter((route) => !actualRoutes.includes(route));

  if (missing.length || extra.length) {
    const parts = [];
    if (missing.length) parts.push(`Missing routes:\n${missing.join('\n')}`);
    if (extra.length) parts.push(`Extra routes:\n${extra.join('\n')}`);
    throw new Error(parts.join('\n\n'));
  }
}

validateCoverage();
fs.writeFileSync(outputPath, `${JSON.stringify(collection, null, 2)}\n`);
console.log(`Wrote ${outputPath}`);
