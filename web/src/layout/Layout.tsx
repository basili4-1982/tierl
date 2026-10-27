import { useEffect, useRef, useState } from 'react';
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom';
import { useCurrentUser, useLogout } from '../features/auth/session';
import './layout.css';

const CREATE_PATH = '/tierlists/new';
const MAIN_ID = 'main-content';

function navClass({ isActive }: { isActive: boolean }) {
  return isActive ? 'nav-link is-active' : 'nav-link';
}

function AccountNav() {
  const { data: user, isPending } = useCurrentUser();
  const logout = useLogout();

  if (isPending) return null;
  if (!user) {
    return (
      <>
        <NavLink to="/login" className={navClass}>
          Вход
        </NavLink>
        <NavLink to="/register" className="nav-link nav-link--accent">
          Регистрация
        </NavLink>
      </>
    );
  }
  return (
    <>
      <NavLink to="/profile" className={navClass}>
        Профиль
      </NavLink>
      <span className="nav-user">{user.display_name}</span>
      <button
        type="button"
        className="nav-link nav-button"
        disabled={logout.isPending}
        onClick={() => logout.mutate()}
      >
        Выйти
      </button>
    </>
  );
}

function CreateLink() {
  const { data: user, isPending } = useCurrentUser();
  // Guests go through sign-in and come back to the editor (see LoginPage `from`).
  if (!isPending && !user) {
    return (
      <Link to="/login" state={{ from: CREATE_PATH }} className="nav-link">
        Создать опрос
      </Link>
    );
  }
  return (
    <NavLink to={CREATE_PATH} className={navClass}>
      Создать опрос
    </NavLink>
  );
}

/** Shared shell: skip link, header with navigation, content outlet, footer. */
export function Layout() {
  // The menu is open only for the path it was opened on, so navigating closes it.
  const location = useLocation();
  const [openPath, setOpenPath] = useState<string | null>(null);
  const open = openPath === location.pathname;
  const setOpen = (value: boolean) => setOpenPath(value ? location.pathname : null);
  const toggleRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpenPath(null);
        toggleRef.current?.focus();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open]);

  return (
    <div className="layout">
      <a className="skip-link" href={`#${MAIN_ID}`}>
        К основному содержимому
      </a>
      <header className="site-header">
        <div className="site-header__inner">
          <Link to="/" className="brand">
            tierl
          </Link>
          <button
            ref={toggleRef}
            type="button"
            className="menu-toggle"
            aria-expanded={open}
            aria-controls="site-nav"
            onClick={() => setOpen(!open)}
          >
            {open ? 'Закрыть меню' : 'Меню'}
          </button>
          <nav id="site-nav" aria-label="Основная" className="site-nav" data-open={open}>
            <NavLink to="/catalog" className={navClass}>
              Каталог
            </NavLink>
            <CreateLink />
            <AccountNav />
          </nav>
        </div>
      </header>
      <div id={MAIN_ID} tabIndex={-1} className="content">
        <Outlet />
      </div>
      <footer className="site-footer">
        <p>tierl — опросы-тирлисты</p>
      </footer>
    </div>
  );
}
