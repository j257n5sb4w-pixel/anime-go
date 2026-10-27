package main

// 登录 / 首次注册页面。
// 刻意做成自包含的单页（不引用主 UI 的脚本），因为这两个页面必须在"未登录"状态下可用。

const authPageCSS = `
:root{color-scheme:dark}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
  background:#0f1115;color:#e6e6e6;
  font:14px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif}
.card{width:100%;max-width:380px;background:#171a21;border:1px solid #262b36;border-radius:14px;
  padding:30px 28px;box-shadow:0 18px 50px rgba(0,0,0,.45)}
.brand{display:flex;align-items:center;gap:10px;margin-bottom:6px}
.dot{width:10px;height:10px;border-radius:50%;background:#4f8cff;box-shadow:0 0 12px #4f8cff}
h1{margin:0;font-size:19px;font-weight:600;letter-spacing:.2px}
.sub{color:#8b93a3;font-size:12.5px;margin:2px 0 22px}
label{display:block;font-size:12.5px;color:#9aa3b2;margin:14px 0 6px}
input{width:100%;padding:11px 12px;border-radius:9px;border:1px solid #2c3341;background:#0f131a;
  color:#e6e6e6;font-size:14px;outline:none;transition:border-color .15s}
input:focus{border-color:#4f8cff}
button{width:100%;margin-top:22px;padding:11px;border:0;border-radius:9px;cursor:pointer;
  background:#4f8cff;color:#fff;font-size:14px;font-weight:600;transition:opacity .15s}
button:hover{opacity:.9}
button:disabled{opacity:.5;cursor:default}
.msg{margin-top:14px;font-size:12.5px;min-height:18px;line-height:1.5}
.err{color:#ff6b6b}
.ok{color:#4ecb71}
.hint{margin-top:16px;font-size:11.5px;color:#6b7383;line-height:1.7;border-top:1px solid #232833;padding-top:14px}
code{background:#0f131a;padding:1px 5px;border-radius:4px;color:#9aa3b2;font-size:11.5px}
`

const setupHTML = `<!DOCTYPE html>
<html lang="zh-CN"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>初始化 · anime-go</title>
<style>` + authPageCSS + `</style>
</head><body>
<form class="card" id="f" autocomplete="on">
  <div class="brand"><span class="dot"></span><h1>anime-go</h1></div>
  <div class="sub">第一次使用，先创建一个管理员账号</div>

  <label for="u">账号</label>
  <input id="u" name="username" autocomplete="username" placeholder="例如 admin" required>
  <label for="p">密码</label>
  <input id="p" name="password" type="password" autocomplete="new-password" placeholder="至少 6 位" required>
  <label for="p2">确认密码</label>
  <input id="p2" type="password" autocomplete="new-password" placeholder="再输一次" required>

  <button type="submit" id="btn">创建并登录</button>
  <div class="msg" id="msg"></div>
  <div class="hint">
    这个账号仅保存在本机 <code>auth.json</code>，密码经 PBKDF2 加盐哈希后存储，不会被明文保存。<br>
    创建后会自动生成一枚 <b>API Key</b>，供 AI 或脚本远程调用，可在「设置」页查看与重置。
  </div>
</form>
<script>
const $ = id => document.getElementById(id);
$('f').addEventListener('submit', async e => {
  e.preventDefault();
  const u = $('u').value.trim(), p = $('p').value, p2 = $('p2').value;
  if(!u){ $('msg').innerHTML = '<span class="err">请输入账号</span>'; return; }
  if(p.length < 6){ $('msg').innerHTML = '<span class="err">密码至少 6 位</span>'; return; }
  if(p !== p2){ $('msg').innerHTML = '<span class="err">两次输入的密码不一致</span>'; return; }
  $('btn').disabled = true; $('btn').textContent = '创建中…';
  try{
    const r = await fetch('/api/setup', {
      method:'POST',
      headers:{'Content-Type':'application/json','X-Requested-With':'anime-go'},
      body: JSON.stringify({username:u, password:p})
    });
    const d = await r.json();
    if(d.ok){ location.href = '/'; return; }
    $('msg').innerHTML = '<span class="err">'+ (d.error||'创建失败') +'</span>';
  }catch(err){
    $('msg').innerHTML = '<span class="err">网络错误：'+err+'</span>';
  }
  $('btn').disabled = false; $('btn').textContent = '创建并登录';
});
$('u').focus();
</script>
</body></html>`

const loginHTML = `<!DOCTYPE html>
<html lang="zh-CN"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>登录 · anime-go</title>
<style>` + authPageCSS + `</style>
</head><body>
<form class="card" id="f" autocomplete="on">
  <div class="brand"><span class="dot"></span><h1>anime-go</h1></div>
  <div class="sub">请登录后继续</div>

  <label for="u">账号</label>
  <input id="u" name="username" autocomplete="username" required>
  <label for="p">密码</label>
  <input id="p" name="password" type="password" autocomplete="current-password" required>

  <button type="submit" id="btn">登录</button>
  <div class="msg" id="msg"></div>
  <div class="hint">
    登录状态保持 __TTL_TEXT__（可在「设置 → 登录」里改：填 0 单次有效、填 -1 永久）。
    忘记密码可在服务器上删除 <code>auth.json</code> 后重新初始化
    （会同时重置 API Key）。
  </div>
</form>
<script>
const $ = id => document.getElementById(id);
$('f').addEventListener('submit', async e => {
  e.preventDefault();
  $('btn').disabled = true; $('btn').textContent = '登录中…';
  try{
    const r = await fetch('/api/login', {
      method:'POST',
      headers:{'Content-Type':'application/json','X-Requested-With':'anime-go'},
      body: JSON.stringify({username:$('u').value.trim(), password:$('p').value})
    });
    const d = await r.json();
    if(d.ok){ location.href = '/'; return; }
    $('msg').innerHTML = '<span class="err">'+ (d.error||'登录失败') +'</span>';
  }catch(err){
    $('msg').innerHTML = '<span class="err">网络错误：'+err+'</span>';
  }
  $('btn').disabled = false; $('btn').textContent = '登录';
});
$('u').focus();
</script>
</body></html>`
