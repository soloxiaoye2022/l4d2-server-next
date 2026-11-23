package controller

import (
	"net/http"
	"os"
	"path/filepath"

	"l4d2-manager-next/consts"
	"l4d2-manager-next/logic"

	"github.com/gin-gonic/gin"
)

type ServerInfoResponse struct {
	Hostname       string                   `json:"hostname"`
	HostnameError  string                   `json:"hostname_error"`
	HostnameSource string                   `json:"hostname_source"`
	Detection      *logic.HostnameDetection `json:"detection"`
	Motd           string                   `json:"motd"`
	Host           string                   `json:"host"`
}

type UpdateServerInfoRequest struct {
	Hostname string `json:"hostname"`
	Motd     string `json:"motd"`
	Host     string `json:"host"`
}

func readFileContent(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return logic.DecodeTextContent(content), nil
}

func writeFileContent(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// GetServerInfo 汇总服务器信息。
//
// 服务器名不再只认 l4d2_hostname.txt：同一台服务器可能装 Neko ServerName、
// L4D2 中文服务器名等不同插件，各自存在不同位置。这里用 RCON 拿运行时真实
// 生效值，再用配置文件做补充证据，避免出现"插件明明在跑却提示未启用"。
func GetServerInfo(c *gin.Context) {
	motdPath := filepath.Join(consts.GamePath, "motd.txt")
	hostPath := filepath.Join(consts.GamePath, "host.txt")

	resp := ServerInfoResponse{}

	detection := logic.DetectHostnameSources()
	resp.Detection = detection
	resp.Hostname, resp.HostnameSource, resp.HostnameError = logic.ResolveHostname(detection)

	if content, err := readFileContent(motdPath); err == nil {
		resp.Motd = content
	}
	if content, err := readFileContent(hostPath); err == nil {
		resp.Host = content
	}

	c.JSON(http.StatusOK, resp)
}

func UpdateServerInfo(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "admin" {
		FailWithError(c, http.StatusForbidden, "需要管理员权限")
		return
	}

	var req UpdateServerInfoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数错误: %v", err)
		return
	}
	defer LogOp(c, "更新服务器信息")()

	// 服务器名按已检测到的配置文件优先级写入，兼容多种插件。
	written, warning, err := logic.SaveServerHostname(req.Hostname)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存服务器名失败: " + err.Error()})
		return
	}

	motdPath := filepath.Join(consts.GamePath, "motd.txt")
	hostPath := filepath.Join(consts.GamePath, "host.txt")

	if err := writeFileContent(motdPath, req.Motd); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存公告失败: " + err.Error()})
		return
	}
	if err := writeFileContent(hostPath, req.Host); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存标题失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "保存成功",
		"hostname_file":   written,
		"hostname_notice": warning,
	})
}
