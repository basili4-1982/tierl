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
  try {
    await navigator.clipboard.writeText(url);
    return 'copied';
  } catch {
    return 'failed';
  }
}
