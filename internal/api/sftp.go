package api

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const maxEditableTextSize = 2 << 20

type sftpConnection struct {
	*sftp.Client
	ssh      *ssh.Client
	closeSSH bool
}

func (c *sftpConnection) Close() error {
	err := c.Client.Close()
	if c.closeSSH {
		_ = c.ssh.Close()
	}
	return err
}

func (a *API) openSFTP(r *http.Request) (*sftpConnection, error) {
	if sessionID := strings.TrimSpace(r.URL.Query().Get("session")); sessionID != "" {
		session, err := a.terminals.Get(currentUser(r).ID, sessionID)
		if err != nil {
			return nil, errors.New("terminal connection is not available")
		}
		if session.Meta().HostID != chi.URLParam(r, "hostID") {
			return nil, errors.New("terminal does not belong to this host")
		}
		client := session.SSHClient()
		if client == nil {
			return nil, errors.New("terminal connection is not available")
		}
		result, err := sftp.NewClient(client)
		if err != nil {
			return nil, err
		}
		return &sftpConnection{Client: result, ssh: client}, nil
	}
	client, _, err := a.terminals.DialSaved(r.Context(), currentUser(r).ID, chi.URLParam(r, "hostID"))
	if err != nil {
		return nil, err
	}
	result, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &sftpConnection{Client: result, ssh: client, closeSSH: true}, nil
}

func cleanRemotePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "."
	}
	return path.Clean(value)
}

func writableRemotePath(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" || strings.ContainsRune(raw, '\x00') || len(raw) > 4096 {
		return "", errors.New("invalid remote path")
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == "/" || cleaned == ".." || strings.Trim(cleaned, "/") == ".." {
		return "", errors.New("refusing root or parent path")
	}
	return cleaned, nil
}

func (a *API) sftpList(w http.ResponseWriter, r *http.Request) {
	client, err := a.openSFTP(r)
	if err != nil {
		writeError(w, 502, "sftp_connection_failed", err.Error())
		return
	}
	defer client.Close()
	dir := cleanRemotePath(r.URL.Query().Get("path"))
	entries, err := client.ReadDir(dir)
	if err != nil {
		writeError(w, 502, "sftp_list_failed", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{"name": entry.Name(), "path": path.Join(dir, entry.Name()), "size": entry.Size(), "mode": entry.Mode().String(), "directory": entry.IsDir(), "symlink": entry.Mode()&os.ModeSymlink != 0, "modifiedAt": entry.ModTime()})
	}
	writeJSON(w, 200, map[string]any{"path": dir, "entries": out})
}

func (a *API) sftpDownload(w http.ResponseWriter, r *http.Request) {
	client, err := a.openSFTP(r)
	if err != nil {
		writeError(w, 502, "sftp_connection_failed", err.Error())
		return
	}
	defer client.Close()
	remotePath := cleanRemotePath(r.URL.Query().Get("path"))
	info, err := client.Lstat(remotePath)
	if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		writeError(w, 400, "invalid_download", "只能下载普通文件")
		return
	}
	file, err := client.Open(remotePath)
	if err != nil {
		writeError(w, 502, "sftp_open_failed", err.Error())
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", stringInt(info.Size()))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(remotePath)}))
	_, _ = io.Copy(w, file)
}

func editableTextVersion(content []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

func readEditableRemoteFile(client *sftp.Client, remotePath string) ([]byte, os.FileInfo, error) {
	info, err := client.Lstat(remotePath)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("只能编辑普通文本文件")
	}
	if info.Size() > maxEditableTextSize {
		return nil, nil, errors.New("文件超过 2 MiB 在线编辑上限")
	}
	file, err := client.Open(remotePath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxEditableTextSize+1))
	if err != nil {
		return nil, nil, err
	}
	if len(content) > maxEditableTextSize {
		return nil, nil, errors.New("文件超过 2 MiB 在线编辑上限")
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return nil, nil, errors.New("文件不是有效的 UTF-8 文本")
	}
	return content, info, nil
}

