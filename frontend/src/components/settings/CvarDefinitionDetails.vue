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

    <div v-if="definition.values?.length" class="mt-3 grid gap-2 md:grid-cols-2">
      <div
        v-for="item in definition.values"
        :key="`${definition.name}-${item.value}`"
        :class="['rounded-md border px-2.5 py-2 text-xs leading-5', valueToneClass(item.tone)]"
      >
        <div class="flex flex-wrap items-center gap-1.5">
          <code class="font-semibold">{{ item.value }}</code>
          <span class="font-medium">{{ item.label }}</span>
        </div>
        <div class="mt-0.5 opacity-90">{{ item.effect }}</div>
      </div>
    </div>

    <div
      v-if="definition.range"
      class="mt-3 rounded-md border border-sky-200 bg-sky-50 px-3 py-2 text-xs leading-5 text-sky-900 dark:border-sky-900 dark:bg-sky-950/30 dark:text-sky-200"
    >
      <div class="font-medium">
        可填写范围：{{ definition.range.min }} - {{ definition.range.max }} {{ definition.range.unit || '' }}
        <span v-if="definition.range.recommended">；建议：{{ definition.range.recommended }}</span>
      </div>
      <div class="mt-1">调低：{{ definition.range.lowerEffect }}</div>
      <div>调高：{{ definition.range.higherEffect }}</div>
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
  import type { L4D2CvarDefinition, CvarValueTone } from '../../data/l4d2Cvars';

  defineProps<{
    definition: L4D2CvarDefinition | null;
  }>();

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
