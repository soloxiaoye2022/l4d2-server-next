<template>
  <div
    v-if="definition"
    class="rounded-lg border border-gray-200 bg-white/80 p-3 dark:border-gray-700 dark:bg-gray-900/50"
  >
    <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ definition.displayName }}</span>
          <code class="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-sky-700 dark:bg-gray-800 dark:text-sky-300">
            {{ definition.name }}
          </code>
          <a-tag class="m-0">{{ definition.type }}</a-tag>
        </div>
        <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ definition.description }}</p>
      </div>
      <div class="shrink-0 text-left sm:text-right">
        <div class="text-xs text-gray-400">默认值</div>
        <div class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ definition.defaultValue || '空' }}</div>
      </div>
    </div>

    <div v-if="definition.values?.length" class="mt-3">
      <div v-if="selectable" class="mb-1.5 text-xs text-gray-500 dark:text-gray-400">
        点击下方取值即可直接写入当前参数
      </div>
      <div class="grid gap-2 md:grid-cols-2">
        <button
          v-for="item in definition.values"
          :key="`${definition.name}-${item.value}`"
          type="button"
          :disabled="!selectable"
          :aria-pressed="isSelected(item.value)"
          :class="[
            'rounded-md border px-2.5 py-2 text-left text-xs leading-5 transition',
            valueToneClass(item.tone),
            selectable ? 'cursor-pointer hover:shadow-sm' : 'cursor-default',
            isSelected(item.value) ? 'ring-2 ring-sky-500 ring-offset-1 dark:ring-offset-gray-900' : '',
          ]"
          @click="pickValue(item.value)"
        >
          <div class="flex flex-wrap items-center gap-1.5">
            <code class="font-semibold">{{ item.value }}</code>
            <span class="font-medium">{{ item.label }}</span>
            <span
              v-if="isSelected(item.value)"
              class="ml-auto rounded bg-sky-600 px-1.5 py-0.5 text-[10px] font-semibold text-white"
            >
              已选
            </span>
          </div>
          <div class="mt-0.5 opacity-90">{{ item.effect }}</div>
        </button>
      </div>
    </div>

    <div
      v-if="definition.range"
      class="mt-3 rounded-md border border-sky-200 bg-sky-50 px-3 py-2 text-xs leading-5 text-sky-900 dark:border-sky-900 dark:bg-sky-950/30 dark:text-sky-200"
    >
      <div class="font-medium">
        可填写范围：{{ definition.range.min }} - {{ definition.range.max }} {{ definition.range.unit || '' }}
        <span v-if="definition.range.recommended">；建议：{{ definition.range.recommended }}</span>
        <span v-if="definition.range.hardLimit" class="ml-1 font-normal opacity-80">（引擎硬边界，超出会被截断或拒绝）</span>
      </div>
      <div class="mt-1">调低：{{ definition.range.lowerEffect }}</div>
      <div>调高：{{ definition.range.higherEffect }}</div>

      <div v-if="selectable" class="mt-2 border-t border-sky-200/70 pt-2 dark:border-sky-900/60">
        <div class="mb-1.5 text-[11px] font-medium text-sky-800 dark:text-sky-200">常用取值（点选即写入）</div>
        <div class="flex flex-wrap items-center gap-1.5">
          <button
            v-for="preset in numberPresets"
            :key="`${definition.name}-preset-${preset.value}`"
            type="button"
            :aria-pressed="customMode ? false : isSelected(preset.value)"
            :class="[
              'rounded border px-2 py-1 text-[11px] transition',
              customMode || !isSelected(preset.value)
                ? 'border-sky-300 bg-white text-sky-800 hover:bg-sky-100 dark:border-sky-800 dark:bg-gray-900 dark:text-sky-200 dark:hover:bg-sky-950/60'
                : 'border-sky-600 bg-sky-600 text-white',
            ]"
            @click="pickPreset(preset.value)"
          >
            <code class="font-semibold">{{ preset.value }}</code>
            <span class="ml-1">{{ preset.label }}</span>
          </button>
        </div>
        <div class="mt-2 flex flex-col gap-1.5 sm:flex-row sm:items-center">
          <span class="shrink-0 text-[11px] text-sky-800 dark:text-sky-200">自定义值</span>
          <a-input-number
            v-model:value="customNumber"
            class="w-full sm:w-40"
            size="small"
            :min="definition.range.min"
            :max="definition.range.max"
            :placeholder="`${definition.range.min} - ${definition.range.max}`"
            @focus="enterCustomMode"
            @change="onCustomChange"
          />
          <a-button size="small" type="primary" :disabled="!customReady" @click="applyCustom">写入</a-button>
        </div>
        <div v-if="customError" class="mt-1 text-[11px] text-rose-600 dark:text-rose-400">{{ customError }}</div>
        <div v-else-if="customMode" class="mt-1 text-[11px] text-sky-700 dark:text-sky-300">
          已切换到自定义输入，上方预设选择已取消
        </div>
      </div>
    </div>

    <div
      v-if="definition.warning"
      class="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300"
    >
      注意：{{ definition.warning }}
    </div>

    <div class="mt-3 flex flex-col gap-1 border-t border-dashed border-gray-200 pt-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400 sm:flex-row sm:items-center sm:justify-between">
      <span>示例：<code class="text-gray-700 dark:text-gray-300">{{ definition.exampleCommand }}</code></span>
      <span>{{ definition.source }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, ref, watch } from 'vue';
  import {
    resolveCvarRangePresets,
    type CvarValueTone,
    type L4D2CvarDefinition,
  } from '../../data/l4d2Cvars';

  const props = withDefaults(
    defineProps<{
      definition: L4D2CvarDefinition | null;
      /** 允许点击取值写入命令行；游客只读时为 false。 */
      selectable?: boolean;
      /** 当前命令行中已生效的值，用于高亮。 */
      selectedValue?: string;
    }>(),
    {
      selectable: false,
      selectedValue: '',
    },
  );

  const emit = defineEmits<{
    (event: 'select', value: string): void;
  }>();

  const customNumber = ref<number | undefined>(undefined);
  const customMode = ref(false);
  const customError = ref('');

  const normalize = (raw: string) => raw.trim().replace(/^"|"$/g, '');

  const isSelected = (value: string) => {
    if (!props.selectable) return false;
    return normalize(props.selectedValue) === normalize(value);
  };

  const numberPresets = computed(() =>
    props.definition ? resolveCvarRangePresets(props.definition) : [],
  );

  const customReady = computed(() => {
    if (!customMode.value) return false;
    if (customNumber.value === undefined) return false;
    return customError.value === '';
  });

  watch(
    () => [props.definition?.name, props.selectedValue] as const,
    () => {
      customMode.value = false;
      customNumber.value = undefined;
      customError.value = '';
    },
  );

  const pickValue = (value: string) => {
    if (!props.selectable) return;
    customMode.value = false;
    customError.value = '';
    emit('select', value);
  };

  const pickPreset = (value: string) => {
    if (!props.selectable) return;
    customMode.value = false;
    customNumber.value = undefined;
    customError.value = '';
    emit('select', value);
  };

  const enterCustomMode = () => {
    if (!props.selectable) return;
    customMode.value = true;
  };

  const onCustomChange = (value: number | string | null) => {
    if (!props.selectable) return;
    customMode.value = true;
    customError.value = '';
    const range = props.definition?.range;
    if (value === null || value === '' || value === undefined) {
      customNumber.value = undefined;
      return;
    }
    const numeric = Number(value);
    if (Number.isNaN(numeric)) {
      customNumber.value = undefined;
      customError.value = '请输入数字';
      return;
    }
    if (range && (numeric < range.min || numeric > range.max)) {
      customNumber.value = numeric;
      customError.value = `超出可填写范围 ${range.min} - ${range.max}`;
      return;
    }
    customNumber.value = numeric;
  };

  const applyCustom = () => {
    if (!props.selectable || !customReady.value || customNumber.value === undefined) return;
    emit('select', String(customNumber.value));
  };

  const valueToneClass = (tone: CvarValueTone = 'neutral') => {
    if (tone === 'good') {
      return 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-300';
    }
    if (tone === 'warning') {
      return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300';
    }
    if (tone === 'danger') {
      return 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-300';
    }
    return 'border-gray-200 bg-gray-50 text-gray-700 dark:border-gray-700 dark:bg-gray-900/60 dark:text-gray-300';
  };
</script>
