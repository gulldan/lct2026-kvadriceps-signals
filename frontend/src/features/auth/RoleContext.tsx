import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import { i18n } from "@/shared/i18n";
import type { Role } from "@/shared/types";
import { useAuth } from "./AuthContext";

// Геттеры, а не строки: подпись переводится при смене языка.
export const ROLE_META: Record<
  Role,
  {
    label: string;
    home: string;
  }
> = {
  user: {
    get label() {
      return i18n.t("auth:roles.user");
    },
    home: "/",
  },
  operator: {
    get label() {
      return i18n.t("auth:roles.operator");
    },
    home: "/operator/documents",
  },
  admin: {
    get label() {
      return i18n.t("auth:roles.admin");
    },
    home: "/admin",
  },
};

const DEFAULT_ORDER: Role[] = ["admin", "operator", "user"];

interface RoleContextValue {
  role: Role;
  available: Role[];
  setRole: (role: Role) => void;
}

const RoleContext = createContext<RoleContextValue | null>(null);

function storageKey(userID: string): string {
  return `rag.viewRole.${userID}`;
}

function pickRole(available: Role[], userID?: string): Role {
  if (userID) {
    const remembered = localStorage.getItem(storageKey(userID)) as Role | null;
    if (remembered && available.includes(remembered)) return remembered;
  }
  return DEFAULT_ORDER.find((r) => available.includes(r)) ?? "user";
}

export function RoleProvider({ children }: { children: ReactNode }) {
  const { auth, roles } = useAuth();
  const available = useMemo<Role[]>(() => (roles.length > 0 ? roles : ["user"]), [roles]);
  const userID = auth?.user.id;
  const [role, setRoleState] = useState<Role>(() => pickRole(available, userID));

  useEffect(() => {
    setRoleState(pickRole(available, userID));
  }, [available, userID]);

  const value = useMemo(
    () => ({
      role,
      available,
      setRole: (next: Role) => {
        if (!available.includes(next)) return;
        setRoleState(next);
        if (userID) localStorage.setItem(storageKey(userID), next);
      },
    }),
    [role, available, userID],
  );
  return <RoleContext.Provider value={value}>{children}</RoleContext.Provider>;
}

export function useRole(): RoleContextValue {
  const ctx = useContext(RoleContext);
  if (!ctx) throw new Error("useRole must be used within RoleProvider");
  return ctx;
}
