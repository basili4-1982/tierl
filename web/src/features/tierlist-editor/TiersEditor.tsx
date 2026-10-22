import { DEFAULT_TIERS, move, newTier } from './model';
import { LIMITS } from './validation';
import type { DraftTier, FieldErrors } from './validation';

interface Props {
  tiers: DraftTier[];
  errors: FieldErrors;
  disabled: boolean;
  onChange: (tiers: DraftTier[]) => void;
}

export function TiersEditor({ tiers, errors, disabled, onChange }: Props) {
  const patch = (i: number, p: Partial<DraftTier>) =>
    onChange(tiers.map((t, j) => (j === i ? { ...t, ...p } : t)));

  return (
    <fieldset disabled={disabled} aria-describedby={errors.tiers ? 'te-tiers-err' : undefined}>
      <legend>
        Тиры ({tiers.length} из {LIMITS.minTiers}–{LIMITS.maxTiers})
      </legend>
      {errors.tiers && (
        <p id="te-tiers-err" className="te-error">
          {errors.tiers}
        </p>
      )}
      <ol className="te-list">
        {tiers.map((t, i) => {
          const n = i + 1;
          const nameErr = errors[`tiers.${i}.name`];
          const colorErr = errors[`tiers.${i}.color`];
          return (
            <li key={t.key}>
              <div className="te-row">
                <div className="te-field">
                  <label htmlFor={`tier-name-${t.key}`}>Название тира {n}</label>
                  <input
                    id={`tier-name-${t.key}`}
                    type="text"
                    value={t.name}
                    aria-invalid={nameErr ? true : undefined}
                    aria-describedby={nameErr ? `tier-name-err-${t.key}` : undefined}
                    onChange={(e) => patch(i, { name: e.target.value })}
                  />
                  {nameErr && (
                    <p id={`tier-name-err-${t.key}`} className="te-error">
                      {nameErr}
                    </p>
                  )}
                </div>
                <div className="te-field">
                  <label htmlFor={`tier-color-${t.key}`}>Цвет тира {n} (#rrggbb)</label>
                  <div className="te-row">
                    <input
                      id={`tier-color-${t.key}`}
                      type="text"
                      value={t.color}
                      maxLength={7}
                      aria-invalid={colorErr ? true : undefined}
                      aria-describedby={colorErr ? `tier-color-err-${t.key}` : undefined}
                      onChange={(e) => patch(i, { color: e.target.value })}
                      style={{ width: '8rem' }}
                    />
                    <input
                      type="color"
                      aria-label={`Выбор цвета тира ${n}`}
                      value={/^#[0-9a-fA-F]{6}$/.test(t.color) ? t.color.toLowerCase() : '#cccccc'}
                      onChange={(e) => patch(i, { color: e.target.value })}
                    />
                  </div>
                  {colorErr && (
                    <p id={`tier-color-err-${t.key}`} className="te-error">
                      {colorErr}
                    </p>
                  )}
                </div>
              </div>
              <div className="te-row">
                <button
                  type="button"
                  aria-label={`Поднять тир ${n}`}
                  disabled={i === 0}
                  onClick={() => onChange(move(tiers, i, -1))}
                >
                  ↑
                </button>
                <button
                  type="button"
                  aria-label={`Опустить тир ${n}`}
                  disabled={i === tiers.length - 1}
                  onClick={() => onChange(move(tiers, i, 1))}
                >
                  ↓
                </button>
                <button
                  type="button"
                  aria-label={`Удалить тир ${n}`}
                  onClick={() => onChange(tiers.filter((_, j) => j !== i))}
                >
                  Удалить
                </button>
              </div>
            </li>
          );
        })}
      </ol>
      <div className="te-row">
        <button
          type="button"
          onClick={() => onChange([...tiers, newTier()])}
          disabled={tiers.length >= LIMITS.maxTiers}
        >
          Добавить тир
        </button>
        <button
          type="button"
          onClick={() => onChange(DEFAULT_TIERS.map((t) => newTier(t.name, t.color)))}
        >
          Тиры по умолчанию (S–F)
        </button>
      </div>
    </fieldset>
  );
}
