import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import type { FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { ConfirmDialog } from '../tierlist-editor/ConfirmDialog';
import { TextField } from '../auth/AuthForm';
import { useFormFeedback } from '../auth/useFormFeedback';
import { compact, validateNewPassword } from '../auth/validation';
import {
  changePassword,
  deleteTierlist,
  fetchMyTierlists,
  setTierlistStatus,
  type TierlistSummary,
} from './api';
import './profile.css';

const MY_KEY = ['me', 'tierlists'] as const;

const STATUS_LABELS: Record<string, string> = {
  open: 'открыт',
  closed: 'закрыт',
  draft: 'черновик',
  hidden: 'скрыт',
};

function statusLabel(status: string) {
  return STATUS_LABELS[status] ?? status;
}

export function ProfilePage() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: MY_KEY, queryFn: fetchMyTierlists });
  const [confirm, setConfirm] = useState<TierlistSummary | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const refresh = () => qc.invalidateQueries({ queryKey: MY_KEY });

  const statusMutation = useMutation({
    mutationFn: (vars: { id: string; status: 'public' | 'closed' }) =>
      setTierlistStatus(vars.id, vars.status),
    onSuccess: (_data, vars) => {
      setActionError(null);
      setNotice(vars.status === 'closed' ? 'Опрос закрыт.' : 'Опрос снова открыт.');
      void refresh();
    },
    onError: (err: unknown) => {
      setNotice(null);
      setActionError(messageOf(err));
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteTierlist(id),
    onSuccess: () => {
      setActionError(null);
      setNotice('Опрос удалён.');
      setConfirm(null);
      void refresh();
    },
    onError: (err: unknown) => {
      setActionError(messageOf(err));
      setConfirm(null);
    },
  });

  return (
    <main className="profile">
      <h1>Профиль</h1>

      {list.data && list.data.items.length > 0 && (
        <p className="profile-meta">
          Всего опросов: {list.data.items.length}. Ответов:{' '}
          {list.data.items.reduce((sum, t) => sum + t.submissions_count, 0)}.
        </p>
      )}

      <section aria-labelledby="my-lists-title">
        <h2 id="my-lists-title">Мои опросы</h2>
        {notice && (
          <p role="status" className="profile-notice">
            {notice}
          </p>
        )}
        {actionError && (
          <p role="alert" className="profile-error">
            {actionError}
          </p>
        )}

        {list.isPending && <p role="status">Загрузка…</p>}
        {list.isError && (
          <div>
            <p role="alert">Не удалось загрузить опросы.</p>
            <button type="button" onClick={() => void list.refetch()}>
              Повторить
            </button>
          </div>
        )}
        {list.data && list.data.items.length === 0 && (
          <p>
            У вас ещё нет опросов. <Link to="/tierlists/new">Создать опрос</Link>.
          </p>
        )}
        {list.data && list.data.items.length > 0 && (
          <ul className="profile-list">
            {list.data.items.map((t) => {
              const closed = t.status === 'closed';
              return (
                <li key={t.id}>
                  <div>
                    <h3>
                      <Link to={`/t/${encodeURIComponent(t.slug)}`}>{t.title}</Link>
                    </h3>
                    <p className="profile-meta">
                      Статус: {statusLabel(t.status)}. Ответов: {t.submissions_count}. Элементов:{' '}
                      {t.items_count}.
                    </p>
                  </div>
                  <div className="profile-actions">
                    <Link to={`/tierlists/${encodeURIComponent(t.slug)}/edit`}>Редактировать</Link>
                    <button
                      type="button"
                      disabled={statusMutation.isPending}
                      onClick={() =>
                        statusMutation.mutate({ id: t.id, status: closed ? 'public' : 'closed' })
                      }
                    >
                      {closed ? 'Открыть' : 'Закрыть'}
                    </button>
                    <button
                      type="button"
                      className="profile-danger"
                      disabled={deleteMutation.isPending}
                      onClick={() => setConfirm(t)}
                    >
                      Удалить
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      <PasswordForm />

      {confirm && (
        <ConfirmDialog
          title="Удалить опрос?"
          description={`«${confirm.title}» и все ответы будут удалены без возможности восстановления.`}
          confirmLabel="Удалить"
          busy={deleteMutation.isPending}
          onConfirm={() => deleteMutation.mutate(confirm.id)}
          onCancel={() => setConfirm(null)}
        />
      )}
    </main>
  );
}

function PasswordForm() {
  const fb = useFormFeedback();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [repeat, setRepeat] = useState('');
  const [notice, setNotice] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: (vars: { current: string; next: string }) =>
      changePassword(vars.current, vars.next),
    onSuccess: () => {
      fb.clear();
      setCurrent('');
      setNext('');
      setRepeat('');
      setNotice('Пароль изменён. Остальные сессии закрыты.');
    },
    onError: (err: unknown) => {
      setNotice(null);
      fb.fail(err, { password: 'new_password' });
    },
  });

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    setNotice(null);
    const errors = compact({
      current_password: current ? undefined : 'Введите текущий пароль',
      new_password: validateNewPassword(next),
      repeat: next !== repeat ? 'Пароли не совпадают' : undefined,
    });
    if (fb.invalid(errors)) return;
    mutation.mutate({ current, next });
  }

  return (
    <section aria-labelledby="password-title" className="profile-password">
      <h2 id="password-title">Смена пароля</h2>
      {notice && (
        <p role="status" className="profile-notice">
          {notice}
        </p>
      )}
      {fb.formError && (
        <p role="alert" className="profile-error">
          {fb.formError}
        </p>
      )}
      <form noValidate onSubmit={onSubmit} aria-busy={mutation.isPending}>
        <TextField
          label="Текущий пароль"
          name="current_password"
          type="password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          error={fb.fieldErrors.current_password}
        />
        <TextField
          label="Новый пароль"
          name="new_password"
          type="password"
          autoComplete="new-password"
          hint="От 10 до 128 символов"
          value={next}
          onChange={(e) => setNext(e.target.value)}
          error={fb.fieldErrors.new_password}
        />
        <TextField
          label="Повторите новый пароль"
          name="repeat"
          type="password"
          autoComplete="new-password"
          value={repeat}
          onChange={(e) => setRepeat(e.target.value)}
          error={fb.fieldErrors.repeat}
        />
        <button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? 'Меняем…' : 'Сменить пароль'}
        </button>
      </form>
    </section>
  );
}

function messageOf(err: unknown): string {
  if (err instanceof Error && err.message) return err.message;
  return 'Не удалось выполнить действие. Попробуйте ещё раз.';
}
