import { Route } from 'react-router-dom';
import { ForgotPasswordPage } from './ForgotPasswordPage';
import { LoginPage } from './LoginPage';
import { RegisterPage } from './RegisterPage';
import { ResetPasswordPage } from './ResetPasswordPage';
import { VerifyEmailPage } from './VerifyEmailPage';

/** Auth routes, to be spread inside <Routes>. */
export function authRoutes() {
  return [
    <Route key="register" path="/register" element={<RegisterPage />} />,
    <Route key="login" path="/login" element={<LoginPage />} />,
    <Route key="verify" path="/verify-email" element={<VerifyEmailPage />} />,
    <Route key="forgot" path="/forgot-password" element={<ForgotPasswordPage />} />,
    <Route key="reset" path="/reset-password" element={<ResetPasswordPage />} />,
  ];
}
