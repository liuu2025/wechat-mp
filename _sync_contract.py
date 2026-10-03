# -*- coding: utf-8 -*-
# 同步沙箱 / 文档 / AI Prompt / README 到 session_tag + poll_token 新契约
def load(p):
    return open(p, 'rb').read().decode('utf-8')

def save(p, t):
    open(p, 'wb').write(t.encode('utf-8'))

t = load('web/console.html')

# ===== 沙箱 =====
old = '''              <label>透传参数：</label>
              <input type="text" id="extraParam" value="guest_session_9527" placeholder="session_id 等">'''
new = '''              <label>会话标识：</label>
              <input type="text" id="extraParam" value="guest_session_9527" placeholder="后端签发的 session_tag（必填）">'''
assert t.count(old) == 1
t = t.replace(old, new)

old = '''    let latestActiveCode = "";'''
new = '''    let latestActiveCode = "";
    let latestPollToken = "";'''
assert t.count(old) == 1
t = t.replace(old, new)

old = '''          body: JSON.stringify({ project, key, extra, expire_seconds: 300 })
        });
        const json = await res.json();
        if (json.code === 0 && json.data) {
          latestActiveCode = json.data.code;'''
new = '''          body: JSON.stringify({ project, key, session_tag: extra, expire_seconds: 300 })
        });
        const json = await res.json();
        if (json.code === 0 && json.data) {
          latestActiveCode = json.data.code;
          latestPollToken = json.data.poll_token || '';'''
assert t.count(old) == 1
t = t.replace(old, new)

old = '''          startPolling(latestActiveCode);'''
new = '''          startPolling(latestActiveCode, latestPollToken);'''
assert t.count(old) == 1
t = t.replace(old, new)

old = '''    function startPolling(code) {
      if (currentPollingTimer) clearInterval(currentPollingTimer);
      currentPollingTimer = setInterval(async () => {
        try {
          const res = await fetch(`/api/code/check?code=${code}`);'''
new = '''    function startPolling(code, token) {
      if (currentPollingTimer) clearInterval(currentPollingTimer);
      currentPollingTimer = setInterval(async () => {
        try {
          const res = await fetch(`/api/code/check?poll_token=${token}`);'''
assert t.count(old) == 1
t = t.replace(old, new)

# ===== 接入文档：流向二（网站弹码）契约提示与示例 =====
old = '''电脑与手机浏览器完全通用</span>
            </div>'''
new = '''电脑与手机浏览器完全通用</span>
            </div>
              <div style="font-size:12.5px; color:#f59e0b; background:rgba(245,158,11,0.10); border:1px solid rgba(245,158,11,0.35); padding:8px 12px; border-radius:6px; margin:10px 0;">
                接入契约：创建时必须携带 session_tag（后端签发的会话标识）或转发 X-Forwarded-For 用户真实 IP，二者缺一返回 403；轮询必须使用 create 响应中的 poll_token（高熵轮询密钥，6 位码本身不可用于查询）。
              </div>'''
assert t.count(old) == 1
t = t.replace(old, new)

old = '''                <td><code>{"project": "your_project", "key": "项目安全密钥", "extra": "可选自定义会话参数", "expire_seconds": 300}</code></td>'''
new = '''                <td><code>{"project": "your_project", "key": "项目安全密钥", "session_tag": "会话标识（必填，后端签发）", "expire_seconds": 300}</code></td>'''
assert t.count(old) == 1
t = t.replace(old, new)

# AI Prompt 请求行（先替换，避免与表格/图示冲突）
old = '''GET ${baseUrl}/api/code/check?code=640798'''
new = '''GET ${baseUrl}/api/code/check?poll_token=<create 返回的 poll_token>'''
assert t.count(old) == 1
t = t.replace(old, new)

# 图示行
old = '''      │ 4. 轮询 GET /api/code/check?code=640798│                                      │'''
new = '''      │ 4. 轮询 GET /api/code/check?poll_token│                                      │'''
assert t.count(old) == 1
t = t.replace(old, new)

