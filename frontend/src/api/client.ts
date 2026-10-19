import type {
  Expense,
  ExpenseDraft,
  ExpenseFilter,
  ExpensePatch,
  User,
} from '../types';

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  });

  if (!response.ok) {
    let message = `HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // ignore non-JSON error bodies
    }
    throw new ApiError(response.status, message);
  }

  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export const api = {
  listUsers(): Promise<User[]> {
    return request<User[]>('/users');
  },

  listExpenses(filter: ExpenseFilter): Promise<Expense[]> {
    const params = new URLSearchParams();
    if (filter.user_id !== undefined) params.set('user_id', String(filter.user_id));
    if (filter.from) params.set('from', filter.from);
    if (filter.to) params.set('to', filter.to);
    if (filter.category) params.set('category', filter.category);
    params.set('limit', '500');
    const query = params.toString();
    return request<Expense[]>(`/expenses${query ? `?${query}` : ''}`);
  },

  createExpense(draft: ExpenseDraft): Promise<Expense> {
    return request<Expense>('/expenses', {
      method: 'POST',
      body: JSON.stringify(draft),
    });
  },

  updateExpense(id: string, patch: ExpensePatch): Promise<Expense> {
    return request<Expense>(`/expenses/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    });
  },

  deleteExpense(id: string): Promise<void> {
    return request<void>(`/expenses/${id}`, { method: 'DELETE' });
  },
};
