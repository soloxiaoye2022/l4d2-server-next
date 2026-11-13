package controller

import (
	"l4d2-manager-next/logic"
	"net/http"

	"github.com/gin-gonic/gin"
)

var (
	getSourceTVSupportStatus = logic.GetSourceTVSupportStatus
	installSourceTVSupport   = logic.InstallSourceTVSupport
)

func GetSourceTVSupport(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	status, err := getSourceTVSupportStatus()
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "%v", err)
		return
	}
	c.JSON(http.StatusOK, status)
}

func InstallSourceTVSupport(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	defer LogOp(c, "安装 L4D2 SourceTV 修复扩展")()
	status, err := installSourceTVSupport()
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "%v", err)
		return
	}
	c.JSON(http.StatusOK, status)
}
