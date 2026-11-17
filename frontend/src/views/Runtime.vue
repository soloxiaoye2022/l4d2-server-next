<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100">运行时管理</h1>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
          接管宿主机上任意方式部署的 L4D2 服务端，统一启停、看日志、迁移运行时
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <a-button :loading="store.loading" @click="reload">
          <template #icon><ReloadOutlined /></template>
          刷新
        </a-button>
        <a-button :loading="detecting" @click="openDetect">
          <template #icon><ScanOutlined /></template>
          扫描宿主机
        </a-button>
      </div>
    </div>

    <a-alert
      v-if="store.agentMode === 'disabled'"
      type="warning"
      show-icon
      message="宿主机 Agent 不可用"
      description="面板当前无法控制宿主机进程。请确认已挂载 /control、已安装 l4d2-runtime.path，或面板运行在可执行 systemctl/docker 的机器上。"
    />
    <a-alert
      v-else-if="store.agentMode === 'local'"
      type="info"
      show-icon
      message="本地模式"
      description="面板在自身进程内执行命令，只能管理本机 Docker 与裸进程；systemd/screen/tmux 需要安装宿主机 Agent。"
    />

    <a-alert
      v-for="warning in store.warnings"
      :key="warning"
      type="info"
      show-icon
      :message="warning"
    />

    <div v-if="store.loading && store.runtimes.length === 0" class="flex h-64 items-center justify-center">
      <a-spin size="large" />
    </div>

    <a-result
      v-else-if="store.loadError"
      status="warning"
      title="读取运行时失败"
      :sub-title="store.loadError"
    >
      <template #extra>
        <a-button type="primary" @click="reload">重新读取</a-button>
      </template>
    </a-result>

    <template v-else>
      <!-- 默认运行时概览 -->
      <a-card
        v-if="store.defaultRuntime"
        :bordered="false"
        class="shadow-sm dark:bg-gray-800 rounded-xl"
      >
        <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
                {{ store.defaultRuntime.name }}
              </h2>
              <a-tag color="blue" class="!m-0">{{ store.kindLabel(store.defaultRuntime.kind) }}</a-tag>
              <a-tag :color="stateColorOf(store.defaultRuntime.id)" class="!m-0">
                {{ store.stateLabel(healthOf(store.defaultRuntime.id)?.state) }}
              </a-tag>
              <a-tag v-if="store.defaultRuntime.origin === 'legacy'" class="!m-0">旧部署识别</a-tag>
              <a-tag v-if="!store.defaultRuntime.capabilities.persistent" color="orange" class="!m-0">
                重启后不自恢复
              </a-tag>
            </div>
            <p class="mt-2 break-all text-xs text-gray-500 dark:text-gray-400">
              {{ store.defaultRuntime.spec.working_dir || '未记录工作目录' }}
            </p>
            <div class="mt-3 grid grid-cols-2 gap-3 text-xs sm:grid-cols-4">
              <div>
                <div class="text-gray-500 dark:text-gray-400">PID</div>
                <div class="font-medium text-gray-800 dark:text-gray-200">
                  {{ healthOf(store.defaultRuntime.id)?.pid || '—' }}
                </div>
              </div>
              <div>
                <div class="text-gray-500 dark:text-gray-400">端口</div>
                <div class="font-medium text-gray-800 dark:text-gray-200">
                  {{ store.defaultRuntime.spec.port || '—' }}
                </div>
              </div>
              <div>
                <div class="text-gray-500 dark:text-gray-400">端口监听</div>
                <div class="font-medium text-gray-800 dark:text-gray-200">
                  {{ healthOf(store.defaultRuntime.id)?.port_listening ? '是' : '否' }}
                </div>
              </div>
              <div>
                <div class="text-gray-500 dark:text-gray-400">RCON</div>
                <div class="font-medium text-gray-800 dark:text-gray-200">
                  {{ healthOf(store.defaultRuntime.id)?.rcon_reachable ? '可达' : '不可达' }}
                </div>
              </div>
            </div>
          </div>
          <div class="flex flex-wrap gap-2">
            <a-button
              :disabled="!store.defaultRuntime.capabilities.can_start"
              :loading="busy === 'start'"
              @click="runAction(store.defaultRuntime.id, 'start')"
            >
              启动
            </a-button>
            <a-button
              :disabled="!store.defaultRuntime.capabilities.can_stop"
              :loading="busy === 'stop'"
              danger
              @click="runAction(store.defaultRuntime.id, 'stop')"
            >
              停止
            </a-button>
            <a-button
              type="primary"
              :disabled="!store.defaultRuntime.capabilities.can_restart"
              :loading="busy === 'restart'"
              @click="runAction(store.defaultRuntime.id, 'restart')"
            >
              重启
            </a-button>
            <a-button @click="openLogs(store.defaultRuntime.id)">日志</a-button>
            <a-button @click="openMigrate(store.defaultRuntime.id)">迁移</a-button>
          </div>
        </div>
        <a-alert
          v-if="healthOf(store.defaultRuntime.id)?.reason"
          class="mt-4"
          :type="healthOf(store.defaultRuntime.id)?.state === 'degraded' ? 'warning' : 'info'"
          show-icon
          :message="healthOf(store.defaultRuntime.id)?.reason"
        />
      </a-card>

      <!-- 运行时列表 -->
      <a-card :bordered="false" class="shadow-sm dark:bg-gray-800 rounded-xl" :body-style="{ padding: '0' }">
        <div class="flex items-center justify-between border-b border-gray-100 p-4 dark:border-gray-700">
          <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">
            全部运行时（{{ store.runtimes.length }}）
          </h2>
          <a-button size="small" @click="openCreate">新建运行时</a-button>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full min-w-[880px] text-left text-sm">
            <thead class="bg-gray-50 text-gray-500 dark:bg-gray-900/50 dark:text-gray-400">
              <tr>
                <th class="px-4 py-3 font-medium">名称</th>
                <th class="px-4 py-3 font-medium">类型</th>
                <th class="px-4 py-3 font-medium">状态</th>
                <th class="px-4 py-3 font-medium">游戏目录</th>
                <th class="px-4 py-3 font-medium">端口</th>
                <th class="px-4 py-3 font-medium">来源</th>
                <th class="px-4 py-3 font-medium">操作</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-gray-700">
              <tr v-if="store.runtimes.length === 0">
                <td colspan="7" class="px-4 py-10 text-center text-gray-500 dark:text-gray-400">
                  还没有运行时，点击右上角"扫描宿主机"接管已有服务端
                </td>
              </tr>
              <tr v-for="item in store.runtimes" :key="item.id" class="hover:bg-gray-50 dark:hover:bg-gray-700/30">
                <td class="px-4 py-3">
                  <div class="font-medium text-gray-800 dark:text-gray-200">{{ item.name }}</div>
                  <div class="text-xs text-gray-500 dark:text-gray-400">{{ item.id }}</div>
                </td>
                <td class="px-4 py-3">
                  <a-tag color="blue">{{ store.kindLabel(item.kind) }}</a-tag>
                </td>
                <td class="px-4 py-3">
                  <a-tag :color="stateColorOf(item.id)">{{ store.stateLabel(healthOf(item.id)?.state) }}</a-tag>
                </td>
                <td class="px-4 py-3">
                  <span class="break-all text-xs text-gray-600 dark:text-gray-400">
                    {{ item.spec.working_dir || item.game_path || '—' }}
                  </span>
                </td>
                <td class="px-4 py-3 text-gray-600 dark:text-gray-400">{{ item.spec.port || '—' }}</td>
                <td class="px-4 py-3 text-xs text-gray-500 dark:text-gray-400">
                  {{ originLabel(item.origin) }}
                </td>
                <td class="px-4 py-3">
                  <div class="flex flex-wrap gap-1">
                    <a-button size="small" type="link" @click="openDetail(item.id)">详情</a-button>
                    <a-button size="small" type="link" @click="openLogs(item.id)">日志</a-button>
                    <a-button
                      size="small"
                      type="link"
                      :disabled="!item.capabilities.can_restart"
                      @click="runAction(item.id, 'restart')"
                    >
                      重启
                    </a-button>
                    <a-button size="small" type="link" @click="openMigrate(item.id)">迁移</a-button>
                    <a-button
                      v-if="item.adopted"
                      size="small"
                      type="link"
                      danger
                      @click="confirmDetach(item)"
                    >
                      撤销接管
                    </a-button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </a-card>

      <!-- 事件流 -->
      <a-card :bordered="false" class="shadow-sm dark:bg-gray-800 rounded-xl">
        <template #title>最近事件</template>
        <a-timeline v-if="events.length > 0">
          <a-timeline-item v-for="(event, index) in events" :key="`${event.at}-${index}`">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ formatTime(event.at) }}</div>
            <div class="text-sm text-gray-700 dark:text-gray-300">
              <span class="font-medium">{{ event.runtime_id }}</span>
              · {{ store.kindLabel(event.kind) }}
              · {{ event.message || event.state }}
            </div>
          </a-timeline-item>
        </a-timeline>
        <div v-else class="text-sm text-gray-500 dark:text-gray-400">暂无事件</div>
      </a-card>
    </template>

    <!-- 检测抽屉 -->
    <a-drawer
      v-model:open="detectOpen"
      title="扫描宿主机服务端"
      width="720"
      :destroy-on-close="false"
    >
      <div class="space-y-4">
        <a-alert
          v-if="detectResult?.warnings?.length"
          type="warning"
          show-icon
          message="部分运行时扫描失败"
          :description="detectResult.warnings.join('；')"
        />
        <div class="flex items-center justify-between">
          <span class="text-sm text-gray-500 dark:text-gray-400">
            共 {{ detectResult?.candidates.length || 0 }} 个候选
          </span>
          <a-button size="small" :loading="detecting" @click="runDetect">重新扫描</a-button>
        </div>

        <a-empty v-if="!detecting && (detectResult?.candidates.length || 0) === 0" description="没有发现候选服务端" />

        <div v-for="candidate in detectResult?.candidates || []" :key="candidate.key" class="runtime-candidate">
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-medium text-gray-800 dark:text-gray-200">{{ candidate.name }}</span>
            <a-tag color="blue" class="!m-0">{{ store.kindLabel(candidate.kind) }}</a-tag>
            <a-tag :color="candidate.running ? 'success' : 'default'" class="!m-0">
              {{ candidate.running ? '运行中' : '未运行' }}
            </a-tag>
            <a-tag v-if="candidate.persistence === 'ephemeral'" color="orange" class="!m-0">
              重启后不自恢复
            </a-tag>
            <a-tag v-if="candidate.already_runtime_id" class="!m-0">
              已接管：{{ candidate.already_runtime_id }}
            </a-tag>
          </div>

          <div class="mt-2 grid grid-cols-1 gap-1 text-xs text-gray-600 dark:text-gray-400 sm:grid-cols-2">
            <div class="break-all">目录：{{ candidate.game_path || '未识别' }}</div>
            <div>端口：{{ candidate.port || '未识别' }}</div>
            <div class="sm:col-span-2 break-all">命令：{{ commandLine(candidate.spec) }}</div>
          </div>

          <div class="mt-2">
            <div class="mb-1 flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
              <span>置信度</span>
              <a-progress
                :percent="Math.round(candidate.confidence * 100)"
                size="small"
                :show-info="false"
                class="!mb-0 max-w-[180px]"
              />
              <span>{{ Math.round(candidate.confidence * 100) }}%</span>
            </div>
            <ul class="list-disc pl-5 text-xs text-gray-500 dark:text-gray-400">
              <li v-for="(evidence, index) in candidate.evidence" :key="index">{{ evidence }}</li>
            </ul>
          </div>

          <div v-if="!candidate.adoptable" class="mt-2 text-xs text-orange-600 dark:text-orange-400">
            {{ candidate.reason || '不可接管' }}
          </div>
          <div v-else class="mt-3 flex flex-wrap items-center gap-2">
            <a-input v-model:value="adoptIDs[candidate.key]" size="small" placeholder="运行时 ID（小写字母/数字/连字符）" class="max-w-[240px]" />
            <a-input v-model:value="adoptNames[candidate.key]" size="small" placeholder="显示名称（可选）" class="max-w-[220px]" />
            <a-button size="small" type="primary" :loading="adoptingKey === candidate.key" @click="adopt(candidate)">
              接管
            </a-button>
          </div>
        </div>
      </div>
    </a-drawer>

    <!-- 日志弹窗 -->
    <a-modal v-model:open="logsOpen" title="运行时日志" width="880" :footer="null">
      <div class="mb-2 flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
        <span>来源：{{ logsSource || '未知' }}</span>
        <a-button size="small" :loading="logsLoading" @click="loadLogs">刷新</a-button>
      </div>
      <pre class="runtime-log">{{ logsText || '暂无日志' }}</pre>
    </a-modal>

    <!-- 迁移向导 -->
    <a-modal
      v-model:open="migrateOpen"
      title="迁移运行时"
      width="760"
      :confirm-loading="migrating"
      :ok-button-props="{ disabled: !migratePlan || migratePlan.blockers.length > 0 }"
      ok-text="执行迁移"
      cancel-text="取消"
      @ok="applyMigration"
    >
      <div class="space-y-4">
        <div>
          <div class="mb-1 text-sm text-gray-600 dark:text-gray-400">源运行时</div>
          <a-tag color="blue">{{ migrateSource?.name || '—' }}</a-tag>
          <span class="ml-2 text-xs text-gray-500 dark:text-gray-400">
            {{ migrateSource ? store.kindLabel(migrateSource.kind) : '' }}
          </span>
        </div>

        <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
          <div>
            <div class="mb-1 text-sm text-gray-600 dark:text-gray-400">目标类型</div>
            <a-select v-model:value="migrateTargetKind" class="w-full" @change="planMigration">
              <a-select-option
                v-for="kind in migratableKinds"
                :key="kind"
                :value="kind"
                :disabled="kind === migrateSource?.kind"
              >
                {{ store.kindLabel(kind) }}
              </a-select-option>
            </a-select>
          </div>
          <div>
            <div class="mb-1 text-sm text-gray-600 dark:text-gray-400">新运行时 ID</div>
            <a-input v-model:value="migrateTargetID" placeholder="留空自动生成" @blur="planMigration" />
          </div>
        </div>

        <a-spin v-if="planning" />
        <template v-else-if="migratePlan">
          <a-alert
            v-if="migratePlan.blockers.length > 0"
            type="error"
            show-icon
            message="迁移被阻止"
            :description="migratePlan.blockers.join('；')"
          />
          <div>
            <div class="mb-1 text-sm font-medium text-gray-700 dark:text-gray-300">执行步骤</div>
            <ol class="list-decimal pl-5 text-sm text-gray-600 dark:text-gray-400">
              <li v-for="(step, index) in migratePlan.steps" :key="index">{{ step }}</li>
            </ol>
          </div>
          <div>
            <div class="mb-1 text-sm font-medium text-gray-700 dark:text-gray-300">风险提示</div>
            <ul class="list-disc pl-5 text-sm text-orange-600 dark:text-orange-400">
              <li v-for="(risk, index) in migratePlan.risks" :key="index">{{ risk }}</li>
            </ul>
          </div>
        </template>
      </div>
    </a-modal>

    <!-- 详情弹窗 -->
    <a-modal v-model:open="detailOpen" title="运行时详情" width="760" :footer="null">
      <pre class="runtime-log">{{ detailText }}</pre>
    </a-modal>

    <!-- 新建运行时 -->
    <a-modal
      v-model:open="createOpen"
      title="新建运行时"
      width="720"
      :confirm-loading="creating"
      ok-text="创建"
      cancel-text="取消"
      @ok="createRuntime"
    >
      <div class="space-y-3">
        <a-input v-model:value="createForm.id" placeholder="运行时 ID（必填，小写字母/数字/连字符）" />
        <a-input v-model:value="createForm.name" placeholder="显示名称（可选）" />
        <a-select v-model:value="createForm.kind" class="w-full" placeholder="运行时类型">
          <a-select-option v-for="kind in capabilitiesList" :key="kind" :value="kind">
            {{ store.kindLabel(kind) }}
          </a-select-option>
        </a-select>
        <a-input v-model:value="createForm.workingDir" placeholder="工作目录，例如 /home/ubuntu/steamcmd/l4d2" />
        <a-input v-model:value="createForm.executable" placeholder="可执行文件，例如 /home/ubuntu/steamcmd/l4d2/srcds_run" />
        <a-input v-model:value="createForm.args" placeholder="启动参数，例如 -game left4dead2 -console +port 27015" />
        <a-input-number v-model:value="createForm.port" class="w-full" placeholder="游戏端口" :min="1" :max="65535" />
        <a-input v-model:value="createForm.handle" placeholder="句柄：服务名 / 容器名 / 会话名（按类型填写）" />
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
  import { computed, onMounted, onUnmounted, reactive, ref } from 'vue';
  import { message, Modal } from 'ant-design-vue';
  import { ReloadOutlined, ScanOutlined } from '@ant-design/icons-vue';
  import {
    api,
    type RuntimeDescriptor,
    type RuntimeDiscoveryCandidate,
    type RuntimeHealth,
    type RuntimeKind,
    type RuntimeLaunchSpec,
    type RuntimeMigrationPlan,
    type RuntimeEvent,
  } from '../services/api';
  import { useRuntimeStore } from '../stores/runtime';

  const store = useRuntimeStore();

  const busy = ref('');
  const detecting = ref(false);
  const detectOpen = ref(false);
  const detectResult = ref<{ candidates: RuntimeDiscoveryCandidate[]; scanned_at: string; warnings: string[] } | null>(null);
  const adoptIDs = reactive<Record<string, string>>({});
  const adoptNames = reactive<Record<string, string>>({});
  const adoptingKey = ref('');

  const logsOpen = ref(false);
  const logsLoading = ref(false);
  const logsText = ref('');
  const logsSource = ref('');
  const logsTarget = ref('');

  const detailOpen = ref(false);
  const detailText = ref('');

  const migrateOpen = ref(false);
  const migrateSource = ref<RuntimeDescriptor | null>(null);
  const migrateTargetKind = ref<RuntimeKind>('systemd');
  const migrateTargetID = ref('');
  const migratePlan = ref<RuntimeMigrationPlan | null>(null);
  const planning = ref(false);
  const migrating = ref(false);

  const createOpen = ref(false);
  const creating = ref(false);
  const createForm = reactive({
    id: '',
    name: '',
    kind: 'systemd' as RuntimeKind,
    workingDir: '',
    executable: '',
    args: '',
    port: 27015,
    handle: '',
  });

  const events = ref<RuntimeEvent[]>([]);
  let statusTimer: number | null = null;

  const migratableKinds = computed(() =>
    (['systemd', 'systemd-user', 'docker', 'screen', 'tmux', 'nohup'] as RuntimeKind[])
  );

  // store 里没有暴露 capabilitiesList，这里从运行时列表推导一次。
  const capabilitiesList = computed(() => {
    const kinds = new Set<string>(migratableKinds.value);
    for (const item of store.runtimes) kinds.add(item.kind);
    return [...kinds] as RuntimeKind[];
  });

  function healthOf(id: string): RuntimeHealth | null {
    return store.healthOf(id);
  }

  function stateColorOf(id: string) {
    return store.stateColor(healthOf(id)?.state);
  }

  function originLabel(origin: string) {
    switch (origin) {
      case 'legacy':
        return '旧部署识别';
      case 'detected':
        return '扫描接管';
      case 'migrated':
        return '迁移生成';
      case 'installed':
        return '安装生成';
      default:
        return '手动创建';
    }
  }

  function commandLine(spec: RuntimeLaunchSpec | undefined): string {
    if (!spec) return '—';
    const parts = [spec.executable, ...(spec.args || [])].filter(Boolean);
    return parts.length > 0 ? parts.join(' ') : '未识别';
  }

  function formatTime(value?: string) {
    if (!value) return '—';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString();
  }

  async function reload() {
    await store.load(true);
    await store.refreshAllStatus();
    await loadEvents();
  }

  async function loadEvents() {
    try {
      const result = await api.getRuntimeEvents('', 30);
      events.value = result.events || [];
    } catch {
      events.value = [];
    }
  }

  async function runAction(id: string, action: 'start' | 'stop' | 'restart') {
    busy.value = action;
    try {
      const result = await api.runtimeAction(id, action);
      if (result.status === 'success') {
        message.success(result.message || '操作已提交');
      } else {
        message.error(result.message || '操作失败');
      }
      await store.refreshStatus(id);
      await loadEvents();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '操作失败');
    } finally {
      busy.value = '';
    }
  }

  async function openDetect() {
    detectOpen.value = true;
    await runDetect();
  }

  async function runDetect() {
    detecting.value = true;
    try {
      detectResult.value = await api.detectRuntimes();
      for (const candidate of detectResult.value.candidates) {
        if (!adoptIDs[candidate.key]) {
          adoptIDs[candidate.key] = suggestID(candidate);
        }
      }
    } catch (error) {
      message.error(error instanceof Error ? error.message : '扫描失败');
    } finally {
      detecting.value = false;
    }
  }

  function suggestID(candidate: RuntimeDiscoveryCandidate): string {
    const base = (candidate.target.unit_name || candidate.target.container_name || candidate.target.session_name || candidate.name || candidate.kind)
      .toString()
      .toLowerCase()
      .replace(/[^a-z0-9-]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 24);
    return base || candidate.kind;
  }

  async function adopt(candidate: RuntimeDiscoveryCandidate) {
    const id = (adoptIDs[candidate.key] || '').trim();
    if (!id) {
      message.warning('请先填写运行时 ID');
      return;
    }
    adoptingKey.value = candidate.key;
    try {
      await api.adoptRuntime({ candidate_key: candidate.key, id, name: (adoptNames[candidate.key] || '').trim() });
      message.success('接管成功');
      detectOpen.value = false;
      await reload();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '接管失败');
    } finally {
      adoptingKey.value = '';
    }
  }

  function confirmDetach(item: RuntimeDescriptor) {
    Modal.confirm({
      title: `撤销接管 ${item.name}？`,
      content: '只会删除面板里的登记，不会停止正在运行的游戏进程。',
      okText: '撤销接管',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        try {
          await api.detachRuntime(item.id);
          message.success('已撤销接管');
          await reload();
        } catch (error) {
          message.error(error instanceof Error ? error.message : '撤销失败');
        }
      },
    });
  }

  async function openLogs(id: string) {
    logsTarget.value = id;
    logsOpen.value = true;
    logsText.value = '';
    logsSource.value = '';
    await loadLogs();
  }

  async function loadLogs() {
    if (!logsTarget.value) return;
    logsLoading.value = true;
    try {
      const result = await api.getRuntimeLogs(logsTarget.value, 300);
      logsText.value = (result.lines || []).join('\n');
      logsSource.value = result.source || '';
    } catch (error) {
      logsText.value = error instanceof Error ? error.message : '读取日志失败';
    } finally {
      logsLoading.value = false;
    }
  }

  async function openDetail(id: string) {
    try {
      const result = await api.getRuntime(id);
      detailText.value = JSON.stringify({ runtime: result.runtime, events: result.events }, null, 2);
      detailOpen.value = true;
    } catch (error) {
      message.error(error instanceof Error ? error.message : '读取详情失败');
    }
  }

  function openMigrate(id: string) {
    migrateSource.value = store.runtimes.find((item) => item.id === id) ?? null;
    migratePlan.value = null;
    migrateTargetID.value = '';
    migrateTargetKind.value = 'systemd';
    if (migrateSource.value?.kind === 'systemd') {
      migrateTargetKind.value = 'systemd-user';
    }
    migrateOpen.value = true;
    void planMigration();
  }

  async function planMigration() {
    if (!migrateSource.value || !migrateTargetKind.value) return;
    planning.value = true;
    migratePlan.value = null;
    try {
      const options = migrateTargetID.value.trim() ? { id: migrateTargetID.value.trim() } : undefined;
      migratePlan.value = await api.planRuntimeMigration({
        source_id: migrateSource.value.id,
        target_kind: migrateTargetKind.value,
        options,
      });
      migrateTargetID.value = migratePlan.value.target_id;
    } catch (error) {
      message.error(error instanceof Error ? error.message : '生成迁移计划失败');
    } finally {
      planning.value = false;
    }
  }

  async function applyMigration() {
    if (!migratePlan.value) return;
    migrating.value = true;
    try {
      const result = await api.applyRuntimeMigration(migratePlan.value.token);
      if (result.status === 'success') {
        message.success(result.message || '迁移完成');
        migrateOpen.value = false;
        await reload();
      } else {
        message.error(result.message || '迁移失败，已尝试回滚');
      }
    } catch (error) {
      message.error(error instanceof Error ? error.message : '迁移失败');
    } finally {
      migrating.value = false;
    }
  }

  function openCreate() {
    createForm.id = '';
    createForm.name = '';
    createForm.kind = 'systemd';
    createForm.workingDir = '';
    createForm.executable = '';
    createForm.args = '';
    createForm.port = 27015;
    createForm.handle = '';
    createOpen.value = true;
  }

  async function createRuntime() {
    if (!createForm.id.trim()) {
      message.warning('运行时 ID 必填');
      return;
    }
    creating.value = true;
    try {
      const target: Record<string, string> = {};
      const handle = createForm.handle.trim();
      if (handle) {
        if (createForm.kind === 'systemd' || createForm.kind === 'systemd-user') target.unit_name = handle;
        else if (createForm.kind === 'docker') target.container_name = handle;
        else if (createForm.kind === 'screen' || createForm.kind === 'tmux') target.session_name = handle;
        else if (createForm.kind === 'lgsm') target.lgsm_script = handle;
      }
      const args = createForm.args
        .split(/\s+/)
        .map((item) => item.trim())
        .filter(Boolean);
      await api.createRuntime({
        id: createForm.id.trim(),
        name: createForm.name.trim(),
        kind: createForm.kind,
        target: target as RuntimeDescriptor['target'],
        spec: {
          working_dir: createForm.workingDir.trim(),
          executable: createForm.executable.trim(),
          args,
          port: createForm.port,
          auto_restart: true,
        },
      });
      message.success('运行时已创建');
      createOpen.value = false;
      await reload();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '创建失败');
    } finally {
      creating.value = false;
    }
  }

  function startPolling() {
    stopPolling();
    statusTimer = window.setInterval(() => {
      if (document.visibilityState === 'visible') {
        void store.refreshAllStatus();
      }
    }, 5000);
  }

  function stopPolling() {
    if (statusTimer !== null) {
      window.clearInterval(statusTimer);
      statusTimer = null;
    }
  }

  onMounted(async () => {
    await reload();
    startPolling();
  });

  onUnmounted(() => {
    stopPolling();
  });
</script>

<style scoped>
  .runtime-candidate {
    border: 1px solid rgb(229 231 235);
    border-radius: 0.75rem;
    padding: 0.875rem 1rem;
    background: rgb(249 250 251);
  }

  :global(.dark) .runtime-candidate {
    border-color: rgb(55 65 81);
    background: rgb(31 41 55);
  }

  .runtime-log {
    max-height: 420px;
    overflow: auto;
    background: rgb(17 24 39);
    color: rgb(229 231 235);
    border-radius: 0.5rem;
    padding: 0.75rem 1rem;
    font-size: 0.75rem;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
