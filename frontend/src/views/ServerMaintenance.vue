<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100">服务端维护</h1>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
          检查并更新求生服务端，安装 L4D2 SourceTV 修复
        </p>
      </div>
      <a-button :loading="loading" @click="loadAll">
        <template #icon><ReloadOutlined /></template>
        刷新状态
      </a-button>
    </div>

    <div v-if="loading" class="flex h-64 items-center justify-center">
      <a-spin size="large" />
    </div>

    <template v-else>
      <a-result
        v-if="loadError"
        status="warning"
        title="维护状态读取失败"
        :sub-title="loadError"
      >
        <template #extra>
          <a-button type="primary" @click="loadAll">重新读取</a-button>
        </template>
      </a-result>

      <template v-else>
        <div class="maintenance-panel">
          <div class="maintenance-panel-header">
            <div class="flex min-w-0 items-center gap-3">
              <span class="maintenance-panel-icon">
                <CloudDownloadOutlined />
              </span>
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    求生服务端更新
                  </h2>
                  <a-tag :color="updateTagColor" class="!m-0">{{ updateTagText }}</a-tag>
                </div>
                <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
                  Steam App 222860 · Linux/Windows 双平台更新并校验 Linux 服务端
                </p>
              </div>
            </div>
            <a-spin v-if="maintenanceBusy" size="small" />
          </div>

          <div class="space-y-4 p-4">
            <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
              <div class="maintenance-metric">
                <span class="maintenance-metric-label">当前已安装</span>
                <span class="maintenance-metric-value">
                  {{ maintenance?.installed_build_id || '尚未读取' }}
                </span>
                <span class="text-xs text-gray-500 dark:text-gray-400">Steam buildid</span>
              </div>
              <div class="maintenance-metric">
                <span class="maintenance-metric-label">公共分支最新</span>
                <span class="maintenance-metric-value">
                  {{ maintenance?.latest_build_id || '尚未检查' }}
                </span>
                <span class="text-xs text-gray-500 dark:text-gray-400">
                  {{ maintenance?.update_available ? '存在可用更新' : '当前无更新' }}
                </span>
              </div>
              <div class="maintenance-metric">
                <span class="maintenance-metric-label">Game Server</span>
                <span class="maintenance-metric-value">
                  {{ maintenance?.service_name || 'l4d2.service' }}
                </span>
                <span class="break-all text-xs text-gray-500 dark:text-gray-400">
                  {{ maintenance?.install_directory || '读取中' }}
                </span>
              </div>
            </div>

            <a-alert
              v-if="maintenance?.message"
              :type="updateAlertType"
              show-icon
              :message="maintenance.message"
              :description="maintenanceDescription"
            />

            <div class="flex flex-wrap gap-2">
              <a-button
                type="primary"
                ghost
                :loading="checking"
                :disabled="maintenanceBusy"
                @click="checkUpdate"
              >
                <template #icon><ReloadOutlined /></template>
                检查更新
              </a-button>
              <a-button
                type="primary"
                danger
                :loading="updating"
                :disabled="maintenanceBusy"
                @click="confirmUpdate"
              >
                <template #icon><CloudDownloadOutlined /></template>
                {{ maintenance?.update_available ? '立即更新服务端' : '重新校验并更新' }}
              </a-button>
            </div>

            <a-collapse v-if="maintenance?.log_tail" ghost>
              <a-collapse-panel key="log" header="查看 SteamCMD 更新日志">
                <pre class="maintenance-log">{{ maintenance.log_tail }}</pre>
              </a-collapse-panel>
            </a-collapse>

            <a-alert
              type="warning"
              show-icon
              message="更新会中断在线玩家"
              description="立即更新会先停止 l4d2.service，执行 SteamCMD，更新完成后重新启动游戏服。请提前通知玩家。"
            />
          </div>
        </div>

        <div class="maintenance-panel">
          <div class="maintenance-panel-header">
            <div class="flex min-w-0 items-center gap-3">
              <span class="maintenance-panel-icon">
                <SafetyCertificateOutlined />
              </span>
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    L4D2 SourceTV 修复
                  </h2>
                  <a-tag :color="sourceTVTagColor" class="!m-0">{{ sourceTVTagText }}</a-tag>
                </div>
                <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
                  L4D2 原版 SourceTV 广播、录像和断线处理不完整，需要 sourcetvsupport 扩展。
                </p>
              </div>
            </div>
            <a-spin v-if="sourceTVLoading" size="small" />
          </div>

          <div class="space-y-4 p-4">
            <a-alert
              v-if="sourceTVStatus && !sourceTVStatus.sourcemod_present"
              type="error"
              show-icon
              message="未检测到 SourceMod"
              description="请先安装 Metamod:Source 和 SourceMod，再安装 SourceTV 修复扩展。"
            />

            <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
              <div class="maintenance-metric">
                <span class="maintenance-metric-label">内置修复版本</span>
                <span class="maintenance-metric-value">{{ sourceTVStatus?.version || '读取中' }}</span>
                <span class="text-xs text-gray-500 dark:text-gray-400">
                  {{ sourceTVStatus?.platform === 'windows' ? 'Windows' : 'Linux' }} / SourceMod 1.12
                </span>
              </div>
              <div class="maintenance-metric">
                <span class="maintenance-metric-label">启动参数</span>
                <span class="maintenance-metric-value">
                  {{ sourceTVStartupReady ? '已配置广播' : '未完整配置' }}
                </span>
                <span class="text-xs text-gray-500 dark:text-gray-400">
                  -hltv +tv_enable 1 +tv_port
                  {{ sourceTVStatus?.startup_tv_port || sourceTVStatus?.recommended_tv_port || 27021 }}
                </span>
              </div>
              <div class="maintenance-metric">
                <span class="maintenance-metric-label">加载状态</span>
                <span class="maintenance-metric-value">{{ sourceTVRestartLabel }}</span>
                <span class="text-xs text-gray-500 dark:text-gray-400">
                  安装不会自动重启游戏服
                </span>
              </div>
            </div>

            <a-alert
              type="warning"
              show-icon
              message="已知限制"
              :description="sourceTVStatus?.known_limitation || 'SourceTV 中继功能仍不完整，建议使用直接观战连接。'"
            />

            <div class="flex flex-wrap gap-2">
              <a-button
                type="primary"
                :loading="sourceTVInstalling"
                :disabled="!sourceTVStatus?.available || !sourceTVStatus?.sourcemod_present"
                @click="confirmInstallSourceTV"
              >
                <template #icon><CloudDownloadOutlined /></template>
                {{ sourceTVStatus?.installed ? '重新安装并配置 SourceTV' : '一键安装并配置 SourceTV' }}
              </a-button>
              <a-button @click="router.push('/server-startup')">
                <template #icon><SettingOutlined /></template>
                启动项管理
              </a-button>
              <a
                v-if="sourceTVStatus?.repository_url"
                :href="sourceTVStatus.repository_url"
                target="_blank"
                rel="noopener noreferrer"
                class="inline-flex items-center gap-1 rounded-md px-3 py-1.5 text-sm text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950/40"
              >
                <LinkOutlined />
                修复仓库
              </a>
            </div>
          </div>
        </div>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
  import { computed, h, onMounted, onUnmounted, ref } from 'vue';
  import { useRouter } from 'vue-router';
  import { message, Modal } from 'ant-design-vue';
  import {
    CloudDownloadOutlined,
    LinkOutlined,
    ReloadOutlined,
    SafetyCertificateOutlined,
    SettingOutlined,
    WarningOutlined,
  } from '@ant-design/icons-vue';
  import {
    api,
    type ServerMaintenanceStatus,
    type SourceTVSupportStatus,
  } from '../services/api';

  const router = useRouter();
  const loading = ref(true);
  const loadError = ref('');
  const maintenance = ref<ServerMaintenanceStatus | null>(null);
  const sourceTVStatus = ref<SourceTVSupportStatus | null>(null);
  const checking = ref(false);
  const updating = ref(false);
  const sourceTVLoading = ref(false);
  const sourceTVInstalling = ref(false);
  let pollTimer: number | null = null;

  const maintenanceBusy = computed(
    () =>
      checking.value ||
      updating.value ||
      maintenance.value?.status === 'pending' ||
      maintenance.value?.status === 'running'
  );
  const updateTagColor = computed(() => {
    if (maintenance.value?.status === 'error') return 'error';
    if (maintenanceBusy.value) return 'processing';
    if (maintenance.value?.update_available) return 'warning';
    if (maintenance.value?.status === 'success' && !maintenance.value.update_available) return 'success';
    return 'default';
  });
  const updateTagText = computed(() => {
    if (maintenance.value?.status === 'error') return '维护失败';
    if (maintenanceBusy.value) return maintenance.value?.action === 'update' ? '正在更新' : '正在检查';
    if (maintenance.value?.update_available) return '有可用更新';
    if (maintenance.value?.status === 'success') return '已是最新版本';
    return '等待检查';
  });
  const updateAlertType = computed(() => {
    if (maintenance.value?.status === 'error') return 'error';
    if (maintenance.value?.status === 'success') return 'success';
    if (maintenanceBusy.value) return 'info';
    return maintenance.value?.update_available ? 'warning' : 'info';
  });
  const maintenanceDescription = computed(() => {
    if (maintenance.value?.status === 'running' || maintenance.value?.status === 'pending') {
      return '宿主机正在执行任务，页面会自动刷新。';
    }
    if (maintenance.value?.update_available) {
      return `当前 build ${maintenance.value.installed_build_id}，最新 build ${maintenance.value.latest_build_id}。`;
    }
    if (maintenance.value?.latest_build_id) {
      return `已安装 build ${maintenance.value.installed_build_id}，公共分支 build ${maintenance.value.latest_build_id}。`;
    }
    return '检查时会通过 SteamCMD 查询公共分支 buildid。';
  });
  const sourceTVTagColor = computed(() => {
    if (!sourceTVStatus.value) return 'default';
    if (!sourceTVStatus.value.sourcemod_present) return 'error';
    if (!sourceTVStatus.value.installed) return 'warning';
    return sourceTVStatus.value.load_state === 'loaded' ? 'success' : 'processing';
  });
  const sourceTVTagText = computed(() => {
    if (!sourceTVStatus.value) return '检测中';
    if (!sourceTVStatus.value.sourcemod_present) return '缺少 SourceMod';
    if (!sourceTVStatus.value.installed) return '需要修复补丁';
    if (sourceTVStatus.value.load_state === 'loaded') return '修复已加载';
    if (sourceTVStatus.value.load_state === 'unknown') return '已安装，加载未知';
    return '已安装，待重启';
  });
  const sourceTVStartupReady = computed(
    () =>
      Boolean(sourceTVStatus.value?.startup_broadcast_enabled) &&
      Boolean(sourceTVStatus.value?.startup_tv_enabled)
  );
  const sourceTVRestartLabel = computed(() => {
    if (!sourceTVStatus.value?.installed) return '尚未安装';
    if (sourceTVStatus.value.load_state === 'loaded') return '已加载';
    if (sourceTVStatus.value.load_state === 'unknown') return '加载状态未知';
    return '等待重启加载';
  });

  const stopPolling = () => {
    if (pollTimer !== null) {
      window.clearTimeout(pollTimer);
      pollTimer = null;
    }
  };

  const pollMaintenance = async () => {
    try {
      maintenance.value = await api.getServerMaintenanceStatus();
      if (maintenance.value.status === 'pending' || maintenance.value.status === 'running') {
        pollTimer = window.setTimeout(pollMaintenance, 2000);
      } else {
        checking.value = false;
        updating.value = false;
        stopPolling();
      }
    } catch (error: any) {
      checking.value = false;
      updating.value = false;
      message.error(error.message || '读取维护状态失败');
    }
  };

  const loadAll = async () => {
    loading.value = true;
    loadError.value = '';
    sourceTVLoading.value = true;
    try {
      const [maintenanceStatus, sourceTV] = await Promise.all([
        api.getServerMaintenanceStatus(),
        api.getSourceTVSupport(),
      ]);
      maintenance.value = maintenanceStatus;
      sourceTVStatus.value = sourceTV;
      if (maintenanceStatus.status === 'pending' || maintenanceStatus.status === 'running') {
        pollMaintenance();
      }
    } catch (error: any) {
      loadError.value = error.message || '维护状态读取失败';
    } finally {
      loading.value = false;
      sourceTVLoading.value = false;
    }
  };

  const checkUpdate = async () => {
    checking.value = true;
    stopPolling();
    try {
      maintenance.value = await api.checkL4D2ServerUpdate();
      pollMaintenance();
    } catch (error: any) {
      checking.value = false;
      message.error(error.message || '检查更新失败');
    }
  };

  const confirmUpdate = () => {
    Modal.confirm({
      title: '立即更新求生服务端？',
      icon: () => h(WarningOutlined, { style: { color: '#ff4d4f' } }),
      content:
        '更新会先停止游戏服务器，所有在线玩家将断开。SteamCMD 更新完成后会重新启动 l4d2.service。',
      okText: '停服并更新',
      okType: 'danger',
      onOk: async () => {
        updating.value = true;
        stopPolling();
        try {
          maintenance.value = await api.updateL4D2Server();
          pollMaintenance();
        } catch (error: any) {
          updating.value = false;
          message.error(error.message || '提交更新任务失败');
        }
      },
    });
  };

  const confirmInstallSourceTV = () => {
    Modal.confirm({
      title: '安装并配置 L4D2 SourceTV？',
      icon: () => h(WarningOutlined, { style: { color: '#faad14' } }),
      content:
        '将安装内置的 sourcetvsupport 扩展，并把 -hltv、+tv_enable 1、+tv_port 27021 写入启动项。需要重启游戏服后才会加载。',
      okText: '安装并配置',
      onOk: async () => {
        sourceTVInstalling.value = true;
        try {
          sourceTVStatus.value = await api.installSourceTVSupport();
          await api.configureSourceTVStartup();
          sourceTVStatus.value = await api.getSourceTVSupport();
          message.success('SourceTV 修复已安装并配置；重启游戏服后生效');
        } catch (error: any) {
          message.error(error.message || 'SourceTV 安装失败');
        } finally {
          sourceTVInstalling.value = false;
        }
      },
    });
  };

  onMounted(loadAll);
  onUnmounted(stopPolling);
