<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100">启动项管理</h1>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
          管理 L4D2 服务端启动参数与{{ platform.startupConfigLabel }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <a-button :loading="loading" @click="loadConfig">
          <template #icon><ReloadOutlined /></template>
          刷新
        </a-button>
        <a-button type="primary" :loading="saving" :disabled="!canSave" @click="confirmSave">
          <template #icon><SaveOutlined /></template>
          保存并重载
        </a-button>
      </div>
    </div>

    <div v-if="loading" class="flex h-64 items-center justify-center">
      <a-spin size="large" />
    </div>

    <a-result
      v-else-if="loadError"
      status="warning"
      title="无法读取启动项"
      :sub-title="loadError"
    >
      <template #extra>
        <a-button type="primary" @click="loadConfig">重新读取</a-button>
      </template>
    </a-result>

    <template v-else-if="config">
      <a-alert
        v-if="!config.available"
        type="warning"
        show-icon
        :message="
          platform.hasSystemd
            ? '当前部署未提供可编辑的 systemd 启动项文件'
            : '当前平台未找到可编辑的启动脚本'
        "
        :description="
          platform.hasSystemd
            ? '请确认服务单元快照已挂载到 /control/l4d2.service。'
            : `请确认启动脚本位于 ${platform.capabilities.default_control_dir}，或通过 L4D2_STARTUP_UNIT_PATH 指定路径。`
        "
      />

      <template v-else>
        <a-alert
          v-if="!hasGameDirectory"
          type="error"
          show-icon
          message="缺少 -game left4dead2"
          description="L4D2 服务端必须保留游戏目录参数，否则 srcds 无法加载正确游戏。"
        />

        <a-alert
          v-if="duplicateDefinitions.length > 0"
          type="warning"
          show-icon
          message="存在重复启动项"
          :description="`请只保留一个：${duplicateDefinitions.join('、')}`"
        />

        <div class="startup-panel">
          <div class="startup-panel-header">
            <div class="flex min-w-0 items-center gap-3">
              <span class="startup-panel-icon">
                <SettingOutlined />
              </span>
              <div class="min-w-0">
                <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
                  基本启动项
                  <span class="ml-2 text-xs font-normal text-gray-500 dark:text-gray-400">
                    {{ config.service_name }}
                  </span>
                </h2>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ platform.startupConfigPathLabel }}{{ config.service_name }}
                </p>
              </div>
            </div>
            <a-tag :color="reloadTagColor" class="!m-0">{{ reloadStatusLabel }}</a-tag>
          </div>

          <div class="grid grid-cols-1 gap-4 p-4 lg:grid-cols-2">
            <label class="startup-field">
              <span class="startup-field-label">工作目录</span>
              <a-input
                v-model:value="workingDirectory"
                class="font-mono"
                :placeholder="platform.gamePathPlaceholder"
              />
            </label>
            <label class="startup-field">
              <span class="startup-field-label">启动程序</span>
              <a-input
                v-model:value="executable"
                class="font-mono"
                :placeholder="platform.execStartPlaceholder"
              />
            </label>
          </div>
        </div>

        <div class="startup-panel">
          <div class="startup-panel-header">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">推荐预设</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                预设会追加或更新对应参数，不会删除现有自定义项
              </p>
            </div>
            <a-button size="small" @click="openCatalog">
              <template #icon><AppstoreAddOutlined /></template>
              全部启动项
            </a-button>
          </div>
          <div class="grid grid-cols-1 gap-3 p-4 md:grid-cols-2 xl:grid-cols-3">
            <button
              v-for="preset in startupPresets"
              :key="preset.id"
              type="button"
              class="preset-card"
              @click="applyPreset(preset)"
            >
              <span class="flex min-w-0 flex-1 flex-col text-left">
                <span class="flex flex-wrap items-center gap-2">
                  <span class="font-semibold text-gray-900 dark:text-gray-100">{{ preset.name }}</span>
                  <a-tag v-if="preset.risk === 'warning'" color="warning" class="!m-0">需插件</a-tag>
                </span>
                <span class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
                  {{ preset.description }}
                </span>
              </span>
              <PlusOutlined class="shrink-0 text-blue-500" />
            </button>
          </div>
        </div>

        <div class="startup-panel">
          <div class="startup-panel-header startup-panel-header--stackable">
            <div class="min-w-0 flex-1">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">启动参数</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                中文注释会随参数写入{{ platform.startupConfigLabel }}
              </p>
            </div>
            <div class="flex w-full flex-col gap-2 sm:w-auto sm:flex-row">
              <a-input
                v-model:value="entrySearch"
                allow-clear
                class="w-full sm:w-64"
                placeholder="搜索中文名、参数或作用"
              >
                <template #prefix><SearchOutlined /></template>
              </a-input>
              <a-button @click="openTextEditor">
                <template #icon><FormOutlined /></template>
                文本编辑
              </a-button>
              <a-button type="primary" ghost @click="openCatalog">
                <template #icon><PlusOutlined /></template>
                添加启动项
              </a-button>
            </div>
          </div>

          <div class="space-y-3 p-4">
            <div
              v-for="entry in filteredEntries"
              :key="entry.id"
              class="startup-entry"
              :class="{ 'startup-entry--required': startupEntryIsRequired(entry) }"
            >
              <div class="min-w-0 flex-1">
                <div class="mb-2 flex flex-wrap items-center gap-2">
                  <span class="font-semibold text-gray-900 dark:text-gray-100">{{ entry.name }}</span>
                  <a-tag :color="categoryColor(entry.category)" class="!m-0">
                    {{ startupCategoryLabels[entry.category] }}
                  </a-tag>
                  <a-tag v-if="entry.risk === 'warning'" color="warning" class="!m-0">谨慎</a-tag>
                  <a-tag v-if="entry.risk === 'advanced'" class="!m-0">高级</a-tag>
                  <a-tag v-if="entry.requiresPlugin" color="processing" class="!m-0">
                    {{ entry.requiresPlugin }}
                  </a-tag>
                  <a-tag v-if="startupDefinitionForEntry(entry)?.integration === 'sourcetv'" color="warning" class="!m-0">
                    需 SourceTV 修复
                  </a-tag>
                  <a-tag v-if="startupEntryIsRequired(entry)" color="blue" class="!m-0">必需</a-tag>
                </div>

                <div class="startup-comment">
                  <InfoCircleOutlined class="mt-0.5 shrink-0" />
                  <span>{{ entry.comment || entry.description }}</span>
                </div>

                <div class="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-[minmax(140px,0.65fr)_minmax(0,1.35fr)]">
                  <a-select
                    v-if="startupEntryAliases(entry).length > 1"
                    v-model:value="entry.token"
                    :options="startupEntryAliases(entry).map((alias) => ({ label: alias, value: alias }))"
                  />
                  <code v-else class="startup-token">{{ entry.token }}</code>

                  <template v-if="startupDefinitionForEntry(entry)?.value">
                    <a-select
                      v-if="startupDefinitionForEntry(entry)?.value?.kind === 'select'"
                      v-model:value="entry.value"
                      :options="startupDefinitionForEntry(entry)?.value?.options"
                    />
                    <a-input-number
                      v-else-if="startupDefinitionForEntry(entry)?.value?.kind === 'number'"
                      :value="numberValue(entry)"
                      :min="startupDefinitionForEntry(entry)?.value?.min"
                      :max="startupDefinitionForEntry(entry)?.value?.max"
                      class="w-full"
                      @update:value="(value) => updateNumberValue(entry, value)"
                    />
                    <a-input
                      v-else
                      v-model:value="entry.value"
                      :placeholder="startupDefinitionForEntry(entry)?.value?.placeholder"
                    />
                  </template>
                  <div v-else class="startup-flag">
                    <CheckCircleOutlined />
                    无参数开关
                  </div>
                </div>
              </div>

              <div class="flex shrink-0 items-center gap-1 self-start">
                <a-button
                  v-if="!entry.definitionId"
                  type="text"
                  shape="circle"
                  title="编辑自定义参数"
                  @click="openCustomEditor(entry)"
                >
                  <template #icon><EditOutlined /></template>
                </a-button>
                <a-button
                  v-if="!startupEntryIsRequired(entry)"
                  type="text"
                  danger
                  shape="circle"
                  title="移除启动项"
                  @click="removeEntry(entry.id)"
                >
                  <template #icon><DeleteOutlined /></template>
                </a-button>
              </div>
            </div>

            <div v-if="entries.length === 0" class="startup-empty">
              暂无启动参数
            </div>
            <div v-else-if="filteredEntries.length === 0" class="startup-empty">
              未找到匹配的启动参数
            </div>

            <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
              <a-button type="dashed" block @click="openCatalog">
                <template #icon><AppstoreAddOutlined /></template>
                从内置启动项添加
              </a-button>
              <a-button type="dashed" block @click="openCustomEditor()">
                <template #icon><PlusOutlined /></template>
                添加自定义启动项
              </a-button>
            </div>
          </div>
        </div>

        <div class="startup-panel">
          <div class="startup-panel-header">
            <div class="flex items-center gap-2">
              <CodeOutlined class="text-blue-500" />
              <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">最终启动项</h2>
            </div>
            <a-button size="small" @click="copyExecStart">
              <template #icon><CopyOutlined /></template>
              复制
            </a-button>
          </div>

          <div class="space-y-4 p-4">
            <pre class="startup-preview">{{ finalExecStart }}</pre>

            <a-collapse ghost>
              <a-collapse-panel key="comments" header="查看写入服务定义的中文注释">
                <pre class="startup-comment-preview">{{ startupComments.map((line) => `# ${line}`).join('\n') }}</pre>
              </a-collapse-panel>
            </a-collapse>

            <a-alert
              v-if="config.reload_message"
              :type="reloadAlertType"
              show-icon
              :message="config.reload_message"
              :description="reloadDescription"
            />

            <div class="startup-actions">
              <div class="flex flex-wrap gap-2">
                <a-button :disabled="!dirty || saving" @click="resetForm">
                  <template #icon><UndoOutlined /></template>
                  撤销修改
                </a-button>
                <a-button
                  v-if="config.restart_command_available"
                  danger
                  :loading="restarting"
                  :disabled="saving || restarting"
                  @click="confirmRestart"
                >
                  <template #icon><ReloadOutlined /></template>
                  重启游戏服
                </a-button>
              </div>
              <span class="text-xs text-gray-500 dark:text-gray-400">
                保存不会自动重启游戏服
              </span>
            </div>
          </div>
        </div>
      </template>
    </template>

    <a-modal
      v-model:open="catalogOpen"
      title="选择启动项"
      width="min(980px, 96vw)"
      :footer="null"
    >
      <div class="mb-4 grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_180px]">
        <a-input v-model:value="catalogSearch" allow-clear placeholder="搜索中文名、参数或作用">
          <template #prefix><SearchOutlined /></template>
        </a-input>
        <a-select
          v-model:value="catalogCategory"
          :options="[
            { label: '全部分类', value: 'all' },
            ...startupCategoryOrder.map((category) => ({
              label: startupCategoryLabels[category],
              value: category,
            })),
          ]"
        />
      </div>

      <div class="max-h-[65vh] space-y-5 overflow-y-auto pr-1">
        <section v-for="group in filteredCatalogGroups" :key="group.category">
          <h3 class="mb-2 text-sm font-semibold text-gray-800 dark:text-gray-200">
            {{ startupCategoryLabels[group.category] }}
          </h3>
          <div class="space-y-2">
            <div
              v-for="definition in group.items"
              :key="definition.id"
              class="catalog-row"
            >
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="font-medium text-gray-900 dark:text-gray-100">{{ definition.name }}</span>
                  <a-tag v-if="definition.recommended" color="blue" class="!m-0">推荐</a-tag>
                  <a-tag v-if="definition.risk === 'warning'" color="warning" class="!m-0">谨慎</a-tag>
                  <a-tag v-if="definition.risk === 'advanced'" class="!m-0">高级</a-tag>
                  <a-tag v-if="definition.integration === 'sourcetv'" color="warning" class="!m-0">
                    需修复扩展
                  </a-tag>
                  <code class="text-xs text-emerald-600 dark:text-emerald-400">
                    {{ definition.aliases.join(' / ') }}
                  </code>
                </div>
                <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
                  {{ definition.description }}
                </p>
              </div>
              <a-button
                v-if="isDefinitionAdded(definition.id) && definition.required"
                type="primary"
                ghost
                size="small"
                class="startup-catalog-action"
                disabled
              >
                必需
              </a-button>
              <a-button
                v-else-if="isDefinitionAdded(definition.id)"
                type="primary"
                danger
                ghost
                size="small"
                class="startup-catalog-action"
                @click="removeDefinition(definition.id)"
              >
                <template #icon><DeleteOutlined /></template>
                移除
              </a-button>
              <a-button
                v-else
                type="primary"
                ghost
                size="small"
                class="startup-catalog-action"
                @click="addDefinition(definition)"
              >
                <template #icon><PlusOutlined /></template>
                添加
              </a-button>
            </div>
          </div>
        </section>
        <a-empty v-if="filteredCatalogGroups.length === 0" description="没有匹配的启动项" />
      </div>
      <div class="mt-4 border-t border-gray-100 pt-4 dark:border-gray-700">
        <a-button block class="startup-catalog-action" @click="openCustomEditor()">
          <template #icon><PlusOutlined /></template>
          找不到需要的内置项？添加自定义启动项
        </a-button>
      </div>
    </a-modal>

    <a-modal
      v-model:open="customEditorOpen"
      :title="editingCustomEntry ? '编辑自定义启动项' : '添加自定义启动项'"
      width="min(680px, 95vw)"
      :mask-closable="false"
      @cancel="customEditorError = ''"
    >
      <a-alert
        v-if="customEditorError"
        class="mb-4"
        type="error"
        show-icon
        message="无法应用启动项"
        :description="customEditorError"
      />
      <a-form layout="vertical">
        <a-form-item label="中文名称">
          <a-input v-model:value="customDraft.name" placeholder="例如：自定义地图参数" />
        </a-form-item>
        <a-form-item label="中文注释">
          <a-textarea
            v-model:value="customDraft.comment"
            :rows="3"
            placeholder="说明这个参数的作用、适用场景和注意事项"
          />
        </a-form-item>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <a-form-item label="启动选项" required>
            <a-input v-model:value="customDraft.token" class="font-mono" placeholder="-custom" />
          </a-form-item>
          <a-form-item label="参数值">
            <a-input v-model:value="customDraft.value" class="font-mono" placeholder="可留空" />
          </a-form-item>
        </div>
      </a-form>
      <template #footer>
        <a-button @click="customEditorOpen = false">取消</a-button>
        <a-button type="primary" @click="applyCustomEditor">应用到页面</a-button>
      </template>
    </a-modal>

    <a-modal
      v-model:open="textEditorOpen"
      title="文本编辑启动项"
      width="min(900px, 95vw)"
      :mask-closable="false"
      @cancel="textEditorError = ''"
    >
      <a-alert
        class="mb-4"
        type="info"
        show-icon
        message="应用到页面草稿"
        :description="`这里编辑完整的 ExecStart 命令行。应用后仍需点击“保存并重载”才会写入${platform.startupConfigLabel}。`"
      />
      <a-alert
        v-if="textEditorError"
        class="mb-4"
        type="error"
        show-icon
        message="无法应用文本"
        :description="textEditorError"
      />
      <a-textarea
        v-model:value="textEditorDraft"
        :rows="12"
        class="font-mono text-sm"
        spellcheck="false"
      />
      <template #footer>
        <a-button @click="textEditorOpen = false">取消</a-button>
        <a-button type="primary" @click="applyTextEditor">应用到页面</a-button>
      </template>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
  import { computed, h, onMounted, reactive, ref } from 'vue';
  import { message, Modal } from 'ant-design-vue';
  import {
    AppstoreAddOutlined,
    CheckCircleOutlined,
    CodeOutlined,
    CopyOutlined,
    DeleteOutlined,
    EditOutlined,
    ExclamationCircleOutlined,
    FormOutlined,
    InfoCircleOutlined,
    PlusOutlined,
    ReloadOutlined,
    SaveOutlined,
    SearchOutlined,
    SettingOutlined,
    UndoOutlined,
  } from '@ant-design/icons-vue';
  import {
    startupCategoryLabels,
    startupCategoryOrder,
    startupOptions,
    startupPresets,
    type StartupOptionCategory,
    type StartupOptionDefinition,
    type StartupPreset,
  } from '../data/startupOptions';
  import {
    buildStartupComments,
    createCustomStartupEntry,
    createStartupEntry,
    hydrateStartupComments,
    parseStartupCommand,
    serializeStartupCommand,
    startupDefinitionForEntry,
    startupEntryAliases,
    startupEntryIsRequired,
    type StartupEntry,
  } from '../utils/startupOptions';
  import { api, type ServerStartupConfig } from '../services/api';
  import { copyToClipboard } from '../utils/clipboard';
  import { usePlatformStore } from '../stores/platform';

  interface StartupDraftSnapshot {
    workingDirectory: string;
    executable: string;
    command: string;
    comments: string[];
  }

  const platform = usePlatformStore();
  const loading = ref(true);
  const saving = ref(false);
  const restarting = ref(false);
  const loadError = ref('');
  const config = ref<ServerStartupConfig | null>(null);
  const workingDirectory = ref('');
  const executable = ref('');
  const entries = ref<StartupEntry[]>([]);
  const savedSnapshot = ref<StartupDraftSnapshot>({
    workingDirectory: '',
    executable: '',
    command: '',
    comments: [],
  });

  const entrySearch = ref('');
  const catalogOpen = ref(false);
  const catalogSearch = ref('');
  const catalogCategory = ref<StartupOptionCategory | 'all'>('all');

  const customEditorOpen = ref(false);
  const editingCustomEntry = ref<StartupEntry | null>(null);
  const customEditorError = ref('');
  const customDraft = reactive({ name: '', comment: '', token: '', value: '' });

  const textEditorOpen = ref(false);
  const textEditorDraft = ref('');
  const textEditorError = ref('');

  const finalExecStart = computed(() => serializeStartupCommand(executable.value, entries.value));
  const startupComments = computed(() => buildStartupComments(entries.value));
  const dirty = computed(() => {
    const saved = savedSnapshot.value;
    return (
      workingDirectory.value !== saved.workingDirectory ||
      executable.value !== saved.executable ||
      finalExecStart.value !== saved.command ||
      JSON.stringify(startupComments.value) !== JSON.stringify(saved.comments)
    );
  });
  const hasGameDirectory = computed(() =>
    entries.value.some(
      (entry) => entry.definitionId === 'game' && entry.value.trim() === 'left4dead2'
    )
  );
  const duplicateDefinitions = computed(() => {
    const counts = new Map<string, number>();
    for (const entry of entries.value) {
      if (!entry.definitionId) continue;
      counts.set(entry.definitionId, (counts.get(entry.definitionId) || 0) + 1);
    }
    return [...counts.entries()]
      .filter(([, count]) => count > 1)
      .map(([definitionId]) => startupDefinitionForEntry({ definitionId } as StartupEntry)?.name || definitionId);
  });
  const canSave = computed(
    () =>
      Boolean(config.value?.available) &&
      dirty.value &&
      Boolean(workingDirectory.value.trim()) &&
      Boolean(executable.value.trim()) &&
      hasGameDirectory.value &&
      duplicateDefinitions.value.length === 0
  );
  const filteredEntries = computed(() => {
    const query = entrySearch.value.trim().toLocaleLowerCase();
    if (!query) return entries.value;
    return entries.value.filter((entry) =>
      [entry.name, entry.token, entry.value, entry.comment, entry.description]
        .join(' ')
        .toLocaleLowerCase()
        .includes(query)
    );
  });
  const filteredCatalogGroups = computed(() => {
    const query = catalogSearch.value.trim().toLocaleLowerCase();
    return startupCategoryOrder
      .filter((category) => catalogCategory.value === 'all' || catalogCategory.value === category)
      .map((category) => ({
        category,
        items: startupOptions.filter((definition) => {
          if (definition.category !== category) return false;
          if (!query) return true;
          return [
            definition.name,
            definition.description,
            definition.aliases.join(' '),
            startupCategoryLabels[definition.category],
          ]
            .join(' ')
            .toLocaleLowerCase()
            .includes(query);
        }),
      }))
      .filter((group) => group.items.length > 0);
  });
  const reloadStatusLabel = computed(() => {
    switch (config.value?.reload_status) {
      case 'success':
        return platform.hasSystemd ? 'systemd 已重载' : '已保存';
      case 'pending':
        return '等待重载';
      case 'error':
        return '重载失败';
      case 'unavailable':
        return '未连接';
      default:
        return '已连接';
    }
  });
  const reloadTagColor = computed(() => {
    switch (config.value?.reload_status) {
      case 'success':
        return 'success';
      case 'pending':
        return 'processing';
      case 'error':
        return 'error';
      case 'unavailable':
        return 'default';
      default:
        return 'blue';
    }
  });
  const reloadAlertType = computed(() => {
    if (config.value?.reload_status === 'error') return 'error';
    if (config.value?.reload_status === 'success') return 'success';
    return 'info';
  });
  const reloadDescription = computed(() => {
    if (config.value?.reload_status === 'success') {
      return platform.hasSystemd
        ? '启动项已写入 systemd，重启游戏服后生效。'
        : '启动项已保存到启动脚本，重启游戏服后生效。';
    }
    if (config.value?.reload_status === 'error') {
      return platform.hasSystemd
        ? '系统未应用新的服务定义，请检查宿主机 systemd 状态。'
        : '系统未应用新的启动脚本，请检查文件权限。';
    }
    return '宿主机正在应用新的服务定义。';
  });

  const categoryColor = (category: StartupOptionCategory) => {
    switch (category) {
      case 'core':
        return 'blue';
      case 'network':
        return 'cyan';
      case 'gameplay':
        return 'green';
      case 'security':
        return 'orange';
      case 'hltv':
        return 'purple';
      case 'wrapper':
        return 'geekblue';
      default:
        return 'default';
    }
  };

  const numberValue = (entry: StartupEntry): number | undefined => {
    if (entry.value.trim() === '') return undefined;
    const value = Number(entry.value);
    return Number.isFinite(value) ? value : undefined;
  };

  const updateNumberValue = (entry: StartupEntry, value: string | number | null) => {
    entry.value = value === null ? '' : String(value);
  };

  const snapshotCurrent = (): StartupDraftSnapshot => ({
    workingDirectory: workingDirectory.value,
    executable: executable.value,
    command: finalExecStart.value,
    comments: [...startupComments.value],
  });

  const applyConfig = (value: ServerStartupConfig) => {
    config.value = value;
    const parsed = parseStartupCommand(value.exec_start);
    workingDirectory.value = value.working_directory;
    executable.value = parsed.executable;
    entries.value = hydrateStartupComments(parsed.entries, value.startup_comments);
    savedSnapshot.value = snapshotCurrent();
  };

  const loadConfig = async () => {
    loading.value = true;
    loadError.value = '';
    try {
      applyConfig(await api.getServerStartup());
    } catch (error: any) {
      loadError.value = error.message || '启动项读取失败';
    } finally {
      loading.value = false;
    }
  };

  const openCatalog = () => {
    catalogSearch.value = '';
    catalogCategory.value = 'all';
    catalogOpen.value = true;
  };

  const isDefinitionAdded = (definitionId: string) =>
    entries.value.some((entry) => entry.definitionId === definitionId);

  const addDefinition = (definition: StartupOptionDefinition) => {
    if (isDefinitionAdded(definition.id)) {
      message.info(`${definition.name} 已存在`);
      return;
    }
    const entry = createStartupEntry(definition.id);
    if (!entry) return;
    entries.value.push(entry);
  };

  const removeDefinition = (definitionId: string) => {
    const entry = entries.value.find((item) => item.definitionId === definitionId);
    if (!entry || startupEntryIsRequired(entry)) return;
    entries.value = entries.value.filter((item) => item.id !== entry.id);
  };

  const removeEntry = (id: string) => {
    const entry = entries.value.find((item) => item.id === id);
    if (!entry || startupEntryIsRequired(entry)) return;
    entries.value = entries.value.filter((item) => item.id !== id);
  };

  const applyPreset = (preset: StartupPreset) => {
    for (const item of preset.items) {
      const existing = entries.value.find((entry) => entry.definitionId === item.definitionId);
      if (existing) {
        if (item.value !== undefined) existing.value = item.value;
        continue;
      }
      const entry = createStartupEntry(item.definitionId, undefined, item.value);
      if (entry) entries.value.push(entry);
    }
    message.info(`已应用预设“${preset.name}”，请保存并重载`);
  };

  const isOptionToken = (value: string) => /^[+-][A-Za-z]/.test(value);

  const openCustomEditor = (entry?: StartupEntry) => {
    catalogOpen.value = false;
    editingCustomEntry.value = entry || null;
    customEditorError.value = '';
    customDraft.name = entry?.name || '';
    customDraft.comment = entry?.comment || '';
    customDraft.token = entry?.token || '';
    customDraft.value = entry?.value || '';
    customEditorOpen.value = true;
  };

  const applyCustomEditor = () => {
    const token = customDraft.token.trim();
    if (!token) {
      customEditorError.value = '启动选项不能为空。';
      return;
    }
    if (/\s/.test(token)) {
      customEditorError.value = '启动选项本身不能包含空格，参数值请填写到下一栏。';
      return;
    }
    if (!isOptionToken(token)) {
      customEditorError.value = '启动选项通常应以 + 或 - 开头，例如 -custom 或 +custom。';
      return;
    }
    if (/\r|\n/.test(customDraft.value)) {
      customEditorError.value = '参数值不能包含换行。';
      return;
    }
    const duplicate = entries.value.find(
      (entry) => entry.id !== editingCustomEntry.value?.id && entry.token === token
    );
    if (duplicate) {
      customEditorError.value = `启动选项 ${token} 已存在，请修改原条目。`;
      return;
    }

    const name = customDraft.name.trim() || `自定义参数 ${token}`;
    const comment = customDraft.comment.trim() || `自定义启动参数 ${token}`;
    if (editingCustomEntry.value) {
      editingCustomEntry.value.name = name;
      editingCustomEntry.value.comment = comment;
      editingCustomEntry.value.description = comment;
      editingCustomEntry.value.token = token;
      editingCustomEntry.value.value = customDraft.value;
    } else {
      entries.value.push(
        createCustomStartupEntry(token, customDraft.value, name, comment)
      );
    }
    customEditorOpen.value = false;
  };

  const openTextEditor = () => {
    textEditorDraft.value = finalExecStart.value;
    textEditorError.value = '';
    textEditorOpen.value = true;
  };

  const applyTextEditor = () => {
    const command = textEditorDraft.value.trim();
    if (!command) {
      textEditorError.value = '启动命令不能为空。';
      return;
    }
    if (/\r|\n/.test(command)) {
      textEditorError.value = 'ExecStart 必须是一行，不能包含换行。';
      return;
    }
    const parsed = parseStartupCommand(command);
    if (!parsed.executable.startsWith('/')) {
      textEditorError.value = '启动程序必须使用绝对路径。';
      return;
    }
    if (!parsed.entries.some((entry) => entry.definitionId === 'game' && entry.value === 'left4dead2')) {
      textEditorError.value = '完整启动命令必须包含 -game left4dead2。';
      return;
    }
    executable.value = parsed.executable;
    entries.value = parsed.entries;
    textEditorOpen.value = false;
    message.info(`已应用到页面，请点击“保存并重载”写入${platform.startupConfigLabel}`);
  };

  const resetForm = () => {
    applyConfig(config.value as ServerStartupConfig);
  };

  const waitForReload = async (initial: ServerStartupConfig) => {
    let current = initial;
    for (let attempt = 0; attempt < 12; attempt += 1) {
      if (current.reload_status === 'success' || current.reload_status === 'error') break;
      await new Promise((resolve) => window.setTimeout(resolve, 500));
      current = await api.getServerStartup();
      config.value = current;
    }
    applyConfig(current);
    return current;
  };

  const save = async () => {
    if (!hasGameDirectory.value || duplicateDefinitions.value.length > 0) return;
    saving.value = true;
    try {
      const updated = await api.updateServerStartup({
        working_directory: workingDirectory.value.trim(),
        exec_start: finalExecStart.value,
        startup_comments: startupComments.value,
      });
      config.value = updated;
      const result = await waitForReload(updated);
      if (result.reload_status === 'error') {
        message.error(result.reload_message || '重载失败');
      } else {
        message.success(
          platform.hasSystemd
            ? '启动项已保存，systemd 服务定义已重载'
            : '启动项已保存到启动脚本'
        );
      }
    } catch (error: any) {
      message.error(error.message || '启动项保存失败');
    } finally {
      saving.value = false;
    }
  };

  const confirmSave = () => {
    Modal.confirm({
      title: '确认修改服务器启动项？',
      icon: () => h(ExclamationCircleOutlined),
      content: platform.hasSystemd
        ? '保存后会写入 systemd 服务定义和中文注释，并自动执行 daemon-reload。当前游戏服不会自动重启。'
        : '保存后会写入启动脚本和中文注释。当前游戏服不会自动重启，需要重启后生效。',
      okText: '保存并重载',
      onOk: save,
    });
  };

  const copyExecStart = async () => {
    const success = await copyToClipboard(finalExecStart.value);
    if (success) message.success('已复制启动命令');
    else message.error('复制失败，请手动复制');
  };

  const confirmRestart = () => {
    Modal.confirm({
      title: '重启游戏服务器？',
      icon: () => h(ExclamationCircleOutlined),
      content: '所有玩家将断开连接，服务器会使用当前已保存的启动项重新启动。',
      okText: '确认重启',
      okType: 'danger',
      onOk: async () => {
        restarting.value = true;
        try {
          await api.restartServer();
          message.success('重启指令已发送');
        } catch (error: any) {
          message.error(error.message || '重启失败');
        } finally {
          restarting.value = false;
        }
      },
    });
  };

  onMounted(loadConfig);
