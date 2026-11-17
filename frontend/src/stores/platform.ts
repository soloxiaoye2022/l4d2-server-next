import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api, type PlatformCapabilities } from '../services/api';

// 修复 D6：接口失败时不能再假装是 systemd 环境。
// 旧值 systemd:true 会让 Windows/无 systemd 的 Linux 显示一堆点了就报错的按钮。
const FALLBACK: PlatformCapabilities = {
  platform: 'linux',
  systemd: false,
  startup_diagnostics: false,
  process_control: false,
  service_update: false,
  path_separator: '/',
  default_game_path: '/left4dead2',
  default_control_dir: '/control',
  startup_config_kind: 'systemd',
  agent_mode: 'unknown',
  runtime_kinds: [],
  default_runtime_id: '',
  runtime_management: false,
  runtime_detection: false,
};

export const usePlatformStore = defineStore('platform', () => {
  const capabilities = ref<PlatformCapabilities>(FALLBACK);
  const loaded = ref(false);

  const isWindows = computed(() => capabilities.value.platform === 'windows');
  const isLinux = computed(() => capabilities.value.platform === 'linux');
  const hasSystemd = computed(() => capabilities.value.systemd);
  const supportsDiagnostics = computed(() => capabilities.value.startup_diagnostics);
  const supportsServiceUpdate = computed(() => capabilities.value.service_update);
  const agentMode = computed(() => capabilities.value.agent_mode ?? 'unknown');
  const supportsRuntimeManagement = computed(() => capabilities.value.runtime_management === true);
  const supportsRuntimeDetection = computed(() => capabilities.value.runtime_detection === true);

  // 平台相关文案集中在这里，避免各页面各写一套 if
  const startupConfigLabel = computed(() =>
    hasSystemd.value ? 'systemd 服务定义' : '启动脚本'
  );
  const startupConfigPathLabel = computed(() =>
    hasSystemd.value ? '/etc/systemd/system/' : '启动脚本：'
  );
  const gamePathPlaceholder = computed(() =>
    isWindows.value ? 'D:\\SteamCMD\\l4d2' : '/home/ubuntu/steamcmd/l4d2'
  );
  const execStartPlaceholder = computed(() =>
    isWindows.value
      ? 'D:\\SteamCMD\\l4d2\\srcds.exe -game left4dead2'
      : '/home/ubuntu/steamcmd/l4d2/srcds_run -game left4dead2'
  );

  async function load(force = false) {
    if (loaded.value && !force) return capabilities.value;
    try {
      capabilities.value = await api.getPlatform();
      loaded.value = true;
    } catch {
      // 接口不可用时按"能力未知"处理：隐藏所有特权按钮，而不是撒谎说支持
      capabilities.value = FALLBACK;
    }
    return capabilities.value;
  }

  return {
    capabilities,
    loaded,
    isWindows,
    isLinux,
    hasSystemd,
    supportsDiagnostics,
    supportsServiceUpdate,
    agentMode,
    supportsRuntimeManagement,
    supportsRuntimeDetection,
    startupConfigLabel,
    startupConfigPathLabel,
    gamePathPlaceholder,
    execStartPlaceholder,
    load,
  };
});