</script>

<style scoped>
  .maintenance-panel {
    overflow: hidden;
    border: 1px solid rgb(229 231 235);
    border-radius: 0.75rem;
    background: rgb(255 255 255);
    box-shadow: 0 1px 3px rgb(15 23 42 / 0.05);
  }

  :global(.dark .maintenance-panel) {
    border-color: rgb(51 65 85);
    background: rgb(31 41 55);
  }

  .maintenance-panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    border-bottom: 1px solid rgb(243 244 246);
    padding: 1rem;
  }

  :global(.dark .maintenance-panel-header) {
    border-color: rgb(55 65 81);
  }

  .maintenance-panel-icon {
    display: inline-flex;
    flex: none;
    width: 2.5rem;
    height: 2.5rem;
    align-items: center;
    justify-content: center;
    border-radius: 0.5rem;
    background: rgb(239 246 255);
    color: rgb(37 99 235);
  }

  :global(.dark .maintenance-panel-icon) {
    background: rgb(30 58 138 / 0.3);
    color: rgb(96 165 250);
  }

  .maintenance-metric {
    display: flex;
    min-width: 0;
    flex-direction: column;
    gap: 0.25rem;
    border: 1px solid rgb(229 231 235);
    border-radius: 0.5rem;
    background: rgb(249 250 251 / 0.7);
    padding: 0.75rem;
  }

  :global(.dark .maintenance-metric) {
    border-color: rgb(55 65 81);
    background: rgb(17 24 39 / 0.4);
  }

  .maintenance-metric-label {
    color: rgb(107 114 128);
    font-size: 0.75rem;
  }

  .maintenance-metric-value {
    color: rgb(31 41 55);
    font-size: 0.875rem;
    font-weight: 700;
  }

  :global(.dark .maintenance-metric-label) {
    color: rgb(156 163 175);
  }

  :global(.dark .maintenance-metric-value) {
    color: rgb(229 231 235);
  }

  .maintenance-log {
    max-height: 24rem;
    overflow: auto;
    border-radius: 0.5rem;
    background: rgb(17 24 39);
    padding: 0.75rem;
    color: rgb(209 213 219);
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 0.75rem;
    line-height: 1.6;
    white-space: pre-wrap;
  }

  @media (max-width: 639px) {
    .maintenance-panel-header {
      align-items: flex-start;
    }
  }
</style>
