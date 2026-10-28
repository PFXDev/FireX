package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/backup"
	"github.com/PFXDev/FireX/internal/store"
)

// maxBackupUpload bounds an uploaded database. FireX databases are a few
// megabytes; this only stops a runaway upload from filling the disk.
const maxBackupUpload = 512 << 20

func (s *Server) listBackups(c *gin.Context) {
	items, err := s.bkup.List()
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"items":          items,
		"dir":            s.bkup.Dir(),
		"pendingRestore": s.bkup.PendingRestore(),
	})
}

func (s *Server) createBackup(c *gin.Context) {
	entry, err := s.bkup.Create(backup.KindManual)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Set(auditTargetKey, entry.Name)
	c.JSON(http.StatusOK, entry)
}

// uploadBackup streams the first file of a multipart upload into the backup
// directory, so a database taken from another install can be restored here.
func (s *Server) uploadBackup(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBackupUpload)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		failMsg(c, http.StatusBadRequest, "请以 multipart/form-data 上传备份文件。")
		return
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			failMsg(c, http.StatusBadRequest, "请求中没有备份文件。")
			return
		}
		if err != nil {
			fail(c, http.StatusBadRequest, fmt.Errorf("读取上传内容失败：%w", err))
			return
		}
		if part.FileName() == "" {
			continue
		}
		entry, err := s.bkup.Import(part)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				failMsg(c, http.StatusRequestEntityTooLarge, "备份文件超过 512 MiB 上限。")
				return
			}
			fail(c, http.StatusBadRequest, err)
			return
		}
		c.Set(auditTargetKey, part.FileName()+" → "+entry.Name)
		c.JSON(http.StatusOK, entry)
		return
	}
}

func (s *Server) downloadBackup(c *gin.Context) {
	path, err := s.bkup.Path(c.Param("name"))
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.FileAttachment(path, c.Param("name"))
}

func (s *Server) deleteBackup(c *gin.Context) {
	if err := s.bkup.Delete(c.Param("name")); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, backup.ErrNotFound) {
			status = http.StatusNotFound
		}
		fail(c, status, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// restoreBackup stages a backup and restarts FireX into it. The running
// database is snapshotted first, and the response names that snapshot so the
// restore can itself be undone.
func (s *Server) restoreBackup(c *gin.Context) {
	note := store.RestoreNote{IP: c.ClientIP()}
	if admin := currentAdmin(c); admin != nil {
		note.Actor = admin.Username
	}
	safety, err := s.bkup.StageRestore(c.Param("name"), note)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, backup.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, backup.ErrRestorePending):
			status = http.StatusConflict
		}
		fail(c, status, err)
		return
	}
	if err := s.restart("restore " + c.Param("name")); err != nil {
		s.bkup.CancelRestore()
		fail(c, http.StatusConflict, fmt.Errorf("无法重启 FireX：%w", err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"preRestore": safety.Name})
}
