<template>
  <div>
    <div class="flex flex-col">
      <div class="flex items-center justify-between gap-3 mb-6">
        <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100">服务器配置</h1>
        <a-button
          v-if="isAdmin"
          type="primary"
          size="large"
          :loading="saving"
          :disabled="hasTrailingComments"
          @click="save"
        >
          保存修改
        </a-button>
      </div>

      <div v-if="loading" class="flex justify-center items-center h-64">
        <a-spin size="large" />
      </div>

      <div v-else class="flex flex-col gap-6">
        <a-alert
          v-if="!isAdmin"
          message="只读模式"
          description="您当前使用的是授权码登录，仅具有查看权限。如需修改配置，请使用管理员密码登录。"
          type="info"
          show-icon
        />

        <div class="flex flex-col bg-white dark:bg-gray-800 p-4 rounded-lg shadow shrink-0">
          <div
            class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-4 shrink-0"
          >
            <h2 class="text-lg font-semibold text-gray-800 dark:text-gray-200">
              基本设置
              <span class="text-xs font-normal text-gray-500 block sm:inline sm:ml-2"
                >server.cfg</span
              >
            </h2>
            <a-button
              class="server-config-action-button w-full sm:w-auto"
              @click="fixedTextOpen = true"
            >
              <template #icon><FileTextOutlined /></template>
              查看文本
            </a-button>
          </div>

          <div class="flex flex-col gap-4">
            <div class="flex items-center justify-between gap-3">
              <span class="text-gray-700 dark:text-gray-300">隐藏服务器 (sv_tags hidden)</span>
              <a-switch v-model:checked="form.hidden" :disabled="!isAdmin" />
            </div>

            <div class="flex items-center justify-between gap-3">
              <span class="text-gray-700 dark:text-gray-300"
                >开启匹配 (sv_allow_lobby_connect_only)</span
              >
              <a-switch v-model:checked="form.lobby_connect_only" :disabled="!isAdmin" />
            </div>

            <div class="flex flex-col gap-2">
              <span class="text-gray-700 dark:text-gray-300">Steam 组 ID (sv_steamgroup)</span>
              <a-input
                v-model:value="form.steam_group"
                placeholder="输入 Steam 组 ID，留空则删除该设置"
                :disabled="!isAdmin"
              />
              <span v-if="!isAdmin" class="text-xs text-gray-500 dark:text-gray-400">
                Steam 组 ID 已由服务端脱敏，访客无法查看原值。
              </span>
            </div>
          </div>
        </div>

        <div class="flex flex-col bg-white dark:bg-gray-800 p-4 rounded-lg shadow shrink-0">
          <div
            class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-4 shrink-0"
          >
            <div class="flex flex-col sm:flex-row sm:items-center gap-3 min-w-0 flex-1">
              <h2 class="text-lg font-semibold text-gray-800 dark:text-gray-200 shrink-0">
                自定义参数
              </h2>
              <a-input
                v-model:value="customSearch"
                allow-clear
                class="w-full sm:max-w-xs"
                placeholder="搜索指令或注释"
              >
                <template #prefix><SearchOutlined /></template>
              </a-input>
            </div>
            <div class="flex w-full flex-wrap items-center gap-2 sm:w-auto">
              <a-button
                class="server-config-action-button w-full sm:w-auto"
                :disabled="!isAdmin"
                @click="openCvarLibrary"
              >
                <template #icon><BookOutlined /></template>
                参数库
              </a-button>
              <a-button
                class="server-config-action-button w-full sm:w-auto"
                :disabled="!isAdmin"
                @click="openTextEditor"
              >
                <template #icon><FormOutlined /></template>
                文本编辑
              </a-button>
            </div>
          </div>

          <a-alert
            v-if="hasTrailingComments"
            class="mb-4"
            type="warning"
            show-icon
            message="存在未关联指令的末尾注释"
            :description="trailingCommentMessage"
          />

          <div class="flex flex-col gap-3">
            <div
              v-for="item in filteredCustomEntries"
              :key="item.entry.id"
              class="server-config-entry relative rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50/70 dark:bg-gray-900/40 p-3 sm:p-4"
            >
              <div class="min-w-0 flex flex-col gap-2 pr-20">
                <div
                  v-if="item.entry.comments.length"
                  class="px-1 text-sm leading-6 text-gray-500 dark:text-gray-400 whitespace-pre-wrap break-words"
                >
                  {{ item.entry.comments.join('\n') }}
                </div>

                <div class="min-w-0">
                  <code
                    class="block rounded-md border border-gray-200 bg-white px-3 py-2 text-sm leading-6 text-emerald-700 whitespace-pre-wrap break-all dark:border-gray-800 dark:bg-black dark:text-emerald-300"
                    >{{ item.entry.command }}</code
                  >
                  <CvarDefinitionDetails
                    v-if="getCustomEntryDefinition(item.entry.command)"
                    class="mt-3"
                    :definition="getCustomEntryDefinition(item.entry.command)"
                    :selectable="isAdmin && !hasTrailingComments"
                    :selected-value="parseCvarCommand(item.entry.command)?.value ?? ''"
                    @select="(value) => applyCvarValueToEntry(item.index, value)"
                  />
                  <div
                    v-else
                    class="mt-3 rounded-md border border-dashed border-gray-300 px-3 py-2 text-xs leading-5 text-gray-500 dark:border-gray-700 dark:text-gray-400"
                  >
                    这个命令暂未收录中文说明，仍会按原样写入 cfg。可先用参数库搜索，或在游戏控制台执行
                    <code class="text-gray-700 dark:text-gray-300">help 命令名</code>
                    查看服务器返回的帮助。
                  </div>
                </div>
              </div>

              <div
                v-if="isAdmin"
                class="absolute right-3 top-3 sm:right-4 sm:top-4 flex items-center gap-1"
              >
                <a-button
                  class="entry-icon-button"
                  type="text"
                  shape="circle"
                  aria-label="编辑自定义配置"
                  title="编辑"
                  :disabled="hasTrailingComments"
                  @click="openEntryEditor(item.index)"
                >
                  <template #icon><EditOutlined /></template>
                </a-button>
                <a-popconfirm
                  title="确认删除该配置及其注释吗？"
                  ok-text="确定"
                  cancel-text="取消"
                  :get-popup-container="getDeletePopupContainer"
                  :disabled="hasTrailingComments"
                  @confirm="removeCustomConfig(item.index)"
                >
                  <a-button
                    danger
                    class="entry-icon-button"
                    type="text"
                    shape="circle"
                    aria-label="删除自定义配置"
                    title="删除"
                    :disabled="hasTrailingComments"
                  >
                    <template #icon><DeleteOutlined /></template>
                  </a-button>
                </a-popconfirm>
              </div>
            </div>

            <div
              v-if="customEntries.length === 0 && !hasTrailingComments"
              class="text-gray-500 dark:text-gray-400 text-center py-6 bg-gray-50 dark:bg-gray-900 rounded border border-dashed border-gray-300 dark:border-gray-700"
            >
              暂无自定义参数
            </div>
            <div
              v-else-if="filteredCustomEntries.length === 0 && !hasTrailingComments"
              class="text-gray-500 dark:text-gray-400 text-center py-6 bg-gray-50 dark:bg-gray-900 rounded border border-dashed border-gray-300 dark:border-gray-700"
            >
              未找到匹配的自定义参数
            </div>

            <a-button
              v-if="isAdmin"
              type="dashed"
              block
              :disabled="hasTrailingComments"
              @click="openEntryEditor()"
            >
              <template #icon><PlusOutlined /></template>
              添加一行
            </a-button>
          </div>
        </div>
      </div>
    </div>

    <a-modal
      v-model:open="fixedTextOpen"
      title="固定配置（只读）"
      width="min(900px, 95vw)"
      :footer="null"
    >
      <a-alert
        class="mb-4"
        style="margin-bottom: 8px"
        type="info"
        show-icon
        message="当前活动 server.cfg"
        description="仅展示自定义区域之前的固定内容；密码等敏感值已在服务端脱敏。如需修改，请在下方使用自定义配置覆盖对应项。"
      />
      <pre
        v-if="fixedConfig"
        class="m-0 max-h-[65vh] overflow-auto rounded-lg bg-gray-950 p-4 text-sm leading-6 text-gray-100"
        >{{ fixedConfig }}</pre
      >
      <a-empty v-else description="暂无固定配置" />
    </a-modal>

    <a-modal
      v-model:open="textEditorOpen"
      title="文本编辑自定义配置"
      width="min(900px, 95vw)"
      :mask-closable="false"
      @cancel="textEditorError = ''"
    >
      <a-alert
        class="mb-4"
        style="margin-bottom: 8px"
        type="info"
        show-icon
        message="仅应用到页面草稿"
        description="注释使用 //，连续注释归属下一条指令。应用后仍需点击页面右上角“保存修改”才能写入 server.cfg。"
      />
      <a-alert
        v-if="textEditorError"
        class="mb-4"
        style="margin-bottom: 8px"
        type="error"
        show-icon
        message="无法应用文本"
        :description="textEditorError"
      />
      <a-textarea
        v-model:value="textEditorDraft"
        :rows="20"
        class="font-mono text-sm"
        placeholder="// 配置说明\nsm_cvar example_value 1"
      />
      <template #footer>
        <a-button @click="textEditorOpen = false">取消</a-button>
        <a-button type="primary" @click="applyTextEditor">应用到页面</a-button>
      </template>
    </a-modal>

    <a-modal
      v-model:open="entryEditorOpen"
      :title="editingEntryIndex === null ? '添加自定义配置' : '编辑自定义配置'"
      width="min(620px, 95vw)"
      :mask-closable="false"
      @cancel="entryEditorError = ''"
    >
      <a-alert
        v-if="entryEditorError"
        class="mb-4"
        style="margin-bottom: 8px"
        type="error"
        show-icon
        message="无法应用配置"
        :description="entryEditorError"
      />
      <a-form layout="vertical">
        <a-form-item label="注释">
          <a-textarea
            v-model:value="entryCommentDraft"
            :rows="5"
            placeholder="可填写一行或多行注释，无需输入 //"
          />
        </a-form-item>
        <a-form-item label="配置指令" required class="mb-0">
          <a-input
            v-model:value="entryCommandDraft"
            class="font-mono"
            placeholder="例如：sm_cvar example_value 1"
            @press-enter="applyEntryEditor"
          />
        </a-form-item>
      </a-form>
      <template #footer>
        <a-button @click="entryEditorOpen = false">取消</a-button>
        <a-button type="primary" @click="applyEntryEditor">应用到页面</a-button>
      </template>
    </a-modal>

    <a-modal
      v-model:open="cvarLibraryOpen"
      title="L4D2 / SourceMod 参数库"
      width="min(1040px, 96vw)"
      :footer="null"
      destroy-on-close
    >
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
        <a-input
          v-model:value="cvarLibrarySearch"
          allow-clear
          class="min-w-0 flex-1"
          placeholder="搜索参数名、中文说明或分类"
        >
          <template #prefix><SearchOutlined /></template>
        </a-input>
        <a-select v-model:value="cvarLibraryCategory" class="w-full sm:w-44">
          <a-select-option v-for="category in cvarLibraryCategories" :key="category" :value="category">
            {{ category }}
          </a-select-option>
        </a-select>
      </div>

      <div class="mt-4 grid gap-4 lg:grid-cols-[minmax(260px,0.78fr)_minmax(0,1.22fr)]">
        <div class="flex max-h-[62vh] min-h-[300px] flex-col overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
          <div class="flex items-center justify-between border-b border-gray-100 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
            <span>匹配到 {{ filteredCvarCatalog.length }} 项</span>
            <span>点击查看完整说明</span>
          </div>
          <div class="min-h-0 flex-1 overflow-y-auto p-2">
            <button
              v-for="definition in filteredCvarCatalog"
              :key="definition.name"
              type="button"
              :class="[
                'mb-2 w-full rounded-md border px-3 py-2 text-left transition-colors',
                selectedLibraryCvar?.name === definition.name
                  ? 'border-sky-300 bg-sky-50 dark:border-sky-700 dark:bg-sky-950/30'
                  : 'border-transparent hover:border-gray-200 hover:bg-gray-50 dark:hover:border-gray-700 dark:hover:bg-gray-900',
              ]"
              @click="selectLibraryCvar(definition)"
            >
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ definition.displayName }}</span>
                <a-tag class="m-0">{{ definition.category }}</a-tag>
              </div>
              <code class="mt-1 block break-all text-xs text-sky-700 dark:text-sky-300">{{ definition.name }}</code>
              <p class="mt-1 line-clamp-2 text-xs leading-5 text-gray-500 dark:text-gray-400">
                {{ definition.description }}
              </p>
            </button>
            <a-empty v-if="filteredCvarCatalog.length === 0" description="没有匹配的参数" />
          </div>
        </div>

        <div class="flex min-h-[300px] flex-col">
          <CvarDefinitionDetails
            v-if="selectedLibraryCvar"
            :definition="selectedLibraryCvar"
            :selectable="isAdmin"
            :selected-value="librarySelectedValue"
            @select="(value) => (librarySelectedValue = value)"
          />
          <div v-else class="flex flex-1 items-center justify-center rounded-lg border border-dashed border-gray-300 text-sm text-gray-500 dark:border-gray-700">
            从左侧选择一个参数查看说明
          </div>
          <div v-if="selectedLibraryCvar" class="mt-auto flex flex-wrap items-center justify-between gap-2 pt-3">
            <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">
              {{ librarySelectionHint }}
            </p>
            <a-button type="primary" :disabled="!isAdmin" @click="addCatalogEntry(selectedLibraryCvar)">
              <template #icon><PlusOutlined /></template>
              添加到自定义参数
            </a-button>
          </div>
        </div>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
  import { computed, onMounted, reactive, ref } from 'vue';
  import {
    BookOutlined,
    DeleteOutlined,
    EditOutlined,
    FileTextOutlined,
    FormOutlined,
    PlusOutlined,
    SearchOutlined,
  } from '@ant-design/icons-vue';
  import { message } from 'ant-design-vue';
  import { api } from '../services/api';
  import { useAuthStore } from '../stores/auth';
  import CvarDefinitionDetails from '../components/settings/CvarDefinitionDetails.vue';
  import {
    parseCustomConfigLines,
    parseCustomConfigText,
    serializeCustomConfigEntries,
    type CustomConfigEntry,
  } from '../utils/serverConfig';
  import {
    findCvarDefinitionForCommand,
    l4d2CvarCatalog,
    parseCvarCommand,
    type CvarCategory,
    type L4D2CvarDefinition,
  } from '../data/l4d2Cvars';

  const authStore = useAuthStore();
  const isAdmin = computed(() => authStore.isAdmin);

  const loading = ref(true);
  const saving = ref(false);
  const fixedConfig = ref('');
  const fixedTextOpen = ref(false);

  const customEntries = ref<CustomConfigEntry[]>([]);
  const customSearch = ref('');
  const cvarLibraryOpen = ref(false);
  const cvarLibrarySearch = ref('');
  const cvarLibraryCategory = ref<'全部' | CvarCategory>('全部');
  const selectedLibraryCvar = ref<L4D2CvarDefinition | null>(null);
  /** 参数库弹窗中用户点选的取值，添加时写入命令行。 */
  const librarySelectedValue = ref('');
  const librarySelectionHint = computed(() => {
    if (!isAdmin.value) return '当前为只读模式，无法添加参数。';
    if (librarySelectedValue.value) {
      return `将添加：${selectedLibraryCvar.value?.name ?? ''} = ${librarySelectedValue.value}`;
    }
    return '点选上方取值后添加，会直接使用选中的值；未选择时使用默认示例值。';
  });
  const customTextSource = ref('');
  const trailingComments = ref<string[]>([]);
  const trailingStartLine = ref(0);
  const hasTrailingComments = computed(() => trailingComments.value.length > 0);
  const cvarLibraryCategories = computed<Array<'全部' | CvarCategory>>(() => [
    '全部',
    ...Array.from(new Set(l4d2CvarCatalog.map((definition) => definition.category))),
  ]);
  const filteredCvarCatalog = computed(() => {
    const query = cvarLibrarySearch.value.trim().toLocaleLowerCase();
    return l4d2CvarCatalog.filter((definition) => {
      if (cvarLibraryCategory.value !== '全部' && definition.category !== cvarLibraryCategory.value) {
        return false;
      }
      if (!query) return true;
      return [definition.name, definition.displayName, definition.description, definition.category]
        .join(' ')
        .toLocaleLowerCase()
        .includes(query);
    });
  });
  const openCvarLibrary = () => {
    if (!selectedLibraryCvar.value) {
      selectLibraryCvar(filteredCvarCatalog.value[0] ?? null);
    }
    cvarLibraryOpen.value = true;
  };
  /** 用参数名 + 新值重建命令行，保留原有的 sm_cvar 前缀写法。 */
  const buildCvarCommand = (baseCommand: string, value: string) => {
    const parsed = parseCvarCommand(baseCommand);
    const trimmed = value.trim();
    if (!parsed || !trimmed) return baseCommand;
    const prefix = baseCommand.trim().toLowerCase().startsWith('sm_cvar') ? 'sm_cvar ' : '';
    return `${prefix}${parsed.name} ${trimmed}`;
  };
  const selectLibraryCvar = (definition: L4D2CvarDefinition | null) => {
    selectedLibraryCvar.value = definition;
    librarySelectedValue.value = definition ? (parseCvarCommand(definition.exampleCommand)?.value ?? '') : '';
  };
  /** 行内详情点选取值后，直接改写这条自定义参数的命令行。 */
  const applyCvarValueToEntry = (index: number, value: string) => {
    if (!isAdmin.value || hasTrailingComments.value) return;
    const entry = customEntries.value[index];
    if (!entry) return;
    const next = buildCvarCommand(entry.command, value);
    if (next === entry.command) return;
    entry.command = next;
    syncTextSourceFromEntries();
    message.success(`已改为 ${next}，别忘了点右上角“保存修改”`);
  };
  const filteredCustomEntries = computed(() => {
    const query = customSearch.value.trim().toLocaleLowerCase();
    return customEntries.value
      .map((entry, index) => ({ entry, index }))
      .filter(({ entry }) => {
        if (!query) return true;
        if (entry.command.toLocaleLowerCase().includes(query)) return true;
        return entry.comments.some((comment) => comment.toLocaleLowerCase().includes(query));
      });
  });
  const getCustomEntryDefinition = (command: string) => findCvarDefinitionForCommand(command);
  const addCatalogEntry = (definition: L4D2CvarDefinition) => {
    if (!isAdmin.value) return;
    const parsed = parseCvarCommand(definition.exampleCommand);
    const alreadyAdded = parsed
      ? customEntries.value.some((entry) => parseCvarCommand(entry.command)?.name === parsed.name)
      : false;
    if (alreadyAdded) {
      message.info(`${definition.name} 已经在自定义参数中`);
      return;
    }
    const command = buildCvarCommand(definition.exampleCommand, librarySelectedValue.value);
    customEntries.value.push({
      id: `catalog-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      comments: [`参数：${definition.displayName}`, `说明：${definition.description}`, `来源：${definition.source}`],
      command,
    });
    cvarLibraryOpen.value = false;
    syncTextSourceFromEntries();
    message.success(`已添加 ${command}，点“保存修改”后写入 cfg`);
  };
  const trailingCommentMessage = computed(
    () =>
      `自定义配置从第 ${trailingStartLine.value} 行开始有 ${trailingComments.value.length} 行注释没有对应指令。请使用“文本编辑”补充指令或删除这些注释。`
  );

  const textEditorOpen = ref(false);
  const textEditorDraft = ref('');
  const textEditorError = ref('');

  const entryEditorOpen = ref(false);
  const editingEntryIndex = ref<number | null>(null);
  const entryCommentDraft = ref('');
  const entryCommandDraft = ref('');
  const entryEditorError = ref('');

  const form = reactive({
    hidden: false,
    lobby_connect_only: false,
    steam_group: '',
  });

  const setCustomConfigDraft = (lines: string[]) => {
    const parsed = parseCustomConfigLines(lines);
    customEntries.value = parsed.entries;
    trailingComments.value = parsed.trailingComments;
    trailingStartLine.value = parsed.trailingStartLine;
    customTextSource.value = lines.join('\n');
  };

  const syncTextSourceFromEntries = () => {
    customTextSource.value = serializeCustomConfigEntries(customEntries.value).join('\n');
  };

  const getDeletePopupContainer = (triggerNode: HTMLElement) =>
    triggerNode.closest<HTMLElement>('.server-config-entry') ?? document.body;

  const fetchData = async () => {
    try {
      loading.value = true;
      const data = await api.getServerConfig();
      form.hidden = data.hidden;
      form.lobby_connect_only = data.lobby_connect_only;
      form.steam_group = data.steam_group || '';
      fixedConfig.value = data.fixed_config || '';
      setCustomConfigDraft(data.custom_config || []);
    } catch (error: any) {
      message.error('获取服务器配置失败: ' + error.message);
    } finally {
      loading.value = false;
    }
  };

  const openTextEditor = () => {
    if (!isAdmin.value) return;
    textEditorDraft.value = customTextSource.value;
    textEditorError.value = '';
    textEditorOpen.value = true;
  };

  const applyTextEditor = () => {
    const parsed = parseCustomConfigText(textEditorDraft.value);
    if (parsed.trailingComments.length > 0) {
      textEditorError.value = `第 ${parsed.trailingStartLine} 行开始的 ${parsed.trailingComments.length} 行注释没有对应指令，请补充指令或删除末尾注释。`;
      return;
    }
    customEntries.value = parsed.entries;
    trailingComments.value = [];
    trailingStartLine.value = 0;
    syncTextSourceFromEntries();
    textEditorError.value = '';
    textEditorOpen.value = false;
    message.info('已应用到页面，请点击“保存修改”写入服务器配置');
  };

  const openEntryEditor = (index?: number) => {
    if (!isAdmin.value || hasTrailingComments.value) return;
    editingEntryIndex.value = index ?? null;
    entryEditorError.value = '';
    if (index === undefined) {
      entryCommentDraft.value = '';
      entryCommandDraft.value = '';
    } else {
      const entry = customEntries.value[index];
      if (!entry) return;
      entryCommentDraft.value = entry.comments.join('\n');
      entryCommandDraft.value = entry.command;
    }
    entryEditorOpen.value = true;
  };

  const applyEntryEditor = () => {
    const command = entryCommandDraft.value.trim();
    if (!command) {
      entryEditorError.value = '配置指令不能为空。';
      return;
    }
    if (/\r|\n/.test(command)) {
      entryEditorError.value = '单条配置指令不能包含换行，请使用“文本编辑”批量修改。';
      return;
    }

    const commentLines = entryCommentDraft.value
      .replace(/\r\n?/g, '\n')
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line !== '')
      .map((line) => (line.startsWith('//') ? line : `// ${line}`));
    const parsed = parseCustomConfigLines([...commentLines, command]);
    if (parsed.trailingComments.length > 0 || parsed.entries.length !== 1) {
      entryEditorError.value = '请输入一条有效配置指令；只有注释而没有指令无法保存。';
      return;
    }

    const nextEntry = parsed.entries[0];
    if (!nextEntry) {
      entryEditorError.value = '无法解析当前配置，请检查指令内容。';
      return;
    }
    if (editingEntryIndex.value === null) {
      customEntries.value.push(nextEntry);
    } else {
      const currentEntry = customEntries.value[editingEntryIndex.value];
      if (!currentEntry) {
        entryEditorError.value = '原配置已不存在，请关闭弹框后重试。';
        return;
      }
      nextEntry.id = currentEntry.id;
      customEntries.value.splice(editingEntryIndex.value, 1, nextEntry);
    }
    syncTextSourceFromEntries();
    entryEditorError.value = '';
    entryEditorOpen.value = false;
  };

  const removeCustomConfig = (index: number) => {
    if (hasTrailingComments.value) return;
    customEntries.value.splice(index, 1);
    syncTextSourceFromEntries();
  };

  const save = async () => {
    if (hasTrailingComments.value) {
      message.error(trailingCommentMessage.value);
      return;
    }
    try {
      saving.value = true;
      await api.updateServerConfig({
        ...form,
        custom_config: serializeCustomConfigEntries(customEntries.value),
      });
      message.success('保存成功，请重启服务器或换图以应用更改');
      await fetchData();
    } catch (error: any) {
      message.error('保存失败: ' + error.message);
    } finally {
      saving.value = false;
    }
  };

  onMounted(fetchData);
</script>

<style scoped>
  .server-config-action-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  .server-config-action-button :deep(.anticon) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    line-height: 1;
    vertical-align: 0;
  }

  .server-config-action-button :deep(.anticon svg) {
    display: block;
  }

  .entry-icon-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  :deep(.ant-popconfirm-buttons) {
    display: flex;
    justify-content: flex-end;
    flex-wrap: nowrap;
    gap: 8px;
    white-space: nowrap;
  }

  :deep(.ant-popconfirm-buttons button) {
    margin-left: 0 !important;
  }

  :deep(.ant-popconfirm-message) {
    white-space: nowrap;
  }
</style>