func (a *API) sftpReadText(w http.ResponseWriter, r *http.Request) {
	client, err := a.openSFTP(r)
	if err != nil {
		writeError(w, 502, "sftp_connection_failed", err.Error())
		return
	}
	defer client.Close()
	remotePath := cleanRemotePath(r.URL.Query().Get("path"))
	content, info, err := readEditableRemoteFile(client.Client, remotePath)
	if err != nil {
		writeError(w, 400, "text_file_unavailable", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"path": remotePath, "content": string(content), "version": editableTextVersion(content),
		"size": len(content), "modifiedAt": info.ModTime(),
	})
}

func (a *API) sftpWriteText(w http.ResponseWriter, r *http.Request) {
	remotePath, pathErr := writableRemotePath(r.URL.Query().Get("path"))
	if pathErr != nil {
		writeError(w, 400, "invalid_path", "请指定远程文件路径")
		return
	}
	expectedVersion := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), "\"")
	if expectedVersion == "" {
		writeError(w, http.StatusPreconditionRequired, "file_version_required", "缺少文件版本，已拒绝保存")
		return
	}
	content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEditableTextSize+1))
	if err != nil || len(content) > maxEditableTextSize {
		writeError(w, http.StatusRequestEntityTooLarge, "text_file_too_large", "文件超过 2 MiB 在线编辑上限")
		return
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		writeError(w, 400, "invalid_text_file", "只能保存有效的 UTF-8 文本")
		return
	}
	if len(content) == 0 && r.Header.Get("X-Velin-Allow-Empty") != "true" {
		writeError(w, 400, "empty_file_confirmation_required", "清空文件需要明确确认")
		return
	}

	client, err := a.openSFTP(r)
	if err != nil {
		writeError(w, 502, "sftp_connection_failed", err.Error())
		return
	}
	defer client.Close()
	current, info, err := readEditableRemoteFile(client.Client, remotePath)
	if err != nil {
		writeError(w, 400, "text_file_unavailable", err.Error())
		return
	}
	if editableTextVersion(current) != expectedVersion {
		writeError(w, http.StatusConflict, "file_changed", "远程文件已被其他操作修改，请重新加载后再编辑")
		return
	}
	if string(current) == string(content) {
		writeJSON(w, 200, map[string]any{"path": remotePath, "version": expectedVersion, "bytes": len(content)})
		return
	}

	tempPath := path.Join(path.Dir(remotePath), ".velin-edit-"+uuid.NewString()+".tmp")
	temp, err := client.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		writeError(w, 502, "text_save_failed", err.Error())
		return
	}
	cleanup := true
	defer func() {
		_ = temp.Close()
		if cleanup {
			_ = client.Remove(tempPath)
		}
	}()
	if err = temp.Chmod(info.Mode().Perm()); err == nil {
		_, err = temp.Write(content)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		writeError(w, 502, "text_save_failed", err.Error())
		return
	}
	latest, _, err := readEditableRemoteFile(client.Client, remotePath)
	if err != nil || editableTextVersion(latest) != expectedVersion {
		writeError(w, http.StatusConflict, "file_changed", "远程文件在保存期间发生变化，原文件未被覆盖")
		return
	}
	if err = client.PosixRename(tempPath, remotePath); err != nil {
		writeError(w, 502, "atomic_replace_unsupported", "远程服务器不支持安全的原子替换，原文件未被修改")
		return
	}
	cleanup = false
	version := editableTextVersion(content)
	a.store.Audit(currentUser(r).ID, "sftp_text_saved", "host", chi.URLParam(r, "hostID"), ipOf(r), map[string]any{"path": remotePath, "bytes": len(content)})
	writeJSON(w, 200, map[string]any{"path": remotePath, "version": version, "bytes": len(content)})
}

