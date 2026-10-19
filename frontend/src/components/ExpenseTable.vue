<script setup lang="ts">
import { computed } from 'vue';
import type { Category, Expense, User } from '../types';

const props = defineProps<{
  expenses: Expense[];
  users: User[];
}>();

const emit = defineEmits<{
  (e: 'edit', expense: Expense): void;
  (e: 'delete', expense: Expense): void;
}>();

const total = computed(() => props.expenses.reduce((sum, item) => sum + item.amount, 0));

const userNames = computed(() => {
  const map = new Map<number, string>();
  for (const user of props.users) {
    map.set(user.user_id, user.full_name || user.username || String(user.user_id));
  }
  return map;
});

const CATEGORY_STYLES: Record<Category, string> = {
  'еда': 'bg-amber-100 text-amber-800',
  'транспорт': 'bg-sky-100 text-sky-800',
  'жильё': 'bg-violet-100 text-violet-800',
  'развлечения': 'bg-pink-100 text-pink-800',
  'здоровье': 'bg-emerald-100 text-emerald-800',
  'покупки': 'bg-orange-100 text-orange-800',
  'другое': 'bg-slate-100 text-slate-600',
};

function badgeClass(category: Category): string {
  return CATEGORY_STYLES[category] ?? CATEGORY_STYLES['другое'];
}

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleDateString('ru-RU');
}

function formatAmount(value: number): string {
  return value.toLocaleString('ru-RU', { style: 'currency', currency: 'RUB', maximumFractionDigits: 2 });
}

function memberName(expense: Expense): string {
  return userNames.value.get(expense.user_id) ?? String(expense.user_id);
}
</script>

<template>
  <div class="overflow-x-auto">
    <table class="w-full text-sm">
      <thead>
        <tr class="border-b border-slate-200 text-left text-xs font-semibold tracking-wide text-slate-500 uppercase">
          <th class="px-4 py-3 font-semibold">Дата</th>
          <th class="px-4 py-3 font-semibold">Член семьи</th>
          <th class="px-4 py-3 font-semibold">Категория</th>
          <th class="px-4 py-3 text-right font-semibold">Сумма</th>
          <th class="px-4 py-3 font-semibold">Комментарий</th>
          <th class="px-4 py-3 text-right font-semibold">Действия</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-slate-100">
        <tr v-if="expenses.length === 0">
          <td colspan="6" class="px-4 py-12 text-center text-slate-400">Трат не найдено</td>
        </tr>
        <tr v-for="expense in expenses" :key="expense.id" class="transition-colors hover:bg-slate-50">
          <td class="px-4 py-3 whitespace-nowrap text-slate-500">{{ formatDate(expense.spent_at) }}</td>
          <td class="px-4 py-3 font-medium text-slate-800">{{ memberName(expense) }}</td>
          <td class="px-4 py-3">
            <span
              class="inline-flex rounded-full px-2.5 py-0.5 text-xs font-medium"
              :class="badgeClass(expense.category)"
            >
              {{ expense.category }}
            </span>
          </td>
          <td class="px-4 py-3 text-right font-medium tabular-nums text-slate-900">
            {{ formatAmount(expense.amount) }}
          </td>
          <td class="max-w-64 truncate px-4 py-3 text-slate-500">{{ expense.comment || '—' }}</td>
          <td class="px-4 py-3 text-right whitespace-nowrap">
            <button
              class="rounded-md px-2 py-1 text-xs font-medium text-blue-600 transition-colors hover:bg-blue-50"
              type="button"
              @click="emit('edit', expense)"
            >
              Изменить
            </button>
            <button
              class="rounded-md px-2 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-50"
              type="button"
              @click="emit('delete', expense)"
            >
              Удалить
            </button>
          </td>
        </tr>
      </tbody>
      <tfoot v-if="expenses.length > 0" class="border-t border-slate-200 bg-slate-50/60">
        <tr>
          <td colspan="3" class="px-4 py-3 font-semibold text-slate-700">Итого</td>
          <td class="px-4 py-3 text-right font-semibold tabular-nums text-slate-900">
            {{ formatAmount(total) }}
          </td>
          <td colspan="2" class="px-4 py-3 text-slate-500">{{ expenses.length }} шт.</td>
        </tr>
      </tfoot>
    </table>
  </div>
</template>
