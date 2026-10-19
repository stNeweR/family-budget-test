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
  <div v-if="open" class="modal-backdrop" @click.self="emit('close')">
    <div class="modal">
      <h2>{{ expense ? 'Изменить трату' : 'Новая трата' }}</h2>
      <form @submit.prevent="submit">
        <label>
          <span>Член семьи</span>
          <select v-model="form.user_id" :disabled="!!expense" required>
            <option disabled value="">Выберите</option>
            <option v-for="user in users" :key="user.user_id" :value="user.user_id">
              {{ user.full_name || user.username || user.user_id }}
            </option>
          </select>
        </label>

        <label>
          <span>Сумма, ₽</span>
          <input v-model.number="form.amount" type="number" min="1" step="0.01" required />
        </label>

        <label>
          <span>Категория</span>
          <select v-model="form.category">
            <option v-for="item in CATEGORIES" :key="item" :value="item">{{ item }}</option>
          </select>
        </label>

        <label>
          <span>Дата</span>
          <input v-model="form.spent_at" type="date" required />
        </label>

        <label>
          <span>Комментарий</span>
          <input v-model="form.comment" type="text" maxlength="200" placeholder="необязательно" />
        </label>

        <div class="modal-actions">
          <button class="ghost" type="button" @click="emit('close')">Отмена</button>
          <button class="primary" type="submit">Сохранить</button>
        </div>
      </form>
    </div>
  </div>
</template>
