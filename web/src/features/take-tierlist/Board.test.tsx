import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Board } from './Board';
import { sampleTierlist } from './handlers';
import { initBoard } from './placement';

function setup() {
  const dispatch = vi.fn();
  const state = initBoard(
    sampleTierlist.items.map((i) => i.id),
    sampleTierlist.tiers.map((t) => t.id),
  );
  render(
    <Board
      tiers={sampleTierlist.tiers}
      items={sampleTierlist.items}
      state={state}
      dispatch={dispatch}
    />,
  );
  return dispatch;
}

describe('Board', () => {
  it('exposes draggable items to assistive tech with instructions', () => {
    setup();
    const handle = screen.getByText('Go').closest('[role="button"]');
    expect(handle).toHaveAttribute('aria-roledescription');
    expect(handle).toHaveAttribute('tabindex', '0');
    expect(document.body.textContent).toContain('Чтобы взять элемент');
  });

  it('announces keyboard dragging and cancel', async () => {
    const user = userEvent.setup();
    setup();
    const handle = screen.getByText('Go').closest<HTMLElement>('[role="button"]')!;
    handle.focus();
    await user.keyboard(' ');
    expect(document.body.textContent).toContain('Взят элемент «Go»');
    await user.keyboard('{Escape}');
    expect(document.body.textContent).toContain('Перемещение «Go» отменено');
  });

  it('dispatches a move from the accessible select', async () => {
    const user = userEvent.setup();
    const dispatch = setup();
    await user.selectOptions(screen.getByLabelText('Переместить «Go»'), 'S');
    expect(dispatch).toHaveBeenCalledWith({
      type: 'move',
      itemId: sampleTierlist.items[0]!.id,
      to: sampleTierlist.tiers[0]!.id,
    });
  });
});
