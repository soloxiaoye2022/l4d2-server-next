package controller

import (
	"l4d2-manager-next/logic"
	"l4d2-manager-next/middlewares"
	"net/http"

	"github.com/gin-gonic/gin"
)

var (
	getServerStartupConfig    = logic.GetServerStartupConfig
	updateServerStartupConfig = logic.UpdateServerStartup
)

func requireServerStartupAdmin(c *gin.Context) bool {
	if c.GetString("role") != middlewares.RoleAdmin {
		FailWithError(c, http.StatusForbidden, "需要管理员权限")
		return false
	}
	return true
}

func GetServerStartup(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	config, err := getServerStartupConfig()
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "%v", err)
		return
	}
	c.JSON(http.StatusOK, config)
}

func UpdateServerStartup(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	var request logic.UpdateServerStartupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数错误: %v", err)
		return
	}
	defer LogOp(c, "更新服务器启动项")()

	config, err := updateServerStartupConfig(request)
	if err != nil {
		FailWithError(c, http.StatusBadRequest, "%v", err)
		return
	}
	c.JSON(http.StatusOK, config)
}

func GetServerEndpoint(c *gin.Context) {
	c.JSON(http.StatusOK, logic.GetServerEndpoint())
}
