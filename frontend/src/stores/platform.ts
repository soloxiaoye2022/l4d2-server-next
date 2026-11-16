import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api, type PlatformCapabilities } from '../services/api';

const FALLBACK: PlatformCapabilities = {
  platform: 'linux',
  systemd: true,
  startup_diagnostics: true,
  process_control: true,
  service_update: true,
  path_separator: '/',
  default_game_path: '/left4dead2',
  default_control_dir: '/control',
  startup_config_kind: 'systemd',
};

export const usePlatformStore = defineStore('platform', () => {
  const capabilities = ref<PlatformCapabilities>(FALLBACK);
  const loaded = ref(false);

  const isWindows = computed(() => capabilities.value.platform === 'windows');
  const isLinux = computed(() => capabilities.value.platform === 'linux');
  const hasSystemd = computed(() => capabilities.value.systemd);
  const supportsDiagnostics = computed(() => capabilities.value.startup_diagnostics);
  const supportsServiceUpdate = computed(() => capabilities.value.service_update);

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
      // 接口不可用时按 Linux 处理，保证老部署不受影响
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
    startupConfigLabel,
    startupConfigPathLabel,
    gamePathPlaceholder,
    execStartPlaceholder,
    load,
  };
});
