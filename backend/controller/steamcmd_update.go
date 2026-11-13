package controller

import (
	"l4d2-manager-next/logic"
	"net/http"

	"github.com/gin-gonic/gin"
)

var (
	getSteamCMDUpdateConfig      = logic.GetSteamCMDUpdateConfig
	generateSteamCMDUpdateScript = logic.GenerateSteamCMDUpdateScript
)

func GetSteamCMDUpdate(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	config, err := getSteamCMDUpdateConfig()
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "%v", err)
		return
	}
	c.JSON(http.StatusOK, config)
}

func GenerateSteamCMDUpdate(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	var request logic.GenerateSteamCMDUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数错误: %v", err)
		return
	}
	defer LogOp(c, "生成 SteamCMD 更新脚本")()
	config, err := generateSteamCMDUpdateScript(request)
	if err != nil {
		FailWithError(c, http.StatusBadRequest, "%v", err)
		return
	}
	c.JSON(http.StatusOK, config)
}
