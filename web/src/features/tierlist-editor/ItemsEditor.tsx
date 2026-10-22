import { useState } from 'react';
import { move, newItem } from './model';
import { LIMITS } from './validation';
import type { DraftItem, FieldErrors } from './validation';

interface Props {
  items: DraftItem[];
  errors: FieldErrors;
  disabled: boolean;
  onChange: (items: DraftItem[]) => void;
}

export function ItemsEditor({ items, errors, disabled, onChange }: Props) {
  const [focusKey, setFocusKey] = useState<string | null>(null);

  const patch = (i: number, p: Partial<DraftItem>) =>
    onChange(items.map((it, j) => (j === i ? { ...it, ...p } : it)));

  const add = () => {
    const it = newItem();
    setFocusKey(it.key);
    onChange([...items, it]);
  };

  return (
    <fieldset disabled={disabled} aria-describedby={errors.items ? 'te-items-err' : undefined}>
      <legend>
        Элементы ({items.length} из {LIMITS.minItems}–{LIMITS.maxItems})
      </legend>
      {errors.items && (
        <p id="te-items-err" className="te-error">
          {errors.items}
        </p>
      )}
      <ol className="te-list">
        {items.map((it, i) => {
          const n = i + 1;
          const labelErr = errors[`items.${i}.label`];
          const urlErr = errors[`items.${i}.image_url`];
          return (
            <li key={it.key}>
              <div>
                <div className="te-field">
                  <label htmlFor={`item-label-${it.key}`}>Название элемента {n}</label>
                  <input
                    id={`item-label-${it.key}`}
                    type="text"
                    value={it.label}
                    ref={(el) => {
                      if (el && focusKey === it.key) {
                        el.focus();
                        setFocusKey(null);
                      }
                    }}
                    aria-invalid={labelErr ? true : undefined}
                    aria-describedby={labelErr ? `item-label-err-${it.key}` : undefined}
                    onChange={(e) => patch(i, { label: e.target.value })}
                  />
                  {labelErr && (
                    <p id={`item-label-err-${it.key}`} className="te-error">
                      {labelErr}
                    </p>
                  )}
                </div>
                <div className="te-field">
                  <label htmlFor={`item-img-${it.key}`}>
                    Ссылка на картинку {n} (необязательно)
                  </label>
                  <input
                    id={`item-img-${it.key}`}
                    type="url"
                    inputMode="url"
                    value={it.imageUrl}
                    aria-invalid={urlErr ? true : undefined}
                    aria-describedby={urlErr ? `item-img-err-${it.key}` : undefined}
                    onChange={(e) => patch(i, { imageUrl: e.target.value })}
                  />
                  {urlErr && (
                    <p id={`item-img-err-${it.key}`} className="te-error">
                      {urlErr}
                    </p>
                  )}
                </div>
              </div>
              <div className="te-row">
                <button
                  type="button"
                  aria-label={`Поднять элемент ${n}`}
                  disabled={i === 0}
                  onClick={() => onChange(move(items, i, -1))}
                >
                  ↑
                </button>
                <button
                  type="button"
                  aria-label={`Опустить элемент ${n}`}
                  disabled={i === items.length - 1}
                  onClick={() => onChange(move(items, i, 1))}
                >
                  ↓
                </button>
                <button
                  type="button"
                  aria-label={`Удалить элемент ${n}`}
                  onClick={() => onChange(items.filter((_, j) => j !== i))}
                >
                  Удалить
                </button>
              </div>
            </li>
          );
        })}
      </ol>
      <button type="button" onClick={add} disabled={items.length >= LIMITS.maxItems}>
        Добавить элемент
      </button>
    </fieldset>
  );
}
