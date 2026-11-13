<template>
  <a-modal
    :open="open"
    title="启动诊断"
    :footer="null"
    :width="900"
    destroy-on-close
    @update:open="emit('update:open', $event)"
  >
    <div class="space-y-4">
      <div v-if="loading" class="flex flex-col items-center gap-3 py-12">
        <a-spin size="large" />
        <div class="text-sm text-gray-500 dark:text-gray-400">
          正在从宿主机采集 systemctl / journal 信息...
        </div>
      </div>

      <template v-else>
        <a-alert
          :type="summaryType"
          show-icon
          :message="data?.summary || '诊断结果'"
          :description="data?.available === false ? data?.message : undefined"
        />

        <a-descriptions v-if="data?.available" bordered size="small" :column="2">
          <a-descriptions-item label="systemd 状态">
            {{ data.active_state || '-' }} / {{ data.sub_state || '-' }}
          </a-descriptions-item>
          <a-descriptions-item label="MainPID">
            {{ data.main_pid || '-' }}
          </a-descriptions-item>
          <a-descriptions-item label="重启次数">
            {{ data.n_restarts ?? '-' }}
          </a-descriptions-item>
          <a-descriptions-item label="退出结果">
            {{ data.result || '-' }} ({{ data.exec_main_status || '-' }})
          </a-descriptions-item>
          <a-descriptions-item label="游戏端口">
            {{ data.port || '-' }}（{{ data.port_listening ? '已监听' : '未监听' }}）
          </a-descriptions-item>
          <a-descriptions-item label="服务名">
            {{ data.service_name || '-' }}
          </a-descriptions-item>
          <a-descriptions-item label="单元文件" :span="2">
            <span class="break-all">{{ data.unit_path || '-' }}</span>
          </a-descriptions-item>
        </a-descriptions>

        <div v-if="data?.hints?.length" class="space-y-2">
          <div
            v-for="(hint, index) in data.hints"
            :key="index"
            class="rounded-lg border border-gray-200 p-3 dark:border-gray-700"
          >
            <div class="flex items-center gap-2">
              <a-tag :color="tagColor(hint.level)">
                {{ levelLabel(hint.level) }}
              </a-tag>
              <span class="font-semibold text-gray-800 dark:text-gray-100">{{ hint.title }}</span>
            </div>
            <div
              v-if="hint.detail"
              class="mt-2 whitespace-pre-wrap text-sm text-gray-600 dark:text-gray-300"
            >
              {{ hint.detail }}
            </div>
            <div v-if="hint.suggestion" class="mt-2 text-sm text-blue-600 dark:text-blue-400">
              建议：{{ hint.suggestion }}
            </div>
          </div>
        </div>

        <a-collapse ghost>
          <a-collapse-panel v-if="journalLines.length" key="journal" header="journal 原始日志（最近 200 行）">
            <pre
              class="max-h-80 overflow-auto rounded-lg bg-gray-950 p-3 text-xs leading-relaxed text-gray-100"
              >{{ journalText }}</pre
            >
          </a-collapse-panel>
          <a-collapse-panel v-if="data?.status_text" key="status" header="systemctl status">
            <pre
              class="max-h-64 overflow-auto rounded-lg bg-gray-950 p-3 text-xs leading-relaxed text-gray-100"
              >{{ data.status_text }}</pre
            >
          </a-collapse-panel>
          <a-collapse-panel v-if="processesText" key="processes" header="游戏进程">
            <pre
              class="max-h-48 overflow-auto rounded-lg bg-gray-950 p-3 text-xs leading-relaxed text-gray-100"
              >{{ processesText }}</pre
            >
          </a-collapse-panel>
          <a-collapse-panel v-if="history?.length" key="history" header="处置记录（最近 20 条）">
            <div class="max-h-72 space-y-2 overflow-auto">
              <div
                v-for="entry in history"
                :key="entry.id"
                class="rounded-lg border border-gray-200 p-3 text-sm dark:border-gray-700"
              >
                <div class="flex flex-wrap items-center gap-2">
                  <a-tag :color="entryTagColor(entry.status)">{{ statusLabel(entry.status) }}</a-tag>
                  <span class="font-semibold">{{ actionLabel(entry.action) }}</span>
                  <span class="text-xs text-gray-500">{{ formatTime(entry.at) }}</span>
                  <span v-if="entry.operator" class="text-xs text-gray-500">
                    {{ entry.operator }}
                  </span>
                  <span class="text-xs text-gray-500">{{ entry.duration_ms }}ms</span>
                </div>
                <div class="mt-1 text-gray-700 dark:text-gray-300">{{ entry.message }}</div>
                <div v-if="entry.quarantined?.length" class="mt-1 text-xs text-gray-500">
                  隔离：{{ entry.quarantined.map((item) => item.name).join('、') }}
                </div>
              </div>
            </div>
          </a-collapse-panel>
        </a-collapse>

        <div
          v-if="hasBrokenVpk"
          class="rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-900 dark:bg-red-950/40"
        >
          <div class="font-semibold text-red-700 dark:text-red-300">可自动处置</div>
          <div class="mt-1 text-sm text-red-700/80 dark:text-red-300/80">
            隔离会把文件移动到 addons/.quarantine-时间戳/（不删除），并清理 addonlist / maplist
            中的引用；热重载只刷新 addon 路径，不踢人；重启会断开所有玩家。
          </div>
          <div class="mt-3 flex flex-wrap gap-2">
            <a-button
              type="primary"
              :loading="repairing"
              @click="emit('repair', 'quarantine_and_reload')"
            >
              隔离并热重载
            </a-button>
            <a-button danger :loading="repairing" @click="emit('repair', 'quarantine_and_restart')">
              隔离并重启
            </a-button>
          </div>
        </div>

        <div
          v-if="hasPortConflict"
          class="rounded-lg border border-orange-200 bg-orange-50 p-3 dark:border-orange-900 dark:bg-orange-950/40"
        >
          <div class="font-semibold text-orange-700 dark:text-orange-300">端口被其他进程占用</div>
          <div class="mt-1 text-sm text-orange-700/80 dark:text-orange-300/80">
            游戏可能已自动切到下一个端口，玩家连不上。终止占用进程后会重启游戏服。
          </div>
          <div class="mt-3">
            <a-button
              danger
              type="primary"
              :loading="repairing"
              @click="emit('repair', 'kill_port_and_restart')"
            >
              终止占用并重启
            </a-button>
          </div>
        </div>

        <a-alert
          v-if="repairResult"
          :type="repairResultType"
          show-icon
          :message="`上次处置：${repairResult.message}`"
        >
          <template #description>
            <div v-if="repairResult.quarantined?.length" class="text-sm">
              已隔离：{{ quarantinedNames }}
            </div>
            <div v-if="repairResult.quarantine_dir" class="mt-1 break-all text-xs">
              隔离目录：{{ repairResult.quarantine_dir }}
            </div>
            <div v-if="repairResult.cleaned_references?.length" class="mt-1 text-xs">
              引用清理：{{ cleanedReferenceText }}
            </div>
            <div v-if="repairResult.reload?.requested" class="mt-1 text-xs">
              热重载：{{ repairResult.reload.ok ? '成功' : '失败' }}
              <span v-if="repairResult.reload.command">（{{ repairResult.reload.command }}）</span>
              <span v-if="repairResult.reload.message"> {{ repairResult.reload.message }}</span>
            </div>
            <div v-if="repairResult.verify_remaining?.length" class="mt-1 text-xs text-red-600">
              复检仍有异常：{{ repairResult.verify_remaining.map((item) => item.name).join('、') }}
            </div>
            <div v-if="repairResult.killed?.length" class="mt-1 text-xs">
              已终止：{{ killedText }}
            </div>
          </template>
        </a-alert>

        <div class="flex justify-end gap-2">
          <a-button v-if="canRepair" :loading="repairing" @click="emit('repair', 'restart_service')">
            重启服务
          </a-button>
          <a-button :loading="loading" @click="emit('refresh')">重新采集</a-button>
          <a-button type="primary" @click="emit('update:open', false)">关闭</a-button>
        </div>
      </template>
    </div>
  </a-modal>
