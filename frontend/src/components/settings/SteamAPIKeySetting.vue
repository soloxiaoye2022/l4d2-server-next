<template>
  <section
    id="steam-api-setting"
    class="steam-api-setting mt-6 scroll-mt-24 rounded-lg border border-gray-200 bg-white p-5 dark:border-gray-700 dark:bg-gray-800"
  >
    <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div class="flex min-w-0 items-start gap-3">
        <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-sky-50 text-sky-600 dark:bg-sky-950/50 dark:text-sky-400">
          <KeyOutlined class="text-lg" />
        </span>
        <div class="min-w-0">
          <h3 class="text-base font-semibold text-gray-900 dark:text-gray-100">Steam API 密钥</h3>
          <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-gray-400">
            用于查询玩家游戏时长。保存后立即生效，不需要重启面板或游戏服务器。
          </p>
        </div>
      </div>
      <a-tag :color="status.configured ? 'success' : 'warning'" class="shrink-0">
        {{ status.configured ? '已配置' : '未配置' }}
      </a-tag>
    </div>

    <a-alert
      v-if="status.configured"
      class="mt-4"
      type="success"
      show-icon
      :message="`当前来源：${sourceLabel}，密钥：${status.masked_key}`"
    />
    <a-alert
      v-else
      class="mt-4"
      type="warning"
      show-icon
      message="未配置时，在线玩家的游戏时长查询会失败，但不影响服务器连接和其他功能。"
    />

    <div class="mt-5 grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(320px,0.9fr)]">
      <div class="rounded-lg border border-dashed border-sky-200 bg-sky-50/50 p-4 dark:border-sky-900 dark:bg-sky-950/20">
        <h4 class="flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-gray-100">
          <InfoCircleOutlined class="text-sky-600 dark:text-sky-400" />
          获取与填写步骤
        </h4>
        <ol class="mt-3 space-y-3 text-sm leading-6 text-gray-600 dark:text-gray-300">
          <li class="flex gap-2">
            <span class="step-number">1</span>
            <span>
              打开
              <a
                href="https://steamcommunity.com/dev/apikey"
                target="_blank"
                rel="noopener noreferrer"
                class="font-medium text-sky-600 hover:text-sky-700 dark:text-sky-400"
              >
                Steam 开发者 API 密钥页面
                <LinkOutlined class="ml-1 inline-block align-[-0.1em]" />
              </a>
              ，并登录需要申请密钥的 Steam 账号。
            </span>
          </li>
          <li class="flex gap-2">
            <span class="step-number">2</span>
            <span>
              域名一栏填写服务器公网 IP 或绑定域名即可。这里只是申请用途，不会修改服务器，也不会影响玩家连接。
            </span>
          </li>
          <li class="flex gap-2">
            <span class="step-number">3</span>
            <span>
              提交后复制生成的密钥。格式为
              <code class="rounded bg-white px-1 py-0.5 text-xs text-rose-600 dark:bg-gray-900 dark:text-rose-400">32 位十六进制字符</code>
              ，只包含 0-9、A-F。
            </span>
          </li>
          <li class="flex gap-2">
            <span class="step-number">4</span>
            <span>粘贴到右侧输入框并保存。密钥会脱敏显示，不会把完整内容返回到浏览器。</span>
          </li>
        </ol>
      </div>

      <div class="flex flex-col rounded-lg border border-gray-200 p-4 dark:border-gray-700">
        <label for="steam-api-key-input" class="text-sm font-medium text-gray-800 dark:text-gray-200">
          填写或更新密钥
        </label>
        <a-input-password
          id="steam-api-key-input"
          v-model:value="keyInput"
          class="mt-2"
          :maxlength="32"
          placeholder="粘贴 32 位 Steam API Key"
          autocomplete="off"
          @press-enter="saveKey"
        />
        <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">
          如果提示 401/403，请确认密钥有效且没有多余空格；如果密钥被停用，需要重新申请。
        </p>
        <div class="mt-auto flex flex-wrap gap-2 pt-4">
          <a-button type="primary" :loading="saving" :disabled="!keyInput.trim()" @click="saveKey">
            <template #icon><SaveOutlined /></template>
            保存并立即生效
          </a-button>
          <a-button :loading="loading" @click="loadStatus">
            <template #icon><ReloadOutlined /></template>
            刷新状态
          </a-button>
          <a-button v-if="status.source === 'panel'" danger :loading="saving" @click="clearKey">
            <template #icon><DeleteOutlined /></template>
            清除面板密钥
          </a-button>
        </div>
      </div>
    </div>

    <div class="mt-4 flex items-start gap-2 rounded-md bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:bg-amber-950/30 dark:text-amber-300">
      <SafetyCertificateOutlined class="mt-0.5 shrink-0" />
      <span>
        环境变量 <code>STEAM_API_KEY</code> 的优先级高于面板保存值。如果你之后在部署环境中设置了该变量，请清空环境变量后再使用面板密钥。
      </span>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onMounted, ref } from 'vue';
  import { message } from 'ant-design-vue';
  import {
    DeleteOutlined,
    InfoCircleOutlined,
    KeyOutlined,
    LinkOutlined,
    ReloadOutlined,
    SafetyCertificateOutlined,
    SaveOutlined,
  } from '@ant-design/icons-vue';
  import { api, type SteamAPIKeyStatus } from '../../services/api';

  const loading = ref(false);
  const saving = ref(false);
  const keyInput = ref('');
  const status = ref<SteamAPIKeyStatus>({
    configured: false,
    source: 'none',
    masked_key: '',
  });

  const sourceLabel = computed(() => {
    if (status.value.source === 'environment') return '服务器环境变量';
    if (status.value.source === 'panel') return '面板设置';
    return '未配置';
  });

  const loadStatus = async () => {
    loading.value = true;
    try {
      status.value = await api.getSteamAPIKeyStatus();
    } catch (error: any) {
      message.error(error?.message || '读取 Steam API 配置失败');
    } finally {
      loading.value = false;
    }
  };

  const saveKey = async () => {
    const value = keyInput.value.trim();
    if (!/^[0-9a-fA-F]{32}$/.test(value)) {
      message.warning('Steam API Key 必须是 32 位十六进制字符');
      return;
    }
    saving.value = true;
    try {
      status.value = await api.updateSteamAPIKey(value);
      keyInput.value = '';
      message.success('Steam API Key 已保存，现在可以查询游戏时长');
    } catch (error: any) {
      message.error(error?.message || '保存 Steam API Key 失败');
    } finally {
      saving.value = false;
    }
  };

  const clearKey = async () => {
    saving.value = true;
    try {
      status.value = await api.updateSteamAPIKey('');
      message.success('面板保存的 Steam API Key 已清除');
    } catch (error: any) {
      message.error(error?.message || '清除 Steam API Key 失败');
    } finally {
      saving.value = false;
    }
  };

  onMounted(loadStatus);
</script>

<style scoped>
  .step-number {
    display: inline-flex;
    width: 1.35rem;
    height: 1.35rem;
    flex: 0 0 auto;
    align-items: center;
    justify-content: center;
    margin-top: 0.15rem;
    border-radius: 9999px;
    background: rgb(224 242 254);
    color: rgb(2 132 199);
    font-size: 0.75rem;
    font-weight: 600;
  }

  :global(.dark) .step-number {
    background: rgb(8 47 73);
    color: rgb(56 189 248);
  }
</style>
