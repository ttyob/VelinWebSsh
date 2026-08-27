package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"velin-webssh/internal/store"
)

const maxRecordingUploadBytes int64 = 1 << 30

type commandTaskRequest struct {
	UserID string
	TaskID string
}

func (a *API) commandTaskWorker() {
	for request := range a.taskQueue {
		value, err := a.store.CommandTask(request.UserID, request.TaskID)
		if err != nil {
			continue
		}
		started := time.Now().UTC()
		_ = a.store.UpdateCommandTask(request.UserID, request.TaskID, "running", "", "", &started, nil)
		var output []string
		var failures []string
		command := strings.TrimRight(value.Command, "\r\n") + "\r"
		for _, sessionID := range value.SessionIDs {
			session, getErr := a.terminals.Get(request.UserID, sessionID)
			if getErr != nil {
				failures = append(failures, sessionID+": session unavailable")
				continue
			}
			if writeErr := session.WriteTask([]byte(command)); writeErr != nil {
				failures = append(failures, sessionID+": "+writeErr.Error())
				continue
			}
			output = append(output, sessionID+": sent")
		}
		finished := time.Now().UTC()
		status := "completed"
		message := ""
		if len(failures) > 0 {
			status = "failed"
			message = strings.Join(failures, "\n")
		}
		_ = a.store.UpdateCommandTask(request.UserID, request.TaskID, status, strings.Join(output, "\n"), message, &started, &finished)
	}
}

func (a *API) tasks(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.CommandTasks(currentUser(r).ID)
	if err != nil {
		writeError(w, 500, "database_error", "任务读取失败")
		return
	}
	writeJSON(w, 200, nonNil(items))
}

func (a *API) task(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.CommandTask(currentUser(r).ID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 404, "task_not_found", "任务不存在")
		return
	}
	writeJSON(w, 200, item)
}

func (a *API) createTask(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var input struct {
		Command    string   `json:"command"`
		SessionIDs []string `json:"sessionIDs"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Command = strings.TrimSpace(input.Command)
	if input.Command == "" || len([]rune(input.Command)) > 16000 {
		writeError(w, 400, "invalid_task", "命令不能为空且不能超过 16000 个字符")
		return
	}
	if len(input.SessionIDs) == 0 || len(input.SessionIDs) > 50 {
		writeError(w, 400, "invalid_task_targets", "请选择 1 到 50 个终端")
		return
	}
	seen := make(map[string]bool, len(input.SessionIDs))
	for _, id := range input.SessionIDs {
		if id == "" || seen[id] {
			writeError(w, 400, "invalid_task_targets", "任务目标无效")
			return
		}
		seen[id] = true
		if _, err := a.terminals.Get(user.ID, id); err != nil {
			writeError(w, 404, "session_not_attached", "任务目标终端未连接")
			return
		}
	}
	item := store.CommandTask{ID: uuid.NewString(), UserID: user.ID, Command: input.Command, SessionIDs: input.SessionIDs, Status: "queued", CreatedAt: time.Now().UTC()}
	if err := a.store.CreateCommandTask(item); err != nil {
		writeError(w, 500, "database_error", "任务创建失败")
		return
	}
	a.taskQueue <- commandTaskRequest{UserID: user.ID, TaskID: item.ID}
	writeJSON(w, 201, item)
}

func (a *API) recordings(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.Recordings(currentUser(r).ID)
	if err != nil {
		writeError(w, 500, "database_error", "录制记录读取失败")
		return
	}
	for i := range items {
		items[i].Path = ""
	}
	writeJSON(w, 200, nonNil(items))
}

func (a *API) startRecording(w http.ResponseWriter, r *http.Request) {
	value, err := a.terminals.StartRecording(currentUser(r).ID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 409, "recording_start_failed", err.Error())
		return
	}
	writeJSON(w, 201, value)
}

func (a *API) stopRecording(w http.ResponseWriter, r *http.Request) {
	userID, sessionID := currentUser(r).ID, chi.URLParam(r, "id")
	value, err := a.terminals.StopRecording(userID, sessionID)
	if err != nil {
		writeError(w, 404, "recording_not_found", "会话不存在")
		return
	}
	if value.ID == "" {
		writeError(w, 409, "recording_not_active", "当前没有进行中的录制")
		return
	}
	writeJSON(w, 200, value)
}

func (a *API) uploadRecording(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxRecordingUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "recording_too_large", "录制文件不能超过 1 GB")
		return
	}
	value, err := a.terminals.UploadRecording(
		r.Context(),
		currentUser(r).ID,
		chi.URLParam(r, "id"),
		http.MaxBytesReader(w, r.Body, maxRecordingUploadBytes+1),
	)
	if err != nil {
		message := err.Error()
		status := http.StatusConflict
		if strings.Contains(message, "MP4 转码失败") || strings.Contains(message, "ffmpeg") {
			status = http.StatusInternalServerError
		}
		writeError(w, status, "recording_upload_failed", message)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (a *API) downloadRecording(w http.ResponseWriter, r *http.Request) {
	value, err := a.store.Recording(currentUser(r).ID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 404, "recording_not_found", "录制不存在")
		return
	}
	if value.Status == "recording" {
		writeError(w, 409, "recording_active", "请先停止录制")
		return
	}
	name := value.SessionName + "-" + value.ID + ".mp4"
	contentType := "video/mp4"
	if strings.HasSuffix(strings.ToLower(value.Path), ".cast") {
		name = value.SessionName + "-" + value.ID + ".cast"
		contentType = "application/octet-stream"
	}
	servePrivateFile(w, r, value.Path, name, contentType)
}

func safeBackupFile(dataDir, name string) (string, error) {
	name = filepath.Base(name)
	if !strings.HasPrefix(name, "velin-") || !strings.HasSuffix(name, ".db") {
		return "", errors.New("invalid backup name")
	}
	path := filepath.Join(dataDir, name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", errors.New("backup not found")
	}
	return path, nil
}

func servePrivateFile(w http.ResponseWriter, r *http.Request, path, name, contentType string) {
	file, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "file_not_found", "文件不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, 404, "file_not_found", "文件不存在")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(filepath.Base(name), `"`, "")+`"`)
	http.ServeContent(w, r, filepath.Base(name), info.ModTime(), file)
}

func (a *API) downloadBackup(w http.ResponseWriter, r *http.Request) {
	path, err := safeBackupFile(a.cfg.DataDir, chi.URLParam(r, "file"))
	if err != nil {
		writeError(w, 404, "backup_not_found", "备份不存在")
		return
	}
	servePrivateFile(w, r, path, filepath.Base(path), "application/octet-stream")
}

func (a *API) restoreBackup(w http.ResponseWriter, r *http.Request) {
	path, err := safeBackupFile(a.cfg.DataDir, chi.URLParam(r, "file"))
	if err != nil {
		writeError(w, 404, "backup_not_found", "备份不存在")
		return
	}
	if err = store.VerifyBackup(path); err != nil {
		writeError(w, 400, "backup_invalid", "备份完整性校验失败")
		return
	}
	pre := filepath.Join(a.cfg.DataDir, "velin-pre-restore-"+time.Now().UTC().Format("20060102-150405")+".db")
	if err = a.store.Backup(r.Context(), pre); err != nil {
		writeError(w, 500, "backup_failed", "无法创建恢复前备份")
		return
	}
	a.terminals.CloseAll()
	if err = a.store.Restore(r.Context(), path); err != nil {
		writeError(w, 500, "restore_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "preRestore": filepath.Base(pre), "requiresLogin": true})
}
