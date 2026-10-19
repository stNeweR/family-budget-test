export type Category =
  | 'еда'
  | 'транспорт'
  | 'жильё'
  | 'развлечения'
  | 'здоровье'
  | 'покупки'
  | 'другое';

export const CATEGORIES: Category[] = [
  'еда',
  'транспорт',
  'жильё',
  'развлечения',
  'здоровье',
  'покупки',
  'другое',
];

export interface Expense {
  id: string;
  user_id: number;
  amount: number;
  category: Category;
  spent_at: string;
  comment: string;
  created_at: string;
  updated_at: string;
}

export interface User {
  user_id: number;
  username: string;
  full_name: string;
}

export interface ExpenseDraft {
  user_id: number;
  amount: number;
  category: Category;
  spent_at: string;
  comment: string;
}

export interface ExpensePatch {
  amount?: number;
  category?: Category;
  spent_at?: string;
  comment?: string;
}

export interface ExpenseFilter {
  user_id?: number;
  from?: string;
  to?: string;
  category?: Category;
}
