package controller

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"l4d2-manager-next/logic"

	"github.com/gin-gonic/gin"
)

// Restart 保留旧行为，但优先交给运行时子系统。
// 只有显式接管过非 legacy 运行时、且宿主机 Agent 可用时才会走新链路。
func Restart(c *gin.Context) {
	defer LogOp(c, "执行重启操作")()
	if handled, runtimeID, err := logic.ControlViaRuntime("restart"); handled {
		if err != nil {
			FailWithError(c, http.StatusBadGateway, "重启失败（运行时 %s）: %v", runtimeID, err)
			return
		}
		c.String(http.StatusOK, "重启成功，请等待服务器启动")
		return
	}

	// 使用RCON重启
	if os.Getenv("L4D2_RESTART_BY_RCON") == "true" {
		if err := restartByRcon(); err != nil {
			FailWithError(c, http.StatusInternalServerError, "%s", err.Error())
		}
		return
	}

	// 使用命令重启
	restartCmd := os.Getenv("L4D2_RESTART_CMD")
	if restartCmd == "" {
		containerName := os.Getenv("L4D2_CONTAINER_NAME")
		if containerName == "" {
			containerName = "l4d2"
		}
		restartCmd = "docker restart " + containerName
	}

	var err error
	if runtime.GOOS == "windows" {
		err = exec.Command("cmd.exe", "/c", restartCmd).Run()
	} else {
		err = exec.Command("sh", "-c", restartCmd).Run()
	}
	if err != nil {
		FailWithError(c, http.StatusInternalServerError, "重启失败: %v", err)
		return
	}

	c.String(http.StatusOK, "重启成功，请等待服务器启动")
}

// Start 启动游戏服务端。只有接管过运行时才有意义，
// 旧部署没有独立的启动通道，明确报错而不是假装成功。
func Start(c *gin.Context) {
	defer LogOp(c, "启动游戏服务端")()
	handled, runtimeID, err := logic.ControlViaRuntime("start")
	if !handled {
		FailWithError(c, http.StatusBadRequest, "当前部署未接入运行时，无法执行独立启动；请先在“运行时管理”中接管服务端")
		return
	}
	if err != nil {
		FailWithError(c, http.StatusBadGateway, "启动失败（运行时 %s）: %v", runtimeID, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已提交启动请求", "runtime_id": runtimeID})
}

// Stop 停止游戏服务端。
func Stop(c *gin.Context) {
	defer LogOp(c, "停止游戏服务端")()
	handled, runtimeID, err := logic.ControlViaRuntime("stop")
	if !handled {
		FailWithError(c, http.StatusBadRequest, "当前部署未接入运行时，无法执行独立停止；请先在“运行时管理”中接管服务端")
		return
	}
	if err != nil {
		FailWithError(c, http.StatusBadGateway, "停止失败（运行时 %s）: %v", runtimeID, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已提交停止请求", "runtime_id": runtimeID})
}

func restartByRcon() error {
	conn, err := getRconConnection()
	if err != nil {
		return err
	}

	defer conn.Close()

	_, err = conn.Execute("_restart")
	if err != nil {
		return fmt.Errorf("RCON命令执行失败: %v", err)
	}

	return nil
}