</template>

<script setup lang="ts">
  import { computed } from 'vue';
  import type {
    ServerDiagnosticLevel,
    ServerDiagnosticsResult,
    ServerRepairAction,
    ServerRepairHistoryEntry,
    ServerRepairResult,
  } from '../services/api';

  const props = defineProps<{
    open: boolean;
    data: ServerDiagnosticsResult | null;
    loading: boolean;
    repairing?: boolean;
    repairResult?: ServerRepairResult | null;
    history?: ServerRepairHistoryEntry[];
  }>();

  const emit = defineEmits<{
    (e: 'update:open', value: boolean): void;
    (e: 'refresh'): void;
    (e: 'repair', action: ServerRepairAction): void;
  }>();

  const hasBrokenVpk = computed(() =>
    (props.data?.hints || []).some((hint) => hint.title.includes('发现损坏的地图 VPK')),
  );

  const hasPortConflict = computed(() =>
    (props.data?.hints || []).some((hint) => hint.title.includes('游戏端口被其他进程占用')),
  );

  const canRepair = computed(() => props.data?.available === true);

  const killedText = computed(() =>
    (props.repairResult?.killed || [])
      .map((item) => `${item.command}(pid=${item.pid})`)
      .join('、'),
  );

  const repairResultType = computed<'error' | 'warning' | 'info'>(() => {
    const status = props.repairResult?.status;
    if (status === 'ok') return 'info';
    if (status === 'partial') return 'warning';
    return 'error';
  });

  const quarantinedNames = computed(() =>
    (props.repairResult?.quarantined || [])
      .map((item) => `${item.name}（${item.reason}）`)
      .join('、'),
  );

  const cleanedReferenceText = computed(() =>
    (props.repairResult?.cleaned_references || [])
      .map((item) => `${item.file} 移除 ${item.removed} 行`)
      .join('；'),
  );

  const summaryType = computed<'error' | 'warning' | 'info'>(() => {
    const level = props.data?.hints?.[0]?.level;
    if (level === 'error') return 'error';
    if (level === 'warning') return 'warning';
    if (props.data?.available === false) return 'warning';
    return 'info';
  });

  const journalLines = computed(() => props.data?.journal || []);
  const journalText = computed(() => journalLines.value.join('\n'));
  const processesText = computed(() => (props.data?.processes || []).join('\n'));

  const tagColor = (level: ServerDiagnosticLevel) => {
    if (level === 'error') return 'red';
    if (level === 'warning') return 'orange';
    return 'blue';
  };

  const levelLabel = (level: ServerDiagnosticLevel) => {
    if (level === 'error') return '错误';
    if (level === 'warning') return '警告';
    return '提示';
  };

  const actionLabel = (action: string) => {
    if (action === 'quarantine_and_reload') return '隔离并热重载';
    if (action === 'quarantine_and_restart') return '隔离并重启';
    if (action === 'restart_service') return '重启服务';
    if (action === 'kill_port_and_restart') return '终止占用并重启';
    if (action === 'quarantine') return '仅隔离';
    return action;
  };

  const statusLabel = (status: string) => {
    if (status === 'ok') return '成功';
    if (status === 'partial') return '部分完成';
    if (status === 'noop') return '无需处置';
    if (status === 'timeout') return '超时';
    return '失败';
  };

  const entryTagColor = (status: string) => {
    if (status === 'ok') return 'green';
    if (status === 'partial') return 'orange';
    if (status === 'noop') return 'blue';
    return 'red';
  };

  const formatTime = (value: string) => {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN', { hour12: false });
  };
</script>
