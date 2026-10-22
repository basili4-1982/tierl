import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ApiError } from '../../api/client';
import { readableTextColor } from '../take-tierlist/colors';
import { fetchResult, fetchResultStats, type Result, type ResultStats } from './api';
import { formatPercent, formatSimilarity } from './format';
import { shareLink } from './share';
import './result.css';

export function ResultPage() {
  const { id = '' } = useParams();
  const result = useQuery({
    queryKey: ['result', id],
    queryFn: () => fetchResult(id),
    retry: false,
  });
  const stats = useQuery({
    queryKey: ['result', id, 'stats'],
    queryFn: () => fetchResultStats(id),
    retry: false,
  });

  if (result.isPending) {
    return (
      <main className="result" aria-busy="true">
        <p role="status">Загружаем результат…</p>
      </main>
    );
  }
  if (result.isError) {
    const notFound = result.error instanceof ApiError && result.error.status === 404;
    return (
      <main className="result">
        <h1>{notFound ? 'Результат не найден' : 'Не удалось загрузить результат'}</h1>
        {notFound ? (
          <p>Проверьте ссылку: такого результата нет.</p>
        ) : (
          <>
            <p role="alert">Попробуйте ещё раз.</p>
            <button type="button" onClick={() => void result.refetch()}>
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

  const { tierlist } = result.data;
  return (
    <main className="result">
      <h1>Ваш результат: {tierlist.title}</h1>
      <MyPlacement result={result.data} />
      <section aria-labelledby="like-you-title" className="result-card">
        <h2 id="like-you-title">Думают как вы</h2>
        {stats.isPending && <p role="status">Загружаем статистику…</p>}
        {stats.isError && (
          <>
            <p role="alert">Не удалось загрузить статистику.</p>
            <button type="button" onClick={() => void stats.refetch()}>
              Повторить статистику
            </button>
          </>
        )}
        {stats.isSuccess && <LikeYou stats={stats.data} />}
      </section>
      {stats.isSuccess && !stats.data.insufficient_data && (
        <>
          <Distribution result={result.data} stats={stats.data} />
          <Similar stats={stats.data} />
        </>
      )}
      <Actions resultId={id} slug={tierlist.slug} title={tierlist.title} />
    </main>
  );
}

function MyPlacement({ result }: { result: Result }) {
  const { tierlist, submission } = result;
  const tierOf = new Map(submission.placements.map((p) => [p.item_id, p.tier_id]));
  const tiers = [...tierlist.tiers].sort((a, b) => a.position - b.position);
  const items = [...tierlist.items].sort((a, b) => a.position - b.position);
  const skipped = items.filter((i) => !tierOf.has(i.id));
  return (
    <section aria-labelledby="mine-title">
      <h2 id="mine-title">Ваша раскладка</h2>
      <div className="result-board">
        {tiers.map((tier) => {
          const inTier = items.filter((i) => tierOf.get(i.id) === tier.id);
          return (
            <section
              key={tier.id}
              className="result-tier"
              aria-label={`${tier.name}, элементов: ${inTier.length}`}
            >
              <h3
                className="result-tier__name"
                style={{ background: tier.color, color: readableTextColor(tier.color) }}
              >
                {tier.name}
              </h3>
              <ul className="result-tier__items">
                {inTier.map((i) => (
                  <li key={i.id}>{i.label}</li>
                ))}
              </ul>
            </section>
          );
        })}
      </div>
      {skipped.length > 0 && <p>Не размещено: {skipped.map((i) => i.label).join(', ')}.</p>}
    </section>
  );
}

function LikeYou({ stats }: { stats: ResultStats }) {
  if (stats.insufficient_data || stats.like_you_percent === null) {
    return (
      <div className="result-few">
        <p className="result-few__title">Пока мало данных</p>
        <p>
          Ответов: {stats.total_submissions}, для статистики нужно не меньше {stats.min_submissions}
          . Поделитесь ссылкой на опрос, чтобы набрать ответы.
        </p>
      </div>
    );
  }
  return (
    <>
      <p className="result-big">
        <span className="result-big__value">{formatPercent(stats.like_you_percent)}</span>{' '}
        участников думают как вы
      </p>
      <p>
        Считаем тех, чей ответ совпадает с вашим не меньше чем на{' '}
        {formatSimilarity(stats.similarity_threshold)}. Всего ответов: {stats.total_submissions}.
      </p>
    </>
  );
}

function Distribution({ result, stats }: { result: Result; stats: ResultStats }) {
  const { tierlist, submission } = result;
  const tiers = [...tierlist.tiers].sort((a, b) => a.position - b.position);
  const mine = new Map(submission.placements.map((p) => [p.item_id, p.tier_id]));
  return (
    <section aria-labelledby="dist-title">
      <h2 id="dist-title">Распределение по элементам</h2>
      <ul className="result-items">
        {stats.items.map((item) => {
          const byTier = new Map(item.distribution.map((d) => [d.tier_id, d]));
          return (
            <li key={item.item_id} className="result-item">
              <h3>{item.label}</h3>
              <ul className="result-bars">
                {tiers.map((tier) => {
                  const d = byTier.get(tier.id);
                  const percent = d?.percent ?? 0;
                  const isMine = mine.get(item.item_id) === tier.id;
                  return (
                    <li key={tier.id} className="result-bar">
                      <span className="result-bar__tier">{tier.name}</span>
                      <span
                        className="result-bar__track"
                        role="img"
                        aria-label={`Тир ${tier.name}: ${formatPercent(percent)}, ответов: ${d?.count ?? 0}${isMine ? ', ваш выбор' : ''}`}
                      >
                        <span
                          className="result-bar__fill"
                          style={{ width: `${percent}%`, background: tier.color }}
                        />
                      </span>
                      <span className="result-bar__value" aria-hidden="true">
                        {formatPercent(percent)}
                        {isMine ? ' ★' : ''}
                      </span>
                    </li>
                  );
                })}
              </ul>
            </li>
          );
        })}
      </ul>
      <p className="result-legend">★ — ваш выбор.</p>
    </section>
  );
}

function Similar({ stats }: { stats: ResultStats }) {
  return (
    <section aria-labelledby="similar-title">
      <h2 id="similar-title">Самые похожие и непохожие ответы</h2>
      <p>Ответы анонимны: показываем только степень сходства с вашим.</p>
      <div className="result-similar">
        <SimilarList title="Самые похожие" list={stats.similar} />
        <SimilarList title="Самые непохожие" list={stats.dissimilar} />
      </div>
    </section>
  );
}

function SimilarList({ title, list }: { title: string; list: ResultStats['similar'] }) {
  return (
    <div>
      <h3>{title}</h3>
      {list.length === 0 ? (
        <p>Пока нет.</p>
      ) : (
        <ol>
          {list.map((s) => (
            <li key={s.result_id}>Сходство {formatSimilarity(s.similarity)}</li>
          ))}
        </ol>
      )}
    </div>
  );
}

function Actions({ resultId, slug, title }: { resultId: string; slug: string; title: string }) {
  const [message, setMessage] = useState('');
  const onShare = async () => {
    const url = `${window.location.origin}/r/${encodeURIComponent(resultId)}`;
    const outcome = await shareLink(url, title);
    setMessage(
      {
        shared: 'Ссылка отправлена.',
        copied: 'Ссылка скопирована.',
        failed: 'Не удалось скопировать ссылку.',
        cancelled: '',
      }[outcome],
    );
  };
  return (
    <section aria-label="Действия" className="result-actions">
      <button type="button" onClick={() => void onShare()}>
        Поделиться ссылкой
      </button>
      <Link to={`/t/${encodeURIComponent(slug)}`}>Пройти ещё раз</Link>
      <p role="status">{message}</p>
    </section>
  );
}
