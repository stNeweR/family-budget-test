<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import FilterBar from './components/FilterBar.vue';
import ExpenseTable from './components/ExpenseTable.vue';
import ExpenseFormModal from './components/ExpenseFormModal.vue';
import { api } from './api/client';
import type { Expense, ExpenseDraft, ExpenseFilter, User } from './types';

const users = ref<User[]>([]);
const expenses = ref<Expense[]>([]);
const filter = ref<ExpenseFilter>({});
const loading = ref(false);
const error = ref('');
const modalOpen = ref(false);
const editing = ref<Expense | null>(null);

async function loadUsers() {
  try {
    users.value = await api.listUsers();
  } catch (err) {
    error.value = message(err);
  }
}

async function loadExpenses() {
  loading.value = true;
  error.value = '';
  try {
    expenses.value = await api.listExpenses(filter.value);
  } catch (err) {
    error.value = message(err);
  } finally {
    loading.value = false;
  }
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function openCreate() {
  editing.value = null;
  modalOpen.value = true;
}

function openEdit(expense: Expense) {
  editing.value = expense;
  modalOpen.value = true;
}

async function save(payload: { id?: string; draft: ExpenseDraft }) {
  error.value = '';
  try {
    if (payload.id) {
      await api.updateExpense(payload.id, {
        amount: payload.draft.amount,
        category: payload.draft.category,
        spent_at: payload.draft.spent_at,
        comment: payload.draft.comment,
      });
    } else {
      await api.createExpense(payload.draft);
    }
    modalOpen.value = false;
    editing.value = null;
    await Promise.all([loadUsers(), loadExpenses()]);
  } catch (err) {
    error.value = message(err);
  }
}

async function remove(expense: Expense) {
  if (!window.confirm(`Удалить трату на ${expense.amount} ₽?`)) return;
  error.value = '';
  try {
    await api.deleteExpense(expense.id);
    await loadExpenses();
  } catch (err) {
    error.value = message(err);
  }
}

watch(filter, loadExpenses, { deep: true });

onMounted(async () => {
  await loadUsers();
  await loadExpenses();
});
</script>

<template>
  <div class="layout">
    <header class="topbar">
      <div>
        <h1>Семейный бюджет</h1>
        <p class="subtitle">Траты всех членов семьи</p>
      </div>
      <button class="primary" type="button" @click="openCreate">Добавить трату</button>
    </header>

    <section class="panel">
      <FilterBar v-model="filter" :users="users" />
    </section>

    <p v-if="error" class="error">{{ error }}</p>

    <section class="panel">
      <p v-if="loading" class="muted">Загрузка…</p>
      <ExpenseTable
        v-else
        :expenses="expenses"
        :users="users"
        @edit="openEdit"
        @delete="remove"
      />
    </section>

    <ExpenseFormModal
      :open="modalOpen"
      :users="users"
      :expense="editing"
      @close="modalOpen = false"
      @save="save"
    />
  </div>
</template>
