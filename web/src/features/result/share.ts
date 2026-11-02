export type ShareOutcome = 'shared' | 'copied' | 'failed' | 'cancelled';

/** Native share sheet when available, otherwise clipboard. */
export async function shareLink(url: string, title: string): Promise<ShareOutcome> {
  if (typeof navigator.share === 'function') {
    try {
      await navigator.share({ title, url });
      return 'shared';
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') return 'cancelled';
      // fall through to the clipboard
    }
  }
  return copyLink(url);
}

export async function copyLink(url: string): Promise<'copied' | 'failed'> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(url);
      return 'copied';
    } catch {
      // Clipboard permissions vary by browser; fall through to the legacy path.
    }
  }
  return legacyCopy(url);
}

function legacyCopy(text: string): 'copied' | 'failed' {
  const exec = document.execCommand?.bind(document);
  if (!exec) return 'failed';
  const area = document.createElement('textarea');
  area.value = text;
  area.setAttribute('readonly', '');
  area.style.position = 'fixed';
  area.style.top = '0';
  area.style.opacity = '0';
  document.body.appendChild(area);
  area.focus();
  area.select();
  let ok: boolean;
  try {
    ok = exec('copy');
  } catch {
    ok = false;
  }
  area.remove();
  return ok ? 'copied' : 'failed';
}