</script>

<style scoped>
  .startup-panel {
    overflow: hidden;
    border: 1px solid rgb(229 231 235);
    border-radius: 0.75rem;
    background: rgb(255 255 255);
    box-shadow: 0 1px 3px rgb(15 23 42 / 0.05);
  }

  :global(.dark .startup-panel) {
    border-color: rgb(51 65 85);
    background: rgb(31 41 55);
  }

  .startup-panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    border-bottom: 1px solid rgb(243 244 246);
    padding: 1rem;
  }

  :global(.dark .startup-panel-header) {
    border-color: rgb(55 65 81);
  }

  .startup-panel-icon {
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

  :global(.dark .startup-panel-icon) {
    background: rgb(30 58 138 / 0.3);
    color: rgb(96 165 250);
  }

  .startup-field {
    display: flex;
    min-width: 0;
    flex-direction: column;
    gap: 0.5rem;
  }

  .startup-field-label {
    color: rgb(55 65 81);
    font-size: 0.875rem;
    font-weight: 600;
  }

  :global(.dark .startup-field-label) {
    color: rgb(209 213 219);
  }

  .preset-card {
    display: flex;
    min-width: 0;
    align-items: center;
    gap: 0.75rem;
    border: 1px solid rgb(229 231 235);
    border-radius: 0.5rem;
    background: rgb(249 250 251 / 0.65);
    padding: 0.875rem;
    transition:
      border-color 0.2s ease,
      background-color 0.2s ease;
  }

  .preset-card:hover {
    border-color: rgb(96 165 250);
    background: rgb(239 246 255);
  }

  :global(.dark .preset-card) {
    border-color: rgb(55 65 81);
    background: rgb(17 24 39 / 0.4);
  }

  :global(.dark .preset-card:hover) {
    border-color: rgb(59 130 246);
    background: rgb(30 58 138 / 0.2);
  }

  .startup-entry {
    display: flex;
    align-items: flex-start;
    gap: 0.75rem;
    border: 1px solid rgb(229 231 235);
    border-radius: 0.5rem;
    background: rgb(249 250 251 / 0.65);
    padding: 0.875rem;
  }

  .startup-entry--required {
    border-left: 3px solid rgb(59 130 246);
  }

  :global(.dark .startup-entry) {
    border-color: rgb(55 65 81);
    background: rgb(17 24 39 / 0.35);
  }

  .startup-comment {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    color: rgb(107 114 128);
    font-size: 0.75rem;
    line-height: 1.5;
  }

  :global(.dark .startup-comment) {
    color: rgb(156 163 175);
  }

  .startup-token,
  .startup-flag {
    display: flex;
    min-height: 32px;
    align-items: center;
    border: 1px solid rgb(209 213 219);
    border-radius: 0.375rem;
    background: rgb(255 255 255);
    padding: 0.25rem 0.75rem;
    color: rgb(4 120 87);
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 0.875rem;
  }

  .startup-flag {
    gap: 0.5rem;
    color: rgb(22 101 52);
  }

  :global(.dark .startup-token),
  :global(.dark .startup-flag) {
    border-color: rgb(75 85 99);
    background: rgb(17 24 39);
    color: rgb(52 211 153);
  }

  .startup-empty {
    border: 1px dashed rgb(209 213 219);
    border-radius: 0.5rem;
    background: rgb(249 250 251);
    padding: 1.5rem;
    color: rgb(107 114 128);
    text-align: center;
    font-size: 0.875rem;
  }

  :global(.dark .startup-empty) {
    border-color: rgb(75 85 99);
    background: rgb(17 24 39);
    color: rgb(156 163 175);
  }

  .startup-preview,
  .startup-comment-preview {
    overflow-x: auto;
    border: 1px solid rgb(229 231 235);
    border-radius: 0.5rem;
    background: rgb(249 250 251);
    padding: 0.875rem;
    color: rgb(55 65 81);
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 0.75rem;
    line-height: 1.7;
    white-space: pre-wrap;
    word-break: break-all;
  }

  :global(.dark .startup-preview),
  :global(.dark .startup-comment-preview) {
    border-color: rgb(55 65 81);
    background: rgb(17 24 39);
    color: rgb(209 213 219);
  }

  .startup-actions {
    display: flex;
    flex-direction: column-reverse;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
    border-top: 1px solid rgb(243 244 246);
    padding-top: 1rem;
  }

  :global(.dark .startup-actions) {
    border-color: rgb(55 65 81);
  }

  .catalog-row {
    display: flex;
    align-items: center;
    gap: 1rem;
    border: 1px solid rgb(229 231 235);
    border-radius: 0.5rem;
    padding: 0.75rem;
  }

  .startup-catalog-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    line-height: 1;
  }

  .startup-catalog-action :deep(.ant-btn-icon) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    line-height: 1;
  }

  .startup-catalog-action :deep(.ant-btn-icon svg) {
    display: block;
  }

  :global(.dark .catalog-row) {
    border-color: rgb(55 65 81);
  }

  @media (min-width: 640px) {
    .startup-actions {
      flex-direction: row;
      align-items: center;
    }
  }

  @media (max-width: 639px) {
    .startup-panel-header--stackable {
      align-items: stretch;
      flex-direction: column;
    }

    .startup-entry {
      gap: 0.5rem;
    }

    .catalog-row {
      align-items: flex-start;
      flex-direction: column;
    }
  }
</style>