func (a *API) sftpUpload(w http.ResponseWriter, r *http.Request) {
	client, err := a.openSFTP(r)
	if err != nil {
		writeError(w, 502, "sftp_connection_failed", err.Error())
		return
	}
	defer client.Close()
	remotePath, pathErr := writableRemotePath(r.URL.Query().Get("path"))
	if pathErr != nil {
		writeError(w, 400, "invalid_path", "请指定远程文件路径")
		return
	}
	if r.URL.Query().Get("overwrite") != "true" {
		if _, statErr := client.Lstat(remotePath); statErr == nil {
			writeError(w, 409, "file_exists", "远程文件已存在")
			return
		}
	}
	if r.ContentLength > 2<<30 {
		writeError(w, 413, "file_too_large", "文件超过 2 GiB 上限")
		return
	}
	file, err := client.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		writeError(w, 502, "sftp_open_failed", err.Error())
		return
	}
	defer file.Close()
	written, err := io.Copy(file, http.MaxBytesReader(w, r.Body, 2<<30))
	if err != nil {
		_ = client.Remove(remotePath)
		writeError(w, 502, "sftp_upload_failed", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"path": remotePath, "bytes": written})
}

func (a *API) sftpMkdir(w http.ResponseWriter, r *http.Request) {
	var in struct{ Path string }
	if !decode(w, r, &in) {
		return
	}
	target, pathErr := writableRemotePath(in.Path)
	if pathErr != nil {
		writeError(w, 400, "invalid_path", "不能在根路径执行此操作")
		return
	}
	client, err := a.openSFTP(r)
	if err != nil {
		writeError(w, 502, "sftp_connection_failed", err.Error())
		return
	}
	defer client.Close()
	if err = client.MkdirAll(target); err != nil {
		writeError(w, 502, "sftp_mkdir_failed", err.Error())
		return
	}
	writeJSON(w, 201, map[string]bool{"ok": true})
}

func (a *API) sftpRename(w http.ResponseWriter, r *http.Request) {
	var in struct{ Source, Destination string }
	if !decode(w, r, &in) {
		return
	}
	source, sourceErr := writableRemotePath(in.Source)
	destination, destinationErr := writableRemotePath(in.Destination)
	if sourceErr != nil || destinationErr != nil || source == destination {
		writeError(w, 400, "invalid_path", "源路径和目标路径无效")
		return
	}
	client, err := a.openSFTP(r)
	if err != nil {
		writeError(w, 502, "sftp_connection_failed", err.Error())
		return
	}
	defer client.Close()
	if _, err = client.Lstat(destination); err == nil {
		writeError(w, 409, "destination_exists", "目标路径已存在")
		return
	}
	if err = client.Rename(source, destination); err != nil {
		writeError(w, 502, "sftp_rename_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func removeRemote(client *sftp.Client, target string, recursive bool) error {
	info, err := client.Lstat(target)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to follow or delete symbolic link")
	}
	if !info.IsDir() {
		return client.Remove(target)
	}
	if !recursive {
		return client.RemoveDirectory(target)
	}
	entries, err := client.ReadDir(target)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Mode()&os.ModeSymlink != 0 {
			return errors.New("directory contains a symbolic link")
		}
		if err = removeRemote(client, path.Join(target, entry.Name()), true); err != nil {
			return err
		}
	}
	return client.RemoveDirectory(target)
}

func (a *API) sftpDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path      string
		Recursive bool
	}
	if !decode(w, r, &in) {
		return
	}
	target, pathErr := writableRemotePath(in.Path)
	if pathErr != nil {
		writeError(w, 400, "invalid_path", "拒绝删除根路径或父级路径")
		return
	}
	client, err := a.openSFTP(r)
	if err != nil {
		writeError(w, 502, "sftp_connection_failed", err.Error())
		return
	}
	defer client.Close()
	if err = removeRemote(client.Client, target, in.Recursive); err != nil {
		writeError(w, 502, "sftp_delete_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func stringInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
