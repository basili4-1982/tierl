import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SEED_EMAIL, SEED_PASSWORD } from '../auth/mocks';
import { renderApp, resetAuthTestState } from '../auth/test-utils';
import { AppRoutes } from '../../routes';

beforeEach(resetAuthTestState);

async function openProfile() {
  const { login } = await import('../auth/api');
  await login({ email: SEED_EMAIL, password: SEED_PASSWORD });
  renderApp(<AppRoutes />, ['/profile']);
  await screen.findByRole('heading', { name: 'Профиль' });
}

describe('profile', () => {
  it('lists the author tier lists with status and aggregates', async () => {
    await openProfile();
    expect(await screen.findByText('Открытый опрос')).toBeInTheDocument();
    expect(screen.getByText('Закрытый опрос')).toBeInTheDocument();
    expect(screen.getByText(/Статус: открыт\. Ответов: 3/)).toBeInTheDocument();
    expect(screen.getByText(/Статус: закрыт\. Ответов: 7/)).toBeInTheDocument();
  });

  it('closes and reopens a list from the list actions', async () => {
    const user = userEvent.setup();
    await openProfile();
    await screen.findByText('Открытый опрос');

    await user.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(await screen.findByText('Опрос закрыт.')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Открыть' }));
    expect(await screen.findByText('Опрос снова открыт.')).toBeInTheDocument();
  });

  it('deletes after confirmation', async () => {
    const user = userEvent.setup();
    await openProfile();
    await screen.findByText('Открытый опрос');

    const remove = screen.getAllByRole('button', { name: 'Удалить' })[0];
    if (!remove) throw new Error('no delete action');
    await user.click(remove);
    const dialog = screen.getByRole('alertdialog', { name: 'Удалить опрос?' });
    await user.click(within(dialog).getByRole('button', { name: 'Удалить' }));

    expect(await screen.findByText('Опрос удалён.')).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText('Открытый опрос')).not.toBeInTheDocument());
  });

  it('changes the password and reports success', async () => {
    const user = userEvent.setup();
    await openProfile();

    await user.type(screen.getByLabelText('Текущий пароль'), SEED_PASSWORD);
    await user.type(screen.getByLabelText('Новый пароль'), 'another-long-password');
    await user.type(screen.getByLabelText('Повторите новый пароль'), 'another-long-password');
    await user.click(screen.getByRole('button', { name: 'Сменить пароль' }));

    expect(
      await screen.findByText('Пароль изменён. Остальные сессии закрыты.'),
    ).toBeInTheDocument();
  });

  it('validates a weak or mismatched new password before sending', async () => {
    const user = userEvent.setup();
    await openProfile();

    await user.type(screen.getByLabelText('Текущий пароль'), SEED_PASSWORD);
    await user.type(screen.getByLabelText('Новый пароль'), 'short');
    await user.type(screen.getByLabelText('Повторите новый пароль'), 'different');
    await user.click(screen.getByRole('button', { name: 'Сменить пароль' }));

    expect(screen.getByText('Длина пароля от 10 до 128 символов')).toBeInTheDocument();
    expect(screen.getByText('Пароли не совпадают')).toBeInTheDocument();
  });
});
