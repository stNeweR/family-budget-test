<script setup lang="ts">
import { computed } from 'vue';
import type { Expense, User } from '../types';

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
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>Дата</th>
          <th>Член семьи</th>
          <th>Категория</th>
          <th class="num">Сумма</th>
          <th>Комментарий</th>
          <th class="actions-col">Действия</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="expenses.length === 0">
          <td colspan="6" class="empty">Трат не найдено</td>
        </tr>
        <tr v-for="expense in expenses" :key="expense.id">
          <td>{{ formatDate(expense.spent_at) }}</td>
          <td>{{ memberName(expense) }}</td>
          <td><span class="badge">{{ expense.category }}</span></td>
          <td class="num">{{ formatAmount(expense.amount) }}</td>
          <td class="comment">{{ expense.comment || '—' }}</td>
          <td class="actions-col">
            <button class="ghost" type="button" @click="emit('edit', expense)">Изменить</button>
            <button class="danger" type="button" @click="emit('delete', expense)">Удалить</button>
          </td>
        </tr>
      </tbody>
      <tfoot v-if="expenses.length > 0">
        <tr>
          <td colspan="3">Итого</td>
          <td class="num">{{ formatAmount(total) }}</td>
          <td colspan="2">{{ expenses.length }} шт.</td>
        </tr>
      </tfoot>
    </table>
  </div>
</template>
