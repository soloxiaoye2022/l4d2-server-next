package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AgentMode 表示面板当前用什么方式驱动宿主机。
const (
	AgentModeBridge   = "bridge"   // 宿主机 Agent 文件桥接（全能力）
	AgentModeLocal    = "local"    // 面板进程内直接执行（Windows 原生/开发机）
	AgentModeDisabled = "disabled" // 无可用通道，只读展示
)

// Client 是面板与宿主机之间的抽象。两种实现：
// FileBridgeClient 走 /control/runtime 请求-结果文件；LocalClient 直接 exec。
type Client interface {
	Mode() string
	Available() bool
	Submit(request JobRequest, timeout time.Duration) (JobResult, error)
	Probe(timeout time.Duration) error
}

// FileBridgeClient 把请求写成 JSON 交给宿主 systemd path unit，
// 然后轮询结果文件。这是 Docker 部署下的唯一全能力通道。
type FileBridgeClient struct {
	// ControlDir 是宿主机与面板共享的控制目录（容器内通常为 /control）。
	ControlDir string
}

func (c *FileBridgeClient) requestDir() string {
	return filepath.Join(c.ControlDir, "runtime", "requests")
}

func (c *FileBridgeClient) resultDir() string {
	return filepath.Join(c.ControlDir, "runtime", "results")
}

func (c *FileBridgeClient) Mode() string { return AgentModeBridge }

// Available 判断桥接目录是否可写。不可写说明 Agent 未安装或未挂载。
func (c *FileBridgeClient) Available() bool {
	if strings.TrimSpace(c.ControlDir) == "" {
		return false
	}
	directory := c.requestDir()
	if err := os.MkdirAll(directory, 0755); err != nil {
		return false
	}
	probe, err := os.CreateTemp(directory, ".probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
}

// Probe 投递一个只读的 probe 任务，验证 Agent 是否真的在跑。
func (c *FileBridgeClient) Probe(timeout time.Duration) error {
	request := JobRequest{
		JobID:       uuid.NewString(),
		Action:      "probe",
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Requester:   "panel",
	}
	result, err := c.Submit(request, timeout)
	if err != nil {
		return err
	}
	if result.Status != JobSuccess {
		return fmt.Errorf("Agent 探测失败: %s", firstNonEmpty(result.Message, result.Status))
	}
	return nil
}

// Submit 写请求并等待结果。超时返回明确错误，方便前端提示"Agent 未安装"。
func (c *FileBridgeClient) Submit(request JobRequest, timeout time.Duration) (JobResult, error) {
	if request.JobID == "" {
		request.JobID = uuid.NewString()
	}
	if request.RequestedAt == "" {
		request.RequestedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if request.Requester == "" {
		request.Requester = "panel"
	}
	if err := os.MkdirAll(c.requestDir(), 0755); err != nil {
		return JobResult{}, fmt.Errorf("创建 Agent 请求目录失败: %w", err)
	}
	if err := os.MkdirAll(c.resultDir(), 0755); err != nil {
		return JobResult{}, fmt.Errorf("创建 Agent 结果目录失败: %w", err)
	}
	requestPath := filepath.Join(c.requestDir(), request.JobID+".json")
	if err := writeJSONAtomic(requestPath, request, 0600); err != nil {
		return JobResult{}, fmt.Errorf("提交 Agent 任务失败: %w", err)
	}

	resultPath := filepath.Join(c.resultDir(), request.JobID+".json")
	deadline := time.Now().Add(timeout)
	for {
		if result, err := readJobResult(resultPath); err == nil {
			_ = os.Remove(requestPath)
			return result, nil
		}
		if time.Now().After(deadline) {
			return JobResult{
				JobID:   request.JobID,
				Action:  request.Action,
				Status:  JobTimeout,
				Message: "等待宿主机 Agent 结果超时，请确认 l4d2-runtime.path 已启用",
			}, nil
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func readJobResult(path string) (JobResult, error) {
	var result JobResult
	data, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.JobID == "" {
		return result, fmt.Errorf("结果文件缺少 job_id")
	}
	return result, nil
}

// LocalClient 在面板进程内直接执行命令，用于 Windows 原生部署与开发机。
// 它没有 root 特权，也看不到宿主机命名空间，能力必须如实降级。
type LocalClient struct{}

func (c *LocalClient) Mode() string { return AgentModeLocal }

func (c *LocalClient) Available() bool {
	return hasExecutable("docker") || hasExecutable("systemctl") || hasExecutable("tasklist")
}

func (c *LocalClient) Probe(time.Duration) error {
	if !c.Available() {
		return fmt.Errorf("当前环境没有可用的进程控制工具")
	}
	return nil
}

// Submit 在本地模式下直接执行受支持的动作。
// 不支持的组合返回 unsupported，由上层决定是否提示安装 Agent。
func (c *LocalClient) Submit(request JobRequest, timeout time.Duration) (JobResult, error) {
	return c.Run(request, timeout), nil
}

func hasExecutable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
