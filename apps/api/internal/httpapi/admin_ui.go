package httpapi

import (
	"bytes"
	_ "embed"
	"html/template"
	"log"
	"net/http"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

//go:embed adminui/admin.css
var adminCSS []byte

//go:embed adminui/admin.js
var adminJS []byte

const adminFavicon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><circle cx="16" cy="16" r="12.6" fill="none" stroke="#486a52" stroke-width="1.4"/><circle cx="16" cy="16" r="3.1" fill="#486a52"/></svg>`

type adminConsoleData struct {
	SignedIn bool
	CSRF     string
	Pending  store.PendingReview
	Total    int
}

var adminConsoleTemplate = template.Must(template.New("admin-console").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>审核 · Pondly</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="stylesheet" href="/admin/admin.css">
{{if .SignedIn}}<meta name="csrf-token" content="{{.CSRF}}">{{end}}
</head>
<body>
<header class="top">
  <a class="wordmark" href="/admin/">Pondly</a>
  <span class="tag">私有审核</span>
  {{if .SignedIn}}
  <span class="top-spacer"></span>
  <span class="session-label">管理员会话</span>
  <button type="button" id="sign-out" class="ghost">退出</button>
  {{end}}
</header>
<main>
{{if not .SignedIn}}
  <section class="signin" aria-labelledby="signin-heading">
    <h1 id="signin-heading">管理登录</h1>
    <p class="lead">仅限私网管理员入口。密码与 TOTP 双重验证。</p>
    <form id="sign-in" class="stack">
      <label>用户名<input name="username" autocomplete="username" required autofocus></label>
      <label>密码<input name="password" type="password" autocomplete="current-password" required></label>
      <label>TOTP 6 位代码<input name="totp_code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required></label>
      <button type="submit" class="primary">进入审核</button>
      <p id="error" class="error" role="alert"></p>
    </form>
  </section>
{{else}}
  <section class="queue" aria-labelledby="queue-heading">
    <div class="queue-head">
      <h1 id="queue-heading">待审核</h1>
      <span class="count" id="pending-count">{{.Total}} 条</span>
      <span class="top-spacer"></span>
      <button type="button" id="refresh" class="ghost">刷新</button>
    </div>
    <p class="notice" id="notice" role="status" hidden></p>
    {{if eq .Total 0}}<p class="empty" id="empty">没有待审内容。</p>{{end}}
    {{if .Pending.Posts}}
    <h2 class="group-title" data-group="posts">帖子 <span>{{len .Pending.Posts}}</span></h2>
    {{range .Pending.Posts}}
    <article class="item" data-kind="posts" data-id="{{.ID}}">
      <div class="meta">
        <span class="kind">帖子</span>
        <span class="author">{{if .AuthorDisplayName}}{{.AuthorDisplayName}}{{else}}{{.AuthorHandle}}{{end}} <span class="handle">@{{.AuthorHandle}}</span></span>
        {{if .CommunityName}}<span class="community">{{.CommunityName}}</span>{{end}}
        <time>{{.CreatedAt.Format "01-02 15:04"}}</time>
      </div>
      {{if .Title}}<h3>{{.Title}}</h3>{{end}}
      <p class="content">{{.Content}}</p>
      <div class="actions">
        <textarea rows="2" maxlength="500" placeholder="审核理由，至少 10 个字"></textarea>
        <div class="buttons">
          <button type="button" class="approve">通过</button>
          <button type="button" class="reject">拒绝</button>
        </div>
      </div>
      <p class="error" role="alert"></p>
    </article>
    {{end}}
    {{end}}
    {{if .Pending.Comments}}
    <h2 class="group-title" data-group="comments">评论 <span>{{len .Pending.Comments}}</span></h2>
    {{range .Pending.Comments}}
    <article class="item" data-kind="comments" data-id="{{.ID}}">
      <div class="meta">
        <span class="kind">评论</span>
        <span class="author">{{if .AuthorDisplayName}}{{.AuthorDisplayName}}{{else}}{{.AuthorHandle}}{{end}} <span class="handle">@{{.AuthorHandle}}</span></span>
        <span class="community">回复 {{if .PostTitle}}「{{.PostTitle}}」{{else}}帖子{{end}}</span>
        <time>{{.CreatedAt.Format "01-02 15:04"}}</time>
      </div>
      <p class="content">{{.Content}}</p>
      <div class="actions">
        <textarea rows="2" maxlength="500" placeholder="审核理由，至少 10 个字"></textarea>
        <div class="buttons">
          <button type="button" class="approve">通过</button>
          <button type="button" class="reject">拒绝</button>
        </div>
      </div>
      <p class="error" role="alert"></p>
    </article>
    {{end}}
    {{end}}
  </section>
{{end}}
</main>
<script src="/admin/admin.js" defer></script>
</body>
</html>
`))

func (a *AdminApp) adminConsole(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")

	data := adminConsoleData{}
	if a.store != nil {
		if session, ok := a.currentAdminSession(r); ok {
			csrf, err := auth.CSRFToken(session.ID, session.CSRFKey)
			if err != nil {
				fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
				return
			}
			pending, err := a.store.ListPendingReview(r.Context())
			if err != nil {
				failStore(w, r, err)
				return
			}
			data = adminConsoleData{
				SignedIn: true,
				CSRF:     csrf,
				Pending:  pending,
				Total:    len(pending.Posts) + len(pending.Comments),
			}
		}
	}
	var page bytes.Buffer
	if err := adminConsoleTemplate.Execute(&page, data); err != nil {
		log.Printf("admin console render: %v", err)
		http.Error(w, "console unavailable", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(page.Bytes())
}

func (a *AdminApp) adminAsset(contentType string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body)
	}
}
