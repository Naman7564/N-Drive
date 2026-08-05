package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

const landingHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<meta name="theme-color" content="#080d1b">
<meta name="description" content="N-Drive — a private, self-hosted file storage workspace. Your files, your server, your rules.">
<title>N-Drive · Private File Storage</title>
<style nonce="__NONCE__">
:root{color-scheme:dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;--bg:#080d1b;--panel:#10182b;--panel-2:#151f36;--panel-3:#1a2743;--line:#263653;--line-soft:#1d2a45;--text:#f4f7ff;--muted:#91a0bd;--faint:#60708e;--brand:#67d4ff;--brand-2:#8b7cff;--danger:#fb7185;--success:#4ade80;--shadow:0 24px 80px #0005;--glow:0 0 80px #67d4ff18}
*{box-sizing:border-box;margin:0;padding:0}
html{scroll-behavior:smooth}
html,body{min-height:100%;background:var(--bg);color:var(--text)}
body{background:radial-gradient(ellipse 90% 60% at 10% -10%,#1b3268 0,transparent 55%),radial-gradient(ellipse 70% 50% at 90% 5%,#35265e 0,transparent 50%),radial-gradient(ellipse 50% 40% at 50% 100%,#0f1e3a 0,transparent 60%),var(--bg);overflow-x:hidden}
a{color:inherit;text-decoration:none}
button,input{font:inherit;cursor:pointer}
img{display:block}

/* ── NAV ── */
.nav{position:fixed;top:0;left:0;right:0;z-index:100;padding:0 clamp(20px,5vw,80px);height:64px;display:flex;align-items:center;gap:20px;background:linear-gradient(to bottom,#080d1bcc,transparent);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)}
.nav-brand{display:flex;align-items:center;gap:10px;font-weight:800;letter-spacing:-.03em;font-size:18px;flex:1}
.brand-mark{display:grid;place-items:center;width:36px;height:36px;border-radius:11px;background:linear-gradient(135deg,var(--brand),#b7f1ff);color:#06101f;font-size:18px;font-weight:900;box-shadow:0 6px 20px #67d4ff30}
.nav-actions{display:flex;align-items:center;gap:10px}
.btn{display:inline-flex;align-items:center;gap:8px;border-radius:10px;padding:9px 18px;font-weight:700;font-size:14px;transition:.18s;border:1px solid transparent;white-space:nowrap}
.btn-ghost{background:transparent;border-color:var(--line);color:var(--muted)}
.btn-ghost:hover{border-color:var(--brand);color:var(--brand)}
.btn-primary{background:linear-gradient(135deg,var(--brand),#72a8ff);color:#06101f;box-shadow:0 8px 24px #67d4ff22}
.btn-primary:hover{filter:brightness(1.08);transform:translateY(-1px);box-shadow:0 12px 32px #67d4ff30}
.btn-lg{padding:14px 28px;font-size:15px;border-radius:12px}
.btn-outline{background:transparent;border-color:var(--line);color:var(--text)}
.btn-outline:hover{border-color:color-mix(in srgb,var(--brand) 60%,transparent);background:color-mix(in srgb,var(--brand) 6%,transparent);color:var(--brand)}

/* ── HERO ── */
.hero{min-height:100vh;display:grid;grid-template-columns:1fr 1fr;gap:clamp(40px,5vw,80px);align-items:center;padding:100px clamp(20px,5vw,80px) 48px;position:relative;overflow:hidden}
/* dot-grid texture */
.hero::before{content:'';position:absolute;inset:0;background-image:radial-gradient(circle,#ffffff09 1px,transparent 1px);background-size:28px 28px;mask-image:radial-gradient(ellipse 80% 80% at 50% 50%,black 40%,transparent 100%);pointer-events:none;z-index:0}
/* animated blobs */
.hero-blob{position:absolute;border-radius:50%;filter:blur(80px);pointer-events:none;z-index:0;will-change:transform}
.hero-blob-1{width:520px;height:520px;background:radial-gradient(circle,#1d4a9044 0,transparent 70%);top:-120px;left:-80px;animation:blob1 14s ease-in-out infinite}
.hero-blob-2{width:440px;height:440px;background:radial-gradient(circle,#3b216444 0,transparent 70%);bottom:-80px;right:-60px;animation:blob2 18s ease-in-out infinite}
.hero-blob-3{width:300px;height:300px;background:radial-gradient(circle,#67d4ff14 0,transparent 70%);top:40%;left:40%;animation:blob3 22s ease-in-out infinite}
@keyframes blob1{0%,100%{transform:translate(0,0) scale(1)}33%{transform:translate(40px,-30px) scale(1.06)}66%{transform:translate(-20px,20px) scale(.96)}}
@keyframes blob2{0%,100%{transform:translate(0,0) scale(1)}40%{transform:translate(-30px,20px) scale(1.08)}70%{transform:translate(20px,-30px) scale(.94)}}
@keyframes blob3{0%,100%{transform:translate(0,0) scale(1)}50%{transform:translate(-40px,30px) scale(1.1)}}
.hero-text{position:relative;z-index:1}
.hero-visual{position:relative;z-index:1}
.hero-eyebrow{display:inline-flex;align-items:center;gap:8px;border:1px solid color-mix(in srgb,var(--brand) 30%,transparent);background:color-mix(in srgb,var(--brand) 8%,transparent);color:var(--brand);border-radius:99px;padding:6px 14px;font-size:12px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;margin-bottom:20px}
.hero-eyebrow-dot{width:6px;height:6px;border-radius:50%;background:var(--brand);box-shadow:0 0 0 3px #67d4ff28;animation:pulse 2.4s ease-in-out infinite}
@keyframes pulse{0%,100%{box-shadow:0 0 0 3px #67d4ff28}50%{box-shadow:0 0 0 6px #67d4ff14}}
.hero h1{font-size:clamp(36px,4.5vw,68px);font-weight:900;letter-spacing:-.045em;line-height:1.06;margin-bottom:20px}
.hero h1 em{font-style:normal;background:linear-gradient(120deg,var(--brand),var(--brand-2));-webkit-background-clip:text;-webkit-text-fill-color:transparent;background-clip:text}
.hero-sub{font-size:clamp(14px,1.4vw,17px);color:var(--muted);line-height:1.7;margin-bottom:36px;max-width:460px}
.hero-actions{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:24px}
.hero-trust{display:flex;flex-direction:column;gap:8px;color:var(--faint);font-size:12px}
.hero-trust-item{display:flex;align-items:center;gap:7px}
.hero-trust-item span{color:var(--success);font-size:13px;flex:0 0 auto}

/* ── MOCKUP ── */
.hero-mockup{width:100%;position:relative}
.mockup-glow{position:absolute;inset:-60px;background:radial-gradient(ellipse at 40% 40%,#67d4ff12 0,transparent 65%),radial-gradient(ellipse at 70% 80%,#8b7cff0e 0,transparent 55%);pointer-events:none;z-index:0}
.mockup-frame{border:1px solid var(--line);border-radius:18px;background:var(--panel);box-shadow:0 32px 100px #00000090,0 0 0 1px #ffffff06,0 0 60px #67d4ff08;overflow:hidden;position:relative;z-index:1;animation:float 6s ease-in-out infinite}
@keyframes float{0%,100%{transform:translateY(0) rotate(0deg)}25%{transform:translateY(-8px) rotate(.3deg)}75%{transform:translateY(-4px) rotate(-.2deg)}}
.mockup-bar{height:40px;background:var(--panel-2);border-bottom:1px solid var(--line-soft);display:flex;align-items:center;gap:8px;padding:0 16px}
.mockup-dot{width:10px;height:10px;border-radius:50%}
.mockup-dot:nth-child(1){background:#ff5f57}
.mockup-dot:nth-child(2){background:#febc2e}
.mockup-dot:nth-child(3){background:#28c840}
.mockup-url{flex:1;background:var(--panel-3);border-radius:6px;height:22px;margin:0 12px;display:flex;align-items:center;padding:0 10px;font-size:11px;color:var(--faint)}
.mockup-body{display:flex;min-height:340px}
.mockup-sidebar{width:180px;flex:0 0 180px;border-right:1px solid var(--line-soft);padding:16px 10px;display:flex;flex-direction:column;gap:4px}
.mock-nav-item{border-radius:8px;padding:8px 10px;font-size:12px;display:flex;align-items:center;gap:8px;color:var(--muted)}
.mock-nav-item.active{background:linear-gradient(90deg,#67d4ff1f,#8b7cff18);border:1px solid var(--line);color:var(--text)}
.mock-nav-icon{width:16px;text-align:center;font-size:13px}
.mockup-main{flex:1;padding:20px}
.mock-topbar{display:flex;align-items:center;justify-content:space-between;margin-bottom:16px}
.mock-title{font-size:16px;font-weight:800;letter-spacing:-.03em}
.mock-actions{display:flex;gap:8px}
.mock-btn{height:28px;border-radius:7px;padding:0 12px;font-size:11px;font-weight:700;display:flex;align-items:center}
.mock-btn.primary{background:linear-gradient(135deg,var(--brand),#72a8ff);color:#06101f}
.mock-btn.secondary{border:1px solid var(--line);background:var(--panel-2);color:var(--text)}
.mock-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin-bottom:16px}
.mock-stat{border:1px solid var(--line);border-radius:10px;padding:10px;background:color-mix(in srgb,var(--panel) 90%,transparent)}
.mock-stat-label{font-size:9px;color:var(--muted);text-transform:uppercase;letter-spacing:.06em;margin-bottom:6px}
.mock-stat-val{font-size:18px;font-weight:800;letter-spacing:-.04em}
.mock-files{border:1px solid var(--line);border-radius:10px;overflow:hidden;background:color-mix(in srgb,var(--panel) 90%,transparent)}
.mock-file-head{display:grid;grid-template-columns:1fr 80px 80px 40px;padding:8px 12px;border-bottom:1px solid var(--line-soft);font-size:9px;text-transform:uppercase;letter-spacing:.07em;color:var(--faint)}
.mock-file-row{display:grid;grid-template-columns:1fr 80px 80px 40px;padding:9px 12px;border-bottom:1px solid var(--line-soft);font-size:11px;align-items:center}
.mock-file-row:last-child{border-bottom:0}
.mock-file-name{display:flex;align-items:center;gap:8px}
.mock-file-icon{width:24px;height:24px;border-radius:6px;display:grid;place-items:center;font-size:11px}
.mock-file-icon.img{background:#67d4ff15;color:var(--brand)}
.mock-file-icon.pdf{background:#fb718515;color:var(--danger)}
.mock-file-icon.folder{background:#fbbf2415;color:var(--warning)}
.mock-muted{color:var(--muted)}
.mock-more{color:var(--faint);font-size:14px;text-align:center}

/* ── FEATURES ── */
.section{padding:clamp(48px,7vw,80px) clamp(20px,5vw,80px)}
.section-label{font-size:12px;font-weight:700;letter-spacing:.1em;text-transform:uppercase;color:var(--brand);margin-bottom:14px}
.section-title{font-size:clamp(28px,4vw,48px);font-weight:900;letter-spacing:-.04em;line-height:1.1;margin-bottom:16px;max-width:600px}
.section-sub{font-size:16px;color:var(--muted);line-height:1.65;max-width:520px;margin-bottom:56px}
.features-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:16px}
.feat{border:1px solid var(--line);border-radius:18px;padding:28px;background:color-mix(in srgb,var(--panel) 85%,transparent);transition:.22s;position:relative;overflow:hidden}
.feat::before{content:'';position:absolute;inset:0;background:radial-gradient(ellipse 60% 50% at 0 0,var(--feat-glow,#67d4ff0a),transparent);pointer-events:none}
.feat:hover{border-color:color-mix(in srgb,var(--brand) 35%,transparent);background:color-mix(in srgb,var(--panel) 95%,transparent);transform:translateY(-3px)}
.feat-icon{width:44px;height:44px;border-radius:13px;display:grid;place-items:center;font-size:20px;margin-bottom:18px}
.feat-icon.blue{background:#67d4ff16;color:var(--brand)}
.feat-icon.purple{background:#8b7cff16;color:var(--brand-2)}
.feat-icon.green{background:#4ade8016;color:var(--success)}
.feat-icon.amber{background:#fbbf2416;color:var(--warning)}
.feat-icon.rose{background:#fb718516;color:var(--danger)}
.feat-icon.teal{background:#2dd4bf16;color:#2dd4bf}
.feat h3{font-size:16px;font-weight:750;letter-spacing:-.02em;margin-bottom:8px}
.feat p{font-size:13px;color:var(--muted);line-height:1.65}

/* ── DETAIL SECTION ── */
.detail{display:grid;grid-template-columns:1fr 1fr;gap:clamp(40px,6vw,80px);align-items:center}
.detail.flip{direction:rtl}
.detail.flip>*{direction:ltr}
.detail-text .section-label{margin-bottom:10px}
.detail-text .section-title{margin-bottom:14px;max-width:none}
.detail-text .section-sub{margin-bottom:28px}
.detail-list{display:grid;gap:12px}
.detail-item{display:flex;align-items:flex-start;gap:12px}
.detail-check{width:22px;height:22px;border-radius:6px;background:#4ade8016;color:var(--success);display:grid;place-items:center;font-size:13px;flex:0 0 auto;margin-top:1px}
.detail-item p{font-size:14px;color:var(--muted);line-height:1.6}
.detail-item strong{display:block;font-size:14px;color:var(--text);margin-bottom:2px;font-weight:650}
.detail-card{border:1px solid var(--line);border-radius:18px;background:var(--panel);padding:28px;box-shadow:var(--shadow)}
.detail-card-title{font-size:13px;font-weight:700;color:var(--muted);margin-bottom:16px;display:flex;align-items:center;gap:8px}
.storage-bar-wrap{background:var(--panel-2);border:1px solid var(--line);border-radius:12px;padding:16px}
.storage-bar-head{display:flex;justify-content:space-between;font-size:12px;margin-bottom:10px;color:var(--muted)}
.storage-bar-head b{color:var(--text)}
.storage-bar{height:8px;border-radius:99px;background:var(--line);overflow:hidden;margin-bottom:8px}
.storage-bar-fill{height:100%;width:62%;background:linear-gradient(90deg,var(--brand),var(--brand-2));border-radius:inherit}
.storage-bar-note{font-size:11px;color:var(--faint)}
.file-list-mock{margin-top:14px;display:grid;gap:2px}
.flm-row{display:flex;align-items:center;gap:10px;padding:9px 10px;border-radius:9px;font-size:12px;transition:.12s}
.flm-row:hover{background:var(--panel-2)}
.flm-icon{width:28px;height:28px;border-radius:7px;display:grid;place-items:center;font-size:12px;flex:0 0 auto}
.flm-name{flex:1;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.flm-meta{color:var(--faint);font-size:11px;white-space:nowrap}
.flm-check{color:var(--success);font-size:13px}
.upload-mock{margin-top:14px}
.upload-zone{border:1.5px dashed color-mix(in srgb,var(--brand) 45%,transparent);border-radius:12px;padding:20px;text-align:center;background:color-mix(in srgb,var(--brand) 5%,transparent);margin-bottom:12px}
.upload-zone-label{color:var(--brand);font-weight:700;font-size:13px;margin-bottom:4px}
.upload-zone-sub{color:var(--muted);font-size:11px}
.upload-progress-row{display:flex;align-items:center;gap:10px;padding:8px 10px;background:var(--panel-2);border-radius:9px;font-size:12px}
.upr-name{flex:1;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.upr-bar{flex:0 0 80px;height:4px;border-radius:99px;background:var(--line);overflow:hidden}
.upr-bar-fill{height:100%;background:linear-gradient(90deg,var(--brand),var(--brand-2));border-radius:inherit}
.upr-pct{color:var(--brand);font-size:11px;font-weight:700;white-space:nowrap}
.upr-done{color:var(--success)}

/* ── STATS BANNER ── */
.stats-banner{padding:clamp(32px,5vw,64px) clamp(20px,5vw,80px);text-align:center}
.stats-inner{border:1px solid var(--line);border-radius:24px;background:color-mix(in srgb,var(--panel) 85%,transparent);padding:clamp(32px,5vw,64px);display:grid;grid-template-columns:repeat(3,1fr);gap:32px;position:relative;overflow:hidden}
.stats-inner::before{content:'';position:absolute;inset:0;background:radial-gradient(ellipse 60% 80% at 50% 0,#67d4ff08,transparent 60%);pointer-events:none}
.stat-big{position:relative}
.stat-big-val{font-size:clamp(36px,5vw,64px);font-weight:900;letter-spacing:-.05em;background:linear-gradient(135deg,var(--brand),var(--brand-2));-webkit-background-clip:text;-webkit-text-fill-color:transparent;background-clip:text;line-height:1}
.stat-big-label{font-size:14px;color:var(--muted);margin-top:8px}
.stat-big-divider{position:absolute;right:0;top:10%;bottom:10%;width:1px;background:var(--line)}

/* ── CTA ── */
.cta{padding:clamp(56px,8vw,96px) clamp(20px,5vw,80px);text-align:center}
.cta-inner{max-width:640px;margin:0 auto}
.cta h2{font-size:clamp(32px,5vw,56px);font-weight:900;letter-spacing:-.04em;line-height:1.08;margin-bottom:20px}
.cta h2 em{font-style:normal;background:linear-gradient(120deg,var(--brand),var(--brand-2));-webkit-background-clip:text;-webkit-text-fill-color:transparent;background-clip:text}
.cta p{font-size:17px;color:var(--muted);line-height:1.65;margin-bottom:36px}
.cta-actions{display:flex;align-items:center;gap:12px;justify-content:center;flex-wrap:wrap}
.cta-note{margin-top:20px;font-size:12px;color:var(--faint)}

/* ── FOOTER ── */
footer{border-top:1px solid var(--line-soft);padding:clamp(24px,4vw,40px) clamp(20px,5vw,80px);display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap;color:var(--faint);font-size:12px}
.footer-brand{display:flex;align-items:center;gap:8px;font-weight:700;color:var(--muted);font-size:13px}
.footer-links{display:flex;gap:20px}
.footer-links a{color:var(--faint);transition:.15s}
.footer-links a:hover{color:var(--muted)}

/* ── RESPONSIVE ── */
@media(max-width:900px){
  .features-grid{grid-template-columns:repeat(2,1fr)}
  .hero{grid-template-columns:1fr;text-align:center;padding-top:90px}
  .hero h1{font-size:clamp(36px,7vw,56px)}
  .hero-sub{max-width:100%}
  .hero-actions{justify-content:center}
  .hero-trust{flex-direction:row;flex-wrap:wrap;justify-content:center;gap:12px}
  .detail{grid-template-columns:1fr}
  .detail.flip{direction:ltr}
  .mockup-sidebar{display:none}
  .mock-stats{grid-template-columns:repeat(2,1fr)}
  .stats-inner{grid-template-columns:1fr;gap:24px}
  .stat-big-divider{display:none}
}
@media(max-width:600px){
  .features-grid{grid-template-columns:1fr}
  .nav-actions .btn-ghost{display:none}
  .hero h1{font-size:38px}
  .hero-trust{gap:12px}
  .mock-stats{grid-template-columns:repeat(2,1fr)}
  .mockup-main{padding:14px}
}
@media(prefers-reduced-motion:no-preference){
  .hero-text>*{animation:rise .5s cubic-bezier(0,.7,.3,1) both}
  .hero-text .hero-eyebrow{animation-delay:0s}
  .hero-text h1{animation-delay:.07s}
  .hero-text .hero-sub{animation-delay:.14s}
  .hero-text .hero-actions{animation-delay:.2s}
  .hero-text .hero-trust{animation-delay:.26s}
  .hero-visual{animation:rise .7s .32s cubic-bezier(0,.7,.3,1) both}
  @keyframes rise{from{opacity:0;transform:translateY(12px)}to{opacity:1;transform:none}}
}
@media(prefers-reduced-motion:reduce){
  .hero-blob,.mockup-frame,.hero-eyebrow-dot{animation:none}
}
/* ── SCROLL REVEAL ── */
.reveal{opacity:0;transform:translateY(20px);transition:opacity .55s cubic-bezier(0,.7,.3,1),transform .55s cubic-bezier(0,.7,.3,1)}
.reveal.visible{opacity:1;transform:none}
.reveal-delay-1{transition-delay:.08s}
.reveal-delay-2{transition-delay:.16s}
.reveal-delay-3{transition-delay:.24s}
.reveal-delay-4{transition-delay:.32s}
.reveal-delay-5{transition-delay:.40s}
.reveal-delay-6{transition-delay:.48s}
@media(prefers-reduced-motion:reduce){.reveal{opacity:1;transform:none;transition:none}}
</style>
</head>
<body>

<nav class="nav">
  <div class="nav-brand">
    <span class="brand-mark">✦</span>
    <span>N-Drive</span>
  </div>
  <div class="nav-actions">
    <a href="/app" class="btn btn-ghost">Sign in</a>
    <a href="/app" class="btn btn-primary">Open workspace →</a>
  </div>
</nav>

<!-- HERO -->
<section class="hero">
  <!-- Background elements -->
  <div class="hero-blob hero-blob-1"></div>
  <div class="hero-blob hero-blob-2"></div>
  <div class="hero-blob hero-blob-3"></div>

  <!-- Left: text -->
  <div class="hero-text">
    <div class="hero-eyebrow">
      <span class="hero-eyebrow-dot"></span>
      Private · Self-hosted · Yours
    </div>
    <h1>Your files.<br><em>Your server.</em><br>Your rules.</h1>
    <p class="hero-sub">N-Drive is a private, self-hosted file workspace. No subscriptions, no third-party access, no telemetry — just your files running on your own server.</p>
    <div class="hero-actions">
      <a href="/app" class="btn btn-primary btn-lg">Open workspace →</a>
      <a href="/app" class="btn btn-outline btn-lg">Sign in</a>
    </div>
    <div class="hero-trust">
      <div class="hero-trust-item"><span>✓</span> No cloud dependency</div>
      <div class="hero-trust-item"><span>✓</span> Checksum verified uploads</div>
      <div class="hero-trust-item"><span>✓</span> JWT session protection</div>
      <div class="hero-trust-item"><span>✓</span> Up to 5 GB per file</div>
    </div>
  </div>

  <!-- Right: floating mockup -->
  <div class="hero-visual">
    <div class="hero-mockup">
      <div class="mockup-glow"></div>
      <div class="mockup-frame">
        <div class="mockup-bar">
          <div class="mockup-dot"></div>
          <div class="mockup-dot"></div>
          <div class="mockup-dot"></div>
          <div class="mockup-url">🔒 your-server.local:8080</div>
        </div>
        <div class="mockup-body">
          <div class="mockup-sidebar">
            <div class="mock-nav-item active"><span class="mock-nav-icon">▦</span>Overview</div>
            <div class="mock-nav-item"><span class="mock-nav-icon">▰</span>My files</div>
            <div class="mock-nav-item"><span class="mock-nav-icon">♲</span>Trash</div>
          </div>
          <div class="mockup-main">
            <div class="mock-topbar">
              <span class="mock-title">Overview</span>
              <div class="mock-actions">
                <div class="mock-btn secondary">＋ New folder</div>
                <div class="mock-btn primary">↑ Upload</div>
              </div>
            </div>
            <div class="mock-stats">
              <div class="mock-stat"><div class="mock-stat-label">Files</div><div class="mock-stat-val">284</div></div>
              <div class="mock-stat"><div class="mock-stat-label">Folders</div><div class="mock-stat-val">32</div></div>
              <div class="mock-stat"><div class="mock-stat-label">Storage</div><div class="mock-stat-val">18 GB</div></div>
              <div class="mock-stat"><div class="mock-stat-label">Trash</div><div class="mock-stat-val">3</div></div>
            </div>
            <div class="mock-files">
              <div class="mock-file-head"><span>Name</span><span>Type</span><span>Modified</span><span></span></div>
              <div class="mock-file-row">
                <div class="mock-file-name"><div class="mock-file-icon folder">▰</div><span>Design assets</span></div>
                <span class="mock-muted">Folder</span><span class="mock-muted">Aug 5</span><span class="mock-more">⋯</span>
              </div>
              <div class="mock-file-row">
                <div class="mock-file-name"><div class="mock-file-icon img">▧</div><span>hero-banner.png</span></div>
                <span class="mock-muted">image/png</span><span class="mock-muted">Aug 4</span><span class="mock-more">⋯</span>
              </div>
              <div class="mock-file-row">
                <div class="mock-file-name"><div class="mock-file-icon pdf">▥</div><span>brief-v2.pdf</span></div>
                <span class="mock-muted">application/pdf</span><span class="mock-muted">Aug 3</span><span class="mock-more">⋯</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</section>

<!-- FEATURES -->
<section class="section">
  <div class="section-label">Everything you need</div>
  <h2 class="section-title">Built for control, not convenience theatre</h2>
  <p class="section-sub">N-Drive keeps it simple: upload, organize, download. No bloat. No SaaS lock-in. Just your files on your machine.</p>
  <div class="features-grid">
    <div class="feat reveal reveal-delay-1" style="--feat-glow:#67d4ff0a">
      <div class="feat-icon blue">🔒</div>
      <h3>Private by design</h3>
      <p>All data stays on your own server. No third-party access, no analytics, no call-home. Your workspace is truly yours.</p>
    </div>
    <div class="feat reveal reveal-delay-2" style="--feat-glow:#8b7cff0a">
      <div class="feat-icon purple">↑</div>
      <h3>Fast file uploads</h3>
      <p>Drag-and-drop or file picker. Real-time progress per file, up to 5 GB each. Multiple files upload sequentially with live status.</p>
    </div>
    <div class="feat reveal reveal-delay-3" style="--feat-glow:#4ade800a">
      <div class="feat-icon green">✓</div>
      <h3>Checksum verified</h3>
      <p>Every file is integrity-checked on upload. You know exactly what arrived on disk — corrupted transfers are caught before they land.</p>
    </div>
    <div class="feat reveal reveal-delay-4" style="--feat-glow:#fbbf240a">
      <div class="feat-icon amber">▰</div>
      <h3>Folder organisation</h3>
      <p>Create nested folders, move files between them, rename anything. Breadcrumb navigation keeps you oriented at any depth.</p>
    </div>
    <div class="feat reveal reveal-delay-5" style="--feat-glow:#67d4ff0a">
      <div class="feat-icon blue">◓</div>
      <h3>Storage tracking</h3>
      <p>Live disk usage meter in the sidebar. See exactly how much space is used, free, and total — no surprises.</p>
    </div>
    <div class="feat reveal reveal-delay-6" style="--feat-glow:#fb71850a">
      <div class="feat-icon rose">♲</div>
      <h3>Trash & recovery</h3>
      <p>Deleted files go to Trash first. Restore with one click or purge permanently when you're sure. No accidental data loss.</p>
    </div>
  </div>
</section>

<!-- DETAIL 1: Upload -->
<section class="section" style="padding-top:0">
  <div class="detail reveal">
    <div class="detail-text">
      <div class="section-label">File management</div>
      <h2 class="section-title">Upload anything. Find it instantly.</h2>
      <p class="section-sub">Images, PDFs, videos, archives — N-Drive handles any file type. Search across your entire workspace in real time.</p>
      <div class="detail-list">
        <div class="detail-item">
          <div class="detail-check">✓</div>
          <div><strong>Drag-and-drop upload</strong><p>Drop files directly onto the upload zone. Progress bars and per-file status keep you informed.</p></div>
        </div>
        <div class="detail-item">
          <div class="detail-check">✓</div>
          <div><strong>Real-time search</strong><p>Instant search across all your files and folders — results appear as you type.</p></div>
        </div>
        <div class="detail-item">
          <div class="detail-check">✓</div>
          <div><strong>Download anytime</strong><p>One-click downloads with a progress overlay. Large files stream smoothly with live speed readout.</p></div>
        </div>
      </div>
    </div>
    <div class="detail-card">
      <div class="detail-card-title">↑ Upload files</div>
      <div class="upload-mock">
        <div class="upload-zone">
          <div class="upload-zone-label">Choose files or drag and drop</div>
          <div class="upload-zone-sub">Up to 5 GB each · Any file type</div>
        </div>
        <div style="display:grid;gap:6px">
          <div class="upload-progress-row">
            <span class="upr-name">design-system.fig</span>
            <div class="upr-bar"><div class="upr-bar-fill" style="width:100%"></div></div>
            <span class="upr-done">✓</span>
          </div>
          <div class="upload-progress-row">
            <span class="upr-name">brand-assets.zip</span>
            <div class="upr-bar"><div class="upr-bar-fill" style="width:68%"></div></div>
            <span class="upr-pct">68%</span>
          </div>
          <div class="upload-progress-row">
            <span class="upr-name">presentation.mp4</span>
            <div class="upr-bar"><div class="upr-bar-fill" style="width:0%"></div></div>
            <span class="upr-pct" style="color:var(--faint)">—</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</section>

<!-- DETAIL 2: Storage -->
<section class="section" style="padding-top:0">
  <div class="detail flip reveal">
    <div class="detail-text">
      <div class="section-label">Storage & security</div>
      <h2 class="section-title">Know exactly what's on your drive</h2>
      <p class="section-sub">Live storage tracking, JWT-protected sessions, CSRF-hardened API, and checksum verification on every file.</p>
      <div class="detail-list">
        <div class="detail-item">
          <div class="detail-check">✓</div>
          <div><strong>Live storage meter</strong><p>See used, free, and total disk space at a glance in the sidebar — always up to date.</p></div>
        </div>
        <div class="detail-item">
          <div class="detail-check">✓</div>
          <div><strong>JWT + refresh tokens</strong><p>Short-lived access tokens with silent refresh. Sessions expire cleanly. No cookies storing credentials.</p></div>
        </div>
        <div class="detail-item">
          <div class="detail-check">✓</div>
          <div><strong>CSRF protection</strong><p>Every mutating API call requires a verified CSRF token. No cross-site request forgery vectors.</p></div>
        </div>
      </div>
    </div>
    <div class="detail-card">
      <div class="detail-card-title">◓ Server storage</div>
      <div class="storage-bar-wrap">
        <div class="storage-bar-head"><span>Storage used</span><b>62%</b></div>
        <div class="storage-bar"><div class="storage-bar-fill"></div></div>
        <div class="storage-bar-note">18.4 GB used · 11.6 GB free · 30 GB total</div>
      </div>
      <div class="file-list-mock">
        <div class="flm-row">
          <div class="flm-icon img" style="background:#67d4ff15;color:var(--brand)">▧</div>
          <span class="flm-name">hero-banner.png</span>
          <span class="flm-meta">4.2 MB</span>
          <span class="flm-check">✓</span>
        </div>
        <div class="flm-row">
          <div class="flm-icon pdf" style="background:#fb718515;color:var(--danger)">▥</div>
          <span class="flm-name">contract-final.pdf</span>
          <span class="flm-meta">892 KB</span>
          <span class="flm-check">✓</span>
        </div>
        <div class="flm-row">
          <div class="flm-icon folder" style="background:#fbbf2415;color:var(--warning)">▰</div>
          <span class="flm-name">Project assets</span>
          <span class="flm-meta">Folder</span>
          <span class="flm-check">✓</span>
        </div>
        <div class="flm-row">
          <div class="flm-icon" style="background:#8b7cff15;color:var(--brand-2);width:28px;height:28px;border-radius:6px;display:grid;place-items:center;font-size:11px">▤</div>
          <span class="flm-name">notes-q3.txt</span>
          <span class="flm-meta">14 KB</span>
          <span class="flm-check">✓</span>
        </div>
      </div>
    </div>
  </div>
</section>

<!-- STATS -->
<div class="stats-banner">
  <div class="stats-inner reveal">
    <div class="stat-big">
      <div class="stat-big-val">5 GB</div>
      <div class="stat-big-label">Max file size per upload</div>
      <div class="stat-big-divider"></div>
    </div>
    <div class="stat-big">
      <div class="stat-big-val">100%</div>
      <div class="stat-big-label">Private — zero telemetry</div>
      <div class="stat-big-divider"></div>
    </div>
    <div class="stat-big">
      <div class="stat-big-val">Self-hosted</div>
      <div class="stat-big-label">Your server, your data, forever</div>
    </div>
  </div>
</div>

<!-- CTA -->
<section class="cta">
  <div class="cta-inner reveal">
    <h2>Ready to take back<br><em>your files?</em></h2>
    <p>Sign in to your private workspace. Everything runs on your own server — no accounts to create, no SaaS to trust.</p>
    <div class="cta-actions">
      <a href="/app" class="btn btn-primary btn-lg">Open workspace →</a>
      <a href="/app" class="btn btn-outline btn-lg">Sign in</a>
    </div>
    <div class="cta-note">Running on your server · Private by design · No third-party access</div>
  </div>
</section>

<footer>
  <div class="footer-brand">
    <span class="brand-mark" style="width:26px;height:26px;border-radius:8px;font-size:13px">✦</span>
    N-Drive
  </div>
  <div class="footer-links">
    <a href="/app">Workspace</a>
    <a href="/health">Health</a>
  </div>
  <div>Private · Self-hosted · Yours</div>
</footer>

<script nonce="__NONCE__">
(function(){
  // Scroll-reveal via IntersectionObserver
  var els=document.querySelectorAll('.reveal');
  if(!els.length)return;
  var io=new IntersectionObserver(function(entries){
    entries.forEach(function(e){
      if(e.isIntersecting){e.target.classList.add('visible');io.unobserve(e.target)}
    });
  },{threshold:0.12,rootMargin:'0px 0px -40px 0px'});
  els.forEach(function(el){io.observe(el)});

  // Nav: add solid bg on scroll
  var nav=document.querySelector('.nav');
  if(nav){
    window.addEventListener('scroll',function(){
      nav.style.background=window.scrollY>40
        ?'color-mix(in srgb,#080d1b 92%,transparent)'
        :'';
    },{passive:true});
  }
})();
</script>
</body>
</html>`

func webLanding(w http.ResponseWriter, r *http.Request) {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	nonce := base64.RawURLEncoding.EncodeToString(raw[:])
	page := strings.ReplaceAll(landingHTML, "__NONCE__", nonce)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+nonce+"'; script-src 'nonce-"+nonce+"'; frame-ancestors 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}
