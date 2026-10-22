import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useReducer, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ApiError } from '../../api/client';
import {
  fetchMySubmission,
  fetchTierlist,
  saveSubmission,
  type Submission,
  type Tierlist,
} from './api';
import { Board } from './Board';
import {
  boardReducer,
  canSubmit,
  initBoard,
  placedCount,
  requiredCount,
  toPlacements,
} from './placement';
import './take-tierlist.css';

export function TakeTierlistPage() {
  const { slug = '' } = useParams();
  const tierlist = useQuery({
    queryKey: ['tierlist', slug],
    queryFn: () => fetchTierlist(slug),
    retry: false,
  });
  const mine = useQuery({
    queryKey: ['tierlist', slug, 'mine'],
    queryFn: () => fetchMySubmission(slug),
    enabled: tierlist.isSuccess,
    retry: false,
  });

  if (tierlist.isPending || (tierlist.isSuccess && mine.isPending)) {
    return (
      <main className="take" aria-busy="true">
        <p role="status">Загружаем опрос…</p>
      </main>
    );
  }
  if (tierlist.isError) {
    const notFound = tierlist.error instanceof ApiError && tierlist.error.status === 404;
    return (
      <main className="take">
        <h1>{notFound ? 'Опрос не найден' : 'Не удалось загрузить опрос'}</h1>
        {notFound ? (
          <p>Проверьте ссылку: такого опроса нет или он удалён.</p>
        ) : (
          <>
            <p role="alert">Попробуйте ещё раз.</p>
            <button type="button" onClick={() => void tierlist.refetch()}>
              Повторить
            </button>
          </>
        )}
        <p>
          <Link to="/">На главную</Link>
        </p>
      </main>
    );
  }
  if (mine.isError) {
    return (
      <main className="take">
        <h1>{tierlist.data.title}</h1>
        <p role="alert">Не удалось загрузить ваш предыдущий ответ.</p>
        <button type="button" onClick={() => void mine.refetch()}>
          Повторить
        </button>
      </main>
    );
  }
  return <Taker slug={slug} tierlist={tierlist.data} previous={mine.data ?? null} />;
}

function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 429) return 'Слишком много попыток. Подождите немного и повторите.';
    if (e.body?.message) return e.body.message;
  }
  return 'Не удалось отправить ответ. Проверьте соединение и повторите.';
}

function Taker({
  slug,
  tierlist,
  previous,
}: {
  slug: string;
  tierlist: Tierlist;
  previous: Submission | null;
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [closedByServer, setClosedByServer] = useState(false);
  const itemIds = tierlist.items.map((i) => i.id);
  const [state, dispatch] = useReducer(boardReducer, undefined, () =>
    initBoard(
      itemIds,
      tierlist.tiers.map((t) => t.id),
      previous?.placements,
    ),
  );

  const save = useMutation({
    mutationFn: () => saveSubmission(slug, toPlacements(state), previous !== null),
    onSuccess: async (sub) => {
      queryClient.setQueryData(['tierlist', slug, 'mine'], sub);
      await navigate(`/r/${sub.id}`);
    },
    onError: (e) => {
      if (e instanceof ApiError && e.body?.code === 'tierlist_closed') setClosedByServer(true);
    },
  });

  const total = tierlist.items.length;
  const need = requiredCount(total);
  const placed = placedCount(state);
  const closed = tierlist.status === 'closed' || closedByServer;

  if (closed) {
    return (
      <main className="take">
        <h1>{tierlist.title}</h1>
        <p role="status">Опрос закрыт, новые ответы не принимаются.</p>
        {previous && <Link to={`/r/${previous.id}`}>Посмотреть ваш результат</Link>}
      </main>
    );
  }

  const ready = canSubmit(state, total);
  return (
    <main className="take">
      <h1>{tierlist.title}</h1>
      {tierlist.description && <p>{tierlist.description}</p>}
      {previous && (
        <p className="take-note">
          Вы уже отвечали на этот опрос. Измените расстановку, чтобы перепройти.{' '}
          <Link to={`/r/${previous.id}`}>Текущий результат</Link>
        </p>
      )}
      <Board
        tiers={tierlist.tiers}
        items={tierlist.items}
        state={state}
        dispatch={dispatch}
        disabled={save.isPending}
      />
      <div className="take-actions">
        <p id="take-progress" role="status">
          Размещено {placed} из {total}
          {!ready && `; нужно не меньше ${need}`}
        </p>
        {save.isError && <p role="alert">{errorMessage(save.error)}</p>}
        <button
          type="button"
          className="take-submit"
          disabled={!ready || save.isPending}
          aria-describedby="take-progress"
          onClick={() => save.mutate()}
        >
          {save.isPending ? 'Отправляем…' : previous ? 'Обновить ответ' : 'Отправить ответ'}
        </button>
      </div>
    </main>
  );
}
