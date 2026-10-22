import { copyLink, shareLink } from './share';

const writeText = vi.fn();
beforeEach(() => {
  writeText.mockReset().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
});

afterEach(() => {
  Reflect.deleteProperty(navigator, 'share');
  vi.restoreAllMocks();
});

const setShare = (fn: unknown) =>
  Object.defineProperty(navigator, 'share', { value: fn, configurable: true });

describe('shareLink', () => {
  it('reports cancel without copying', async () => {
    setShare(vi.fn().mockRejectedValue(new DOMException('x', 'AbortError')));
    expect(await shareLink('u', 't')).toBe('cancelled');
  });

  it('falls back to the clipboard when share fails', async () => {
    setShare(vi.fn().mockRejectedValue(new Error('boom')));
    expect(await shareLink('http://x/r/1', 't')).toBe('copied');
    expect(writeText).toHaveBeenCalledWith('http://x/r/1');
  });

  it('reports a clipboard failure', async () => {
    writeText.mockRejectedValue(new Error('denied'));
    expect(await copyLink('u')).toBe('failed');
  });
});
