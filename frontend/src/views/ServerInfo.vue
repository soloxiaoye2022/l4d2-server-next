<template>
  <div>
    <div class="flex flex-col">
      <div class="flex justify-between items-center mb-6">
        <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100">服务器信息</h1>
        <a-button type="primary" size="large" @click="save" :loading="saving" v-if="isAdmin"
          >保存修改</a-button
        >
      </div>

      <div v-if="loading" class="flex justify-center items-center h-64">
        <a-spin size="large" />
      </div>

      <div v-else class="flex flex-col gap-6">
        <!-- Hostname Column -->
        <div class="flex flex-col bg-white dark:bg-gray-800 p-4 rounded-lg shadow shrink-0">
          <h2 class="text-lg font-semibold mb-4 text-gray-800 dark:text-gray-200 shrink-0">
            服务器名
            <span class="text-xs font-normal text-gray-500 ml-2 block sm:inline">
              {{ hostnameFileLabel }}
            </span>
          </h2>

          <a-alert
            v-if="hostnameError"
            type="warning"
            show-icon
            class="mb-4"
            message="未能确定服务器名来源"
            :description="hostnameError"
          />

          <div v-if="detection" class="mb-4 space-y-3">
            <!-- 运行时生效值 -->
            <div class="rounded border border-gray-200 dark:border-gray-700 p-3">
              <div class="mb-2 flex flex-wrap items-center gap-2">
                <span class="text-sm font-semibold text-gray-800 dark:text-gray-200">运行中实际生效</span>
                <a-tag :color="detection.effective_ok ? 'success' : 'default'" class="!m-0">
                  {{ detection.effective_ok ? 'RCON 已读取' : '未读取到' }}
                </a-tag>
                <a-tag v-if="hostnameSource === 'rcon'" color="blue" class="!m-0">面板显示此值</a-tag>
              </div>
              <code
                v-if="detection.effective"
                class="block break-all bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100 p-2 rounded text-xs"
              >
                {{ detection.effective }}
              </code>
              <p v-else class="text-xs text-gray-500 dark:text-gray-400">
                {{ detection.effective_error || '服务器未运行或 RCON 不可达。' }}
              </p>
            </div>

            <!-- 已加载的服务器名插件 -->
            <div v-if="detection.plugins.length > 0" class="rounded border border-gray-200 dark:border-gray-700 p-3">
              <div class="mb-2 text-sm font-semibold text-gray-800 dark:text-gray-200">
                已加载的服务器名插件
                <span class="ml-1 text-xs font-normal text-gray-500">
                  按运行时插件名识别，共 {{ detection.plugins.length }} 个
                </span>
              </div>
              <div class="flex flex-wrap gap-2">
                <a-tag v-for="plugin in detection.plugins" :key="plugin" color="processing" class="!m-0">
                  {{ plugin }}
                </a-tag>
              </div>
            </div>
            <p v-else class="text-xs text-gray-500 dark:text-gray-400">
              未在 sm plugins list 中发现服务器名相关插件。
            </p>

            <!-- 配置文件证据 -->
            <div v-if="detection.sources.length > 0" class="rounded border border-gray-200 dark:border-gray-700 p-3">
              <div class="mb-2 text-sm font-semibold text-gray-800 dark:text-gray-200">
                配置文件
                <span class="ml-1 text-xs font-normal text-gray-500">
                  保存时按此顺序写入第一个文件
                </span>
              </div>
              <div class="space-y-2">
                <div
                  v-for="source in detection.sources"
                  :key="source.file"
                  class="rounded bg-gray-50 dark:bg-gray-900 p-2"
                >
                  <div class="mb-1 flex flex-wrap items-center gap-2">
                    <span class="text-xs font-semibold text-gray-800 dark:text-gray-200">{{ source.plugin }}</span>
                    <a-tag v-if="source.is_template" color="warning" class="!m-0">模板</a-tag>
                    <a-tag v-if="sourceRawIsShown(source)" color="blue" class="!m-0">面板显示此值</a-tag>
                    <code class="text-[11px] text-gray-500 dark:text-gray-400">{{ source.file }}</code>
                  </div>
                  <code class="block break-all text-xs text-gray-900 dark:text-gray-100">{{ source.raw }}</code>
                  <p v-if="source.note" class="mt-1 text-[11px] text-gray-500 dark:text-gray-400">
                    {{ source.note }}
                  </p>
                </div>
              </div>
            </div>
          </div>

          <textarea
            v-model="form.hostname"
            rows="2"
            class="w-full p-3 bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100 border border-gray-300 dark:border-gray-700 rounded focus:ring-2 focus:ring-blue-500 focus:border-blue-500 outline-none resize-none font-mono text-sm disabled:cursor-not-allowed disabled:opacity-70"
            :disabled="!isAdmin"
          ></textarea>
          <p v-if="hostnameNotice" class="mt-2 text-xs text-orange-600 dark:text-orange-400">
            {{ hostnameNotice }}
          </p>
        </div>

        <!-- Title Column -->
        <div class="flex flex-col bg-white dark:bg-gray-800 p-4 rounded-lg shadow shrink-0">
          <h2 class="text-lg font-semibold mb-4 text-gray-800 dark:text-gray-200 shrink-0">
            服务器信息
            <span class="text-xs font-normal text-gray-500 ml-2 block sm:inline">host.txt</span>
          </h2>
          <textarea
            v-model="form.host"
            rows="3"
            class="w-full p-3 bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100 border border-gray-300 dark:border-gray-700 rounded focus:ring-2 focus:ring-blue-500 focus:border-blue-500 outline-none resize-none font-mono text-sm disabled:cursor-not-allowed disabled:opacity-70"
            :disabled="!isAdmin"
          ></textarea>
        </div>

        <!-- Motd Column -->
        <div class="flex flex-col bg-white dark:bg-gray-800 p-4 rounded-lg shadow shrink-0">
          <h2 class="text-lg font-semibold mb-4 text-gray-800 dark:text-gray-200 shrink-0">
            服务器公告
            <span class="text-xs font-normal text-gray-500 ml-2 block sm:inline">motd.txt</span>
          </h2>
          <textarea
            v-model="form.motd"
            rows="15"
            class="w-full p-3 bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100 border border-gray-300 dark:border-gray-700 rounded focus:ring-2 focus:ring-blue-500 focus:border-blue-500 outline-none resize-none font-mono text-sm disabled:cursor-not-allowed disabled:opacity-70"
            :disabled="!isAdmin"
          ></textarea>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { ref, onMounted, reactive, computed } from 'vue';
  import { api, type HostnameDetection, type HostnameSource } from '../services/api';
  import { message } from 'ant-design-vue';
  import { useAuthStore } from '../stores/auth';

  const authStore = useAuthStore();
  const isAdmin = computed(() => authStore.isAdmin);

  const loading = ref(true);
  const saving = ref(false);
  const hostnameError = ref('');
  const hostnameNotice = ref('');
  const hostnameSource = ref('');
  const detection = ref<HostnameDetection | null>(null);

  const form = reactive({
    hostname: '',
    host: '',
    motd: '',
  });

  // 标题旁的文件提示：优先显示实际会被写入的文件，其次显示“多来源”。
  const hostnameFileLabel = computed(() => {
    const sources = detection.value?.sources;
    if (!sources || sources.length === 0) return '未检测到配置文件';
    if (sources.length === 1) return sources[0]?.file || '';
    return `检测到 ${sources.length} 个来源`;
  });

  // 面板正在显示哪个配置文件的值，用于在列表里加标记。
  const sourceRawIsShown = (source: HostnameSource) => {
    if (hostnameSource.value !== 'file') return false;
    return source.raw === form.hostname;
  };

  const fetchData = async () => {
    try {
      loading.value = true;
      const data = await api.getServerInfo();
      form.hostname = data.hostname;
      form.host = data.host;
      form.motd = data.motd;
      hostnameError.value = data.hostname_error || '';
      hostnameSource.value = data.hostname_source || '';
      detection.value = data.detection;
      hostnameNotice.value = '';
    } catch (e: any) {
      message.error('获取服务器信息失败: ' + e.message);
    } finally {
      loading.value = false;
    }
  };

  const save = async () => {
    try {
      saving.value = true;
      hostnameNotice.value = '';
      const result = await api.updateServerInfo(form);
      message.success('保存成功');
      hostnameNotice.value = result.hostname_notice || '';
      // 保存后重新读取，刷新 RCON 生效值与来源标记
      await fetchData();
      if (result.hostname_notice) {
        hostnameNotice.value = result.hostname_notice;
      }
    } catch (e: any) {
      message.error('保存失败: ' + e.message);
    } finally {
      saving.value = false;
    }
  };

  onMounted(() => {
    fetchData();
  });
</script>
