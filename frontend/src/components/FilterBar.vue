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
  <div class="filter-bar">
    <label>
      <span>Член семьи</span>
      <select v-model="userId">
        <option value="">Все</option>
        <option v-for="user in users" :key="user.user_id" :value="user.user_id">
          {{ user.full_name || user.username || user.user_id }}
        </option>
      </select>
    </label>

    <label>
      <span>Категория</span>
      <select v-model="category">
        <option value="">Все</option>
        <option v-for="item in CATEGORIES" :key="item" :value="item">{{ item }}</option>
      </select>
    </label>

    <label>
      <span>С</span>
      <input v-model="from" type="date" />
    </label>

    <label>
      <span>По</span>
      <input v-model="to" type="date" />
    </label>

    <button class="ghost" type="button" @click="reset">Сбросить</button>
  </div>
</template>
