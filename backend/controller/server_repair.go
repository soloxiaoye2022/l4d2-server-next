package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"l4d2-manager-next/logic"

	"github.com/gin-gonic/gin"
)

var requestServerRepair = logic.RequestServerRepair

type serverRepairPayload struct {
	Action string `json:"action"`
}

// PostServerRepair quarantines broken addon VPKs and optionally hot reloads the
// addon paths or restarts the game service. Only administrators may trigger it.
func PostServerRepair(c *gin.Context) {
	defer LogOp(c, "执行坏包隔离处置")()
	if !requireServerStartupAdmin(c) {
		return
	}
	var payload serverRepairPayload
	_ = c.ShouldBindJSON(&payload)
	operator := strings.TrimSpace(c.GetString("auth_code_remark"))
	if operator == "" {
		operator = "admin"
	}
	result, err := requestServerRepair(strings.TrimSpace(payload.Action), operator, 45*time.Second)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetServerRepairHistory returns previous repair attempts, newest first.
func GetServerRepairHistory(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	limit := 20
	var payload struct {
		Limit int `json:"limit"`
	}
	if err := c.ShouldBindJSON(&payload); err == nil && payload.Limit > 0 {
		limit = payload.Limit
	} else if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	entries, err := logic.ListRepairHistory(limit)
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "读取处置记录失败: %v", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries})
}
