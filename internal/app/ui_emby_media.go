package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (app *UIApp) handleEmbyStream(writer http.ResponseWriter, request *http.Request) {
	if !app.mediaResources().snapshot().Enabled {
		app.handleEmbyLegacyStream(writer, request)
		return
	}
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	if request.Method == http.MethodHead {
		writer.Header().Set("Content-Type", "application/octet-stream")
		return
	}
	owner := request.URL.Query().Get("account")
	app.playbackMu.Lock()
	var session *playbackSession
	for _, candidate := range app.playbacks {
		if candidate.viewer == nil && candidate.accountID == owner && candidate.mediaReady != nil && candidate.dramaID == id && len(candidate.tasks) == 1 && candidate.tasks[0].Chapter.ID == chapter && candidate.mediaError == nil && (candidate.media == nil || !candidate.media.failed.Load() && candidate.media.ctx.Err() == nil && (candidate.media.proxy == nil || candidate.media.proxy.Err() == nil)) {
			session = candidate
			break
		}
	}
	if session == nil {
		if len(app.playbacks) >= app.mediaResources().snapshot().MaxSessions {
			app.playbackMu.Unlock()
			writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "同时播放数量已达上限，请稍后再试"})
			return
		}
		sessionID := randomHex(24)
		if sessionID == "" {
			app.playbackMu.Unlock()
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		parent, cancel := context.WithCancel(context.Background())
		session = &playbackSession{id: sessionID, accountID: owner, dramaID: id, run: 1, state: "opening", mediaReady: make(chan struct{}), cancel: cancel,
			tasks: []Task{{DramaID: id, Chapter: Chapter{ID: chapter}}}, expires: time.Now().Add(playbackIdleTimeout)}
		if app.playbacks == nil {
			app.playbacks = make(map[string]*playbackSession)
		}
		app.playbacks[sessionID] = session
		session.timer = time.AfterFunc(playbackIdleTimeout, func() { app.expirePlayback(sessionID) })
		query := url.Values{"id": {id}, "chapter": {chapter}, "key": {request.URL.Query().Get("key")}}
		if owner != "" {
			query.Set("account", owner)
		}
		origin := request.Header.Get("Origin")
		go app.prepareEmbyMedia(parent, session, query.Encode(), origin)
	}
	session.mediaWaiters++
	app.touchPlaybackLocked(session)
	app.playbackMu.Unlock()
	defer func() {
		app.playbackMu.Lock()
		session.mediaWaiters--
		var cancel context.CancelFunc
		if session.mediaWaiters == 0 && session.state == "opening" && app.playbacks[session.id] == session {
			delete(app.playbacks, session.id)
			session.timer.Stop()
			cancel = session.cancel
		}
		app.playbackMu.Unlock()
		if cancel != nil {
			cancel()
		}
	}()
	select {
	case <-session.mediaReady:
	case <-request.Context().Done():
		return
	}
	app.playbackMu.Lock()
	media, err := session.media, session.mediaError
	app.playbackMu.Unlock()
	if err != nil || media == nil {
		if err == nil {
			err = errors.New("播放会话已结束")
		}
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "准备 Emby 播放失败：" + app.redactError(err)})
		return
	}
	if media.plan.Player == "legacy" {
		app.closePlayback(session.id)
		app.handleEmbyLegacyStream(writer, request)
		return
	}
	writer.Header().Set("Location", media.plan.URL)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusFound)
}

func (app *UIApp) prepareEmbyMedia(parent context.Context, session *playbackSession, query, origin string) {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	task, downloadID, err := app.embyTask(ctx, session.dramaID, session.tasks[0].Chapter.ID)
	var media *playbackMediaSession
	if err == nil {
		media, err = app.resolveMediaSession(parent, ctx, task, downloadID, 0)
	}
	if err == nil {
		base := "/api/emby/media/" + session.id + "/"
		err = app.planMedia(ctx, media, playbackClientCapabilities{MP4: true, NativeHLS: true}, "auto", sourceFromDramaID(task.DramaID), origin, base, query)
	}
	app.playbackMu.Lock()
	if err == nil && (app.playbacks[session.id] != session || ctx.Err() != nil) {
		err = context.Canceled
	}
	if err == nil {
		previous := session.cancel
		session.cancel = func() { previous(); media.Close() }
		session.tasks, session.media, session.state = []Task{task}, media, "streaming"
		session.duration = media.plan.Duration
		media.plan.Run = session.run
		app.touchPlaybackLocked(session)
	} else {
		session.mediaError, session.state = err, "failed"
	}
	close(session.mediaReady)
	app.playbackMu.Unlock()
	if err != nil {
		if media != nil {
			media.Close()
		}
		app.closePlayback(session.id)
	}
}

func (app *UIApp) handleEmbyMediaAsset(writer http.ResponseWriter, request *http.Request) {
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/api/emby/media/"), "/")
	if len(parts) != 2 {
		http.NotFound(writer, request)
		return
	}
	app.playbackMu.Lock()
	session := app.playbacks[parts[0]]
	if session == nil || session.viewer != nil || session.media == nil || session.accountID != request.URL.Query().Get("account") || len(session.tasks) != 1 || session.tasks[0].DramaID != id || session.tasks[0].Chapter.ID != chapter {
		app.playbackMu.Unlock()
		writer.WriteHeader(http.StatusGone)
		return
	}
	media := session.media
	active := &playbackRunContext{id: session.id, run: session.run, session: session, ctx: media.ctx}
	app.touchPlaybackLocked(session)
	app.playbackMu.Unlock()
	writer = &playbackActivityWriter{ResponseWriter: writer, app: app, run: active}
	app.serveMediaSession(writer, request, media)
}
