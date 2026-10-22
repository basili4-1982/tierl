import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { ApiError } from '../../api/client';
import {
  apiFieldErrors,
  closeTierlist,
  createTierlist,
  deleteTierlist,
  getTierlist,
  updateTierlist,
} from './api';
import type { Tierlist } from './api';
import { ConfirmDialog } from './ConfirmDialog';
import { ItemsEditor } from './ItemsEditor';
import { draftFromTierlist, emptyDraft, toStructure } from './model';
import { Preview } from './Preview';
import { TiersEditor } from './TiersEditor';
import { LIMITS, validateDraft } from './validation';
import type { Draft, FieldErrors, Visibility } from './validation';
import './editor.css';

interface Problem {
  code?: string;
  message: string;
  fields: FieldErrors;
}

function toProblem(e: unknown): Problem {
  if (e instanceof ApiError) {
    return {
      code: e.body?.code,
      message: e.body?.message ?? `Ошибка сервера (HTTP ${e.status})`,
      fields: apiFieldErrors(e),
    };
  }
  return {
    message: 'Не удалось связаться с сервером. Проверьте соединение и повторите.',
    fields: {},
  };
}

export function TierlistEditorPage() {
  const { slug } = useParams();
  return <Editor key={slug ?? 'new'} slug={slug} />;
}

type Confirm = 'close' | 'delete' | null;