# 表格行（其余残留）
n = t.count('api/code/check?code=640798')
print('remaining check?code refs:', n)
if n:
    t = t.replace('api/code/check?code=640798', 'api/code/check?poll_token=<create 返回的 poll_token>')

old = '''                <td><code>{"project": "your_project", "key": "项目安全密钥", "extra": "可选自定义会话参数", "expire_seconds": 300}</code></td>'''
# 上面已替换；此处检查是否还有 extra 表格行
print('remaining extra td:', t.count(old))

old = '''  body: JSON.stringify({ project: 'your_project', key: 'sec_your_key', expire_seconds: 300 })
  });
  const { data } = await createRes.json();
  const code = data.code;

  // 网页展示该验证码，引导用户发给公众号
  document.getElementById('code-view').innerText = code;

  // 步骤 2: 定时器轮询状态 (每 1.5 秒查一次)
  const timer = setInterval(async () => {
    const checkRes = await fetch(`http://中枢域名/api/code/check?code=${code}`);'''
new = '''  body: JSON.stringify({ project: 'your_project', key: 'sec_your_key', session_tag: 'sess_abc123', expire_seconds: 300 })
  });
  const { data } = await createRes.json();
  const code = data.code;
  const pollToken = data.poll_token; // 轮询密钥：业务后端凭此查询状态

  // 网页展示该验证码，引导用户发给公众号
  document.getElementById('code-view').innerText = code;

  // 步骤 2: 定时器轮询状态 (每 1.5 秒查一次，携带 poll_token)
  const timer = setInterval(async () => {
    const checkRes = await fetch(`http://中枢域名/api/code/check?poll_token=${pollToken}`);'''
assert t.count(old) == 1
t = t.replace(old, new)

# ===== AI Prompt 模板（避开反引号，用无反引号子串） =====
old = '''"expire_seconds": 300, "extra": "<可选自定义参数>" }'''
new = '''"expire_seconds": 300, "session_tag": "<后端签发的会话标识，必填>" }'''
assert t.count(old) == 1
t = t.replace(old, new)

old = '''{ "code": 0, "data": { "code": "640798", "project": "your_project", "project_name": "项目名称", "expire_seconds": 300 } }'''
new = '''{ "code": 0, "data": { "code": "640798", "poll_token": "<轮询密钥>", "project": "your_project", "project_name": "项目名称", "expire_seconds": 300 } }'''
assert t.count(old) == 1
t = t.replace(old, new)

save('web/console.html', t)
print('console.html synced')

# ===== README =====
r = load('README.md')

old = '''      "project": "shop",
      "key": "sec_shop_8f9a2b1c",
      "expire_seconds": 300,
      "extra": "user_session_10086"'''
new = '''      "project": "shop",
      "key": "sec_shop_8f9a2b1c",
      "session_tag": "user_session_10086",
      "expire_seconds": 300'''
assert r.count(old) == 1
r = r.replace(old, new)

old = '''      "data": {
        "code": "839201",
        "project": "shop",
        "project_name": "商城系统",
        "expire_seconds": 300
      }'''
new = '''      "data": {
        "code": "839201",
        "poll_token": "9f2c7a1e8b3d4f6a9c0e1d2b3a4f5c6d",
        "project": "shop",
        "project_name": "商城系统",
        "expire_seconds": 300
      }'''
assert r.count(old) == 1
r = r.replace(old, new)

old = '''* **接口**：`GET /api/code/check?code=839201`'''
new = '''* **接口**：`GET /api/code/check?poll_token=9f2c7a1e8b3d4f6a9c0e1d2b3a4f5c6d`'''
assert r.count(old) == 1
r = r.replace(old, new)

old = '''### 2. 申请 6 位验证码（流向二）'''
new = '''### 2. 申请 6 位验证码（流向二）

> 接入契约：请求必须携带 `session_tag`（后端签发的会话标识，同一访客/尝试保持稳定）或转发 `X-Forwarded-For` 用户真实 IP，二者缺一返回 403；轮询必须使用响应中的 `poll_token`，6 位码本身不可用于查询。'''
assert r.count(old) == 1
r = r.replace(old, new)

save('README.md', r)
print('README synced')
