<script setup lang="ts">
import { reactive, watch } from 'vue';
import { CATEGORIES, type Category, type Expense, type ExpenseDraft, type User } from '../types';

const props = defineProps<{
  open: boolean;
  users: User[];
  expense: Expense | null;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'save', payload: { id?: string; draft: ExpenseDraft }): void;
}>();

const form = reactive<{
  user_id: number | '';
  amount: number | '';
  category: Category;
  spent_at: string;
  comment: string;
}>({
  user_id: '',
  amount: '',
  category: 'еда',
  spent_at: today(),
  comment: '',
});

const inputClass =
  'w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-800 shadow-sm transition-colors focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30 disabled:bg-slate-100 disabled:text-slate-500';

function today(): string {
  return new Date().toISOString().slice(0, 10);
}

watch(
  () => [props.open, props.expense, props.users] as const,
  () => {
    if (!props.open) return;
    if (props.expense) {
      form.user_id = props.expense.user_id;
      form.amount = props.expense.amount;
      form.category = props.expense.category;
      form.spent_at = props.expense.spent_at.slice(0, 10);
      form.comment = props.expense.comment;
    } else {
      form.user_id = props.users[0]?.user_id ?? '';
      form.amount = '';
      form.category = 'еда';
      form.spent_at = today();
      form.comment = '';
    }
  },
  { immediate: true },
);

function submit() {
  if (form.user_id === '' || form.amount === '' || Number(form.amount) <= 0) {
    return;
  }
  emit('save', {
    id: props.expense?.id,
    draft: {
      user_id: Number(form.user_id),
      amount: Number(form.amount),
      category: form.category,
      spent_at: form.spent_at,
      comment: form.comment.trim(),
    },
  });
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4 backdrop-blur-sm"
    @click.self="emit('close')"
  >
    <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl">
      <h2 class="mb-4 text-lg font-semibold text-slate-900">
        {{ expense ? 'Изменить трату' : 'Новая трата' }}
      </h2>
      <form class="grid gap-4" @submit.prevent="submit">
        <label class="flex flex-col gap-1.5 text-sm font-medium text-slate-600">
          <span>Член семьи</span>
          <select v-model="form.user_id" :class="inputClass" :disabled="!!expense" required>
            <option disabled value="">Выберите</option>
            <option v-for="user in users" :key="user.user_id" :value="user.user_id">
              {{ user.full_name || user.username || user.user_id }}
            </option>
          </select>
        </label>

        <label class="flex flex-col gap-1.5 text-sm font-medium text-slate-600">
          <span>Сумма, ₽</span>
          <input v-model.number="form.amount" :class="inputClass" type="number" min="1" step="0.01" required />
        </label>

        <label class="flex flex-col gap-1.5 text-sm font-medium text-slate-600">
          <span>Категория</span>
          <select v-model="form.category" :class="inputClass">
            <option v-for="item in CATEGORIES" :key="item" :value="item">{{ item }}</option>
          </select>
        </label>

        <label class="flex flex-col gap-1.5 text-sm font-medium text-slate-600">
          <span>Дата</span>
          <input v-model="form.spent_at" :class="inputClass" type="date" required />
        </label>

        <label class="flex flex-col gap-1.5 text-sm font-medium text-slate-600">
          <span>Комментарий</span>
          <input
            v-model="form.comment"
            :class="inputClass"
            type="text"
            maxlength="200"
            placeholder="необязательно"
          />
        </label>

        <div class="mt-1 flex justify-end gap-2">
          <button
            class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-600 shadow-sm transition-colors hover:bg-slate-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            type="button"
            @click="emit('close')"
          >
            Отмена
          </button>
          <button
            class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2"
            type="submit"
          >
            Сохранить
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
