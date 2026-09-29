import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import {
  loadAuth,
  restoreAuth,
  endSession,
  saveAuth,
  setAuthChangedHandler,
  setUnauthorizedHandler,
  type TokenResponse,
} from "@/shared/api/client";
import { login as apiLogin } from "@/features/auth/api";
import { Spinner } from "@/shared/ui/Spinner";
import type { Role } from "@/shared/types";

const KNOWN_ROLES: Role[] = ["user", "operator", "admin"];

interface AuthContextValue {
  auth: TokenResponse | null;
  roles: Role[];
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [auth, setAuth] = useState<TokenResponse | null>(() => loadAuth());

  const [ready, setReady] = useState(false);
  const clearSession = useCallback(() => {
    saveAuth(null);
    setAuth(null);
  }, []);
  const logout = useCallback(async () => {
    await endSession();
    clearSession();
    const channel = new BroadcastChannel("rag-auth");
    // Канал доступен только своему origin; второй аргумент существует лишь у window.postMessage.
    // oxlint-disable-next-line unicorn/require-post-message-target-origin
    channel.postMessage("logout");
    channel.close();
  }, [clearSession]);

  useEffect(() => {
    setUnauthorizedHandler(clearSession);
  }, [clearSession]);

  // Между вкладками передаётся только событие выхода, без токенов.
  useEffect(() => {
    let active = true;
    setAuthChangedHandler(setAuth);
    const channel = new BroadcastChannel("rag-auth");
    channel.addEventListener("message", (event: MessageEvent<unknown>) => {
      if (event.data === "logout") clearSession();
    });
    void restoreAuth().then((restored) => {
      if (active) {
        setAuth(restored);
        setReady(true);
      }
    });
    return () => {
      active = false;
      setAuthChangedHandler(null);
      channel.close();
    };
  }, [clearSession]);

  const login = useCallback(async (username: string, password: string) => {
    const t = await apiLogin(username, password);
    saveAuth(t);
    setAuth(t);
  }, []);

  const roles = useMemo<Role[]>(() => {
    const have = new Set(auth?.user.roles ?? []);
    return KNOWN_ROLES.filter((r) => have.has(r));
  }, [auth]);

  const value = useMemo(() => ({ auth, roles, login, logout }), [auth, roles, login, logout]);
  return (
    <AuthContext.Provider value={value}>{ready ? children : <Spinner />}</AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
