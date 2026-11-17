import { defineStore } from 'pinia';
import { computed, ref } from 'vue';
import {
  api,
  type RuntimeDescriptor,
  type RuntimeHealth,
  type RuntimeKind,
  type RuntimeListResult,
} from '../services/api';

// 运行时类型的中文名称。后端也会返回 capabilities，这里只负责展示文案。
const KIND_LABELS: Record<string, string> = {
  systemd: 'systemd 服务',
  'systemd-user': 'systemd 用户服务',
  docker: 'Docker 容器',
  screen: 'screen 会话',
  tmux: 'tmux 会话',
  nohup: 'nohup 后台进程',
  bare: '裸进程',
  mcsm: 'MCSManager 实例',
  lgsm: 'LinuxGSM 脚本',
};

const STATE_LABELS: Record<string, string> = {
  running: '运行中',
  starting: '启动中',
  stopped: '已停止',
  degraded: '异常',
  crashed: '已崩溃',
  unknown: '未知',
};

const STATE_COLORS: Record<string, string> = {
  running: 'success',
  starting: 'processing',
  stopped: 'default',
  degraded: 'warning',
  crashed: 'error',
  unknown: 'default',
};

export const useRuntimeStore = defineStore('runtime', () => {
  const runtimes = ref<RuntimeDescriptor[]>([]);
  const defaultId = ref('');
  const agentMode = ref<'bridge' | 'local' | 'disabled'>('disabled');
  const warnings = ref<string[]>([]);
  const loading = ref(false);
  const loadError = ref('');
  const health = ref<Record<string, RuntimeHealth>>({});

  const defaultRuntime = computed(
    () => runtimes.value.find((item) => item.id === defaultId.value) ?? runtimes.value[0] ?? null
  );
  const activeRuntimes = computed(() =>
    runtimes.value.filter((item) => item.enabled && !item.archive_at)
  );
  const agentAvailable = computed(() => agentMode.value !== 'disabled');

  function kindLabel(kind: RuntimeKind | string): string {
    return KIND_LABELS[kind] ?? String(kind);
  }

  function stateLabel(state?: string): string {
    if (!state) return STATE_LABELS.unknown ?? '未知';
    return STATE_LABELS[state] ?? state;
  }

  function stateColor(state?: string): string {
    if (!state) return STATE_COLORS.unknown ?? 'default';
    return STATE_COLORS[state] ?? 'default';
  }

  function healthOf(id: string): RuntimeHealth | null {
    return health.value[id] ?? null;
  }

  async function load(force = false) {
    if (loading.value && !force) return;
    loading.value = true;
    loadError.value = '';
    try {
      const result: RuntimeListResult = await api.getRuntimeList();
      runtimes.value = result.runtimes ?? [];
      defaultId.value = result.default_id ?? '';
      agentMode.value = result.agent_mode ?? 'disabled';
      warnings.value = result.warnings ?? [];
      for (const item of runtimes.value) {
        if (item.last_health) {
          health.value[item.id] = item.last_health;
        }
      }
    } catch (error) {
      loadError.value = error instanceof Error ? error.message : '读取运行时失败';
    } finally {
      loading.value = false;
    }
  }

  async function refreshStatus(id: string) {
    try {
      const result = await api.getRuntimeStatus(id);
      health.value[id] = result.status;
      const target = runtimes.value.find((item) => item.id === id);
      if (target) target.last_health = result.status;
    } catch {
      // 单次状态失败不弹窗，页面会保留上一次数据并显示"未知"
    }
  }

  async function refreshAllStatus() {
    await Promise.all(activeRuntimes.value.map((item) => refreshStatus(item.id)));
  }

  function reset() {
    runtimes.value = [];
    defaultId.value = '';
    agentMode.value = 'disabled';
    warnings.value = [];
    health.value = {};
    loadError.value = '';
  }

  return {
    runtimes,
    defaultId,
    agentMode,
    warnings,
    loading,
    loadError,
    health,
    defaultRuntime,
    activeRuntimes,
    agentAvailable,
    kindLabel,
    stateLabel,
    stateColor,
    healthOf,
    load,
    refreshStatus,
    refreshAllStatus,
    reset,
  };
});
