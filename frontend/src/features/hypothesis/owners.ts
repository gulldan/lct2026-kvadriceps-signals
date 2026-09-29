import { useEffect, useMemo, useState } from "react";

import { adminListUsers, type ApiAccount } from "@/features/admin";
import { useAuth } from "@/features/auth";

// Один список на все экраны гипотез: иначе карточка заново ждала запроса.
let accounts: Promise<ApiAccount[]> | null = null;

function loadAccounts() {
  accounts ??= adminListUsers().catch(() => {
    accounts = null;
    return [];
  });
  return accounts;
}

export function useOwnerNames(): (ownerId: string | undefined) => string | null {
  const { roles } = useAuth();
  const isAdmin = roles.includes("admin");
  const [users, setUsers] = useState<ApiAccount[] | null>(null);

  // Имя владельца первому экрану не нужно, а запрос отнимал у него соединение к хосту.
  useEffect(() => {
    if (!isAdmin) return;
    let cancelled = false;
    const load = () =>
      void loadAccounts().then((list) => {
        if (!cancelled) setUsers(list);
      });
    if (accounts) {
      load();
      return () => {
        cancelled = true;
      };
    }
    const request = window.requestIdleCallback ?? ((fn: () => void) => window.setTimeout(fn, 1500));
    const cancel = window.cancelIdleCallback ?? window.clearTimeout;
    const handle = request(load, { timeout: 4000 });
    return () => {
      cancelled = true;
      cancel(handle);
    };
  }, [isAdmin]);

  const byId = useMemo(() => new Map(users?.map((u) => [u.id, u.username])), [users]);

  // Обрывок идентификатора выглядит как окончательное имя, поэтому до ответа автора нет.
  return useMemo(
    () => (ownerId) => {
      if (!isAdmin || !ownerId || !users) return null;
      return byId.get(ownerId) ?? ownerId.slice(0, 8);
    },
    [byId, isAdmin, users],
  );
}
