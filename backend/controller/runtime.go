package controller

import (
	"errors"
	"net/http"
	"strings"

	"l4d2-manager-next/logic"
	runtimelogic "l4d2-manager-next/logic/runtime"

	"github.com/gin-gonic/gin"
)

// errInvalidInteger 表示查询参数不是合法整数，调用方据此使用默认值。
var errInvalidInteger = errors.New("invalid integer")

// runtimeManager 返回已初始化的运行时管理器。
func runtimeManager() *runtimelogic.Manager {
	logic.InitRuntimeManager()
	return runtimelogic.Default()
}

// requireRuntimeAdmin 运行时管理全部是管理员操作：能停别人的服，风险最高。
func requireRuntimeAdmin(c *gin.Context) bool {
	return requireServerStartupAdmin(c)
}

// ListRuntimes 返回全部运行时、默认运行时与能力矩阵。
func ListRuntimes(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	manager := runtimeManager()
	result, err := manager.ListResult("")
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "%v", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetRuntime 返回单个运行时详情，附带事件流。
func GetRuntime(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		var body struct {
			ID string `json:"id" form:"id"`
		}
		_ = c.ShouldBind(&body)
		id = strings.TrimSpace(body.ID)
	}
	manager := runtimeManager()
	item, err := manager.Resolve(id)
	if err != nil {
		FailWithError(c, http.StatusNotFound, "%v", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"runtime": item,
		"events":  runtimelogic.ListEvents(item.ID, 50),
	})
}

// GetRuntimeStatus 采集一次健康快照。
func GetRuntimeStatus(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	id := strings.TrimSpace(c.Query("id"))
	manager := runtimeManager()
	snapshot, err := manager.Status(id)
	if err != nil {
		FailWithError(c, http.StatusBadGateway, "%v", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":     snapshot,
		"state_text": runtimelogic.StateLabel(snapshot.State),
	})
}

// GetRuntimeLogs 读取运行时日志尾部。
func GetRuntimeLogs(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	id := strings.TrimSpace(c.Query("id"))
	tail := 200
	if raw := strings.TrimSpace(c.Query("tail")); raw != "" {
		if parsed, err := parseIntInRange(raw, 1, 2000); err == nil {
			tail = parsed
		}
	}
	manager := runtimeManager()
	payload, err := manager.Logs(id, tail)
	if err != nil {
		FailWithError(c, http.StatusBadGateway, "%v", err)
		return
	}
	c.JSON(http.StatusOK, payload)
}

// PostRuntimeAction 执行 start / stop / restart。
func PostRuntimeAction(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	var body struct {
		ID     string `json:"id" form:"id"`
		Action string `json:"action" form:"action"`
	}
	if err := c.ShouldBind(&body); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数无效")
		return
	}
	action := strings.TrimSpace(body.Action)
	defer LogOp(c, "运行时动作 "+action)()
	manager := runtimeManager()
	result, err := manager.Action(strings.TrimSpace(body.ID), action)
	if err != nil {
		FailWithError(c, http.StatusBadRequest, "%v", err)
		return
	}
	status := http.StatusOK
	if result.Status != runtimelogic.JobSuccess {
		status = http.StatusBadGateway
	}
	c.JSON(status, result)
}

