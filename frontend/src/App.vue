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
  <div class="mx-auto max-w-6xl px-4 py-8 sm:px-6">
    <header class="mb-6 flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight text-slate-900">Семейный бюджет</h1>
        <p class="mt-1 text-sm text-slate-500">Траты всех членов семьи</p>
      </div>
      <button
        class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2"
        type="button"
        @click="openCreate"
      >
        Добавить трату
      </button>
    </header>

    <section class="mb-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <FilterBar v-model="filter" :users="users" />
    </section>

    <p
      v-if="error"
      class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700"
    >
      {{ error }}
    </p>

    <section class="rounded-xl border border-slate-200 bg-white shadow-sm">
      <p v-if="loading" class="px-4 py-12 text-center text-sm text-slate-500">Загрузка…</p>
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
