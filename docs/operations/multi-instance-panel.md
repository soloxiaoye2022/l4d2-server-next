# 单面板多实例设计

当前面板以单个游戏目录、单个 RCON 地址和单个 systemd/Docker 服务为目标。直接在现有进程里循环切换全局变量会导致并发请求互相污染，因此多实例不能通过临时修改 `consts.GamePath` 和 `L4D2_RCON_URL` 实现。

## 推荐结构

采用“实例注册表 + 请求级实例上下文”的两层结构。

### 实例注册表

新增 `data/instances.json`，每一项至少包含：

```json
{
  "id": "survival-1",
  "name": "写实一服",
  "kind": "systemd",
  "game_path": "/left4dead2",
  "rcon_url": "127.0.0.1:27015",
  "rcon_password_env": "INSTANCE_SURVIVAL_1_RCON_PASSWORD",
  "restart_command": "touch /control/survival-1/restart",
  "startup_unit_path": "/control/survival-1/l4d2.service",
  "service_name": "l4d2-survival-1.service",
  "manager_url": "http://127.0.0.1:27101"
}
```

密码不能直接返回前端，建议引用环境变量或独立密钥文件。

### 请求级上下文

- 前端在每次 API 请求中发送 `X-L4D2-Instance: <id>`。
- 后端中间件验证实例 ID，把实例配置放入 `gin.Context`。
- Controller 不再直接读取全局 `consts.GamePath`、`L4D2_RCON_URL` 或 `L4D2_RESTART_CMD`。
- Logic 层通过显式 `InstanceContext` 参数获取路径、RCON、服务和重启命令。
- 缓存键必须包含实例 ID，避免地图、插件、监控和玩家统计串数据。

## 第一阶段

完成实例注册表、顶部实例切换器、请求头和只读实例状态。先让一个面板可以切换并查看现有多个管理器的状态，不立即提供创建和删除。

## 第二阶段

增加实例生命周期接口：

- 创建：选择端口、名称、部署方式、游戏目录和镜像。
- 启动、停止、重启：调用 Docker Compose 或受控的宿主机 systemd helper。
- 删除：默认保留数据卷，必须二次确认并生成备份。
- 端口检查：同时检查游戏端口、RCON、SourceTV、管理器端口和实例重名。

## 第三阶段

统一监控、备份、地图和插件视图。每个实例仍保留独立数据目录和独立容器，避免一个实例的插件或配置影响其他实例。

## 不推荐的方案

- 在一个进程里切换全局 `GamePath` 和 RCON 环境变量。
- 所有实例共用一个游戏目录或一个 `data/` 数据库。
- 用前端只隐藏实例的写操作而由后端共享同一权限上下文。
- 通过拼接未验证的实例 ID 生成文件路径或命令。

## 现有基础

仓库的 Linux 多实例安装器已经能生成独立容器、数据卷、面板端口和 Compose 服务。单面板多实例功能应复用这套部署边界，而不是把多个游戏服塞进同一个管理器进程。
