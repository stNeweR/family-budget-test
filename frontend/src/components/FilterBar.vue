<script setup lang="ts">
import { computed } from 'vue';
import { CATEGORIES, type ExpenseFilter, type User } from '../types';

const props = defineProps<{
  users: User[];
  modelValue: ExpenseFilter;
}>();

const emit = defineEmits<{
  (e: 'update:modelValue', value: ExpenseFilter): void;
}>();

function patch(changes: Partial<ExpenseFilter>) {
  const next = { ...props.modelValue, ...changes };
  (Object.keys(next) as (keyof ExpenseFilter)[]).forEach((key) => {
    if (next[key] === undefined || next[key] === '') delete next[key];
  });
  emit('update:modelValue', next);
}

const userId = computed({
  get: () => props.modelValue.user_id ?? '',
  set: (value: string | number) => patch({ user_id: value === '' ? undefined : Number(value) }),
});

const category = computed({
  get: () => props.modelValue.category ?? '',
  set: (value: string) => patch({ category: (value || undefined) as ExpenseFilter['category'] }),
});

const from = computed({
  get: () => props.modelValue.from ?? '',
  set: (value: string) => patch({ from: value || undefined }),
});

const to = computed({
  get: () => props.modelValue.to ?? '',
  set: (value: string) => patch({ to: value || undefined }),
});

function reset() {
  emit('update:modelValue', {});
}
</script>

<template>
  <div class="grid grid-cols-1 items-end gap-3 sm:grid-cols-2 lg:grid-cols-5">
    <label class="flex flex-col gap-1.5 text-xs font-medium text-slate-500">
      <span>Член семьи</span>
      <select
        v-model="userId"
        class="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-800 shadow-sm transition-colors focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
      >
        <option value="">Все</option>
        <option v-for="user in users" :key="user.user_id" :value="user.user_id">
          {{ user.full_name || user.username || user.user_id }}
        </option>
      </select>
    </label>

    <label class="flex flex-col gap-1.5 text-xs font-medium text-slate-500">
      <span>Категория</span>
      <select
        v-model="category"
        class="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-800 shadow-sm transition-colors focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
      >
        <option value="">Все</option>
        <option v-for="item in CATEGORIES" :key="item" :value="item">{{ item }}</option>
      </select>
    </label>

    <label class="flex flex-col gap-1.5 text-xs font-medium text-slate-500">
      <span>С</span>
      <input
        v-model="from"
        class="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-800 shadow-sm transition-colors focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
        type="date"
      />
    </label>

    <label class="flex flex-col gap-1.5 text-xs font-medium text-slate-500">
      <span>По</span>
      <input
        v-model="to"
        class="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-800 shadow-sm transition-colors focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
        type="date"
      />
    </label>

    <button
      class="h-[38px] rounded-lg border border-slate-300 bg-white px-4 text-sm font-medium text-slate-600 shadow-sm transition-colors hover:bg-slate-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
      type="button"
      @click="reset"
    >
      Сбросить
    </button>
  </div>
</template>