// PostRuntimeDetect 触发宿主机扫描。
func PostRuntimeDetect(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	defer LogOp(c, "扫描宿主机服务端")()
	manager := runtimeManager()
	result, err := manager.Detect()
	if err != nil {
		FailWithError(c, http.StatusBadGateway, "%v", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// PostRuntimeAdopt 接管扫描到的服务端。
func PostRuntimeAdopt(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	var body struct {
		CandidateKey string `json:"candidate_key" form:"candidate_key"`
		ID           string `json:"id" form:"id"`
		Name         string `json:"name" form:"name"`
	}
	if err := c.ShouldBind(&body); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数无效")
		return
	}
	defer LogOp(c, "接管服务端 "+body.ID)()
	manager := runtimeManager()
	item, err := manager.Adopt(strings.TrimSpace(body.CandidateKey), body.ID, body.Name)
	if err != nil {
		FailWithError(c, http.StatusBadRequest, "%v", err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// PostRuntimeDetach 撤销接管，不动进程。
func PostRuntimeDetach(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	var body struct {
		ID string `json:"id" form:"id"`
	}
	if err := c.ShouldBind(&body); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数无效")
		return
	}
	defer LogOp(c, "撤销接管 "+body.ID)()
	manager := runtimeManager()
	if err := manager.Detach(strings.TrimSpace(body.ID)); err != nil {
		FailWithError(c, http.StatusBadRequest, "%v", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已撤销接管，游戏进程未受影响"})
}

// PostRuntimeCreate 新建运行时。
func PostRuntimeCreate(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	var body struct {
		runtimelogic.Descriptor
		// 指针用来区分"没传"与"传了 false"：没传时默认启用。
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数无效")
		return
	}
	descriptor := body.Descriptor
	if body.Enabled != nil {
		descriptor.Enabled = *body.Enabled
	}
	defer LogOp(c, "新建运行时 "+descriptor.ID)()
	manager := runtimeManager()
	item, err := manager.Create(descriptor, body.Enabled)
	if err != nil {
		FailWithError(c, http.StatusBadRequest, "%v", err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// PostRuntimeUpdate 修改名称 / 启动规格 / 启用状态。
func PostRuntimeUpdate(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	var body struct {
		ID      string                   `json:"id"`
		Name    string                   `json:"name"`
		Spec    *runtimelogic.LaunchSpec `json:"spec"`
		Enabled *bool                    `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数无效")
		return
	}
	defer LogOp(c, "更新运行时 "+body.ID)()
	manager := runtimeManager()
	item, err := manager.Update(body.ID, body.Name, body.Spec, body.Enabled)
	if err != nil {
		FailWithError(c, http.StatusBadRequest, "%v", err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// PostRuntimeDelete 删除或归档运行时。
func PostRuntimeDelete(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	var body struct {
		ID   string `json:"id"`
		Mode string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数无效")
		return
	}
	defer LogOp(c, "删除运行时 "+body.ID)()
	manager := runtimeManager()
	if err := manager.Delete(strings.TrimSpace(body.ID), strings.TrimSpace(body.Mode)); err != nil {
		FailWithError(c, http.StatusBadRequest, "%v", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// PostRuntimeMigratePlan 生成迁移计划。
func PostRuntimeMigratePlan(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	var body struct {
		SourceID string                        `json:"source_id"`
		Target   string                        `json:"target_kind"`
		Options  runtimelogic.MigrationOptions `json:"options"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数无效")
		return
	}
	manager := runtimeManager()
	plan, err := manager.PlanMigration(body.SourceID, runtimelogic.Kind(body.Target), body.Options)
	if err != nil {
		FailWithError(c, http.StatusBadRequest, "%v", err)
		return
	}
	c.JSON(http.StatusOK, plan)
}

// PostRuntimeMigrateApply 执行迁移计划。
func PostRuntimeMigrateApply(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		FailWithError(c, http.StatusBadRequest, "请求参数无效")
		return
	}
	defer LogOp(c, "执行运行时迁移")()
	manager := runtimeManager()
	result, err := manager.ApplyMigration(strings.TrimSpace(body.Token))
	if err != nil {
		FailWithError(c, http.StatusBadGateway, "%v", err)
		return
	}
	status := http.StatusOK
	if result.Status != runtimelogic.JobSuccess {
		status = http.StatusBadGateway
	}
	c.JSON(status, result)
}

// GetRuntimeEvents 返回事件流，用于"什么时候挂的"回溯。
func GetRuntimeEvents(c *gin.Context) {
	if !requireRuntimeAdmin(c) {
		return
	}
	id := strings.TrimSpace(c.Query("id"))
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := parseIntInRange(raw, 1, 500); err == nil {
			limit = parsed
		}
	}
	c.JSON(http.StatusOK, gin.H{"events": runtimelogic.ListEvents(id, limit)})
}

// parseIntInRange 解析并限制在区间内，越界返回错误由调用方用默认值兜底。
func parseIntInRange(raw string, min int, max int) (int, error) {
	value := 0
	for _, char := range raw {
		if char < '0' || char > '9' {
			return 0, errInvalidInteger
		}
		value = value*10 + int(char-'0')
		if value > max*10 {
			return 0, errInvalidInteger
		}
	}
	if value < min || value > max {
		return 0, errInvalidInteger
	}
	return value, nil
}
