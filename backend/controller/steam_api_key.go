package controller

import (
	"l4d2-manager-next/logic"
	"l4d2-manager-next/middlewares"
	"net/http"

	"github.com/gin-gonic/gin"
)

type steamAPIKeyUpdateRequest struct {
	APIKey string `json:"api_key"`
}

func requireSteamAPIAdmin(c *gin.Context) bool {
	if c.GetString("role") == middlewares.RoleAdmin {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "只有管理员可以配置 Steam API Key"})
	return false
}

func GetSteamAPIKeySetting(c *gin.Context) {
	if !requireSteamAPIAdmin(c) {
		return
	}
	c.JSON(http.StatusOK, logic.GetSteamAPIKeyStatus())
}

func UpdateSteamAPIKeySetting(c *gin.Context) {
	if !requireSteamAPIAdmin(c) {
		return
	}
	defer LogOp(c, "更新 Steam API Key 配置")()
	var request steamAPIKeyUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	if _, err := logic.SetSteamAPIKey(request.APIKey); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, logic.GetSteamAPIKeyStatus())
}
