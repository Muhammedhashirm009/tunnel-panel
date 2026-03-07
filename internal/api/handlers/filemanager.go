package handlers

import (
	"fmt"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/filemanager"
	"github.com/Muhammedhashirm009/tunnel-panel/internal/httputil"
)

// FileManagerHandler handles file management endpoints
type FileManagerHandler struct{}

// NewFileManagerHandler creates a new handler
func NewFileManagerHandler() *FileManagerHandler {
	return &FileManagerHandler{}
}

// Browse handles GET /api/files/browse?path=/some/dir
func (h *FileManagerHandler) Browse(c *gin.Context) {
	path := c.DefaultQuery("path", "/")
	result, err := filemanager.Browse(path)
	if err != nil {
		httputil.Error(c, 400, err.Error())
		return
	}
	httputil.Success(c, result)
}

// ReadFile handles GET /api/files/read?path=/some/file
func (h *FileManagerHandler) ReadFile(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httputil.Error(c, 400, "path is required")
		return
	}

	content, err := filemanager.ReadFile(path)
	if err != nil {
		httputil.Error(c, 400, err.Error())
		return
	}
	httputil.Success(c, content)
}

// WriteFile handles POST /api/files/write
func (h *FileManagerHandler) WriteFile(c *gin.Context) {
	var req struct {
		Path    string `json:"path" binding:"required"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, 400, "path is required")
		return
	}

	if err := filemanager.WriteFile(req.Path, req.Content); err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, gin.H{"message": "file saved", "path": req.Path})
}

// CreateFile handles POST /api/files/create
func (h *FileManagerHandler) CreateFile(c *gin.Context) {
	var req struct {
		Path  string `json:"path" binding:"required"`
		IsDir bool   `json:"is_dir"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, 400, "path is required")
		return
	}

	var err error
	if req.IsDir {
		err = filemanager.CreateDirectory(req.Path)
	} else {
		err = filemanager.CreateFile(req.Path)
	}
	if err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Created(c, gin.H{"message": "created", "path": req.Path})
}

// Rename handles POST /api/files/rename
func (h *FileManagerHandler) Rename(c *gin.Context) {
	var req struct {
		Path    string `json:"path" binding:"required"`
		NewName string `json:"new_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, 400, "path and new_name are required")
		return
	}

	if err := filemanager.Rename(req.Path, req.NewName); err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, gin.H{"message": "renamed"})
}

// Move handles POST /api/files/move
func (h *FileManagerHandler) Move(c *gin.Context) {
	var req struct {
		Source string `json:"source" binding:"required"`
		Dest   string `json:"dest" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, 400, "source and dest are required")
		return
	}

	if err := filemanager.Move(req.Source, req.Dest); err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, gin.H{"message": "moved"})
}

// CopyFiles handles POST /api/files/copy
func (h *FileManagerHandler) CopyFiles(c *gin.Context) {
	var req struct {
		Source string `json:"source" binding:"required"`
		Dest   string `json:"dest" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, 400, "source and dest are required")
		return
	}

	if err := filemanager.Copy(req.Source, req.Dest); err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, gin.H{"message": "copied"})
}

// Delete handles POST /api/files/delete
func (h *FileManagerHandler) Delete(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, 400, "path is required")
		return
	}

	if err := filemanager.Delete(req.Path); err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, gin.H{"message": "deleted"})
}

// Chmod handles POST /api/files/chmod
func (h *FileManagerHandler) Chmod(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
		Mode string `json:"mode" binding:"required"` // e.g. "0755"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, 400, "path and mode are required")
		return
	}

	modeVal, err := strconv.ParseUint(req.Mode, 8, 32)
	if err != nil {
		httputil.Error(c, 400, "invalid mode: "+req.Mode)
		return
	}

	if err := filemanager.ChangePermissions(req.Path, os.FileMode(modeVal)); err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, gin.H{"message": "permissions changed"})
}

// Search handles GET /api/files/search?path=/&query=filename
func (h *FileManagerHandler) Search(c *gin.Context) {
	root := c.DefaultQuery("path", "/")
	query := c.Query("query")
	if query == "" {
		httputil.Error(c, 400, "query is required")
		return
	}

	results, err := filemanager.Search(root, query, 50)
	if err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}
	httputil.Success(c, results)
}

// Upload handles POST /api/files/upload (multipart form)
func (h *FileManagerHandler) Upload(c *gin.Context) {
	destDir := c.PostForm("path")
	if destDir == "" {
		destDir = "/tmp"
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		httputil.Error(c, 400, "no file provided: "+err.Error())
		return
	}
	defer file.Close()

	savedPath, err := filemanager.SaveUpload(destDir, header.Filename, file)
	if err != nil {
		httputil.Error(c, 500, err.Error())
		return
	}

	httputil.Success(c, gin.H{
		"message":  "uploaded",
		"path":     savedPath,
		"filename": header.Filename,
		"size":     header.Size,
	})
}

// Download handles GET /api/files/download?path=/some/file
func (h *FileManagerHandler) Download(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httputil.Error(c, 400, "path is required")
		return
	}

	name, size, err := filemanager.GetDownloadInfo(path)
	if err != nil {
		httputil.Error(c, 404, err.Error())
		return
	}

	info, _ := os.Stat(path)
	if info != nil && info.IsDir() {
		// Zip download for directories
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
		filemanager.ZipDownload(path, c.Writer)
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	c.Header("Content-Length", fmt.Sprintf("%d", size))
	c.File(path)
}
