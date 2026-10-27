package main

// ============================================================
//  Web UI（v0.4）：左侧栏五视图
//
//  首页 / 订阅 / 下载 / 日志 / 设置
//
//  设计原则：
//   - 单文件内嵌 HTML，无外部依赖，Zero 2W 上零构建成本
//   - 左栏固定，右侧内容区切换（纯 CSS + class 切换，无路由库）
//   - 手机端左栏收成顶部横向标签
//   - 所有写操作都受 ValidateSavePath 约束
// ============================================================

const webUI = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>anime-go</title>
<style>
*{box-sizing:border-box}
body{font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;margin:0;background:#f5f6f8;color:#222;font-size:15px;line-height:1.6;display:flex;min-height:100vh}

/* ===== 左侧栏 ===== */
.sidebar{width:190px;flex:0 0 190px;background:#fff;border-right:1px solid #eef0f4;padding:18px 0;display:flex;flex-direction:column;position:sticky;top:0;height:100vh}
.logo{padding:0 18px 16px;font-size:17px;font-weight:600;color:#222;letter-spacing:.3px}
.logo span{color:#4a7cff}
.nav{display:flex;flex-direction:column;gap:2px;padding:0 10px}
.nav a{display:flex;align-items:center;gap:9px;padding:10px 12px;border-radius:8px;color:#555;text-decoration:none;font-size:14.5px;cursor:pointer;transition:background .12s}
.nav a:hover{background:#f5f7fb}
.nav a.active{background:#eef3ff;color:#4a7cff;font-weight:500}
.nav a .ico{width:18px;text-align:center;font-size:15px;opacity:.85}
.nav a .badge{margin-left:auto;background:#4a7cff;color:#fff;font-size:11px;padding:1px 6px;border-radius:9px;min-width:18px;text-align:center}
.nav a.active .badge{background:#4a7cff}
.sidefoot{margin-top:auto;padding:12px 18px;font-size:11.5px;color:#bbb;line-height:1.6}

/* ===== 主内容区 ===== */
main{flex:1;min-width:0;padding:20px 22px;max-width:1100px}
.view{display:none}
.view.active{display:block}
h1{font-size:19px;margin:0 0 3px;font-weight:600}
.sub{color:#999;font-size:13px;margin-bottom:16px}

/* ===== 卡片与表单 ===== */
.card{background:#fff;border-radius:10px;padding:16px;margin-bottom:14px;box-shadow:0 1px 3px rgba(0,0,0,.06)}
.card h2{font-size:14.5px;margin:0 0 12px;color:#444;font-weight:600;border-left:3px solid #4a7cff;padding-left:8px}
input[type=text],input[type=url],input[type=number],select,textarea{width:100%;padding:9px 10px;border:1px solid #dcdfe6;border-radius:6px;font-size:14.5px;font-family:inherit;background:#fff}
textarea{min-height:60px;resize:vertical;font-size:13px;line-height:1.5}
input:focus,select:focus,textarea:focus{outline:none;border-color:#4a7cff}
button{padding:8px 14px;border:0;border-radius:6px;background:#4a7cff;color:#fff;font-size:14px;cursor:pointer;font-family:inherit;white-space:nowrap}
button:hover{background:#3a6aeb}
button:disabled{background:#b5c4e8;cursor:not-allowed}
button.ghost{background:#fff;color:#4a7cff;border:1px solid #cfd8ee}
button.ghost:hover{background:#f0f4ff}
button.danger{background:#fff;color:#d93025;border:1px solid #f5c6c2}
button.danger:hover{background:#fdf0ef}
button.sm{padding:4px 9px;font-size:12.5px}
.row{display:flex;gap:8px;margin-top:10px;flex-wrap:wrap}
.row>*{flex:1;min-width:120px}
.row>button{flex:0 0 auto}
label{display:block;font-size:13px;color:#666;margin:10px 0 4px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{text-align:left;padding:9px 6px;border-bottom:1px solid #eef0f4;vertical-align:middle}
th{color:#999;font-weight:500;font-size:12px}
.ok{color:#1a9c4d}.err{color:#d93025}.warn{color:#e08a00}
pre{background:#1e1e1e;color:#d4d4d4;padding:12px;border-radius:6px;overflow:auto;font-size:12px;max-height:520px;margin:8px 0 0;line-height:1.5}
.hint{font-size:12px;color:#999;margin-top:6px}
.hidden{display:none!important}
.pill{display:inline-block;padding:1px 7px;border-radius:10px;font-size:11px}
.pill.on{background:#e6f6ec;color:#1a9c4d}
.pill.off{background:#f0f0f0;color:#999}
.pill.err{background:#fdf0ef;color:#d93025}
.acts{display:flex;gap:5px;justify-content:flex-end}
.modal{position:fixed;inset:0;background:rgba(0,0,0,.45);display:flex;align-items:flex-start;justify-content:center;padding:20px;overflow:auto;z-index:99}
.modal.hidden{display:none!important}
.modal .box{background:#fff;border-radius:12px;padding:18px;width:100%;max-width:560px}
.modal h3{margin:0 0 12px;font-size:16px}
.chk{display:flex;align-items:center;gap:8px;margin-top:12px;font-size:13px;color:#666}
.chk input{width:auto;flex:0 0 auto}
/* ===== 删除弹窗 ===== */
.delcheck{display:flex;align-items:flex-start;gap:9px;margin-top:2px;font-size:13.5px;color:#333;
  cursor:pointer;padding:11px 12px;border:1px solid #e6e9f0;border-radius:9px;background:#fafbfd;line-height:1.45}
.delcheck:hover{border-color:#cfd6e4}
.delcheck input{width:16px;height:16px;flex:0 0 auto;margin-top:1px;cursor:pointer}
.delcheck span{flex:1}
.delhint{font-size:12px;color:#8a93a3;margin-top:9px;line-height:1.55;min-height:34px}

/* ===== 首页统计块 ===== */
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(120px,1fr));gap:10px;margin-bottom:14px}
.stat{background:#fff;border-radius:10px;padding:13px 14px;box-shadow:0 1px 3px rgba(0,0,0,.06)}
.stat .k{font-size:11.5px;color:#999;margin-bottom:3px}
.stat .v{font-size:20px;font-weight:600;color:#222}
.stat .v small{font-size:12px;font-weight:400;color:#999;margin-left:2px}
.weekbar{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:14px}
.weekbar .d{display:flex;align-items:center;gap:7px;padding:7px 13px;border-radius:9px;
  background:#fff;font-size:13px;color:#666;border:1px solid #eef0f4;cursor:pointer;
  transition:.15s;user-select:none;min-width:74px;justify-content:center}
.weekbar .d:hover{border-color:#d6ddeb;background:#fafbff;color:#333}
.weekbar .d .wl{letter-spacing:.5px}         /* 周一…周日 都是两个字，宽度天然一致 */
.weekbar .d b{font-weight:600;font-size:12px;min-width:16px;height:18px;line-height:18px;
  text-align:center;border-radius:9px;background:#f2f4f8;color:#8b93a3;padding:0 5px}
.weekbar .d.today{border-color:#cfd8ee;color:#4a7cff}
.weekbar .d.today b{background:#e4ecff;color:#4a7cff}
.weekbar .d.on{background:#eef3ff;border-color:#4a7cff;color:#2b5cff;font-weight:600;
  box-shadow:0 1px 4px rgba(74,124,255,.18)}
.weekbar .d.on b{background:#4a7cff;color:#fff}
.weekbar .d.zero{color:#c2c8d2}
.weekbar .d.zero b{background:#f5f6f9;color:#c8cedb}
.weekbar .d.und{color:#a08b4f;border-style:dashed}
.weekbar .d.mv{color:#2f7d32;border-style:solid;border-color:#cfe6d5}
.weekbar .d.mv b{background:#e8f5eb;color:#2f7d32}
.weekbar .d.mv.on{background:#e8f5eb;border-color:#2f7d32;color:#226b28}
.weekbar .d.mv.on b{background:#2f7d32;color:#fff}
/* 日期标题行 */
.dayhead{display:flex;align-items:center;gap:9px;margin:0 0 11px}
.dayhead h2{font-size:14.5px;margin:0;color:#444;font-weight:600}
.daycount{font-size:12px;color:#a3aab8}
.dayhead .ghost{margin-left:auto;padding:4px 10px;font-size:12px}
.subcard.dim{opacity:.62}

/* ===== 订阅卡（首页用）===== */
.subgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(240px,1fr));gap:12px}
.subcard{background:#fff;border-radius:10px;padding:14px;box-shadow:0 1px 3px rgba(0,0,0,.06);display:flex;flex-direction:column;gap:6px}
.subcard .t{font-size:14px;font-weight:500;line-height:1.45;color:#222}
.subcard .m{font-size:12px;color:#999;display:flex;gap:10px;flex-wrap:wrap}
.subcard .m b{color:#4a7cff;font-weight:500}
.empty{background:#fff;border-radius:10px;padding:36px 20px;text-align:center;color:#aaa;font-size:13.5px}

/* ===== 下载进度条 ===== */
.bar{height:5px;background:#eef0f4;border-radius:3px;overflow:hidden;margin-top:5px}
.bar i{display:block;height:100%;background:#4a7cff;border-radius:3px;transition:width .3s}
.bar i.done{background:#1a9c4d}
.dlname{font-size:13px;line-height:1.4;word-break:break-all}
.dlmeta{font-size:11.5px;color:#999;margin-top:3px}

/* ===== 订阅页右上角的 mikan 搜索 ===== */
.pagehead{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;flex-wrap:wrap;margin-bottom:16px}
.pagehead .sub{margin:0}
.mksearch{display:flex;gap:6px;align-items:center;flex:0 0 auto}
.mksearch input{width:236px}
.mksearch button{padding:9px 15px}
.mkgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(108px,1fr));gap:10px;max-height:430px;overflow:auto}
.mkcard{background:#fff;border:1px solid #eef0f4;border-radius:8px;overflow:hidden;cursor:pointer;transition:.12s;display:flex;flex-direction:column}
.mkcard:hover{border-color:#4a7cff;box-shadow:0 2px 8px rgba(74,124,255,.16);transform:translateY(-1px)}
.mkcard img{width:100%;aspect-ratio:1/1.42;object-fit:cover;display:block;background:#f5f6f8}
.mkcard .n{font-size:12px;padding:6px 7px;line-height:1.35;color:#333;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
.mkcard .ph{width:100%;aspect-ratio:1/1.42;background:#f0f2f6;display:flex;align-items:center;justify-content:center;color:#c8ccd4;font-size:22px}
.mkhead{display:flex;align-items:center;gap:10px;margin-bottom:10px}
.mkhead img{width:46px;height:64px;object-fit:cover;border-radius:5px;background:#f5f6f8}
.mkhead .ph{width:46px;height:64px;border-radius:5px;background:#f0f2f6;display:flex;align-items:center;justify-content:center;color:#c8ccd4;font-size:18px}
.mkhead .t{font-size:14px;font-weight:500;line-height:1.4}
.mkhead .m{font-size:12px;color:#999;margin-top:2px}
.mkgroup{display:flex;align-items:center;gap:10px;padding:9px 0;border-bottom:1px solid #f2f4f8}
.mkgroup:last-child{border-bottom:0;padding-bottom:0}
.mkgroup .l{flex:1;min-width:0}
.mkgroup .l b{font-weight:500;font-size:13.5px;color:#333}
.mkgroup .l span{display:block;font-size:11.5px;color:#a9afba;word-break:break-all;margin-top:1px}

/* ===== 新增订阅弹窗 ===== */
.addbtn{display:inline-flex;align-items:center;gap:7px;padding:10px 18px;font-size:14px;
  font-weight:500;border-radius:10px;background:#4a7cff;color:#fff;border:0;cursor:pointer;
  box-shadow:0 2px 8px rgba(74,124,255,.24);transition:.12s}
.addbtn:hover{background:#3b6bf0;box-shadow:0 3px 12px rgba(74,124,255,.32)}
.addbtn:active{transform:translateY(1px)}
.addbox{max-width:1020px;display:flex;flex-direction:column;max-height:92vh;padding:0}
.addbox .mhead{display:flex;align-items:center;justify-content:space-between;gap:10px;
  padding:15px 20px;border-bottom:1px solid #eef0f4;flex:0 0 auto}
.addbox .mtitle{font-size:15.5px;font-weight:600;color:#222}
.addbox .mbody{padding:16px 20px 20px;overflow:auto;flex:1 1 auto;min-height:0}
.addbox .sep{height:1px;background:#eef0f4;margin:16px 0 14px}
.sechead{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:12px}
.sechead>b{font-size:14px;color:#333;font-weight:600}
.sechead select{width:auto;min-width:118px;padding:6px 9px;font-size:12.5px}
.sechead .mksearch{margin-left:auto}
.sechead .mksearch input{width:190px;padding:6px 9px;font-size:12.5px}
.sechead .mksearch button{padding:5px 11px;font-size:12.5px}
.prevwrap{max-height:240px;overflow:auto;border:1px solid #eef0f4;border-radius:8px}
.prevwrap table{margin:0}
.prevwrap th{position:sticky;top:0;background:#f8f9fc;z-index:1}

/* 订阅名来源（mikan / TMDB 二选一） */
.namesrc{display:flex;gap:8px;margin-top:7px;flex-wrap:wrap}
.namesrc .rd{display:flex;align-items:center;gap:7px;margin:0;cursor:pointer;
  padding:7px 11px;border:1px solid #e6e9f0;border-radius:8px;background:#fafbfd;
  font-size:13px;color:#4a5160;transition:.12s;flex:1 1 220px;min-width:0}
.namesrc .rd:hover{border-color:#cfd8ee;background:#f7f9ff}
.namesrc .rd input{width:15px;height:15px;flex:0 0 auto;margin:0;cursor:pointer;accent-color:#4a7cff}
.namesrc .rd .nm{color:#9aa1ad;font-size:12px;margin-left:2px;
  overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1 1 auto;min-width:0}
.namesrc .rd.sel{border-color:#4a7cff;background:#eef3ff;color:#2b5cff;font-weight:500}
.namesrc .rd.sel .nm{color:#5c74b8}
/* TMDB 条目条 */
.tmdb{display:flex;align-items:center;gap:10px;padding:10px 12px;border:1px solid #e6e9f0;
  border-radius:9px;background:#fafbfd;flex-wrap:wrap}
.tmdb .tml{flex:1 1 240px;min-width:0;line-height:1.45}
.tmdb .tml b{font-size:13.5px;color:#2e3440;font-weight:600}
.tmdb .tml .sub{margin:0;font-size:11.5px;color:#9aa1ad;display:block;
  overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.tmdb .tlink{font-size:12px;color:#4a7cff;text-decoration:none;white-space:nowrap}
.tmdb .tlink:hover{text-decoration:underline}
.tmdb.miss .tml b{color:#8a93a3;font-weight:500}
#tmdbPick{border:1px solid #e6e9f0;border-radius:9px;padding:0 12px 12px;margin-top:8px;background:#fcfdff}
#tmdbCand{margin-top:9px;max-height:250px;overflow:auto}
.tmcand{display:flex;align-items:center;gap:10px;padding:7px 0;border-bottom:1px solid #f2f4f8;cursor:pointer}
.tmcand:last-child{border-bottom:0}
.tmcand:hover .tl b{color:#2b5cff}
.tmcand img{width:34px;height:48px;object-fit:cover;border-radius:4px;background:#f0f2f6;flex:0 0 auto}
.tmcand .ph{width:34px;height:48px;border-radius:4px;background:#f0f2f6;flex:0 0 auto;
  display:flex;align-items:center;justify-content:center;color:#c8ccd4;font-size:15px}
.tmcand .tl{flex:1 1 auto;min-width:0;line-height:1.4}
.tmcand .tl b{font-size:13px;color:#2e3440;font-weight:500}
.tmcand .tl span{display:block;font-size:11.5px;color:#a3aab8;
  overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.tmcand .yr{font-size:11.5px;color:#9aa1ad;flex:0 0 auto}

/* 按星期分组 */
.sday{margin-bottom:16px}
.sday:last-child{margin-bottom:2px}
.sdayh{display:flex;align-items:center;gap:8px;margin-bottom:8px}
.sdayh .lb{font-size:13px;font-weight:600;color:#3d4453}
.sdayh .ct{font-size:11px;color:#9aa1ad;background:#f2f4f8;border-radius:20px;
  padding:1px 8px;line-height:1.6}
.sday.today .sdayh .lb{color:#4a7cff}
.sday.today .sdayh .ct{background:#e8efff;color:#4a7cff}
.sgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(96px,1fr));gap:10px}
.scard{background:#fff;border:1px solid #eef0f4;border-radius:8px;overflow:hidden;
  cursor:pointer;transition:.12s;display:flex;flex-direction:column}
.scard:hover{border-color:#4a7cff;box-shadow:0 2px 8px rgba(74,124,255,.16);transform:translateY(-1px)}
.scard img{width:100%;aspect-ratio:1/1.42;object-fit:cover;display:block;background:#f5f6f8}
.scard .ph{width:100%;aspect-ratio:1/1.42;background:#f0f2f6;display:flex;align-items:center;
  justify-content:center;color:#c8ccd4;font-size:20px}
.scard .n{font-size:11.5px;padding:5px 6px;line-height:1.35;color:#333;
  display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}

/* ===== 订阅封面墙 ===== */
.subgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(172px,1fr));gap:16px}
.subcard{position:relative;border-radius:11px;overflow:hidden;background:#fff;border:1px solid #eceff4;
  cursor:pointer;transition:.16s;display:flex;flex-direction:column}
.subcard:hover{transform:translateY(-3px);box-shadow:0 10px 24px rgba(20,40,90,.16);border-color:#c8d4ff}
.subcard.off{opacity:.72}
.subcover{position:relative;width:100%;aspect-ratio:2/3;background:#f2f4f8;overflow:hidden}
.subcover img{width:100%;height:100%;object-fit:cover;display:block}
.subcover .noph{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;
  gap:6px;color:#b9c0cc;font-size:11.5px;background:linear-gradient(160deg,#f7f8fb,#eef1f7)}
.subcover .noph b{font-size:26px;font-weight:400;color:#cfd5df}
/* 集数角标：封面右下角，压在图片上，避免和悬浮按钮抢中间位置 */
.subcover .epbadge{position:absolute;right:6px;bottom:6px;z-index:2;
  padding:2px 8px;border-radius:9px;font-size:14px;line-height:1.45;font-weight:600;
  color:#fff;background:rgba(12,18,32,.7);backdrop-filter:blur(3px);
  letter-spacing:.2px;pointer-events:none;white-space:nowrap}
/* 「4」和「/ 12」必须一样大一样白：.subcard .t 会把 span 撑成 14px，
   而角标本身原来是 11px，于是前半截看着比后半截小。这里两边都锁死 14px/白色。 */
.subcover .epbadge .t{font-size:14px;font-weight:600;color:#fff;opacity:1}
.subcover .epbadge.z{background:rgba(190,60,60,.78)}
.subcard:hover .subcover .epbadge{opacity:0;transition:.16s}
/* 剧场版角标：替代集数角标（剧场版没有集数可数） */
.subcover .mvtag{position:absolute;right:6px;bottom:6px;z-index:2;
  padding:2px 8px;border-radius:9px;font-size:11px;line-height:1.6;font-weight:600;
  color:#fff;background:rgba(38,120,72,.86);backdrop-filter:blur(3px);
  letter-spacing:.3px;pointer-events:none;white-space:nowrap}
.subcard:hover .subcover .mvtag{opacity:0;transition:.16s}
/* ===== 订阅体检 ===== */
.ahint{font-size:12.5px;color:#6b7385;margin-bottom:12px;line-height:1.6}
.aitem{border:1px solid #eceff4;border-radius:10px;padding:12px 14px;margin-bottom:10px}
.aitem.err{border-color:#f3c9c9;background:#fdf6f6}
.aitem.warn{border:1px solid #f2dfbc;background:#fdfbf4}
.aitem.ok{border-color:#dbe6dd;background:#fafcfb}
.aihead{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.aihead .t{font-weight:600;font-size:13.5px;color:#2a3446}
.aihead .m{margin-left:auto;display:flex;gap:6px;align-items:center}
.aicnt{font-size:11.5px;color:#8a92a3}
.aiissue{margin-top:9px;padding-left:10px;border-left:2px solid #e3e7ee;font-size:12.5px;line-height:1.65;color:#4a5468}
.aiissue.err{border-left-color:#e5484d}
.aiissue.warn{border-left-color:#e0a43a}
.aiissue.info{border-left-color:#c3cbd8}
.aiissue b{color:#2a3446}
.aiissue .smp{margin-top:4px;font-size:11.5px;color:#8a92a3;word-break:break-all}
.aiissue .fix{margin-top:6px}
/* 悬浮才出现：深色蒙层 + 三个圆形小按钮，居中排布 */
.subcard .ov{position:absolute;inset:0;background:rgba(10,18,35,.42);
  opacity:0;transition:.16s;display:flex;align-items:center;justify-content:center;gap:12px}
.subcard:hover .ov{opacity:1}
.subcard .ov .ib{width:42px;height:42px;border-radius:50%;border:0;cursor:pointer;
  background:rgba(255,255,255,.94);color:#6b7385;font-size:15px;line-height:1;
  display:flex;align-items:center;justify-content:center;
  transition:.12s;box-shadow:0 2px 8px rgba(0,0,0,.22)}
.subcard .ov .ib svg{display:block;width:21px;height:21px}
.subcard .ov .ib:hover{background:#fff;color:#39404e;transform:scale(1.12)}
.subcard .ov .ib.d{background:rgba(240,90,90,.95);color:#fff}
.subcard .ov .ib.d:hover{background:#e5484d;color:#fff}
/* 标题条：只有名字，没有年份 */
.subcard .cap{padding:9px 10px 10px;font-size:12.5px;line-height:1.4;color:#333;text-align:center;
  display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;min-height:38px}
/* 本地文件列表弹窗 */
.flhead{display:flex;align-items:center;gap:10px;margin-bottom:10px;flex-wrap:wrap}
.flpath{font-size:11.5px;color:#8b93a3;word-break:break-all;background:#f6f8fb;
  padding:5px 8px;border-radius:6px;flex:1;min-width:200px;font-family:ui-monospace,Consolas,monospace}
.flsum{font-size:12px;color:#7c8494}
.flist{max-height:380px;overflow:auto;border:1px solid #eef0f4;border-radius:8px}
.flist table{width:100%;border-collapse:collapse;font-size:12.5px}
.flist td{padding:7px 10px;border-bottom:1px solid #f4f6fa;vertical-align:top}
.flist tr:last-child td{border-bottom:0}
.flist tr:hover td{background:#fafbff}
.flist .fn{word-break:break-all}
.flist .fs{color:#9aa1ad;white-space:nowrap;text-align:right;width:88px}
.flist .ft{color:#b0b7c3;white-space:nowrap;width:82px;font-size:11.5px}
.flist .dir td{color:#4a7cff;font-weight:500}
.flist .ic{display:inline-block;width:16px;color:#b0b7c3}
/* 文件列表里按种子分组的小标题 */
.tg{display:flex;align-items:center;gap:8px;margin:12px 0 6px;padding:6px 10px;
  background:#f6f8fc;border-radius:7px;font-size:12.5px}
.tg:first-child{margin-top:0}
.tgname{flex:1;min-width:0;font-weight:600;color:#39405287;word-break:break-all;
  color:#394052}
.tgst{flex:none;font-size:11px;padding:2px 7px;border-radius:9px;
  background:#eef1f6;color:#8b93a3}
.tgst.ok{background:#e8f6ec;color:#31a24c}
.tgpct{flex:none;font-size:11.5px;color:#8b93a3;min-width:44px;text-align:right}
/* 封面候选选择 */
.cvpick{display:grid;grid-template-columns:repeat(auto-fill,minmax(78px,1fr));gap:8px;
  max-height:300px;overflow:auto;padding:2px}
.cvpick .c{border:2px solid transparent;border-radius:7px;overflow:hidden;cursor:pointer;background:#f5f6f8;position:relative}
.cvpick .c:hover{border-color:#4a7cff}
.cvpick .c img{width:100%;aspect-ratio:2/3;object-fit:cover;display:block}
.cvpick .c .n{font-size:10.5px;padding:3px 4px;line-height:1.25;color:#555;
  display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;background:#fff}
/* 编辑弹窗里的封面区 */
.ecv{display:flex;gap:12px;align-items:flex-start;padding:10px;background:#f8f9fc;
  border:1px solid #eceff4;border-radius:9px;margin:0 0 14px}
.ecv-p{width:76px;flex:0 0 76px;aspect-ratio:2/3;border-radius:6px;overflow:hidden;
  background:#eef1f7;position:relative}
.ecv-p img{width:100%;height:100%;object-fit:cover;display:block}
.ecv-p .noph{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;
  justify-content:center;gap:3px;color:#b9c0cc;font-size:10px}
.ecv-p .noph b{font-size:20px;font-weight:400;color:#cfd5df}
.ecv-i{flex:1;min-width:0}
.ecv-n{font-size:13px;font-weight:500;color:#333;line-height:1.35}
.ecv-y{font-size:11.5px;color:#9aa1ad;margin-top:2px}
.ecv-i .tlink{display:inline-block;margin-top:4px;font-size:12px;color:#4a7cff;
  text-decoration:none;white-space:nowrap}
.ecv-i .tlink:hover{text-decoration:underline}
/* ===== 设置页二级菜单 ===== */
.tabbar{display:flex;gap:4px;margin:14px 0 16px;border-bottom:1px solid #e8ebf0;
  overflow-x:auto;scrollbar-width:none}
.tabbar::-webkit-scrollbar{display:none}
.tab{flex:0 0 auto;background:transparent;border:0;border-bottom:2px solid transparent;
  padding:9px 16px;font-size:13.5px;color:#7c8494;cursor:pointer;border-radius:6px 6px 0 0;
  transition:.15s;margin-bottom:-1px;font-family:inherit}
.tab:hover{color:#333;background:#f5f7fb}
.tab.on{color:#2b5cff;border-bottom-color:#2b5cff;font-weight:500;background:#f2f6ff}
/* 当前账号卡片 */
.accrow{display:flex;align-items:center;gap:12px}
.accava{width:44px;height:44px;flex:0 0 44px;border-radius:50%;display:flex;align-items:center;
  justify-content:center;background:linear-gradient(135deg,#4a7cff,#6f9bff);color:#fff;
  font-size:19px;font-weight:500;text-transform:uppercase}
.accinfo{min-width:0;flex:1}
.accname{font-size:15px;font-weight:500;color:#252c3a;line-height:1.3}
.acctip{font-size:12px;color:#9aa1ad;margin-top:2px}

/* ===== 应用内软提示 toast（替代 alert：不打断、不点确认，2 秒后淡出） ===== */
#toastWrap{position:fixed;top:16px;left:50%;transform:translateX(-50%);z-index:9999;
  display:flex;flex-direction:column;align-items:center;gap:8px;pointer-events:none;width:100%;max-width:520px;padding:0 14px}
.toast{pointer-events:auto;display:flex;align-items:flex-start;gap:9px;
  padding:10px 15px;border-radius:10px;font-size:13.5px;line-height:1.5;
  background:rgba(30,38,54,.94);color:#fff;box-shadow:0 6px 24px rgba(15,23,42,.22);
  backdrop-filter:blur(6px);white-space:pre-wrap;word-break:break-word;
  opacity:0;transform:translateY(-6px);transition:opacity .18s ease,transform .18s ease}
.toast.in{opacity:1;transform:translateY(0)}
.toast.out{opacity:0;transform:translateY(-6px)}
.toast .ti{flex:0 0 auto;font-size:14px;line-height:1.35}
.toast.ok{background:rgba(21,122,74,.95)}
.toast.err{background:rgba(178,45,38,.95)}
.toast.warn{background:rgba(173,111,10,.95)}
/* 应用内确认/输入框（替代 confirm / prompt） */
#askModal .box{max-width:440px}
#askMsg{font-size:13.5px;color:#444;line-height:1.62;white-space:pre-wrap;margin-bottom:6px}
#askModal .row{margin-top:18px}

/* ===== 手机端 ===== */
@media(max-width:760px){
  body{display:block}
  .sidebar{width:100%;flex:none;height:auto;position:static;border-right:0;border-bottom:1px solid #eef0f4;padding:10px 0 0}
  .logo{padding:0 14px 8px;font-size:16px}
  .nav{flex-direction:row;overflow-x:auto;padding:0 8px 8px;gap:4px}
  .nav a{padding:7px 11px;font-size:13.5px;white-space:nowrap}
  .nav a .ico{display:none}
  .sidefoot{display:none}
  main{padding:14px}
  .stats{grid-template-columns:repeat(2,1fr)}
}
</style>
</head>
<body>

<!-- ==================== 左侧栏 ==================== -->
<aside class="sidebar">
  <div class="logo">anime<span>-go</span></div>
  <nav class="nav">
    <a onclick="go('home')" id="nav-home" class="active"><span class="ico">🏠</span>首页</a>
    <a onclick="go('subs')" id="nav-subs"><span class="ico">📺</span>订阅<span class="badge" id="badge-subs">0</span></a>
    <a onclick="go('dl')" id="nav-dl"><span class="ico">⬇</span>下载<span class="badge hidden" id="badge-dl">0</span></a>
    <a onclick="go('logs')" id="nav-logs"><span class="ico">📄</span>日志</a>
    <a onclick="go('settings')" id="nav-settings"><span class="ico">⚙</span>设置</a>
  </nav>
  <div class="sidefoot">
    <div id="sf-status">检查间隔 60 分钟</div>
    <div id="sf-mem"></div>
  </div>
</aside>

<main>

<!-- ==================== ① 首页 ==================== -->
<div class="view active" id="view-home">
  <h1>首页</h1>
  <div class="sub" id="homeDate">今天是 —</div>

  <div class="stats">
    <div class="stat"><div class="k">订阅总数</div><div class="v" id="st-total">0</div></div>
    <div class="stat"><div class="k">今日更新</div><div class="v" id="st-today">0</div></div>
    <div class="stat"><div class="k">已推送</div><div class="v" id="st-pushed">0</div></div>
    <div class="stat"><div class="k">下载中</div><div class="v" id="st-dl">0</div></div>
  </div>

  <div class="weekbar" id="weekbar"></div>

  <div class="dayhead">
    <h2 id="dayTitle">今日订阅</h2>
    <span class="daycount" id="dayCount"></span>
    <button class="ghost sm" id="dayBack" style="display:none" onclick="pickWeek('')">回到今天</button>
  </div>
  <div id="todayList"></div>
</div>

<!-- ==================== ② 订阅 ==================== -->
<div class="view" id="view-subs">
  <div class="pagehead">
    <div>
      <h1>订阅</h1>
      <div class="sub">管理全部 RSS 订阅</div>
    </div>
    <button id="btnAddSub" class="addbtn" onclick="openAdd()">
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor"
           stroke-width="2.1" stroke-linecap="round"><path d="M12 5.5v13M5.5 12h13"/></svg>
      订阅
    </button>
  </div>

  <div class="card">
    <div class="row" style="margin-top:0;margin-bottom:12px;align-items:center">
      <h2 style="margin:0;flex:1">我的订阅 <span class="sub" id="subCount" style="font-weight:400"></span></h2>
      <span class="hint" id="lastCheck" style="margin-right:6px"></span>
      <button class="ghost sm" onclick="loadSubs()">刷新</button>
      <button class="ghost sm" onclick="openAudit()" title="逐条核对：有没有因为规则或状态锁死而一集都没下成的订阅">体检</button>
      <button class="ghost sm" onclick="refreshAllCovers()" title="给还没有封面的订阅补抓 TMDB 封面">补抓封面</button>
      <button onclick="checkNow()">立即检查更新</button>
    </div>
    <div id="subGrid" class="subgrid"></div>
    <div id="subEmpty" class="hint" style="display:none;text-align:center;padding:24px 0">
      还没有订阅。点右上角「订阅」按钮，从当季新番里挑一部，或粘贴 RSS 链接。
    </div>
  </div>
</div>

<!-- ==================== ②-1 新增订阅弹窗 ==================== -->
<div class="modal hidden" id="addModal">
  <div class="box addbox">
    <div class="mhead">
      <div class="mtitle">新增订阅</div>
      <button class="ghost sm" onclick="closeAdd()">关闭</button>
    </div>

    <div class="mbody">
      <label>mikan RSS 链接</label>
      <div class="row" style="margin-top:0">
        <input type="text" id="rssurl" style="flex:1;min-width:0"
               placeholder="https://mikanime.tv/RSS/Bangumi?bangumiId=4059&subgroupid=583">
        <button class="ghost" onclick="fillByID()">按番号拼</button>
        <button onclick="preview()" id="btnPrev">预览</button>
      </div>
      <div class="hint">点下面的番剧卡片会自动填好；也可以直接粘贴 RSS 链接</div>

      <div id="meta" class="hidden">
        <label>订阅名（决定文件夹名，可在下面切换用哪个源）</label>
        <input type="text" id="title">
        <label class="ckline" style="display:flex;align-items:center;gap:8px;margin:10px 0 2px">
          <input type="checkbox" id="movie" style="width:auto;margin:0" onchange="applyMovieAdd()">
          <span>这是剧场版（一次性下载）</span>
        </label>
        <div class="hint" id="movieHint" style="margin:0 0 10px">
          勾上后：不按集数去重、只推一次、下完即停；默认落到「设置 → 下载 → 剧场版」里的目录。
          从 mikan 的「剧场版」分类点进来的卡片会自动勾上。
        </div>
        <div class="namesrc">
          <label class="rd"><input type="radio" name="namesrc" value="mikan" checked onchange="applyNameSrc()">
            用 mikan 名<span class="nm" id="nmMikan"></span></label>
          <label class="rd"><input type="radio" name="namesrc" value="tmdb" onchange="applyNameSrc()">
            用 TMDB 名<span class="nm" id="nmTmdb"></span></label>
        </div>

        <label>TMDB 条目（自动识别，识别错了可手动改）</label>
        <div class="tmdb" id="tmdbBar">
          <div class="tml">
            <b id="tmdbName">—</b>
            <span class="sub" id="tmdbSub"></span>
          </div>
          <a class="tlink hidden" id="tmdbLink" href="#" target="_blank" rel="noopener">在 TMDB 打开 ↗</a>
          <button class="ghost sm" type="button" onclick="tmdbEdit()">改</button>
        </div>
        <div id="tmdbPick" class="hidden">
          <div class="row" style="margin-top:8px">
            <input type="text" id="tmdbQuery" style="flex:1;min-width:0" placeholder="输入番名重新搜索 TMDB"
                   onkeydown="if(event.key==='Enter')tmdbSearch()">
            <button class="ghost" type="button" onclick="tmdbSearch()" id="btnTmdb">搜索</button>
          </div>
          <div id="tmdbCand"></div>
        </div>
        <div class="row" id="seasonRow">
          <div style="flex:1">
            <label>季度</label>
            <input type="number" id="season" value="1" min="1">
          </div>
          <div style="flex:1">
            <label title="字幕组连续编号时用它纠正，例如第 2 季从第 13 集开始就填 -12">剧集偏移</label>
            <input type="number" id="epoffset" value="0" step="1"
                   title="填 -12 表示把 E13 写成 E01；不偏移填 0">
          </div>
          <div style="flex:2">
            <label>字幕组（文件名前缀）</label>
            <input type="text" id="subgroup" placeholder="ANi">
          </div>
        </div>
        <div class="hint" id="seasonHint" style="margin:-6px 0 8px">
          季度按番名自动识别（「第二季」→ 2），识别不对直接改数字即可。
          偏移只影响文件名与集数上限，不影响去重，例如 <code>-12</code> 把 <code>S02E13</code> 写成 <code>S02E01</code>。
        </div>
        <label>更新星期（首页展示用，可留空自动推断）</label>
        <select id="weeklabel">
          <option value="">自动推断</option>
          <option>星期一</option><option>星期二</option><option>星期三</option>
          <option>星期四</option><option>星期五</option><option>星期六</option><option>星期日</option>
        </select>
        <div class="row">
          <div style="flex:1">
            <label>画面/字幕过滤（一行一条，全部命中）</label>
            <textarea id="match" style="min-height:64px" placeholder="留空则使用「设置 → 下载」里的全局默认规则"></textarea>
          </div>
          <div style="flex:1">
            <label>排除（一行一条）</label>
            <textarea id="exclude" style="min-height:64px" placeholder="留空则使用「设置 → 下载」里的全局默认规则"></textarea>
          </div>
        </div>
        <div class="hint" id="ruleHint" style="margin:-6px 0 8px">这两项留空即可，会跟随「设置 → 下载」里的全局规则；只有这部番需要特殊处理时才填。</div>
        <div class="row">
          <button onclick="subscribe()" id="btnSub">订阅并下载全部剧集</button>
        </div>
        <div class="hint">订阅后会把 RSS 里现存的剧集全部下载一遍，之后每小时检查新集</div>
      </div>

      <div id="prevCard" style="display:none">
        <div class="row" style="margin-top:14px;align-items:center">
          <label style="margin:0;flex:1">预览结果</label>
          <button class="ghost sm" type="button" onclick="showSeason()">← 重新选番</button>
        </div>
        <div id="prevInfo" class="sub" style="margin:0 0 6px"></div>
        <div class="prevwrap">
          <table id="prevTable"><thead><tr><th style="width:50px">集</th><th>标题</th><th style="width:60px">判断</th></tr></thead><tbody></tbody></table>
        </div>
      </div>

      <div id="seasonWrap">
      <div class="sep"></div>

      <div class="sechead">
        <b>当季新番</b>
        <span class="sub" id="seasonLabel" style="margin:0"></span>
        <select id="seasonSel" onchange="loadSeason()" title="选择季度"></select>
        <div class="mksearch">
          <input type="text" id="mkText" placeholder="搜番名，或 id: 3981" autocomplete="off"
                 onkeydown="if(event.key==='Enter')mkSearch()">
          <button class="ghost sm" onclick="mkSearch()" id="btnMk">搜索</button>
        </div>
      </div>

      <div id="mkPanel" class="hidden">
        <div class="row" style="margin-top:0;margin-bottom:10px;align-items:center">
          <div style="flex:1;font-size:14px;font-weight:600;color:#444" id="mkTitle">搜索结果</div>
          <button class="ghost sm" onclick="mkBack()">← 返回当季</button>
        </div>
        <div id="mkBody"></div>
      </div>

      <div id="seasonBody"><div class="hint">正在加载当季新番…</div></div>
      </div>
    </div>
  </div>
</div>

<!-- ==================== ③ 下载 ==================== -->
<div class="view" id="view-dl">
  <h1>下载</h1>
  <div class="sub">仅显示本程序订阅推送的 qBittorrent 任务</div>

  <div class="stats">
    <div class="stat"><div class="k">任务数</div><div class="v" id="dl-count">0</div></div>
    <div class="stat"><div class="k">下载中</div><div class="v" id="dl-active">0</div></div>
    <div class="stat"><div class="k">已完成</div><div class="v" id="dl-done">0</div></div>
    <div class="stat"><div class="k">总速度</div><div class="v" id="dl-speed">0<small>KB/s</small></div></div>
  </div>

  <div class="card">
    <div class="row" style="margin-top:0;margin-bottom:10px">
      <button class="ghost" onclick="loadDownloads()">刷新</button>
    </div>
    <div id="dlList"></div>
  </div>
</div>

<!-- ==================== ④ 日志 ==================== -->
<div class="view" id="view-logs">
  <h1>日志</h1>
  <div class="sub">最近 200 行运行日志</div>
  <div class="card">
    <button class="ghost" onclick="loadLogs()">刷新日志</button>
    <pre id="logs">加载中…</pre>
  </div>
</div>

<!-- ==================== ⑤ 设置 ==================== -->
<div class="view" id="view-settings">
  <h1>设置</h1>
  <div class="sub">qBittorrent 连接等全局参数</div>

  <!-- 二级菜单：下载 / 抓取 / TMDB / 代理 / 登录 -->
  <div class="tabbar">
    <button class="tab on" data-tab="dl" onclick="setTab('dl')">下载</button>
    <button class="tab" data-tab="fetch" onclick="setTab('fetch')">抓取</button>
    <button class="tab" data-tab="tmdb" onclick="setTab('tmdb')">TMDB</button>
    <button class="tab" data-tab="proxy" onclick="setTab('proxy')">代理</button>
    <button class="tab" data-tab="auth" onclick="setTab('auth')">登录</button>
  </div>

  <!-- ---------- 下载（qBittorrent） ---------- -->
  <div class="tabpane" id="tab-dl">
    <div class="card">
      <h2>qBittorrent</h2>
      <label>主机（IP 或域名）</label>
      <input type="text" id="s_host" placeholder="192.168.50.17">
      <div class="row">
        <div style="flex:1">
          <label>WebUI 端口</label>
          <input type="number" id="s_port" min="1" max="65535" placeholder="8888">
        </div>
        <div style="flex:1">
          <label>分类</label>
          <input type="text" id="s_category" placeholder="anime-go">
        </div>
      </div>
      <label>API Key（留空保持不变）</label>
      <input type="text" id="s_apikey" placeholder="留空则不修改">
      <div class="hint" id="s_apikey_hint">当前：—</div>
      <div class="row" style="margin-top:14px">
        <button class="ghost" onclick="testQbit()" id="btnTest">测试连接</button>
        <button onclick="saveSettings()" id="btnSetSave">保存设置</button>
      </div>
      <div id="setResult" class="hint" style="margin-top:10px"></div>
    </div>

    <div class="card" style="margin-top:14px">
      <h2>下载目录挂载</h2>
      <div class="hint" style="margin-bottom:10px">
        删除订阅并勾选「同时删除下载数据」时，除了让 qBittorrent 删文件，还会把这个订阅的文件夹一起删掉。
        要让本程序看得见那些文件夹，需要把 qBittorrent 容器里的路径挂到本机。
        一行一条，格式 <code>qB 侧路径 = 本机挂载点</code>；匹配时用最长前缀。
      </div>
      <label>挂载映射（一行一条）</label>
      <textarea id="s_mounts" style="min-height:76px" placeholder="/media = /mnt/media&#10;/downloads = /mnt/dl"></textarea>
      <div class="hint" id="s_dl_hint">留空 = 不启用，删除订阅时会保留文件夹。</div>
      <label style="margin-top:12px">追更库根目录（qBittorrent 侧）</label>
      <input type="text" id="s_library" placeholder="/media/新番追更">
      <div class="hint" id="s_library_hint">
        同一部番的第 1/2/3 季都落在这个目录下，共用同一个番名目录。新建订阅时会来这里找同名目录并复用（手动建的目录也算）。
      </div>
      <label style="margin-top:12px">封面来源</label>
      <select id="s_coversrc">
        <option value="mikan">mikan（推荐：每一季各一张海报）</option>
        <option value="tmdb">TMDB（多季共用一张总海报）</option>
      </select>
      <div class="hint" id="s_coversrc_hint">
        mikan 是逐季建条目的，第 1 季和第 2 季的封面不一样；TMDB 把多季合成一个剧集条目，只有一张总海报，订阅页上看起来会一模一样。
      </div>
      <div class="row" style="margin-top:14px">
        <button onclick="saveSettings()" id="btnSetSave2">保存设置</button>
      </div>
    </div>

    <div class="card" style="margin-top:14px">
      <h2>剧场版</h2>
      <div class="hint" style="margin-bottom:10px">
        剧场版是<strong>一次性</strong>任务：推送成功后就不再检查更新，也没有「第几集」的概念。
        新订阅被标成剧场版时会落到下面这个目录，和追更库分开，方便你按「剧场版」整目录刮削。
      </div>
      <label>剧场版下载目录（qBittorrent 侧）</label>
      <input type="text" id="s_movie_save" placeholder="/media/剧场版/{{dirname}}">
      <div class="hint" id="s_movie_save_hint">
        可用变量与保存路径一致：<code>{{dirname}}</code> <code>{{title}}</code> <code>{{filetitle}}</code> <code>{{subgroup}}</code>。
        默认 <code>/media/剧场版/{{dirname}}</code>，一部剧场版一个文件夹。
      </div>
      <label style="margin-top:12px">剧场版重命名模板</label>
      <input type="text" id="s_movie_rename" placeholder="[{{subgroup}}] {{filetitle}}">
      <div class="hint" id="s_movie_rename_hint">
        默认不带季集（<code>[字幕组] 片名</code>）。填了 SxxExx 之类的变量在这里没有意义，但不会报错。
      </div>
      <div class="row" style="margin-top:14px">
        <button onclick="saveSettings()">保存设置</button>
      </div>
    </div>

    <div class="card" style="margin-top:14px">
      <h2>下载过滤规则（全局默认）</h2>
      <div class="hint" style="margin-bottom:10px">
        这里配一次，所有订阅都会用；新番换季、换压制规格时只改这里，不用每部番单独改。
        某部番需要特殊处理时，在它的编辑页里单独填，那边的设置优先。
      </div>
      <div class="row">
        <div style="flex:1">
          <label>识别规则（一行一条，全部命中才下载）</label>
          <textarea id="s_match" style="min-height:84px" placeholder="(1080|2160)[Pp]?"></textarea>
        </div>
        <div style="flex:1">
          <label>排除规则（一行一条，命中任意一条就跳过）</label>
          <textarea id="s_exclude" style="min-height:84px" placeholder="720[Pp]&#10;合集"></textarea>
        </div>
      </div>
      <div class="hint">
        支持正则表达式；前缀 <code>{{字幕组}}:</code> 表示只在字幕组匹配时生效，<code>{{!字幕组}}</code> 表示取反。
      </div>
      <div class="row" style="margin-top:14px">
        <button onclick="saveSettings()" id="btnSetSave3">保存设置</button>
      </div>
    </div>
  </div>

  <!-- ---------- 抓取 ---------- -->
  <div class="tabpane" id="tab-fetch" style="display:none">
    <div class="card">
      <h2>抓取</h2>
      <label>检查间隔（分钟）</label>
      <input type="number" id="s_interval" min="1" max="1440" placeholder="60">
      <div class="hint">每小时检查一次新集；改完重启服务完全生效</div>
      <label>允许写入的路径前缀（一行一条）</label>
      <textarea id="s_prefixes" style="min-height:60px" placeholder="/media/新番追更/"></textarea>
      <div class="hint">安全阀：订阅的保存路径必须落在这里面，否则拒绝推送。追更目录默认已放行。</div>
      <label style="margin-top:12px">bangumi 连接</label>
      <select id="s_mikanhost" onchange="mkHostChanged()"></select>
      <input type="text" id="s_mikanhost_custom" style="margin-top:8px;display:none" placeholder="https://mikanime.tv">
      <div class="hint" id="s_mikanhost_hint">
        抓订阅、搜番、下封面都走这个站点。默认 <code>https://mikanime.tv</code>；
        镜像站的页面结构和番剧 id 与主站一致，可以随时切换，切完建议点一次「立即检查」。
        选「自定义」时填完整地址（含 https://）。
      </div>
      <div class="row" style="margin-top:14px">
        <button onclick="saveSettings()">保存设置</button>
      </div>
      <div id="fetchResult" class="hint" style="margin-top:10px"></div>
    </div>

    <div class="card">
      <h2>未来功能</h2>
      <div id="plannedList" style="font-size:12.5px;color:#aaa;line-height:1.9"></div>
    </div>
  </div>

  <!-- ---------- TMDB ---------- -->
  <div class="tabpane" id="tab-tmdb" style="display:none">
    <div class="card">
      <h2>TMDB 封面</h2>
      <label>API Key（v3）</label>
      <input type="text" id="s_tmdbkey" placeholder="留空则不修改；填 - 清空" style="font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12.5px">
      <div class="hint" id="s_tmdbkey_hint">当前：—</div>
      <label style="margin-top:12px">API 域名</label>
      <input type="text" id="s_tmdbhost" placeholder="https://api.tmdb.org">
      <div class="hint">国内网络 <code>api.themoviedb.org</code> 常被污染，默认走 <code>api.tmdb.org</code>；
        图片走 <code>image.tmdb.org</code>，一般正常。</div>
      <div class="hint" id="s_cvcount">已缓存封面：—</div>
      <div class="row" style="margin-top:14px">
        <button class="ghost" onclick="testTMDB()" id="btnTestTmdb">测试 TMDB</button>
        <button class="ghost" onclick="refreshAllCovers()">补抓全部封面</button>
        <button onclick="saveSettings()">保存设置</button>
      </div>
      <div id="tmdbResult" class="hint" style="margin-top:10px"></div>
    </div>
  </div>

  <!-- ---------- 代理 ---------- -->
  <div class="tabpane" id="tab-proxy" style="display:none">
    <div class="card">
      <h2>出口代理</h2>
      <div class="hint" style="margin-bottom:10px">
        打开后，抓 mikan RSS/番剧页、查 TMDB、下封面这些<strong>公网请求</strong>走这个代理；
        局域网地址（如 qBittorrent 的 192.168.x.x）和 127.0.0.1 始终直连，不会绕代理。
        本机 WebUI 自己的监听端口不受影响。<strong>保存后立即生效</strong>，不用重启。
      </div>
      <label class="ckline" style="display:flex;align-items:center;gap:8px;margin-bottom:12px">
        <input type="checkbox" id="s_proxy_on" style="width:auto;margin:0">
        <span>启用代理</span>
      </label>
      <div class="row">
        <div style="flex:1">
          <label>协议</label>
          <select id="s_proxy_proto">
            <option value="http">HTTP / HTTPS（CONNECT）</option>
            <option value="socks5">SOCKS5</option>
          </select>
        </div>
        <div style="flex:1">
          <label>端口</label>
          <input type="number" id="s_proxy_port" min="1" max="65535" placeholder="20170">
        </div>
      </div>
      <label style="margin-top:12px">主机（IP 或域名）</label>
      <input type="text" id="s_proxy_host" placeholder="192.168.50.17">
      <div class="row" style="margin-top:12px">
        <div style="flex:1">
          <label>用户名（可选）</label>
          <input type="text" id="s_proxy_user" autocomplete="off" placeholder="留空 = 代理不需要认证">
        </div>
        <div style="flex:1">
          <label>密码（可选）</label>
          <input type="password" id="s_proxy_pass" autocomplete="new-password" placeholder="留空保持当前值；填 - 清空">
        </div>
      </div>
      <div class="hint" id="s_proxy_hint">当前：未启用</div>
      <div class="row" style="margin-top:14px">
        <button class="ghost" onclick="testProxy()" id="btnTestProxy">测试代理</button>
        <button onclick="saveSettings()">保存设置</button>
      </div>
      <div id="proxyResult" class="hint" style="margin-top:10px"></div>
    </div>
  </div>

  <!-- ---------- 登录（账号 / 密码 / API Key） ---------- -->
  <div class="tabpane" id="tab-auth" style="display:none">
    <div class="card">
      <h2>当前账号</h2>
      <div class="accrow">
        <div class="accava" id="acc_ava">A</div>
        <div class="accinfo">
          <div class="accname" id="acc_user">—</div>
          <div class="acctip" id="acc_mode">—</div>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>修改密码</h2>
      <label>当前密码</label>
      <input type="password" id="pw_old" placeholder="输入现在的密码" autocomplete="current-password">
      <div class="row">
        <div style="flex:1">
          <label>新密码</label>
          <input type="password" id="pw_new" placeholder="至少 6 位" autocomplete="new-password">
        </div>
        <div style="flex:1">
          <label>确认新密码</label>
          <input type="password" id="pw_new2" placeholder="再输一次" autocomplete="new-password">
        </div>
      </div>
      <div class="hint">改密码后其它设备上的登录会立即失效，需要重新登录。</div>
      <div class="row" style="margin-top:14px">
        <button onclick="changePassword()" id="btnPwSave">修改密码</button>
      </div>
      <div id="pwResult" class="hint" style="margin-top:10px"></div>
    </div>

    <div class="card">
      <h2>登录有效期</h2>
      <label>多长时间后登录失效（分钟）</label>
      <input type="number" id="s_sessttl" placeholder="120">
      <div class="hint" id="s_sessttl_hint">默认 120 分钟（2 小时）。填 <b>0</b> = 单次有效（关闭浏览器即失效）；填 <b>-1</b> = 永久有效，登录一次一直不用再登。</div>
      <div class="row" style="margin-top:12px">
        <button class="ghost" onclick="setTtlQuick(0)">单次有效</button>
        <button class="ghost" onclick="setTtlQuick(120)">2 小时</button>
        <button class="ghost" onclick="setTtlQuick(720)">12 小时</button>
        <button class="ghost" onclick="setTtlQuick(10080)">7 天</button>
        <button class="ghost" onclick="setTtlQuick(-1)">永不失效</button>
      </div>
      <div class="row" style="margin-top:14px">
        <button onclick="saveSessionTTL()" id="btnTtlSave">保存有效期</button>
      </div>
      <div class="hint" style="line-height:1.7;margin-top:8px">
        改长或改永久时其它设备继续在线；<b>改短会让其它设备重新登录</b>（本机会自动续上）。
      </div>
      <div id="ttlResult" class="hint" style="margin-top:8px"></div>
    </div>

    <div class="card">
      <h2>API Key</h2>
      <label>供 AI / 脚本远程调用</label>
      <input type="text" id="acc_apikey" readonly onclick="this.select()" style="font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12.5px">
      <div class="hint" style="line-height:1.7">
        用法：<code>curl -H "X-API-Key: &lt;key&gt;" http://&lt;本机IP&gt;:7788/api/subs</code><br>
        也支持 <code>Authorization: Bearer &lt;key&gt;</code>。凭此 Key 等同已登录，拥有全部权限。<br>
        <b>API Key 不受上面的登录有效期限制，永不过期</b>；只有点「重置 API Key」或删除
        <code>auth.json</code> 才会失效。
      </div>
      <div class="row" style="margin-top:12px">
        <button class="ghost" onclick="rotateKey()" id="btnRotate">重置 API Key</button>
        <button class="ghost" onclick="doLogout()">退出登录</button>
      </div>
      <div id="accResult" class="hint" style="margin-top:10px"></div>
    </div>
  </div>
</div>

</main>

<!-- ==================== 编辑弹窗 ==================== -->
<div id="editModal" class="modal hidden">
  <div class="box">
    <h3 id="editTitle">编辑订阅</h3>
    <!-- 封面区：点「换封面」搜 TMDB，点「重新抓取」按番名自动匹配 -->
    <div class="ecv">
      <div class="ecv-p" id="e_cvp">
        <img id="e_cvimg" alt="" style="display:none">
        <div class="noph" id="e_cvnoph"><b>🎬</b><span>暂无封面</span></div>
      </div>
      <div class="ecv-i">
        <div class="ecv-n" id="e_cvname">还没有封面</div>
        <div class="ecv-y" id="e_cvyear"></div>
        <a class="tlink hidden" id="e_tmdblink" href="#" target="_blank" rel="noopener">在 TMDB 打开 ↗</a>
        <div class="row" style="margin:8px 0 0">
          <button class="ghost sm" onclick="pickCover(editingId)">换封面</button>
          <button class="ghost sm" onclick="refetchCover(editingId)" id="btnRefetch">重新抓取</button>
        </div>
      </div>
    </div>
    <label>番名（目录名）</label>
    <input type="text" id="e_title">
    <label>文件名里的番名（留空自动）</label>
    <input type="text" id="e_filetitle">
    <label>RSS 链接</label>
    <input type="text" id="e_url">
    <div class="row">
      <div style="flex:1" id="e_season_wrap">
        <label>季度</label>
        <input type="number" id="e_season" min="1">
      </div>
      <div style="flex:1" id="e_epoff_wrap">
        <label title="字幕组连续编号时用它纠正">剧集偏移</label>
        <input type="number" id="e_epoffset" step="1" title="填 -12 把 E13 写成 E01">
      </div>
      <div style="flex:2">
        <label>字幕组</label>
        <input type="text" id="e_subgroup">
      </div>
    </div>
    <label>番名目录（留空自动）</label>
    <input type="text" id="e_dirname" placeholder="留空 = 自动复用同名的番名目录">
    <div class="hint">同一部番的第 1/2/3 季会共用这个目录，各自建 Season N 子目录。留空时程序会去追更库里找同名目录并复用。</div>
    <label>更新星期</label>
    <select id="e_weeklabel">
      <option value="">自动推断</option>
      <option>星期一</option><option>星期二</option><option>星期三</option>
      <option>星期四</option><option>星期五</option><option>星期六</option><option>星期日</option>
    </select>
    <label>过滤规则（一行一条，全部命中）</label>
    <textarea id="e_match" placeholder="留空 = 使用「设置 → 下载」里的全局默认规则"></textarea>
    <label>排除规则（一行一条）</label>
    <textarea id="e_exclude" style="min-height:44px" placeholder="留空 = 使用全局默认规则"></textarea>
    <div class="row">
      <div style="flex:1" id="e_total_wrap">
        <label>总集数（0=不限）</label>
        <input type="number" id="e_total" min="0">
      </div>
    </div>
    <label>保存路径模板</label>
    <input type="text" id="e_savepath">
    <div class="hint">留空 = 用默认 <code>/media/新番追更/{{dirname}}/Season {{season_n}}</code>（落在 NAS 的 media/新番追更）。<code>{{dirname}}</code> 是去掉「第二季」这类后缀的番名，多季才会进同一个目录。</div>
    <label>重命名模板</label>
    <input type="text" id="e_rename">
    <div class="chk">
      <input type="checkbox" id="e_movie" onchange="applyMovieEdit()">
      <label for="e_movie" style="margin:0">剧场版（一次性下载，不更新、不识别集数）</label>
    </div>
    <div class="chk">
      <input type="checkbox" id="e_enabled">
      <label for="e_enabled" style="margin:0">启用此订阅</label>
    </div>
    <div class="chk">
      <input type="checkbox" id="e_skipbackfill">
      <label for="e_skipbackfill" style="margin:0">不下载历史剧集（只等新集）</label>
    </div>
    <div class="chk">
      <input type="checkbox" id="e_reset">
      <label for="e_reset" style="margin:0">同时重置抓取进度（下次重新建立基线）</label>
    </div>
    <div class="row" style="margin-top:16px">
      <button class="ghost" onclick="closeEdit()">取消</button>
      <button onclick="saveEdit()" id="btnSave">保存</button>
    </div>
  </div>
</div>

<!-- ==================== 封面选择弹窗 ==================== -->
<div id="cvModal" class="modal hidden">
  <div class="box" style="max-width:620px">
    <h3>选择封面</h3>
    <div class="hint" style="margin-bottom:10px">从 TMDB 搜索番剧，点海报即应用为该订阅的封面。</div>
    <div class="row" style="margin-top:0">
      <input type="text" id="cvQuery" style="flex:1" placeholder="输入番名，如：胆大党"
             onkeydown="if(event.key==='Enter'){event.preventDefault();cvSearch()}">
      <button onclick="cvSearch()" id="btnCvSearch">搜索</button>
    </div>
    <div id="cvMsg" class="hint" style="min-height:18px;margin:8px 0"></div>
    <div id="cvGrid" class="cvpick"></div>
    <div class="row" style="margin-top:14px">
      <button class="ghost" onclick="$('cvModal').classList.add('hidden')">关闭</button>
    </div>
  </div>
</div>

<!-- ==================== 删除弹窗 ==================== -->
<div id="delModal" class="modal hidden">
  <div class="box" style="max-width:420px">
    <h3>删除订阅</h3>
    <div id="delTitle" style="font-weight:600;font-size:15px;margin-bottom:14px"></div>

    <label class="delcheck" for="del_extra">
      <input type="checkbox" id="del_extra" onchange="onDelExtra()">
      <span>同时删除 qBittorrent 下载数据及文件</span>
    </label>
    <div class="delhint" id="delHint">不勾选：只移除这条订阅，种子与已下载文件都保留。</div>

    <div class="row" style="margin-top:20px">
      <button class="ghost" onclick="closeDel()">否</button>
      <button class="danger" onclick="doDelete()" id="btnDel">是</button>
    </div>
  </div>
</div>

<!-- ==================== 本地文件列表弹窗 ==================== -->
<div id="filesModal" class="modal hidden">
  <div class="box" style="max-width:720px">
    <h3 id="filesTitle">本地文件</h3>
    <div class="flhead">
      <div class="flpath" id="filesPath"></div>
      <div class="flsum" id="filesSum"></div>
    </div>
    <div id="filesBody"></div>
    <div class="row" style="margin-top:14px">
      <button class="ghost" onclick="closeFiles()">关闭</button>
      <button class="ghost" onclick="reloadFiles()">刷新</button>
    </div>
  </div>
</div>

<!-- ==================== 订阅体检弹窗 ==================== -->
<div id="auditModal" class="modal hidden">
  <div class="box" style="max-width:880px">
    <h3>订阅体检</h3>
    <div class="ahint" id="auditHint">正在逐条抓取 RSS 并核对 qBittorrent 任务，稍等…</div>
    <div id="auditBody"></div>
    <div class="row" style="margin-top:14px">
      <button class="ghost" onclick="closeAudit()">关闭</button>
      <button class="ghost" onclick="runAudit()">重新体检</button>
    </div>
  </div>
</div>

<!-- ==================== 应用内确认 / 输入框（替代 confirm / prompt） ==================== -->
<div id="askModal" class="modal hidden">
  <div class="box">
    <h3 id="askTitle">确认</h3>
    <div id="askMsg"></div>
    <div id="askInputWrap" class="hidden">
      <input id="askInput" autocomplete="off">
      <div class="hint" id="askInputHint"></div>
    </div>
    <div class="row">
      <button class="ghost" id="askNo" onclick="askResolve(false)">否</button>
      <button class="danger" id="askYes" onclick="askResolve(true)">是</button>
    </div>
  </div>
</div>

<!-- ==================== 软提示容器 ==================== -->
<div id="toastWrap" aria-live="polite"></div>

<script>
const $ = id => document.getElementById(id);
let editingId = null;
let deletingId = null;
let deletingTitle = '';
let currentView = 'home';
let dlTimer = null;

function esc(s){ return String(s==null?'':s).replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c])); }

// 把后端返回的 RFC3339 时间变成「刚刚 / 3 分钟前 / 今天 19:04 / 10-25 09:12」。
// 后端零值（0001-01-01）表示还没跑过，显示占位而不是一个荒唐的年份。
function timeAgo(iso){
  if(!iso) return '';
  const t = new Date(iso);
  if(isNaN(t.getTime()) || t.getFullYear() < 2000) return '';
  const now = new Date();
  const diff = Math.floor((now - t) / 1000);
  if(diff < 0) return '刚刚';
  if(diff < 60) return '刚刚';
  if(diff < 3600) return Math.floor(diff/60)+' 分钟前';
  const pad = n => (n<10?'0':'')+n;
  const hm = pad(t.getHours())+':'+pad(t.getMinutes());
  if(t.toDateString() === now.toDateString()) return '今天 '+hm;
  return pad(t.getMonth()+1)+'-'+pad(t.getDate())+' '+hm;
}

// ============ 请求封装 ============
// 认证靠 HttpOnly 会话 Cookie（登录后由浏览器自动携带），
// 这里只需补 CSRF 自定义头：跨站表单无法伪造它。
function apiPost(path, body){
  return fetch(path, {
    method:'POST',
    headers:{'Content-Type':'application/json', 'X-Requested-With':'anime-go'},
    body: body===undefined ? undefined : JSON.stringify(body)
  });
}

// 把服务端的 401/403 翻成人话，而不是干巴巴地弹一个数字。
function explainAuth(d, fallback){
  if(d && d.error) return d.error;
  return fallback || '操作被拒绝';
}

// ============ 应用内软提示 ============
// 替代浏览器原生 alert：不打断操作、不需要点确认，显示 2 秒后淡出消失。
// type: 'ok' | 'err' | 'warn' | 'info'（默认 info）。点击可提前关闭。
const TOAST_ICON = {ok:'✓', err:'✕', warn:'!', info:'i'};
function toast(msg, type){
  const t = (type === 'ok' || type === 'err' || type === 'warn') ? type : 'info';
  let wrap = document.getElementById('toastWrap');
  if(!wrap){
    wrap = document.createElement('div');
    wrap.id = 'toastWrap';
    document.body.appendChild(wrap);
  }
  const el = document.createElement('div');
  el.className = 'toast ' + t;
  el.innerHTML = '<span class="ti">'+(TOAST_ICON[t]||'i')+'</span><span class="tm">'+esc(msg)+'</span>';
  wrap.appendChild(el);
  requestAnimationFrame(()=>el.classList.add('in'));
  let done = false;
  const close = () => {
    if(done) return;
    done = true;
    el.classList.remove('in'); el.classList.add('out');
    setTimeout(()=>{ try{ el.remove(); }catch(e){} }, 220);
  };
  el.addEventListener('click', close);
  setTimeout(close, 2000);                     // 2 秒后开始淡出
  return el;
}


// ============ 应用内确认框（替代 confirm） ============
// 返回 Promise<boolean>；调用方写 if(!await askConfirm(...)) return;
let askResolveFn = null;
function askConfirm(msg, opt){
  const o = opt || {};
  return new Promise(resolve=>{
    askResolveFn = resolve;
    $('askTitle').textContent = o.title || '请确认';
    $('askMsg').textContent = msg;
    $('askYes').textContent = o.yes || '确定';
    $('askYes').className = o.danger === false ? '' : 'danger';
    $('askNo').textContent = o.no || '取消';
    $('askInputWrap').classList.add('hidden');
    $('askModal').classList.remove('hidden');
  });
}
// 应用内输入框（替代 prompt）：返回 Promise<string|null>
function askPrompt(msg, opt){
  const o = opt || {};
  return new Promise(resolve=>{
    askResolveFn = resolve;
    $('askTitle').textContent = o.title || '请输入';
    $('askMsg').textContent = msg;
    $('askYes').textContent = o.yes || '确定';
    $('askYes').className = '';
    $('askNo').textContent = '取消';
    $('askInputWrap').classList.remove('hidden');
    $('askInput').value = (o.value==null ? '' : String(o.value));
    $('askInputHint').textContent = o.hint || '';
    $('askModal').classList.remove('hidden');
    setTimeout(()=>{ try{ $('askInput').focus(); $('askInput').select(); }catch(e){} }, 30);
  });
}
function askResolve(ok){
  const fn = askResolveFn; askResolveFn = null;
  const isPrompt = !$('askInputWrap').classList.contains('hidden');
  const val = ok ? (isPrompt ? $('askInput').value.trim() : true) : (isPrompt ? null : false);
  $('askModal').classList.add('hidden');
  if(fn) fn(val);
}

// ============ 视图切换 ============
function go(name){
  currentView = name;
  ['home','subs','dl','logs','settings'].forEach(v=>{
    $('view-'+v).classList.toggle('active', v===name);
    $('nav-'+v).classList.toggle('active', v===name);
  });
  // 下载视图自动刷新
  if(dlTimer){ clearInterval(dlTimer); dlTimer = null; }
  if(name==='home'){ loadHome(); }
  if(name==='subs'){ loadSubs(); }
  if(name==='dl'){ loadDownloads(); dlTimer = setInterval(loadDownloads, 5000); }
  if(name==='logs'){ loadLogs(); }
  if(name==='settings'){ restoreTab(); loadSettings(); }
  // 记住位置
  try{ localStorage.setItem('ag_view', name); }catch(e){}
}

function fmtSize(n){
  if(!n||n<0) return '0';
  if(n >= 1073741824) return (n/1073741824).toFixed(2)+' GB';
  if(n >= 1048576) return (n/1048576).toFixed(1)+' MB';
  if(n >= 1024) return (n/1024).toFixed(0)+' KB';
  return n+' B';
}
function fmtSpeed(n){
  if(!n||n<=0) return '0 KB/s';
  if(n >= 1048576) return (n/1048576).toFixed(1)+' MB/s';
  return (n/1024).toFixed(0)+' KB/s';
}
function fmtETA(s){
  if(!s||s<0) return '';
  if(s > 3600) return Math.floor(s/3600)+'小时'+Math.floor(s%3600/60)+'分';
  if(s > 60) return Math.floor(s/60)+'分'+Math.floor(s%60)+'秒';
  return Math.floor(s)+'秒';
}

// ============ 订阅本地文件列表 ============
let filesSubId = null;

async function showFiles(id){
  filesSubId = id;
  $('filesTitle').textContent = '本地文件';
  $('filesPath').textContent = '';
  $('filesSum').textContent = '';
  $('filesBody').innerHTML = '<div class="empty">读取中…</div>';
  $('filesModal').classList.remove('hidden');
  await loadFiles();
}

function closeFiles(){
  $('filesModal').classList.add('hidden');
  filesSubId = null;
}

async function reloadFiles(){
  if(!filesSubId) return;
  await loadFiles();
}

// qBittorrent 状态码 → 中文（同时兼容 4.x 的 paused* 与 5.x 的 stopped*）
function stateText(s){
  const m = {
    downloading:'下载中', stalledDL:'等待做种', metaDL:'获取元数据',
    forcedDL:'下载中', uploading:'做种中', stalledUP:'做种中',
    forcedUP:'做种中', pausedDL:'已暂停', pausedUP:'已完成',
    stoppedDL:'已暂停', stoppedUP:'已完成',
    queuedDL:'排队中', queuedUP:'排队中', checkingDL:'校验中',
    checkingUP:'校验中', checkingResumeData:'校验中', allocating:'分配空间',
    moving:'移动中', error:'出错', missingFiles:'文件丢失', unknown:'未知'
  };
  return m[s] || s || '';
}

async function loadFiles(){
  if(!filesSubId) return;
  const body = $('filesBody');
  try{
    const d = await (await fetch('/api/sub/files?id='+encodeURIComponent(filesSubId))).json();
    if(d.title) $('filesTitle').textContent = d.title + ' · 本地文件';

    if(!d.ok){
      $('filesPath').textContent = '';
      $('filesSum').textContent = '';
      body.innerHTML = '<div class="empty">'+esc(d.error||'读取失败')+'</div>';
      return;
    }
    $('filesPath').textContent = (d.save_paths&&d.save_paths.length) ? d.save_paths.join('  |  ') : '—';

    const files = d.files||[];
    if(!d.exists){
      $('filesSum').textContent = '';
      body.innerHTML = '<div class="empty">'+esc(d.error||'qBittorrent 里还没有任务')+'</div>';
      return;
    }
    if(!files.length){
      $('filesSum').textContent = '';
      body.innerHTML = '<div class="empty">这个任务里还没有文件</div>';
      return;
    }

    $('filesSum').textContent = files.length+' 个文件'+
      (d.truncated ? '（已截断）' : '')+' · 共 '+fmtSize(d.total_size)+
      ' · '+(d.torrent_count||0)+' 个任务';

    // 按种子分组：每个种子一个标题行（带状态与进度），下面挂它的文件
    const order = [];
    const byTor = {};
    (d.torrents||[]).forEach(t=>{ byTor[t.name]=t; order.push(t.name); });
    files.forEach(f=>{ if(!byTor[f.torrent]){ byTor[f.torrent]=null; order.push(f.torrent); } });

    let html = '';
    order.forEach(name=>{
      const t = byTor[name];
      if(!t) return;
      const done = (t.progress||0) >= 1;
      const pct = Math.round((t.progress||0)*1000)/10;
      html += '<div class="tg">'
        + '<span class="tgname">'+esc(t.name)+'</span>'
        + '<span class="tgst'+(done?' ok':'')+'">'+esc(stateText(t.state))+'</span>'
        + '<span class="tgpct">'+(done?'已完成':pct+'%')+'</span>'
        + '</div>';
      html += '<div class="flist"><table>' + files.filter(f=>f.torrent===name).map(f=>{
        const full = (f.progress||0) >= 1;
        return '<tr>'
          + '<td class="fn"><span class="ic">'+(full?'🎬':'⏳')+'</span>'+esc(f.name)+'</td>'
          + '<td class="fs">'+fmtSize(f.size)+'</td>'
          + '<td class="ft">'+(full?'':Math.round((f.progress||0)*1000)/10+'%')+'</td>'
          + '</tr>';
      }).join('') + '</table></div>';
    });
    body.innerHTML = html;
  }catch(e){
    body.innerHTML = '<div class="empty">出错：'+esc(String(e))+'</div>';
  }
}


// ============ ① 首页 ============
async function loadHome(){
  try{
    const d = await (await fetch('/api/home')).json();
    if(!d.ok) return;
    $('homeDate').textContent = '今天是 '+d.today+'　'+d.date;

    // 统计
    let pushed = 0;
    (d.all_subs||[]).forEach(s=>pushed += (s.pushed||0));
    $('st-total').textContent = d.total||0;
    $('st-today').textContent = (d.today_subs||[]).length;
    $('st-pushed').textContent = pushed;

    // 星期条 + 当日列表（可点选）
    homeData = d;
    renderWeekbar(d);
    renderDayList(d);

    // 侧栏
    $('badge-subs').textContent = d.total||0;
    $('sf-status').textContent = '共 '+(d.total||0)+' 条订阅';

    // 顺带取下载数
    loadDlBadge();
  }catch(e){ console.error(e); }
}

// ============ 星期条：可点选，查看每天有哪些订阅 ============
const WEEK_ORDER = ['星期一','星期二','星期三','星期四','星期五','星期六','星期日'];
let homeData = null;    // 缓存 /api/home 结果，切换星期时不再跑网络
let weekPick = '';      // 当前选中的星期；空串 = 跟随今天

// 星期三 -> 周三，保证每个都是两个字，宽度一致
function weekShort(w){ return '周' + w.slice(2); }

// 把 all_subs 按 week_label 分组；没有星期标签的归到「未定」。
// 剧场版单独一档：它没有星期，混进「未定」会让人以为「还没设星期」。
function weekGroups(all){
  const g = {};
  WEEK_ORDER.forEach(w=>{ g[w] = []; });
  g['剧场版'] = [];
  g['未定'] = [];
  (all||[]).forEach(s=>{
    if(s.movie){ g['剧场版'].push(s); return; }
    const k = WEEK_ORDER.indexOf(s.week_label) >= 0 ? s.week_label : '未定';
    g[k].push(s);
  });
  return g;
}

function renderWeekbar(d){
  const g = weekGroups(d.all_subs);
  const picked = weekPick || d.today;
  let html = WEEK_ORDER.map(w=>{
    const n = g[w].length;
    let cls = 'd';
    if(w === d.today) cls += ' today';
    if(w === picked) cls += ' on';
    if(!n) cls += ' zero';
    return '<div class="'+cls+'" onclick="pickWeek(\''+w+'\')" title="'+w+'：'+n+' 个订阅">'
      + '<span class="wl">'+weekShort(w)+'</span><b>'+n+'</b></div>';
  }).join('');
  if(g['剧场版'].length){
    const on = (picked === '剧场版') ? ' on' : '';
    html += '<div class="d mv'+on+'" onclick="pickWeek(\'剧场版\')" title="剧场版：一次性下载，不按星期更新">'
      + '<span class="wl">剧场</span><b>'+g['剧场版'].length+'</b></div>';
  }
  if(g['未定'].length){
    const on = (picked === '未定') ? ' on' : '';
    html += '<div class="d und'+on+'" onclick="pickWeek(\'未定\')" title="还没设置更新星期的订阅">'
      + '<span class="wl">未定</span><b>'+g['未定'].length+'</b></div>';
  }
  $('weekbar').innerHTML = html;
}

function renderDayList(d){
  const g = weekGroups(d.all_subs);
  const picked = weekPick || d.today;
  const list = g[picked] || [];
  const isToday = (picked === d.today);

  // 标题
  const title = picked === '未定' ? '未定星期'
              : picked === '剧场版' ? '剧场版（一次性下载）'
              : (isToday ? '今日订阅' : weekShort(picked)+'的订阅');
  $('dayTitle').textContent = title;
  $('dayCount').textContent = list.length ? (list.length+' 个') : '';
  $('dayBack').style.display = isToday ? 'none' : '';

  if(!list.length){
    const tip = picked === '未定'
      ? '所有订阅都已经设好更新星期了'
      : picked === '剧场版'
        ? '还没有剧场版订阅<br><span style="font-size:12px">在 mikan 的「剧场版」分类里选一部，勾上「这是剧场版」即可</span>'
        : (isToday ? '今天没有更新的订阅<br><span style="font-size:12px">在「订阅」里给番设置更新星期，就会显示在这里</span>'
                   : weekShort(picked)+'没有需要更新的订阅');
    $('todayList').innerHTML = '<div class="empty">'+tip+'</div>';
    return;
  }

  $('todayList').innerHTML = '<div class="subgrid">' + list.map(s=>{
    const badge = !s.enabled ? '<span class="pill off">停用</span>'
                : s.last_error ? '<span class="pill err" title="'+esc(s.last_error)+'">错误</span>'
                : '<span class="pill on">启用</span>';
    // 剧场版没有集数进度：显示「已下载 / 等待资源」
    const meta = s.movie
      ? '<span>'+(s.movie_done ? '✓ 已下载，不再检查' : '等待资源')+'</span>'
      : '<span>最高 <b>E'+s.max_episode+'</b></span><span>已推 <b>'+s.pushed+'</b></span>';
    return '<div class="subcard' + (s.enabled?'':' dim') + '">'
      + '<div class="t">'+esc(s.title)+'</div>'
      + '<div class="m">'
      +   (s.subgroup? '<span>'+esc(s.subgroup)+'</span>':'')
      +   meta
      +   badge
      + '</div>'
      + (timeAgo(s.last_run)? '<div class="m"><span>上次检查 '+esc(timeAgo(s.last_run))+'</span></div>':'')
      + '</div>';
  }).join('') + '</div>';
}

function pickWeek(w){
  weekPick = (w === '') ? '' : w;
  if(!homeData) return;
  renderWeekbar(homeData);
  renderDayList(homeData);
}

async function loadDlBadge(){
  try{
    const d = await (await fetch('/api/downloads')).json();
    if(!d.ok) return;
    const n = (d.summary&&d.summary.downloading)||0;
    $('st-dl').textContent = n;
    const b = $('badge-dl');
    if(n>0){ b.textContent = n; b.classList.remove('hidden'); }
    else b.classList.add('hidden');
  }catch(e){}
}

// ============ ② 订阅 ============
// —— mikan 搜索（照 ani-rss 的做法：搜番剧 → 选字幕组 → 自动填 RSS）——
let mkStore = [];

// ---- 新增订阅弹窗 ----
let seasonInited = false;

function openAdd(){
  $('addModal').classList.remove('hidden');
  if(!seasonInited){
    seasonInited = true;
    loadSeason(true);
  }
  setTimeout(()=>{ try{ $('rssurl').focus(); }catch(e){} }, 80);
}

// 关闭弹窗时把表单清干净。
// 以前只隐藏不清空，下次打开还显示上一部番的名字/预览/字幕组，
// 容易让人以为「这次选好了」，所以关掉就复位。
function closeAdd(){
  $('addModal').classList.add('hidden');
  resetAddForm();
}

// 订阅名来源：mikan 名 / TMDB 名（默认 mikan）
let addMikanName = '';   // mikan 侧番名
let addTmdbName  = '';   // TMDB 侧番名
let addTmdbId    = 0;
let addTmdbMatch = null;

// autoSeason 记住「上一次自动填进去的季度值」。
// 用途：拉预览会重算季度，但不能把用户手改过的数字冲掉——
// 只有当前值仍等于上次自动填的那个（说明用户没动过）才覆盖。
let autoSeason = '1';

function curNameSrc(){
  const el = document.querySelector('input[name="namesrc"]:checked');
  return el ? el.value : 'mikan';
}

// 切换来源时把 #title 换成对应名字。
// 只有当前内容是「自动填的」才覆盖，用户手改过的名字保留。
function applyNameSrc(){
  const src = curNameSrc();
  const name = src === 'tmdb' ? addTmdbName : addMikanName;
  const cur = $('title').value.trim();
  const auto = !cur || cur === addMikanName || cur === addTmdbName;
  if(name && auto) $('title').value = name;
  document.querySelectorAll('.namesrc .rd').forEach(rd=>{
    const inp = rd.querySelector('input');
    rd.classList.toggle('sel', !!inp && inp.checked);
  });
}

function setNames(mikan, tmdb){
  addMikanName = (mikan||'').trim();
  addTmdbName  = (tmdb||'').trim();
  $('nmMikan').textContent = addMikanName ? '：'+addMikanName : '';
  $('nmTmdb').textContent  = addTmdbName  ? '：'+addTmdbName  : '（未识别）';
  $('nmTmdb').className = 'nm' + (addTmdbName ? '' : ' err');
  // 没识别出 TMDB 名时不允许选「用 TMDB 名」，并自动退回 mikan
  const tmdbRd = document.querySelector('.namesrc input[value="tmdb"]');
  if(tmdbRd){
    tmdbRd.disabled = !addTmdbName;
    if(!addTmdbName && tmdbRd.checked){
      const mk = document.querySelector('.namesrc input[value="mikan"]');
      if(mk) mk.checked = true;
    }
  }
  applyNameSrc();
}

// 渲染 TMDB 条目条
function renderTmdb(m){
  addTmdbMatch = m || null;
  addTmdbId = (m && m.id) ? m.id : 0;
  const bar = $('tmdbBar'), link = $('tmdbLink');
  if(!m){
    $('tmdbName').textContent = '未识别';
    $('tmdbSub').textContent  = '可以点「改」手动搜索 TMDB';
    link.classList.add('hidden');
    bar.classList.add('miss');
  }else{
    $('tmdbName').textContent = m.name || ('#'+m.id);
    const bits = [];
    if(m.original_name && m.original_name !== m.name) bits.push(m.original_name);
    if(m.year) bits.push(m.year);
    if(m.id) bits.push('#'+m.id);
    $('tmdbSub').textContent = bits.join('　');
    if(m.url){
      link.href = m.url;
      link.textContent = '在 TMDB 打开 ↗';
      link.classList.remove('hidden');
    }else link.classList.add('hidden');
    bar.classList.remove('miss');
  }
}

// 点「改」：展开手动搜索
function tmdbEdit(){
  const box = $('tmdbPick');
  box.classList.toggle('hidden');
  if(!box.classList.contains('hidden')){
    if(!$('tmdbQuery').value) $('tmdbQuery').value = addMikanName || $('title').value || '';
    if(!$('tmdbCand').children.length) tmdbSearch();
    setTimeout(()=>{ try{ $('tmdbQuery').focus(); }catch(e){} }, 60);
  }
}

async function tmdbSearch(){
  const q = $('tmdbQuery').value.trim();
  if(!q){ $('tmdbQuery').focus(); return; }
  const b = $('btnTmdb');
  b.disabled = true; b.textContent = '搜索中…';
  $('tmdbCand').innerHTML = '<div class="hint">正在搜索 TMDB…</div>';
  try{
    const d = await (await fetch('/api/tmdb/search?q='+encodeURIComponent(q))).json();
    if(!d.ok){ $('tmdbCand').innerHTML = '<div class="err">'+esc(d.error)+'</div>'; return; }
    const list = d.items || [];
    if(!list.length){
      $('tmdbCand').innerHTML = '<div class="hint">没搜到。试试日文原名或去掉副标题。</div>';
      return;
    }
    $('tmdbCand').innerHTML = list.map(x=>{
      const img = x.poster
        ? '<img loading="lazy" src="'+esc(x.poster)+'" alt="">'
        : '<div class="ph">🎬</div>';
      const orig = (x.original_name && x.original_name !== x.name) ? esc(x.original_name) : '';
      return '<div class="tmcand" onclick=\'tmdbSelect('+JSON.stringify(x).replace(/'/g,"&#39;")+')\'>'
        + img + '<div class="tl"><b>'+esc(x.name)+'</b>'
        + (orig ? '<span>'+orig+'</span>' : '')
        + '</div><div class="yr">'+esc(x.year||'')+'</div></div>';
    }).join('');
  }catch(e){
    $('tmdbCand').innerHTML = '<div class="err">出错：'+esc(e)+'</div>';
  }finally{ b.disabled = false; b.textContent = '搜索'; }
}

// 从候选里选一条：补齐 url/year，重画条目条与两个来源选项
function tmdbSelect(x){
  if(!x || !x.id) return;
  const m = {
    id: x.id, name: x.name || '', original_name: x.original_name || '',
    year: x.year || '', url: 'https://www.themoviedb.org/tv/' + x.id,
    poster: x.poster || ''
  };
  renderTmdb(m);
  setNames(addMikanName, m.name);
  $('tmdbPick').classList.add('hidden');
}

// 清除 TMDB 识别结果（回到未识别，命名只能是 mikan）
function tmdbClear(){
  renderTmdb(null);
  setNames(addMikanName, '');
  $('tmdbPick').classList.add('hidden');
}

async function fetchSeason(qs){
  try{
    const r = await fetch('/api/mikan/season' + (qs ? ('?'+qs) : ''));
    const d = await r.json();
    if(!d.ok){
      $('seasonBody').innerHTML = '<div class="err">'+esc(d.error||'加载失败')+'</div>';
      return null;
    }
    return d;
  }catch(e){
    $('seasonBody').innerHTML = '<div class="err">加载失败：'+esc(e)+'</div>';
    return null;
  }
}

function applySeasonOptions(d){
  const sel = $('seasonSel');
  if(sel.options.length) return;                 // 只填一次
  (d.options||[]).forEach(o=>{
    const op = document.createElement('option');
    op.value = o.value;
    op.textContent = o.label;
    sel.appendChild(op);
  });
  if(d.current) sel.value = d.current.year + '-' + d.current.season;
}

// first=true 表示首次打开（不带参数，后端会返回当前季度）
async function loadSeason(first){
  $('mkPanel').classList.add('hidden');
  $('seasonBody').classList.remove('hidden');
  let qs = '';
  if(!first && $('seasonSel').value){
    const p = $('seasonSel').value.split('-');
    if(p.length === 2){
      qs = 'year=' + encodeURIComponent(p[0]) + '&season=' + encodeURIComponent(p[1]);
    }
  }
  $('seasonBody').innerHTML = '<div class="hint">正在加载当季新番…</div>';
  const d = await fetchSeason(qs);
  if(!d) return;
  applySeasonOptions(d);
  renderSeason(d);
}

function renderSeason(d){
  const se = d.season || {};
  $('seasonLabel').textContent = se.label
    ? '· ' + se.label + (se.total ? '　' + se.total + ' 部' : '')
    : '';
  const days = se.days || [];
  if(!days.length){
    $('seasonBody').innerHTML = '<div class="hint">这个季度还没有番剧数据。</div>';
    return;
  }
  const today = new Date().getDay();            // 0 = 星期日
  let h = '';
  days.forEach(g=>{
    const cov = g.items.map(it=>{
      const img = it.cover
        ? '<img loading="lazy" src="'+esc(it.cover)+'" alt="">'
        : '<div class="ph">🎬</div>';
      return '<div class="scard" onclick="mkOpen(\''+esc(it.id)+'\','+(g.dow === 7 ? 1 : 0)+')" title="'+esc(it.title)+'">'
           + img + '<div class="n">'+esc(it.title)+'</div></div>';
    }).join('');
    h += '<div class="sday'+(g.dow === today ? ' today' : '')+'">'
       + '<div class="sdayh"><span class="lb">'+esc(g.label)+'</span>'
       + '<span class="ct">'+g.items.length+'</span></div>'
       + '<div class="sgrid">'+cov+'</div></div>';
  });
  $('seasonBody').innerHTML = h;
}

// 从搜索/字幕组面板回到当季列表
function mkBack(){
  $('mkPanel').classList.add('hidden');
  $('seasonBody').classList.remove('hidden');
}

// 选定番剧/字幕组之后：收起当季列表，只留预览区，避免选错
function hideSeason(){
  $('seasonWrap').classList.add('hidden');
}

// 「← 重新选番」：把当季列表放回来
function showSeason(){
  $('seasonWrap').classList.remove('hidden');
  $('prevCard').style.display = 'none';
  mkBack();
  if(!seasonInited){
    seasonInited = true;
    loadSeason(true);
  }
}

function mkClose(){ mkBack(); }

async function mkSearch(){
  const t = $('mkText').value.trim();
  if(!t){ $('mkText').focus(); return; }
  $('mkPanel').classList.remove('hidden');
  $('seasonBody').classList.add('hidden');
  $('mkTitle').textContent = '搜索「'+t+'」…';
  $('mkBody').innerHTML = '<div class="hint">正在抓取 mikan 页面，第一次可能要几秒…</div>';
  $('btnMk').disabled = true; $('btnMk').textContent = '搜索中…';
  try{
    const d = await (await fetch('/api/mikan/search?text='+encodeURIComponent(t))).json();
    if(!d.ok){ $('mkBody').innerHTML = '<div class="err">'+esc(d.error)+'</div>'; return; }
    if(!d.items || !d.items.length){
      $('mkTitle').textContent = '搜索「'+t+'」';
      $('mkBody').innerHTML = '<div class="hint">没找到番剧。可以试试日文原名，或用 <b>id: 番号</b> 直接定位（番号在 mikan 番剧页网址里）。</div>';
      return;
    }
    $('mkTitle').textContent = '搜索「'+t+'」　'+d.items.length+' 个番剧';
    $('mkBody').innerHTML = '<div class="mkgrid">' + d.items.map(it=>{
      const cov = it.cover ? '<img loading="lazy" src="'+esc(it.cover)+'" alt="">' : '<div class="ph">🎬</div>';
      return '<div class="mkcard" onclick="mkOpen('+esc(it.id)+')">'+cov
           + '<div class="n">'+esc(it.title)+'</div></div>';
    }).join('') + '</div>';
  }catch(e){
    $('mkBody').innerHTML = '<div class="err">出错：'+esc(e)+'</div>';
  }finally{
    $('btnMk').disabled = false; $('btnMk').textContent = '搜索';
  }
}

// mineHint：1 = 从「剧场版」分档点进来的（g.dow === 7）。
// mikan 的番剧页本身不写类型，只有季度页的分档才是权威信号，
// 所以把它一路带到 mkRenderBangumi（见那里的 isMovie 判断）。
async function mkOpen(id, movieHint){
  $('mkPanel').classList.remove('hidden');
  $('seasonBody').classList.add('hidden');
  $('mkTitle').textContent = '读取番剧页…';
  $('mkBody').innerHTML = '<div class="hint">正在读取字幕组列表…</div>';
  try{
    const d = await (await fetch('/api/mikan/bangumi?id='+encodeURIComponent(id))).json();
    if(!d.ok){ $('mkBody').innerHTML = '<div class="err">'+esc(d.error)+'</div>'; return; }
    mkRenderBangumi(d.bangumi, movieHint);
  }catch(e){ $('mkBody').innerHTML = '<div class="err">出错：'+esc(e)+'</div>'; }
}

function mkRenderBangumi(b, movieHint){
  const groups = b.groups || [];
  // 剧场版三条线索，任一成立即可：从「剧场版」分档进来 / 后端在页面里认出来 /
  // 用户之前手动勾过（切字幕组时不该把勾选清掉）。
  const isMovie = !!movieHint || !!b.movie || !!($('movie') && $('movie').checked);
  mkStore = [{rss: b.rss_all, title: b.title, subgroup:'', week: b.week||'', movie:isMovie}];
  groups.forEach(g=>mkStore.push({rss:g.rss, title:b.title, subgroup:g.label, week:b.week||'', movie:isMovie}));

  const cov = b.cover ? '<img src="'+esc(b.cover)+'" alt="">' : '<div class="ph">🎬</div>';
  let h = '<div class="mkhead">'+cov+'<div><div class="t">'+esc(b.title)+'</div>'
        + '<div class="m">番号 '+esc(b.id)
        + (isMovie ? '　<b style="color:#2f7d32">剧场版 · 一次性下载</b>' : (b.week ? '　放送 '+esc(b.week) : ''))
        + '　'+groups.length+' 个字幕组</div></div></div>';

  if(isMovie){
    h += '<div class="hint" style="margin:0 0 10px">这是一部剧场版：选字幕组后只下载一次，之后不再检查更新，'
       + '也不按集数重命名。落盘目录由「设置 → 下载 → 剧场版」决定。</div>';
  }

  h += '<div class="mkgroup"><div class="l"><b>整部番（不指定字幕组）</b>'
     + '<span>'+esc(b.rss_all)+'</span></div>'
     + '<button class="sm" onclick="mkPick(0)">选用</button></div>';

  groups.forEach((g,i)=>{
    h += '<div class="mkgroup"><div class="l"><b>'+esc(g.label)+'</b>'
       + '<span>'+esc(g.rss)+(g.update_day ? '　更新 '+esc(g.update_day) : '')+'</span></div>'
       + '<button class="sm" onclick="mkPick('+(i+1)+')">选用</button></div>';
  });

  $('mkTitle').textContent = '选择字幕组';
  $('mkBody').innerHTML = h;
}

function mkPick(i){
  const x = mkStore[i];
  if(!x) return;
  $('rssurl').value = x.rss;
  $('title').value = x.title;
  $('subgroup').value = x.subgroup || '';
  if(x.week){
    const sel = $('weeklabel');
    for(const o of sel.options){
      if(o.value === x.week || o.text === x.week){ sel.value = o.value; break; }
    }
  }
  // 剧场版：在 mikan 番剧页上就已经知道类型了，这里直接勾上，
  // 不用等 /api/parse 再判一次（用户在预览返回前就能看到正确状态）。
  if($('movie')){
    $('movie').checked = !!x.movie;
    applyMovieAdd();
  }
  $('meta').classList.remove('hidden');
  $('seasonWrap').classList.add('hidden');   // 选定后收起当季列表
  preview();                       // 顺手预览一次，确认能抓到条目
  try{ $('meta').scrollIntoView({block:'nearest', behavior:'smooth'}); }catch(e){}
}

// 按番号拼 RSS：两步应用内输入框（原有 prompt 改成软弹窗）
async function fillByID(){
  const id = await askPrompt('从番剧页网址的 ?bangumiId=4059 里取号码。',
    {title:'输入 mikan 番号（bangumiId）', value:'', hint:'例如 4059'});
  if(!id) return;
  const sg = await askPrompt('可留空（留空 = 全部字幕组）。',
    {title:'字幕组 ID（subgroupid）', value:'583', hint:'ANi=583　LoliHouse=370　六四位元=1243'});
  if(sg === null) return;                 // 第二框点了取消就整体放弃
  let u = mkHostValue()+'/RSS/Bangumi?bangumiId='+encodeURIComponent(id);
  if(sg && sg.trim()) u += '&subgroupid='+encodeURIComponent(sg.trim());
  $('rssurl').value = u;
  preview();                              // 拼好就顺手预览，少点一次
}

async function preview(){
  const u = $('rssurl').value.trim();
  if(!u){ toast('请先填 RSS 链接', 'err'); return; }
  // 把当前勾选状态带给后端：勾了剧场版就按剧场版解析（不识别集数），
  // 没勾则让后端自己判断（三条线索见 handleParse）。
  const mv = ($('movie') && $('movie').checked) ? 1 : 0;
  $('btnPrev').disabled = true; $('btnPrev').textContent = '抓取中…';
  try{
    const d = await (await fetch('/api/parse?url='+encodeURIComponent(u)+'&movie='+mv)).json();
    if(!d.ok){ toast('抓取失败：'+d.error, 'err'); return; }
    if(!$('title').value) $('title').value = d.suggest_title;
    if(d.subgroup && !$('subgroup').value) $('subgroup').value = d.subgroup;
    // 季度自动识别：后端从番名猜（「第二季」→2），认不出返回 0 就保持 1。
    // 只在「用户还没动过这个框」时才覆盖，否则手改完再点一次预览会被冲掉。
    if($('season')){
      const n = String(d.guess_season > 0 ? d.guess_season : 1);
      const cur = $('season').value;
      if(cur === '' || cur === autoSeason){ $('season').value = n; autoSeason = n; }
    }
    // 剧场版：后端从番剧页认出「放送星期 = 剧场版」，或者条目里根本没有集号，
    // 这时自动勾上「这是剧场版」。不覆盖用户手动勾选的结果。
    if(d.movie && $('movie') && !$('movie').checked){
      $('movie').checked = true;
      applyMovieAdd();
    }
    $('meta').classList.remove('hidden');
    // mikan 侧番名以卡片/解析结果为准；TMDB 侧用后端识别结果
    const mkName = d.suggest_title || $('title').value.trim();
    if(d.tmdb){
      renderTmdb(d.tmdb);
      setNames(mkName, d.tmdb.name);
    }else{
      renderTmdb(null);
      setNames(mkName, '');
      if(d.tmdb_error) $('tmdbSub').textContent = d.tmdb_error;
    }
    $('prevCard').style.display = 'block';
    $('seasonWrap').classList.add('hidden');   // 预览阶段不再显示当季列表
    $('prevInfo').textContent = '频道：'+d.feed_title+'　条目 '+d.items.length+' 条';
    const tb = $('prevTable').querySelector('tbody');
    tb.innerHTML = '';
    d.items.slice(0,20).forEach(it=>{
      const tr = document.createElement('tr');
      // 集号显示偏移后的结果；有偏移时把原始集号放进括号，方便核对
      const epIn = Number(it.episode)||0, epOut = (it.ep_out===undefined||it.ep_out===null) ? epIn : Number(it.ep_out);
      const epTxt = 'E'+String(epOut).padStart(2,'0') + (epOut!==epIn ? '<span class="sub" style="opacity:.6"> ('+epIn+')</span>' : '');
      tr.innerHTML = '<td>'+epTxt+'</td>'+
        '<td style="font-size:12px">'+esc(it.title.slice(0,80))+'</td>'+
        '<td class="'+(it.matched?'ok':'err')+'">'+(it.matched?'命中':'跳过')+'</td>';
      tb.appendChild(tr);
    });
    if(d.dir_name) $('prevInfo').textContent += '　目录：'+d.dir_name;
  }catch(e){ toast('出错：'+e, 'err'); }
  finally{ $('btnPrev').disabled=false; $('btnPrev').textContent='预览'; }
}

async function subscribe(){
  const u = $('rssurl').value.trim(), title = $('title').value.trim();
  if(!u||!title){ toast('RSS 链接和番名都要填', 'err'); return; }
  $('btnSub').disabled = true; $('btnSub').textContent='订阅中…';
  try{
    const isMovie = $('movie') ? $('movie').checked : false;
    const body = {
      url: u, title: title,
      subgroup: $('subgroup').value.trim(),
      season: isMovie ? 1 : (parseInt($('season').value)||1),
      ep_offset: isMovie ? 0 : (parseInt($('epoffset') ? $('epoffset').value : 0)||0),
      week_label: $('weeklabel').value,
      movie: isMovie,
      match: $('match').value.split('\n').map(s=>s.trim()).filter(Boolean),
      exclude: $('exclude').value.split('\n').map(s=>s.trim()).filter(Boolean),
      name_source: curNameSrc(),
      tmdb_id: addTmdbId || 0,
      mikan_name: addMikanName || title,
      skip_backfill: false
    };
    const d = await (await apiPost('/api/subscribe', body)).json();
    if(d.ok){
      toast(d.message, 'ok');
      closeAdd();   // closeAdd 内部会 resetAddForm 复位表单
      loadSubs(); loadLogs();
    } else toast('订阅失败：'+d.error, 'err');
  }catch(e){ toast('出错：'+e, 'err'); }
  finally{ $('btnSub').disabled=false; $('btnSub').textContent='订阅并下载全部剧集'; }
}

// 剧场版开关：没有季/集/偏移的概念，把这些输入框收起来，减少误填。
// 不隐藏控件（只 disable + 变灰）会更好吗？——直接隐藏更干净，
// 提交时按 movie 走 season=1/ep_offset=0 的固定值。
function applyMovieAdd(){
  const on = $('movie') ? $('movie').checked : false;
  const row = $('seasonRow');
  if(row) row.style.display = on ? 'none' : '';
  const sh = $('seasonHint');
  if(sh) sh.style.display = on ? 'none' : '';
  const btn = $('btnSub');
  if(btn) btn.textContent = on ? '订阅并下载剧场版' : '订阅并下载全部剧集';
  const hint = $('movieHint');
  if(hint) hint.style.color = on ? '#2f7d32' : '';
}

// 清空新增订阅表单，恢复到刚打开时的样子（保留当季列表与季度选择）
function resetAddForm(){
  $('meta').classList.add('hidden');
  $('prevCard').style.display = 'none';
  $('tmdbPick').classList.add('hidden');
  $('rssurl').value = ''; $('title').value = ''; $('subgroup').value = '';
  // 这些也一起清掉，否则下次打开还残留上一部番的信息
  if($('season'))    $('season').value = '1';
  autoSeason = '1';
  if($('epoffset'))  $('epoffset').value = '0';
  if($('weeklabel')) $('weeklabel').value = '';
  if($('match'))     $('match').value = '';
  if($('exclude'))   $('exclude').value = '';
  if($('movie')){ $('movie').checked = false; applyMovieAdd(); }
  if($('prevInfo'))  $('prevInfo').textContent = '';
  if($('prevTable')) $('prevTable').querySelector('tbody').innerHTML = '';
  if($('tmdbQuery')) $('tmdbQuery').value = '';
  if($('tmdbCand'))  $('tmdbCand').innerHTML = '';
  // 命名来源复位到默认的 mikan
  const mkRd = document.querySelector('.namesrc input[value="mikan"]');
  if(mkRd) mkRd.checked = true;
  renderTmdb(null);
  setNames('', '');
  $('seasonWrap').classList.remove('hidden');
  mkBack();
}

async function loadSubs(){
  try{
    const d = await (await fetch('/api/status')).json();
    const grid = $('subGrid');
    const list = d.subscriptions||[];
    // 「上次检查」：定时循环与手动检查都会刷新这个时间（后端记在 App 上）。
    const lc = timeAgo(d.last_check);
    const nxt = Number(d.interval_minutes||0);
    $('lastCheck').textContent = lc ? ('上次检查 '+lc + (nxt>0 ? ' · 每 '+nxt+' 分钟' : '')) : '';
    $('badge-subs').textContent = list.length;
    $('subCount').textContent = list.length ? '· '+list.length+' 部' : '';
    $('subEmpty').style.display = list.length ? 'none' : 'block';
    grid.innerHTML = '';
    list.forEach(s=>{
      const card = document.createElement('div');
      card.className = 'subcard' + (s.enabled?'':' off');
      // cover_v 是后端给的文件 mtime，换海报就会变。
      // 以前用 s.tmdb_id 当版本号：同一部番各季共用同一个 tmdb_id，
      // 第 2 季换了海报 URL 却不变，浏览器在缓存期内一直显示第 1 季的图。
      const coverSrc = s.cover ? '/api/cover?id='+encodeURIComponent(s.id)+'&v='+(s.cover_v||0) : '';
      // 角标：已有 / 总集数。总集数未知时只写已有集数，一个都没有就不显示。
      const owned = Number(s.owned||0), total = Number(s.total_ep||0);
      let badge = '';
      if(owned > 0){
        badge = total > 0 ? (owned+'<span class="t"> / '+total+'</span>') : String(owned);
      }else if(total > 0){
        badge = '0<span class="t"> / '+total+'</span>';
      }
      const badgeCls = 'epbadge' + ((owned===0 && total>0) ? ' z' : '');
      // 剧场版没有集数角标，改成类型角标：「剧场版」/「剧场版 ✓」
      const mvTag = s.movie
        ? '<div class="mvtag" title="剧场版：一次性下载，不会更新">剧场版'+(s.movie_done?' ✓':'')+'</div>'
        : '';
      // 封面上不放任何文字；只有悬浮时才出现三个按钮
      card.innerHTML =
        '<div class="subcover">'+
          (coverSrc ? '<img src="'+coverSrc+'" alt="" loading="lazy" '+
            'onerror="this.style.display=\'none\';this.parentNode.querySelector(\'.noph\').style.display=\'flex\'">' : '')+
          '<div class="noph" style="display:'+(coverSrc?'none':'flex')+'"><b>🎬</b><span>暂无封面</span></div>'+
          (s.movie ? mvTag : (badge ? '<div class="'+badgeCls+'" title="已有集数 / 总集数">'+badge+'</div>' : ''))+
          '<div class="ov">'+
            '<button class="ib" title="编辑订阅" onclick="event.stopPropagation();editSub(\''+s.id+'\')">'+
              '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h4l9.5-9.5a2.12 2.12 0 0 0-3-3L5 17v3Z"/><path d="M13.5 6.5l3 3"/></svg></button>'+
            '<button class="ib" title="查看本地文件" onclick="event.stopPropagation();showFiles(\''+s.id+'\')">'+
              '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7.4A1.4 1.4 0 0 1 4.4 6h4.3l2 2.4h8.9A1.4 1.4 0 0 1 21 9.8v7.8a1.4 1.4 0 0 1-1.4 1.4H4.4A1.4 1.4 0 0 1 3 17.6V7.4Z"/></svg></button>'+
            '<button class="ib d" title="删除订阅" onclick="event.stopPropagation();delSub(\''+s.id+'\',\''+esc(s.title).replace(/'/g,"\\'")+'\')">'+
              '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16"/><path d="M9.5 7V5.3c0-.72.58-1.3 1.3-1.3h2.4c.72 0 1.3.58 1.3 1.3V7"/><path d="M6.6 7l.78 11.9a1.5 1.5 0 0 0 1.5 1.4h5.84a1.5 1.5 0 0 0 1.5-1.4L17.4 7"/><path d="M10.4 10.8v5.6"/><path d="M13.6 10.8v5.6"/></svg></button>'+
          '</div>'+
        '</div>'+
        '<div class="cap">'+esc(s.title||'')+'</div>';
      card.onclick = ()=>editSub(s.id);
      grid.appendChild(card);
    });
  }catch(e){ console.error(e); }
}

// 编辑弹窗的剧场版开关：藏掉季/集/偏移这些对剧场版没意义的字段，
// 并把保存路径/重命名的 placeholder 换成剧场版默认值（用户没填时才看得出默认是什么）。
function applyMovieEdit(){
  const on = $('e_movie') ? $('e_movie').checked : false;
  [['e_season_wrap',on],['e_epoff_wrap',on],['e_total_wrap',on]].forEach(([id])=>{
    const el = $(id);
    if(el) el.style.display = on ? 'none' : '';
  });
  const sb = $('e_skipbackfill');
  if(sb){
    const box = sb.closest('.chk');
    if(box) box.style.display = on ? 'none' : '';
  }
  const sp = $('e_savepath');
  if(sp){
    const def = on ? (window.__movieSaveDef || '/media/剧场版/{{dirname}}')
                   : '/media/新番追更/{{dirname}}/Season {{season_n}}';
    sp.placeholder = '留空 = 用默认 '+def;
  }
  const rn = $('e_rename');
  if(rn){
    const def = on ? '[{{subgroup}}] {{filetitle}}' : '[{{subgroup}}] {{filetitle}} S{{season}}E{{ep}}';
    rn.placeholder = '留空 = 用默认 '+def;
  }
}

async function editSub(id){
  try{
    const d = await (await fetch('/api/sub/get?id='+encodeURIComponent(id))).json();
    if(!d.ok){ toast('读取失败：'+d.error, 'err'); return; }
    const s = d.subscription;
    editingId = id;
    $('editTitle').textContent = '编辑：' + s.title;
    $('e_title').value = s.title||'';
    $('e_filetitle').value = s.file_title||'';
    $('e_url').value = s.url||'';
    $('e_season').value = s.season||1;
    if($('e_epoffset')) $('e_epoffset').value = s.ep_offset||0;
    if($('e_dirname'))  $('e_dirname').value = s.dir_name||'';
    $('e_subgroup').value = s.subgroup||'';
    $('e_weeklabel').value = s.week_label||'';
    $('e_match').value = (s.match||[]).join('\n');
    $('e_exclude').value = (s.exclude||[]).join('\n');
    $('e_total').value = s.total_episodes||0;
    $('e_savepath').value = s.save_path||'';
    $('e_rename').value = s.rename_template||'';
    $('e_enabled').checked = !!s.enabled;
    $('e_skipbackfill').checked = !!s.skip_backfill;
    if($('e_movie')){
      $('e_movie').checked = !!s.movie;
      applyMovieEdit();
    }
    $('e_reset').checked = false;
    fillEditCover(s);
    $('editModal').classList.remove('hidden');
  }catch(e){ toast('读取失败：'+e, 'err'); }
}

// 把订阅的封面信息渲染进编辑弹窗左侧的小海报
function fillEditCover(s){
  const img = $('e_cvimg'), noph = $('e_cvnoph');
  const name = s.cover_name || '';
  const year = s.cover_year || '';
  $('e_cvname').textContent = name || '还没有封面';
  // 第二行优先显示来源，让用户一眼看出这张图是 mikan 抓的还是 TMDB 抓的
  const fromTxt = s.cover_from === 'mikan' ? 'mikan' : (s.cover_from === 'tmdb' ? 'TMDB' : '');
  const bits = [];
  if(year) bits.push('首播 '+year);
  if(fromTxt) bits.push('封面来自 '+fromTxt);
  if(!year && s.tmdb_id) bits.push('TMDB #'+s.tmdb_id);
  $('e_cvyear').textContent = bits.join('　');
  // TMDB 条目链接：有 tmdb_id 就给可跳转链接，没有就藏起来
  const link = $('e_tmdblink');
  if(link){
    if(s.tmdb_id){
      link.href = 'https://www.themoviedb.org/tv/' + s.tmdb_id;
      link.textContent = '在 TMDB 打开 #' + s.tmdb_id + ' ↗';
      link.classList.remove('hidden');
    }else{
      link.classList.add('hidden');
      link.removeAttribute('href');
    }
  }
  if(s.cover){
    img.src = '/api/cover?id='+encodeURIComponent(s.id)+'&v='+(s.tmdb_id||0)+'&t='+Date.now();
    img.style.display = 'block';
    noph.style.display = 'none';
  }else{
    img.style.display = 'none';
    img.removeAttribute('src');
    noph.style.display = 'flex';
  }
}

// 重新抓取：从 mikan 抓这一季自己的海报（一季一图）
//
// 显式传 source:'mikan'，而不是让后端按全局设置走：
// 这个按钮的语义是「这张图不对，重抓一张」，老订阅的封面多是 TMDB 那张
// 多季共用的总海报，只有强制走 mikan 才能换出这一季专属的那张。
async function refetchCover(id){
  if(!id) return;
  const b = $('btnRefetch');
  b.disabled = true; b.textContent = '抓取中…';
  try{
    const r = await fetch('/api/sub/cover', {method:'POST', headers:{'Content-Type':'application/json'},
      body: JSON.stringify({id: id, source: 'mikan'})});
    const d = await r.json();
    if(!d.ok){ toast('抓取失败：'+(d.error||''), 'err'); return; }
    toast(d.message || '封面已更新', 'ok');
    const g = await (await fetch('/api/sub/get?id='+encodeURIComponent(id))).json();
    if(g.ok) fillEditCover(g.subscription);
    loadSubs();
  }catch(e){ toast('抓取失败：'+e, 'err'); }
  finally{ b.disabled=false; b.textContent='重新抓取'; }
}

// ===== 订阅体检 =====
// 为什么要有：订阅卡上的「最高 E4 · 累计 4」只是程序自己的记账，
// 和 qBittorrent 里的现实可以完全脱节。最典型的一种是过滤规则把条目
// 静默挡掉、却照样记进 seen——界面说回填了 4 条，实际一个任务都没有，
// 而且再也不会重试。这个弹窗就是把「哪一环卡住了」摊开给用户看。

function closeAudit(){ $('auditModal').classList.add('hidden'); }

function openAudit(){
  $('auditModal').classList.remove('hidden');
  runAudit();
}

async function runAudit(){
  $('auditHint').textContent = '正在逐条抓取 RSS 并核对 qBittorrent 任务，稍等…';
  $('auditBody').innerHTML = '';
  try{
    const d = await (await fetch('/api/subs/audit')).json();
    if(!d.ok){ $('auditHint').textContent = '体检失败：'+(d.error||'未知错误'); return; }
    renderAudit(d);
  }catch(e){
    $('auditHint').textContent = '体检失败：'+e;
  }
}

function renderAudit(d){
  const items = d.items||[];
  const bad = items.filter(x=>x.level==='error').length;
  const warn = items.filter(x=>x.level==='warn').length;
  let head = '共 '+items.length+' 条订阅';
  if(bad) head += '，其中 '+bad+' 条有严重问题';
  if(warn) head += (bad? '，':'，')+warn+' 条需要留意';
  if(!bad && !warn) head += '，全部正常';
  head += '。检查时间 '+new Date(d.checked_at).toLocaleString();
  if(d.qbit_ok === false) head += '　（注意：这次连不上 qBittorrent，「已有集数」用的是程序记账值）';
  $('auditHint').textContent = head;

  const body = $('auditBody');
  body.innerHTML = '';
  // 有问题的排前面
  const sorted = items.slice().sort((a,b)=>{
    const ra = a.level==='error'?0 : a.level==='warn'?1 : 2;
    const rb = b.level==='error'?0 : b.level==='warn'?1 : 2;
    return ra - rb;
  });
  sorted.forEach(x=>{
    const cls = x.level==='error'?'err' : x.level==='warn'?'warn' : 'ok';
    const box = document.createElement('div');
    box.className = 'aitem '+cls;
    const cnt = 'RSS '+x.rss+' · 命中规则 '+x.matched+' · 待推 '+x.pending+
      ' · 已有 '+x.owned+(x.total_ep?('/'+x.total_ep):'')+' · qB '+x.qb_count;
    let html = '<div class="aihead"><span class="t">'+esc(x.title)+'</span>'+
      '<span class="m"><span class="aicnt">'+cnt+'</span></span></div>';
    (x.issues||[]).forEach(is=>{
      html += '<div class="aiissue '+esc(is.level||'info')+'">'+
        '<b>'+esc(is.title)+'</b>　'+esc(is.detail)+
        (is.sample ? '<div class="smp">例：'+esc(is.sample)+'</div>' : '')+
        (is.can_reset ? '<div class="fix"><button class="ghost sm" onclick="resetSub(\''+x.id+'\',\''+esc(x.title).replace(/'/g,"\\'")+'\')">重置状态并重推</button></div>' : '')+
      '</div>';
    });
    if(!(x.issues||[]).length){
      html += '<div class="aiissue info">没有发现问题。</div>';
    }
    box.innerHTML = html;
    body.appendChild(box);
  });
  if(!items.length){
    body.innerHTML = '<div class="hint">还没有订阅。</div>';
  }
}

// 重置一条订阅的抓取记录：清掉 seen / pushed_eps，然后从第 1 集开始重新补推。
// 补推走后台队列（先推第 1 集，其余按 2、3… 顺序逐条推），所以这里不用再调 /api/check。
async function resetSub(id, title){
  const ok = await askConfirm(
    '会清掉「哪些条目已经处理过」的记录，然后从第 1 集开始重新推送。\n'+
    '第 1 集会立刻推送，其余集数后台按顺序逐条推送（避免同时写入造成卡顿）。\n'+
    '已经下载好的文件不会被删除，qBittorrent 里已有的任务也不会重复添加。',
    {title:'重置「'+title+'」的抓取记录？', yes:'重置并重推'});
  if(!ok) return;
  try{
    const d = await (await apiPost('/api/sub/reset', {id:id})).json();
    if(!d.ok){ toast('重置失败：'+(d.error||''), 'err'); return; }
    toast(d.message + '\n后台补推进行中，可在首页「下载」或日志里看进度。', 'ok');
    closeAudit();
    loadSubs(); loadLogs();
  }catch(e){ toast('重置失败：'+e, 'err'); }
}


// ===== 封面选择器 =====
let cvTargetId = null;
function pickCover(id){
  if(!id){ toast('请先选择订阅', 'err'); return; }
  cvTargetId = id;
  $('cvMsg').textContent = '';
  $('cvGrid').innerHTML = '';
  $('cvQuery').value = ($('e_title').value || $('editTitle').textContent.replace(/^编辑：/,'')).trim();
  $('cvModal').classList.remove('hidden');
  cvSearch();
}

async function cvSearch(){
  const q = $('cvQuery').value.trim();
  if(!q){ $('cvMsg').textContent = '请输入番名'; return; }
  const b = $('btnCvSearch');
  b.disabled = true; b.textContent = '搜索中…';
  $('cvMsg').textContent = '';
  $('cvGrid').innerHTML = '';
  try{
    const d = await (await fetch('/api/tmdb/search?q='+encodeURIComponent(q))).json();
    if(!d.ok){ $('cvMsg').textContent = '搜索失败：'+(d.error||''); return; }
    if(!d.items.length){ $('cvMsg').textContent = '没有搜到结果'; return; }
    $('cvMsg').textContent = '共 '+d.items.length+' 个结果，点海报应用';
    d.items.forEach(it=>{
      const c = document.createElement('div');
      c.className = 'c';
      c.innerHTML = '<img src="'+it.poster+'" alt="" loading="lazy">'+
        '<div class="n">'+esc(it.name)+(it.year?' · '+it.year:'')+'</div>';
      c.onclick = ()=>cvApply(it.id);
      $('cvGrid').appendChild(c);
    });
  }catch(e){ $('cvMsg').textContent = '搜索失败：'+e; }
  finally{ b.disabled=false; b.textContent='搜索'; }
}

async function cvApply(tmdbId){
  if(!cvTargetId) return;
  $('cvMsg').textContent = '正在下载封面…';
  try{
    const r = await fetch('/api/sub/cover', {method:'POST', headers:{'Content-Type':'application/json'},
      body: JSON.stringify({id: cvTargetId, tmdb_id: tmdbId})});
    const d = await r.json();
    if(!d.ok){ $('cvMsg').textContent = '应用失败：'+(d.error||''); return; }
    $('cvModal').classList.add('hidden');
    if(editingId === cvTargetId){
      const g = await (await fetch('/api/sub/get?id='+encodeURIComponent(cvTargetId))).json();
      if(g.ok) fillEditCover(g.subscription);
    }
    loadSubs();
  }catch(e){ $('cvMsg').textContent = '应用失败：'+e; }
}

// 给所有缺封面的订阅补抓
async function refreshAllCovers(){
  try{
    const r = await fetch('/api/covers/refresh', {method:'POST'});
    const d = await r.json();
    if(!d.ok){ toast('失败：'+(d.error||''), 'err'); return; }
    if(d.queued === 0){ toast(d.message, 'info'); return; }
    toast(d.message, 'ok');
    // 后台抓取需要几秒，过一会儿再刷新列表
    setTimeout(loadSubs, 4000);
    setTimeout(loadSubs, 10000);
  }catch(e){ toast('失败：'+e, 'err'); }
}

function closeEdit(){ $('editModal').classList.add('hidden'); editingId=null; }

async function saveEdit(){
  if(!editingId) return;
  $('btnSave').disabled = true;
  try{
    const body = {
      id: editingId,
      title: $('e_title').value.trim(),
      file_title: $('e_filetitle').value.trim(),
      url: $('e_url').value.trim(),
      season: parseInt($('e_season').value)||1,
      ep_offset: parseInt($('e_epoffset') ? $('e_epoffset').value : 0)||0,
      dir_name: $('e_dirname') ? $('e_dirname').value.trim() : '',
      subgroup: $('e_subgroup').value.trim(),
      week_label: $('e_weeklabel').value,
      match: $('e_match').value.split('\n').map(s=>s.trim()).filter(Boolean),
      exclude: $('e_exclude').value.split('\n').map(s=>s.trim()).filter(Boolean),
      total_episodes: parseInt($('e_total').value)||0,
      save_path: $('e_savepath').value.trim(),
      rename_template: $('e_rename').value.trim(),
      enabled: $('e_enabled').checked,
      movie: $('e_movie') ? $('e_movie').checked : false,
      skip_backfill: $('e_skipbackfill').checked,
      reset_state: $('e_reset').checked
    };
    const d = await (await apiPost('/api/sub/update', body)).json();
    if(d.ok){ toast(d.message, 'ok'); closeEdit(); loadSubs(); }
    else toast('保存失败：'+d.error, 'err');
  }catch(e){ toast('出错：'+e, 'err'); }
  finally{ $('btnSave').disabled=false; }
}

// ============ ③ 下载 ============
async function loadDownloads(){
  try{
    const d = await (await fetch('/api/downloads')).json();
    if(!d.ok){
      $('dlList').innerHTML = '<div class="empty">'+esc(d.error||'读取失败')+'</div>';
      return;
    }
    const sum = d.summary||{};
    $('dl-count').textContent = d.count||0;
    $('dl-active').textContent = sum.downloading||0;
    $('dl-done').textContent = sum.completed||0;
    $('dl-speed').innerHTML = fmtSpeed(sum.dl_speed||0).replace('/s','<small>/s</small>');

    const b = $('badge-dl');
    if((sum.downloading||0)>0){ b.textContent=sum.downloading; b.classList.remove('hidden'); }
    else b.classList.add('hidden');

    const tasks = d.tasks||[];
    if(!tasks.length){
      $('dlList').innerHTML = '<div class="empty">暂无下载任务<br><span style="font-size:12px">订阅推送的番剧会出现在这里</span></div>';
      return;
    }
    $('dlList').innerHTML = '<table><thead><tr><th>任务</th><th style="width:150px">进度</th><th style="width:100px">速度</th><th style="width:76px">状态</th></tr></thead><tbody>'
      + tasks.map(t=>{
        const pct = Math.round((t.progress||0)*100);
        const done = pct>=100;
        const eta = fmtETA(t.eta);
        return '<tr>'
          + '<td><div class="dlname">'+esc(t.name)+'</div>'
          +   '<div class="dlmeta">'+esc(t.sub_title)+' · '+fmtSize(t.size)
          +   (eta? ' · 剩余 '+eta : '') + '</div>'
          +   '<div class="bar"><i class="'+(done?'done':'')+'" style="width:'+pct+'%"></i></div>'
          + '</td>'
          + '<td style="font-size:12px">'+pct+'%　<span style="color:#999">'+fmtSize(t.downloaded)+'</span></td>'
          + '<td style="font-size:12px">'+fmtSpeed(t.dlspeed)+'</td>'
          + '<td style="font-size:12px">'+esc(t.state_text)+'</td>'
          + '</tr>';
      }).join('') + '</tbody></table>';
  }catch(e){ console.error(e); }
}

// ============ 删除弹窗 ============
// 流程：点垃圾桶 → 弹窗（一个勾选框 + 是/否两个按钮）。
// 勾选框默认不点，点它时弹一次警报；警报关掉后不再二次确认，点是就删。
function delSub(id, title){
  deletingId = id;
  deletingTitle = title;
  $('delTitle').textContent = title;
  $('del_extra').checked = false;
  $('delHint').textContent = '不勾选：只移除这条订阅，种子与已下载文件都保留。';
  $('delModal').classList.remove('hidden');
}

function closeDel(){
  $('delModal').classList.add('hidden');
  deletingId = null; deletingTitle = '';
}

// 勾上就弹应用内警报；取消则退回未勾状态。
async function onDelExtra(){
  if(!$('del_extra').checked){
    $('delHint').textContent = '不勾选：只移除这条订阅，种子与已下载文件都保留。';
    return;
  }
  const ok = await askConfirm(
    '将同时删除「' + (deletingTitle||'该订阅') + '」在 qBittorrent 里的任务，' +
    '以及 NAS 上已经下载的本地文件。\n\n' +
    '文件会被立即永久删除，不进入回收站，无法恢复。\n\n' +
    '确定要连同下载数据一起删除吗？',
    {title:'⚠ 请注意', yes:'确定删除', no:'取消'});
  if(ok){
    $('delHint').textContent = '已勾选：将同时删除 qBittorrent 任务和已下载文件，无法恢复。';
  } else {
    $('del_extra').checked = false;
    $('delHint').textContent = '不勾选：只移除这条订阅，种子与已下载文件都保留。';
  }
}

async function doDelete(){
  if(!deletingId) return;
  const extra = $('del_extra').checked;

  $('btnDel').disabled = true;
  try{
    const d = await (await apiPost('/api/sub/delete', {
      id: deletingId,
      delete_torrents: extra,
      delete_files: extra,
      confirm: true
    })).json();
    if(d.ok) { toast(d.message, 'ok'); closeDel(); loadSubs(); }
    else toast('删除失败：'+explainAuth(d), 'err');
  }catch(e){ toast('出错：'+e, 'err'); }
  finally{ $('btnDel').disabled = false; }
}

// ============ ④ 日志 ============
async function loadLogs(){
  try{
    const d = await (await fetch('/api/logs')).json();
    $('logs').textContent = (d.lines||[]).join('\n') || '（暂无日志）';
  }catch(e){ $('logs').textContent = '读取日志失败：'+e; }
}

async function checkNow(){
  try{
    await apiPost('/api/check');
    toast('已触发检查，稍后看日志或订阅列表', 'ok');
    setTimeout(()=>{ loadSubs(); }, 3000);
  }catch(e){ toast('触发失败：'+e, 'err'); }
}

// ============ ⑤ 设置 ============
async function loadSettings(){
  try{
    const d = await (await fetch('/api/settings')).json();
    if(!d.ok) return;
    const s = d.settings;
    $('s_host').value = s.qbit_host||'';
    $('s_port').value = s.qbit_port||'';
    $('s_category').value = s.qbit_category||'';
    $('s_apikey').value = '';
    $('s_apikey').placeholder = s.qbit_apikey_set ? '已设置，留空则不修改' : '未设置，请填写';
    $('s_apikey_hint').textContent = s.qbit_apikey_set ? '当前：'+s.qbit_apikey : '当前：未设置';
    $('s_interval').value = s.interval_minutes||60;
    $('s_prefixes').value = (s.allow_write_prefixes||[]).join('\n');
    // TMDB
    $('s_tmdbkey').value = '';
    $('s_tmdbkey').placeholder = s.tmdb_key_set ? '已设置，留空则不修改' : '未设置，请填写';
    $('s_tmdbkey_hint').textContent = s.tmdb_key_set ? ('当前：'+s.tmdb_key) : '当前：未设置（订阅不会自动抓封面）';
    $('s_tmdbhost').value = s.tmdb_host||'https://api.tmdb.org';
    $('s_cvcount').textContent = '已缓存封面：'+(s.covers_count||0)+' 张';
    // 登录有效期
    const ttl = s.session_ttl_minutes;
    window.__ttlNow = (ttl===undefined||ttl===null) ? 120 : ttl;
    $('s_sessttl').value = window.__ttlNow;
    $('s_sessttl_hint').innerHTML = ttlText(window.__ttlNow);
    // 下载目录挂载
    const mts = Array.isArray(s.mounts) ? s.mounts : [];
    $('s_mounts').value = mts.map(m=>m.qbit+' = '+m.local).join('\n');
    $('s_dl_hint').textContent = mts.length
      ? ('启用中：'+mts.length+' 条映射（'+mts.map(m=>m.qbit+' → '+m.local).join('，')+'），删除订阅时文件夹会一起删。')
      : '留空 = 不启用，删除订阅时会保留文件夹。';
    // 追更库根目录
    $('s_library').value = s.library_root || '/media/新番追更';
    if($('s_coversrc')) $('s_coversrc').value = (s.cover_source === 'tmdb') ? 'tmdb' : 'mikan';
    // 全局默认过滤规则
    $('s_match').value = (s.default_match||[]).join('\n');
    $('s_exclude').value = (s.default_exclude||[]).join('\n');
    // 剧场版默认落盘
    $('s_movie_save').value = s.movie_save_path || '';
    $('s_movie_save').placeholder = s.movie_default_save || '/media/剧场版/{{dirname}}';
    $('s_movie_rename').value = s.movie_rename || '';
    $('s_movie_rename').placeholder = s.movie_default_rename || '[{{subgroup}}] {{filetitle}}';
    window.__movieSaveDef = s.movie_default_save_path || '/media/剧场版/{{dirname}}';
    $('s_movie_rename_hint').innerHTML = '默认不带季集（<code>'+esc(s.movie_default_rename||'')+
      '</code>）。填了 SxxExx 之类的变量在这里没有意义，但不会报错。';
    // bangumi 连接
    mkHostFill(s.mikan_host, s.mikan_hosts||[]);
    // 代理
    const px = s.proxy || {};
    $('s_proxy_on').checked = !!px.enabled;
    $('s_proxy_proto').value = (px.protocol==='socks5') ? 'socks5' : 'http';
    $('s_proxy_host').value = px.host || '';
    $('s_proxy_port').value = px.port || '';
    $('s_proxy_user').value = px.username || '';
    $('s_proxy_pass').value = '';
    $('s_proxy_pass').placeholder = px.password_set ? '已设置，留空则不修改；填 - 清空' : '代理不需要认证就留空';
    $('s_proxy_hint').textContent = '当前：' + (px.describe || '未启用');

    loadAccount();
    $('sf-status').textContent = '检查间隔 '+(s.interval_minutes||60)+' 分钟';

    const pl = d.planned||[];
    $('plannedList').innerHTML = pl.map(p=>
      '· <b style="color:#bbb">'+esc(p.name)+'</b> — '+esc(p.desc)
    ).join('<br>');
  }catch(e){ console.error(e); }
}

// ============ 设置页二级菜单 ============
const TABS = ['dl','fetch','tmdb','proxy','auth'];
function setTab(name){
  if(TABS.indexOf(name)<0) name='dl';
  TABS.forEach(t=>{
    const pane = $('tab-'+t);
    if(pane) pane.style.display = (t===name) ? '' : 'none';
  });
  document.querySelectorAll('.tabbar .tab').forEach(b=>{
    b.classList.toggle('on', b.dataset.tab===name);
  });
  try{ localStorage.setItem('ag_settab', name); }catch(e){}
  if(name==='auth') loadAccount();
  if(name==='tmdb' && !window.__tmdbLoaded){ window.__tmdbLoaded=1; loadSettings(); }
}
function restoreTab(){
  let t = 'dl';
  try{ t = localStorage.getItem('ag_settab') || 'dl'; }catch(e){}
  setTab(t);
}

// ============ 账号 / API Key ============
async function loadAccount(){
  try{
    const d = await (await fetch('/api/auth/info')).json();
    if(!d.ok) return;
    const u = d.username || '—';
    $('acc_user').textContent = u;
    const av = $('acc_ava');
    av.textContent = (u && u !== '—') ? u.slice(0,1) : 'A';
    $('acc_mode').textContent = d.loopback
      ? '本机回环访问，免登录'
      : (d.has_account ? '已登录（远程会话）' : '尚未注册账号');
    $('acc_apikey').value = d.api_key || '';
    // 登录有效期一并回填（切 tab 只调 loadAccount，不会重跑 loadSettings）
    if(d.session_ttl_minutes !== undefined && $('s_sessttl')){
      window.__ttlNow = d.session_ttl_minutes;
      $('s_sessttl').value = d.session_ttl_minutes;
      $('s_sessttl_hint').innerHTML = ttlText(d.session_ttl_minutes);
    }
  }catch(e){ console.error(e); }
}

// 修改密码：两次输入一致 + 旧密码正确
async function changePassword(){
  const oldp = $('pw_old').value;
  const np = $('pw_new').value;
  const np2 = $('pw_new2').value;
  const box = $('pwResult');
  if(!oldp){ box.innerHTML = '<span class="err">请先输入当前密码</span>'; return; }
  if(np.length < 6){ box.innerHTML = '<span class="err">新密码至少 6 位</span>'; return; }
  if(np !== np2){ box.innerHTML = '<span class="err">两次输入的新密码不一致</span>'; return; }
  $('btnPwSave').disabled = true; $('btnPwSave').textContent = '提交中…';
  box.innerHTML = '';
  try{
    const d = await (await apiPost('/api/auth/password',
      {old_password: oldp, new_password: np})).json();
    if(d.ok){
      $('pw_old').value = ''; $('pw_new').value = ''; $('pw_new2').value = '';
      box.innerHTML = '<span class="ok">✓ '+esc(d.message)+'</span>';
    } else {
      box.innerHTML = '<span class="err">✗ '+esc(d.error)+'</span>';
    }
  }catch(e){ box.innerHTML = '<span class="err">出错：'+esc(String(e))+'</span>'; }
  finally{ $('btnPwSave').disabled=false; $('btnPwSave').textContent='修改密码'; }
}

async function rotateKey(){
  const ok = await askConfirm('重置后旧 API Key 立即失效，正在使用它的脚本/AI 会全部断开。确定重置？',
    {title:'重置 API Key', yes:'重置'});
  if(!ok) return;
  $('btnRotate').disabled = true; $('btnRotate').textContent = '重置中…';
  try{
    const d = await (await apiPost('/api/auth/apikey')).json();
    if(d.ok){
      $('acc_apikey').value = d.api_key;
      $('accResult').innerHTML = '<span class="ok">✓ '+esc(d.message)+'</span>';
    } else {
      $('accResult').innerHTML = '<span class="err">✗ '+esc(d.error)+'</span>';
    }
  }catch(e){ $('accResult').innerHTML = '<span class="err">出错：'+esc(String(e))+'</span>'; }
  finally{ $('btnRotate').disabled=false; $('btnRotate').textContent='重置 API Key'; }
}

async function doLogout(){
  const ok = await askConfirm('退出登录？', {title:'退出登录', yes:'退出'});
  if(!ok) return;
  try{ await apiPost('/api/logout'); }catch(e){}
  location.href = '/login';
}

// ---- 登录有效期 ----
// 把分钟数说成人话（与后端 ttlText 一致）
// 特殊值：-1 永久、0 单次有效
function ttlText(m){
  if(m === undefined || m === null || m === '') return '默认 120 分钟（2 小时）';
  m = parseInt(m);
  if(isNaN(m)) return '默认 120 分钟（2 小时）';
  if(m === -1) return '当前：永久有效，登录一次一直有效';
  if(m === 0) return '当前：单次有效，关闭浏览器即失效';
  if(m < 60) return '当前：' + m + ' 分钟后失效';
  if(m % 1440 === 0) return '当前：' + (m/1440) + ' 天后失效';
  if(m % 60 === 0) return '当前：' + (m/60) + ' 小时后失效';
  return '当前：' + m + ' 分钟（约 ' + (m/60).toFixed(1) + ' 小时）后失效';
}
function setTtlQuick(m){
  $('s_sessttl').value = m;
  $('s_sessttl_hint').innerHTML = ttlText(m);
}
async function saveSessionTTL(){
  const raw = $('s_sessttl').value.trim();
  const box = $('ttlResult');
  if(raw === ''){ box.innerHTML = '<span class="err">请填写分钟数，或填 0（单次有效）/-1（永久）</span>'; return; }
  const m = parseInt(raw);
  const special = (m === 0 || m === -1);
  if(isNaN(m) || (!special && (m < 1 || m > 525600))){
    box.innerHTML = '<span class="err">只能填 0（单次有效）、-1（永久），或 1-525600 的分钟数</span>';
    return;
  }
  const changed = (m !== window.__ttlNow);   // 已经是这个值就不用再确认
  if(changed && !special && m < 5){
    if(!await askConfirm('有效期只有 '+m+' 分钟，很快就会掉线，确定吗？', {title:'有效期很短', yes:'就这样'})) return;
  }
  if(changed && m === 0){
    if(!await askConfirm('单次有效：关掉浏览器就要重新登录，确定吗？', {title:'单次有效', yes:'就这样'})) return;
  }
  $('btnTtlSave').disabled = true; $('btnTtlSave').textContent = '保存中…';
  box.innerHTML = '';
  try{
    // 只提交这一个字段，避免顺带改动其它设置
    const d = await (await apiPost('/api/settings/save', {session_ttl_minutes: m})).json();
    if(d.ok){
      box.innerHTML = '<span class="ok">✓ '+esc(d.message)+'</span>';
      $('s_sessttl_hint').innerHTML = ttlText(m);
      loadAccount();
    } else {
      box.innerHTML = '<span class="err">✗ '+esc(d.error)+'</span>';
    }
  }catch(e){ box.innerHTML = '<span class="err">出错：'+esc(String(e))+'</span>'; }
  finally{ $('btnTtlSave').disabled=false; $('btnTtlSave').textContent='保存有效期'; }
}

async function testQbit(){
  $('btnTest').disabled = true; $('btnTest').textContent = '测试中…';
  try{
    const body = {
      qbit_host: $('s_host').value.trim(),
      qbit_port: parseInt($('s_port').value)||0,
      qbit_apikey: $('s_apikey').value.trim()
    };
    const d = await (await apiPost('/api/settings/test', body)).json();
    if(d.ok){
      $('setResult').innerHTML = '<span class="ok">✓ 连接成功　版本 '+esc(d.version)+'　'+d.ms+'ms</span>　<span style="color:#999">'+esc(d.url||'')+'</span>';
    } else {
      $('setResult').innerHTML = '<span class="err">✗ 连接失败：'+esc(d.error)+'</span>';
    }
  }catch(e){ $('setResult').innerHTML = '<span class="err">出错：'+esc(String(e))+'</span>'; }
  finally{ $('btnTest').disabled=false; $('btnTest').textContent='测试连接'; }
}

async function testTMDB(){
  $('btnTestTmdb').disabled = true; $('btnTestTmdb').textContent='测试中…';
  try{
    // 用一次真实搜索验证 Key 与域名是否可用
    const d = await (await fetch('/api/tmdb/search?q='+encodeURIComponent('胆大党'))).json();
    if(d.ok){
      $('tmdbResult').innerHTML = '<span class="ok">✓ TMDB 可用，搜到 '+d.count+' 个结果</span>';
    } else {
      $('tmdbResult').innerHTML = '<span class="err">✗ '+esc(d.error)+'</span>';
    }
  }catch(e){ $('tmdbResult').innerHTML = '<span class="err">出错：'+esc(String(e))+'</span>'; }
  finally{ $('btnTestTmdb').disabled=false; $('btnTestTmdb').textContent='测试 TMDB'; }
}

// ============ bangumi 连接（镜像站选择） ============
// 候选值由后端 /api/settings 的 mikan_hosts 提供，这里只负责渲染，
// 避免域名列表在前后端各维护一份、写着写着就不一致。
let MK_HOSTS = [];
const MK_CUSTOM = '__custom__';

function mkHostFill(host, hosts){
  MK_HOSTS = Array.isArray(hosts) ? hosts : [];
  const sel = $('s_mikanhost');
  if(!sel) return;
  sel.innerHTML = MK_HOSTS.map(h=>
    '<option value="'+esc(h.value || MK_CUSTOM)+'">'+esc(h.label)+'</option>'
  ).join('');
  const cur = (host||'').trim();
  const hit = MK_HOSTS.find(h=>h.value && h.value === cur);
  if(hit){
    sel.value = hit.value;
    $('s_mikanhost_custom').value = '';
    $('s_mikanhost_custom').style.display = 'none';
  } else {
    // 空配置 = 用内置默认（第一项）；否则视为自定义地址
    if(!cur){
      sel.value = MK_HOSTS.length ? (MK_HOSTS[0].value || MK_CUSTOM) : MK_CUSTOM;
      $('s_mikanhost_custom').style.display = (sel.value===MK_CUSTOM) ? '' : 'none';
      $('s_mikanhost_custom').value = '';
    } else {
      const ci = MK_HOSTS.findIndex(h=>!h.value);
      if(ci >= 0) sel.value = MK_CUSTOM; else sel.value = MK_HOSTS.length ? (MK_HOSTS[0].value||MK_CUSTOM) : MK_CUSTOM;
      $('s_mikanhost_custom').value = cur;
      $('s_mikanhost_custom').style.display = '';
    }
  }
  mkHostHint();
}

function mkHostChanged(){
  const sel = $('s_mikanhost');
  const custom = $('s_mikanhost_custom');
  const isCustom = (sel.value === MK_CUSTOM) || !sel.value;
  custom.style.display = isCustom ? '' : 'none';
  if(isCustom) custom.focus();
  mkHostHint();
}

// 当前选中的地址：内置项直接用 value，自定义项取输入框（空=用默认主站）
function mkHostValue(){
  const sel = $('s_mikanhost');
  if(!sel) return '';
  if(sel.value === MK_CUSTOM || !sel.value) return $('s_mikanhost_custom').value.trim();
  return sel.value;
}

function mkHostHint(){
  const sel = $('s_mikanhost');
  const box = $('s_mikanhost_hint');
  if(!sel || !box) return;
  const hit = MK_HOSTS.find(h=>h.value === sel.value);
  const base = '抓订阅、搜番、下封面都走这个站点。选「自定义」时填完整地址（含 https://）。';
  box.innerHTML = (hit && hit.desc ? esc(hit.desc)+'。' : '') + base;
}

async function testProxy(){
  const btn = $('btnTestProxy');
  btn.disabled = true; btn.textContent = '测试中…';
  const box = $('proxyResult');
  box.innerHTML = '';
  try{
    const body = {
      proxy_enabled: $('s_proxy_on').checked,
      proxy_protocol: $('s_proxy_proto').value,
      proxy_host: $('s_proxy_host').value.trim(),
      proxy_port: parseInt($('s_proxy_port').value)||0,
      proxy_username: $('s_proxy_user').value.trim(),
      proxy_password: $('s_proxy_pass').value,
      mikan_host: mkHostValue()
    };
    const d = await (await apiPost('/api/settings/proxy/test', body)).json();
    if(d.ok){
      box.innerHTML = '<span class="ok">✓ '+esc(d.proxy||'')+' → '+esc(d.target||'')+
        '　HTTP '+d.status+'　'+d.latency_ms+' ms</span>';
    } else {
      box.innerHTML = '<span class="err">✗ '+esc(d.error||'测试失败')+'</span>';
    }
  }catch(e){ box.innerHTML = '<span class="err">出错：'+esc(String(e))+'</span>'; }
  finally{ btn.disabled=false; btn.textContent='测试代理'; }
}

// 把提示写到「当前所在分页」的结果框里，避免写到看不见的地方
function settingsResultBox(){
  let tab = 'dl';
  try{ tab = localStorage.getItem('ag_settab') || 'dl'; }catch(e){}
  if(tab === 'tmdb') return $('tmdbResult');
  if(tab === 'fetch') return $('fetchResult');
  if(tab === 'proxy') return $('proxyResult');
  return $('setResult');
}

async function saveSettings(){
  const btns = [$('btnSetSave'), $('btnSetSave2'), $('btnSetSave3')].filter(Boolean);
  btns.forEach(b=>{ b.disabled = true; });
  const box = settingsResultBox() || $('setResult');
  try{
    const body = {
      qbit_host: $('s_host').value.trim(),
      qbit_port: parseInt($('s_port').value)||0,
      qbit_apikey: $('s_apikey').value.trim(),
      qbit_category: $('s_category').value.trim(),
      interval_minutes: parseInt($('s_interval').value)||0,
      allow_write_prefixes: $('s_prefixes').value.split('\n').map(s=>s.trim()).filter(Boolean),
      tmdb_key: $('s_tmdbkey').value.trim(),
      tmdb_host: $('s_tmdbhost').value.trim(),
      // 挂载映射：一行一条「qB 侧路径 = 本机挂载点」
      mounts: $('s_mounts').value.split('\n').map(line=>{
        const i = line.indexOf('=');
        if(i < 0) return null;
        const q = line.slice(0,i).trim(), l = line.slice(i+1).trim();
        if(!q || !l) return null;
        return {qbit: q, local: l};
      }).filter(Boolean),
      library_root: $('s_library') ? $('s_library').value.trim() : '',
      cover_source: $('s_coversrc') ? $('s_coversrc').value : 'mikan',
      default_match: $('s_match').value.split('\n').map(s=>s.trim()).filter(Boolean),
      default_exclude: $('s_exclude').value.split('\n').map(s=>s.trim()).filter(Boolean),
      // 剧场版默认落盘
      movie_save_path: $('s_movie_save').value.trim(),
      movie_rename: $('s_movie_rename').value.trim(),
      // bangumi 连接：选「自定义」时用输入框里的地址
      mikan_host: mkHostValue(),
      // 代理
      proxy_enabled: $('s_proxy_on').checked,
      proxy_protocol: $('s_proxy_proto').value,
      proxy_host: $('s_proxy_host').value.trim(),
      proxy_port: parseInt($('s_proxy_port').value)||0,
      proxy_username: $('s_proxy_user').value.trim(),
      proxy_password: $('s_proxy_pass').value
    };
    const d = await (await apiPost('/api/settings/save', body)).json();
    if(d.ok){
      box.innerHTML = '<span class="ok">✓ '+esc(d.message)+'</span>';
      $('s_apikey').value = '';
      $('s_tmdbkey').value = '';
      $('s_proxy_pass').value = '';
      loadSettings();
    } else {
      box.innerHTML = '<span class="err">✗ '+esc(d.error)+'</span>';
    }
  }catch(e){ box.innerHTML = '<span class="err">出错：'+esc(String(e))+'</span>'; }
  finally{ btns.forEach(b=>{ b.disabled=false; }); }
}

// ============ 初始化 ============
let startView = 'home';
try{ startView = localStorage.getItem('ag_view') || 'home'; }catch(e){}
go(startView);

// 首页每 60 秒刷新一次（在首页时才刷）
setInterval(()=>{ if(currentView==='home') loadHome(); }, 60000);

// 遮罩关闭 + ESC
$('editModal').addEventListener('click', function(e){ if(e.target===this) closeEdit(); });
$('delModal').addEventListener('click', function(e){ if(e.target===this) closeDel(); });
$('filesModal').addEventListener('click', function(e){ if(e.target===this) closeFiles(); });
$('addModal').addEventListener('click', function(e){ if(e.target===this) closeAdd(); });
// 确认框：点遮罩 = 取消；回车 = 确定（输入框场景回车提交）
$('askModal').addEventListener('click', function(e){ if(e.target===this) askResolve(false); });
$('askInput').addEventListener('keydown', function(e){
  if(e.key==='Enter'){ e.preventDefault(); askResolve(true); }
});
document.addEventListener('keydown', function(e){
  if(e.key==='Escape'){
    if(!$('askModal').classList.contains('hidden')){ askResolve(false); return; }
    closeEdit(); closeDel(); closeFiles(); closeAdd();
  }
});
</script>
</body>
</html>`
