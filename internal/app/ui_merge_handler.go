package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (a *UIApp) handleMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := ensureDiskSpace(a.cfg.OutputDir); err != nil {
		writeJSON(w, http.StatusInsufficientStorage, map[string]string{"error": err.Error()})
		return
	}
	ids, deleteEpisodes, ok := readMergeRequest(w, r)
	if !ok {
		return
	}

	if !a.requireDramaSources(w, r, ids) {
		return
	}
	a.mergeMu.Lock()
	defer a.mergeMu.Unlock()

	a.mu.Lock()
	if !a.requireTaskSourcesLocked(w, r, taskSelection{DramaIDs: ids}) {
		a.mu.Unlock()
		return
	}
	byDrama := map[string][]*UITask{}
	for _, id := range a.taskOrder {
		task := a.tasks[id]
		if task == nil || task.Status != uiStatusSuccess {
			continue
		}
		byDrama[task.DramaID] = append(byDrama[task.DramaID], cloneUITask(task))
	}
	a.mu.Unlock()

	results := make([]uiMergeResult, 0, len(ids))
	for _, dramaID := range ids {
		items := byDrama[dramaID]
		res := a.mergeDrama(r.Context(), dramaID, items, deleteEpisodes)
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": results})
}

func (a *UIApp) mergeDrama(ctx context.Context, dramaID string, tasks []*UITask, deleteEpisodes bool) uiMergeResult {
	res := uiMergeResult{DramaID: dramaID}
	if len(tasks) == 0 {
		res.Error = "没有已完成的分集可合并"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Source.Index < tasks[j].Source.Index })
	valid := make([]*UITask, 0, len(tasks))
	seenEp := map[int]bool{}
	startEp, endEp := 0, 0
	for _, task := range tasks {
		if task == nil || task.Path == "" {
			continue
		}
		ep := task.Source.Index
		if ep <= 0 {
			ep = episodeNumber(task.Episode)
		}
		if ep <= 0 || seenEp[ep] {
			continue
		}
		if st, err := os.Stat(task.Path); err == nil && !st.IsDir() && st.Size() > 0 {
			seenEp[ep] = true
			valid = append(valid, task)
			if startEp == 0 || ep < startEp {
				startEp = ep
			}
			if ep > endEp {
				endEp = ep
			}
		}
	}
	if len(valid) == 0 {
		res.Error = "已完成任务的文件不存在"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	for ep := startEp; ep <= endEp; ep++ {
		if !seenEp[ep] {
			res.DramaTitle = valid[0].DramaTitle
			res.StartEpisode = startEp
			res.EndEpisode = endEp
			res.Merged = len(valid)
			res.Error = fmt.Sprintf("缺少第%03d集，未执行合并", ep)
			a.setMergeState(res, "failed", 0, false, deleteEpisodes)
			return res
		}
	}
	res.DramaTitle = valid[0].DramaTitle
	res.Merged = len(valid)
	res.StartEpisode = startEp
	res.EndEpisode = endEp
	dir := filepath.Dir(valid[0].Path)
	outPath := filepath.Join(dir, mergeOutputName(res.DramaTitle, startEp, endEp))
	res.OutputPath = outPath
	if ok, _ := existingGood(outPath, a.cfg.SkipBytes); ok {
		res.OK = true
		res.Skipped = true
		a.setMergeState(res, "success", 100, true, deleteEpisodes)
		return res
	}

	paths := make([]string, 0, len(valid))
	for _, task := range valid {
		paths = append(paths, task.Path)
	}
	partPath := filepath.Join(dir, "."+strings.TrimSuffix(filepath.Base(outPath), filepath.Ext(outPath))+".part.mp4")
	_ = os.Remove(partPath)
	defer os.Remove(partPath)
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	res.Detail = "正在准备 FFmpeg"
	a.setMergeState(res, "running", 0, false, deleteEpisodes)
	ffmpeg, err := a.downloader.ensureFFmpeg(cmdCtx)
	if err != nil {
		res.Error = a.redactError(err)
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	method, err := mergeMediaFiles(cmdCtx, ffmpeg, paths, partPath, func(progress int, detail string) {
		res.Detail = detail
		a.setMergeState(res, "running", progress, false, deleteEpisodes)
	})
	if err != nil {
		res.Error = a.redactError(err)
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	res.Detail = method
	if ok, _ := existingGood(partPath, a.cfg.SkipBytes); !ok {
		res.Error = "合并输出文件无效"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	if err := os.Rename(partPath, outPath); err != nil {
		res.Error = a.redactError(err)
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	res.OK = true
	if deleteEpisodes {
		for _, task := range valid {
			_ = os.Remove(task.Path)
		}
	}
	a.setMergeState(res, "success", 100, false, deleteEpisodes)
	return res
}

func cloneMergeStates(in map[string]*UIMergeState) map[string]*UIMergeState {
	out := make(map[string]*UIMergeState, len(in))
	for id, state := range in {
		if state == nil {
			continue
		}
		copyState := *state
		out[id] = &copyState
	}
	return out
}

func mergeOutputName(title string, start, end int) string {
	if start <= 0 {
		start = 1
	}
	if end < start {
		end = start
	}
	return fmt.Sprintf("%s%d-%d.mp4", safeFilename(title), start, end)
}

func (a *UIApp) setMergeState(res uiMergeResult, status string, progress int, skipped, deleteEpisodes bool) {
	state := &UIMergeState{
		DramaID: res.DramaID, DramaTitle: res.DramaTitle, Status: status, Progress: progress,
		Merged: res.Merged, StartEpisode: res.StartEpisode, EndEpisode: res.EndEpisode,
		Total: res.Merged, OutputPath: res.OutputPath, Detail: res.Detail, Error: res.Error, Skipped: skipped,
		DeleteEpisodes: deleteEpisodes, UpdatedAt: time.Now(),
	}
	a.mu.Lock()
	if a.merges == nil {
		a.merges = map[string]*UIMergeState{}
	}
	a.merges[res.DramaID] = state
	_ = a.saveStateLocked()
	a.mu.Unlock()
}

func readMergeRequest(w http.ResponseWriter, r *http.Request) ([]string, bool, bool) {
	var req struct {
		DramaIDs       []string `json:"dramaIds"`
		DeleteEpisodes bool     `json:"deleteEpisodes"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return nil, false, false
	}
	ids, ok := cleanIDList(w, req.DramaIDs)
	return ids, req.DeleteEpisodes, ok
}
