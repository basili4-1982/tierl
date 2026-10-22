import type { Draft } from './validation';
import { readableText } from './color';
import { isValidColor } from './validation';

export function Preview({ draft }: { draft: Draft }) {
  const items = draft.items.filter((i) => i.label.trim() !== '' || i.imageUrl.trim() !== '');
  return (
    <section aria-labelledby="te-preview-h" className="te-preview">
      <h2 id="te-preview-h">Предпросмотр</h2>
      <h3>{draft.title.trim() || 'Без названия'}</h3>
      {draft.description.trim() && <p>{draft.description}</p>}
      <ol className="te-tiers" aria-label="Тиры">
        {draft.tiers.map((t) => (
          <li key={t.key} className="te-tier">
            <span
              className="te-tier-name"
              style={{
                background: isValidColor(t.color) ? t.color : '#cccccc',
                color: readableText(t.color),
              }}
            >
              {t.name.trim() || '—'}
            </span>
            <span className="te-tier-body" />
          </li>
        ))}
      </ol>
      <h4>Элементы</h4>
      <ul className="te-pool" aria-label="Элементы для расстановки">
        {items.map((i) => (
          <li key={i.key} className="te-chip">
            {i.imageUrl.trim() && <img src={i.imageUrl.trim()} alt="" loading="lazy" />}
            <span>{i.label.trim() || 'Без названия'}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
