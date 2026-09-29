import { Radar, Eye, EyeOff, ArrowRight } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useLocation, useNavigate } from "react-router-dom";

import { Button } from "@/shared/ui/Button";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Input } from "@/shared/ui/Input";
import { FieldRow } from "@/shared/ui/Fields";
import { PageTitle } from "@/shared/ui/PageHeader";
import { Notice } from "@/shared/ui/Notice";
import { Spinner } from "@/shared/ui/Spinner";
import { ThemeSwitcher } from "@/shared/ui/ThemeSwitcher";
import { LanguageSwitcher } from "@/shared/ui/LanguageSwitcher";
import { useAuth } from "@/features/auth/AuthContext";
import { ApiError } from "@/shared/api/client";
import { LoginSignal } from "@/features/auth/ui/LoginSignal";

const ACCESS_ROLES = ["admin", "operator", "researcher"] as const;

export function Login() {
  const { t } = useTranslation("auth");
  const { auth, login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation() as { state?: { from?: string } };
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [invalid, setInvalid] = useState(false);
  const [capsLock, setCapsLock] = useState(false);
  const destination = location.state?.from;
  const from = destination?.startsWith("/") && !destination.startsWith("//") ? destination : "/";

  if (auth) {
    return <Navigate to={from} replace />;
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setInvalid(false);
    try {
      await login(username.trim(), password);
      navigate(from, { replace: true });
    } catch (err) {
      setInvalid(err instanceof ApiError && err.status === 401);
      setError(
        err instanceof ApiError && err.status === 401
          ? t("login.invalidCredentials")
          : err instanceof ApiError && err.status === 503
            ? t("login.unavailable")
            : t("login.failed"),
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="min-h-svh bg-background lg:grid lg:grid-cols-[minmax(0,1.08fr)_minmax(0,1fr)]">
      <aside className="relative m-4 mr-0 hidden min-h-[44rem] overflow-hidden rounded-2xl bg-media-ink text-media-ink-foreground lg:flex lg:flex-col">
        <div className="relative z-10 p-12 pb-0 xl:p-14 xl:pb-0">
          <Radar className="mb-10 size-8" strokeWidth={1.5} aria-hidden />
          <PageTitle as="h2" variant="welcome" className="max-w-lg">
            {t("brand", { ns: "app" })}
          </PageTitle>
        </div>
        <div className="relative my-8 min-h-72 flex-1 overflow-hidden">
          <LoginSignal />
        </div>
        <p className="relative z-10 max-w-md p-12 pt-0 text-base leading-relaxed xl:p-14 xl:pt-0">
          {t("login.subtitle")}
        </p>
      </aside>
      <div className="flex min-h-svh flex-col">
        <header className="flex min-h-24 items-center justify-between gap-4 px-6 sm:px-10 lg:justify-end">
          <div className="flex min-w-0 items-center gap-3 lg:hidden">
            <Radar className="size-7 shrink-0 text-brand" strokeWidth={1.5} aria-hidden />
            <p className="max-w-44 text-sm leading-tight font-semibold">
              {t("brand", { ns: "app" })}
            </p>
          </div>
          <div className="flex shrink-0 gap-1">
            <LanguageSwitcher />
            <ThemeSwitcher />
          </div>
        </header>
        <main className="flex flex-1 items-center justify-center px-6 pt-8 pb-24 sm:px-12 lg:pb-32">
          <div className="w-full max-w-sm">
            <PageTitle className="mb-9">{t("login.title")}</PageTitle>
            <form className="space-y-5" onSubmit={(e) => void submit(e)} aria-busy={busy}>
              <FieldRow id="login-username" label={t("login.username")}>
                <Input
                  id="login-username"
                  autoFocus
                  autoComplete="username"
                  autoCapitalize="none"
                  spellCheck={false}
                  disabled={busy}
                  aria-invalid={invalid || undefined}
                  aria-describedby={error ? "login-error" : undefined}
                  value={username}
                  onChange={(e) => {
                    setUsername(e.target.value);
                    setError("");
                    setInvalid(false);
                  }}
                  required
                />
              </FieldRow>
              <FieldRow id="login-password" label={t("login.password")}>
                <div className="relative w-full">
                  <Input
                    id="login-password"
                    type={showPassword ? "text" : "password"}
                    autoComplete="current-password"
                    className="pr-12"
                    disabled={busy}
                    aria-invalid={invalid || undefined}
                    aria-describedby={
                      [error && "login-error", capsLock && "login-caps"]
                        .filter(Boolean)
                        .join(" ") || undefined
                    }
                    value={password}
                    onChange={(e) => {
                      setPassword(e.target.value);
                      setError("");
                      setInvalid(false);
                    }}
                    onKeyDown={(e) => setCapsLock(e.getModifierState("CapsLock"))}
                    onKeyUp={(e) => setCapsLock(e.getModifierState("CapsLock"))}
                    onBlur={() => setCapsLock(false)}
                    required
                  />
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="absolute top-1 right-1"
                    aria-label={showPassword ? t("login.hidePassword") : t("login.showPassword")}
                    aria-controls="login-password"
                    aria-pressed={showPassword}
                    disabled={busy}
                    onClick={() => setShowPassword((shown) => !shown)}
                  >
                    {showPassword ? <EyeOff aria-hidden /> : <Eye aria-hidden />}
                  </Button>
                </div>
              </FieldRow>
              {capsLock && (
                <output id="login-caps" className="block text-sm text-warn">
                  {t("login.capsLock")}
                </output>
              )}
              {error && (
                <Notice id="login-error" tone="risk" role="alert">
                  {error}
                </Notice>
              )}
              <Button type="submit" className="w-full" disabled={busy}>
                {busy ? t("login.signingIn") : t("login.signIn")}
                {busy ? (
                  <Spinner label={t("login.signingIn")} />
                ) : (
                  <ArrowRight className="size-4" aria-hidden />
                )}
              </Button>
            </form>
            <Disclosure title={t("login.accessTitle")} className="mt-8">
              <p className="text-sm leading-relaxed text-muted-foreground">
                {t("login.accessNote")}
              </p>
              <h2 className="mt-5 mb-2 text-sm font-medium">{t("login.rolesTitle")}</h2>
              <ul className="space-y-1 text-sm text-muted-foreground">
                {ACCESS_ROLES.map((key) => (
                  <li key={key}>
                    <span className="font-medium text-foreground">
                      {t(`login.accessRoles.${key}.role`)}
                    </span>{" "}
                    — {t(`login.accessRoles.${key}.detail`)}
                  </li>
                ))}
              </ul>
            </Disclosure>
          </div>
        </main>
      </div>
    </div>
  );
}