function Editor({ slug }: { slug: string | undefined }) {
  const navigate = useNavigate();
  const location = useLocation();
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [saved, setSaved] = useState<Tierlist | null>(null);
  const [loading, setLoading] = useState(slug !== undefined);
  const [loadProblem, setLoadProblem] = useState<Problem | null>(null);
  const [errors, setErrors] = useState<FieldErrors>({});
  const [problem, setProblem] = useState<Problem | null>(null);
  const [notice, setNotice] = useState<string | null>(
    (location.state as { created?: boolean } | null)?.created ? 'Опрос создан.' : null,
  );
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState<Confirm>(null);
  const [lockedByApi, setLockedByApi] = useState(false);
  const summaryRef = useRef<HTMLDivElement>(null);
  const [focusSummary, setFocusSummary] = useState(0);

  const isEdit = slug !== undefined;
  const locked = lockedByApi || (saved?.submissions_count ?? 0) > 0;
  const closed = saved?.status === 'closed';

  useEffect(() => {
    if (!slug) return;
    let cancelled = false;
    getTierlist(slug)
      .then((t) => {
        if (cancelled) return;
        setSaved(t);
        setDraft(draftFromTierlist(t));
        setLoading(false);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setLoadProblem(toProblem(e));
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [slug]);

  useEffect(() => {
    if (focusSummary > 0) summaryRef.current?.focus();
  }, [focusSummary]);

  const fail = (p: Problem, fields: FieldErrors) => {
    setProblem(p);
    setErrors(fields);
    setFocusSummary((n) => n + 1);
  };

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setDraft((d) => ({ ...d, [k]: v }));

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setNotice(null);
    // Locked structure is not sent, so it is not validated either.
    const found = validateDraft(draft);
    if (locked) {
      for (const k of Object.keys(found))
        if (k.startsWith('items') || k.startsWith('tiers')) delete found[k];
    }
    if (isEdit) delete found.slug; // slug is immutable after creation
    if (Object.keys(found).length > 0) {
      fail({ message: 'Исправьте ошибки в форме.', fields: {} }, found);
      return;
    }
    setProblem(null);
    setErrors({});
    setBusy(true);
    try {
      const base = {
        title: draft.title.trim(),
        description: draft.description,
        visibility: draft.visibility,
      };
      if (!isEdit) {
        const created = await createTierlist({
          ...base,
          ...toStructure(draft),
          ...(draft.visibility === 'public' ? { slug: draft.slug } : {}),
        });
        navigate(`/tierlists/${encodeURIComponent(created.slug)}/edit`, {
          state: { created: true },
        });
      } else {
        const updated = await updateTierlist(
          slug,
          locked ? base : { ...base, ...toStructure(draft) },
        );
        setSaved(updated);
        setNotice('Изменения сохранены.');
      }
    } catch (err) {
      const p = toProblem(err);
      if (p.code === 'tierlist_locked') setLockedByApi(true);
      fail(p, p.fields);
    } finally {
      setBusy(false);
    }
  };

  const runConfirmed = async () => {
    if (!slug || !confirm) return;
    setBusy(true);
    try {
      if (confirm === 'delete') {
        await deleteTierlist(slug);
        navigate('/');
        return;
      }
      setSaved(await closeTierlist(slug));
      setNotice('Опрос закрыт: новые ответы не принимаются.');
      setProblem(null);
    } catch (err) {
      fail(toProblem(err), {});
    } finally {
      setBusy(false);
      setConfirm(null);
    }
  };

  if (loading) {
    return (
      <main className="te">
        <p role="status">Загрузка…</p>
      </main>
    );
  }
  if (loadProblem) {
    return (
      <main className="te">
        <h1>Редактирование опроса</h1>
        <p role="alert">{loadProblem.message}</p>
      </main>
    );
  }
  if (saved && !saved.is_owner) {
    return (
      <main className="te">
        <h1>Редактирование опроса</h1>
        <p role="alert">Редактировать опрос может только его автор.</p>
      </main>
    );
  }

  const fieldErr = (k: string) => errors[k];
  const summaryItems = Object.entries(errors);

  return (
    <main className="te">
      <h1>{isEdit ? 'Редактирование опроса' : 'Новый опрос'}</h1>
      {notice && (
        <p role="status" className="te-notice">
          {notice}
        </p>
      )}
      {isEdit && saved && (
        <p>
          Статус: {closed ? 'закрыт' : 'открыт'}. Ответов: {saved.submissions_count}.
        </p>
      )}
      {locked && (
        <p role="note" className="te-hint">
          Опрос уже проходили: элементы и тиры изменить нельзя. Название, описание и видимость
          доступны.
        </p>
      )}

      <div ref={summaryRef} tabIndex={-1}>
        {problem && (
          <div role="alert" className="te-summary">
            <p>
              {problem.message}
              {problem.code && <small> (код: {problem.code})</small>}
            </p>
            {summaryItems.length > 0 && (
              <ul>
                {summaryItems.map(([k, v]) => (
                  <li key={k}>
                    {k}: {v}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>

      <div className="te-layout">
        <form onSubmit={onSubmit} noValidate aria-label="Форма опроса">
          <div className="te-field">
            <label htmlFor="te-title">Название</label>
            <input
              id="te-title"
              type="text"
              value={draft.title}
              aria-invalid={fieldErr('title') ? true : undefined}
              aria-describedby={fieldErr('title') ? 'te-title-err' : undefined}
              onChange={(e) => set('title', e.target.value)}
            />
            {fieldErr('title') && (
              <p id="te-title-err" className="te-error">
                {fieldErr('title')}
              </p>
            )}
          </div>
          <div className="te-field">
            <label htmlFor="te-desc">Описание (необязательно)</label>
            <textarea
              id="te-desc"
              rows={3}
              value={draft.description}
              aria-invalid={fieldErr('description') ? true : undefined}
              aria-describedby={fieldErr('description') ? 'te-desc-err' : undefined}
              onChange={(e) => set('description', e.target.value)}
            />
            <p className="te-hint">
              {Array.from(draft.description).length} / {LIMITS.maxDescription}
            </p>
            {fieldErr('description') && (
              <p id="te-desc-err" className="te-error">
                {fieldErr('description')}
              </p>
            )}
          </div>

          <fieldset>
            <legend>Видимость</legend>
            {(['unlisted', 'public'] as Visibility[]).map((v) => (
              <div key={v}>
                <label>
                  <input
                    type="radio"
                    name="visibility"
                    value={v}
                    checked={draft.visibility === v}
                    onChange={() => set('visibility', v)}
                  />{' '}
                  {v === 'public' ? 'Публичный (виден в каталоге)' : 'По ссылке (не в каталоге)'}
                </label>
              </div>
            ))}
            {draft.visibility === 'public' && (
              <div className="te-field">
                <label htmlFor="te-slug">Адрес опроса</label>
                <input
                  id="te-slug"
                  type="text"
                  value={draft.slug}
                  readOnly={isEdit}
                  aria-invalid={fieldErr('slug') ? true : undefined}
                  aria-describedby={fieldErr('slug') ? 'te-slug-err' : 'te-slug-hint'}
                  onChange={(e) => set('slug', e.target.value)}
                />
                <p id="te-slug-hint" className="te-hint">
                  Латиница, цифры, _ и -, от {LIMITS.minSlug} до {LIMITS.maxSlug} символов.
                </p>
                {fieldErr('slug') && (
                  <p id="te-slug-err" className="te-error">
                    {fieldErr('slug')}
                  </p>
                )}
              </div>
            )}
            {draft.visibility === 'unlisted' && (
              <p className="te-hint">
                Адрес будет сгенерирован автоматически и будет трудно угадываемым.
              </p>
            )}
          </fieldset>

          <ItemsEditor
            items={draft.items}
            errors={errors}
            disabled={locked}
            onChange={(v) => set('items', v)}
          />
          <TiersEditor
            tiers={draft.tiers}
            errors={errors}
            disabled={locked}
            onChange={(v) => set('tiers', v)}
          />

          <div className="te-row">
            <button type="submit" disabled={busy}>
              {isEdit ? 'Сохранить' : 'Создать опрос'}
            </button>
            {isEdit && !closed && (
              <button type="button" disabled={busy} onClick={() => setConfirm('close')}>
                Закрыть опрос
              </button>
            )}
            {isEdit && (
              <button
                type="button"
                className="te-danger"
                disabled={busy}
                onClick={() => setConfirm('delete')}
              >
                Удалить опрос
              </button>
            )}
          </div>
        </form>
        <Preview draft={draft} />
      </div>

      {confirm && (
        <ConfirmDialog
          title={confirm === 'delete' ? 'Удалить опрос?' : 'Закрыть опрос?'}
          description={
            confirm === 'delete'
              ? 'Опрос и все ответы будут удалены без возможности восстановления.'
              : 'Новые ответы приниматься не будут. Существующие результаты сохранятся.'
          }
          confirmLabel={confirm === 'delete' ? 'Удалить' : 'Закрыть'}
          busy={busy}
          onConfirm={runConfirmed}
          onCancel={() => setConfirm(null)}
        />
      )}
    </main>
  );
}
