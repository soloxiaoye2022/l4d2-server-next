package controller

import (
	"l4d2-manager-next/logic"
	"net/http"

	"github.com/gin-gonic/gin"
)

var (
	getServerMaintenanceStatus = logic.GetServerMaintenanceStatus
	requestServerMaintenance   = logic.RequestServerMaintenance
	configureSourceTVStartup   = logic.ConfigureSourceTVStartup
)

func GetServerMaintenance(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	status, err := getServerMaintenanceStatus()
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "%v", err)
		return
	}
	c.JSON(http.StatusOK, status)
}

func CheckL4D2ServerUpdate(c *gin.Context) {
	requestMaintenanceAction(c, logic.MaintenanceActionCheck)
}

func UpdateL4D2Server(c *gin.Context) {
	requestMaintenanceAction(c, logic.MaintenanceActionUpdate)
}

func requestMaintenanceAction(c *gin.Context, action string) {
	if !requireServerStartupAdmin(c) {
		return
	}
	detail := "检查 L4D2 服务端更新"
	if action == logic.MaintenanceActionUpdate {
		detail = "更新 L4D2 服务端"
	}
	defer LogOp(c, detail)()
	status, err := requestServerMaintenance(action)
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "%v", err)
		return
	}
	c.JSON(http.StatusAccepted, status)
}

func ConfigureSourceTVStartup(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	defer LogOp(c, "配置 SourceTV 启动参数")()
	config, err := configureSourceTVStartup()
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "%v", err)
		return
	}
	c.JSON(http.StatusOK, config)
}
