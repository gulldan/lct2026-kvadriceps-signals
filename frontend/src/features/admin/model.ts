import { useCallback, useEffect, useMemo, useState } from "react";

import { adminGetAppSettings, adminPutAppSettings, type AppSettingView } from "./api";

export function parseBool(v: string): boolean {
  switch (v.trim().toLowerCase()) {
    case "1":
    case "t":
    case "true":
    case "yes":
    case "on":
      return true;
    default:
      return false;
  }
}

export interface AppSettingsState {
  loaded: boolean;
  failed: boolean;
  busy: boolean;
  dirty: boolean;
  /** Эффективное значение: черновик → оверрайд БД → env → дефолт. */
  effective: (key: string) => string;
  item: (key: string) => AppSettingView | undefined;
  setValue: (key: string, value: string) => void;
  save: () => Promise<boolean>;
  discard: () => void;
  reload: () => void;
  resetAll: () => Promise<boolean>;
}

export function useAppSettings(): AppSettingsState {
  const [revision, setRevision] = useState(0);
  const [items, setItems] = useState<AppSettingView[]>([]);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  const [loadVersion, setLoadVersion] = useState(0);
  const reload = useCallback(() => setLoadVersion((version) => version + 1), []);

  useEffect(() => {
    let cancelled = false;
    setLoaded(false);
    setFailed(false);
    adminGetAppSettings()
      .then((res) => {
        if (cancelled) return;
        setItems(res.settings);
        setRevision(res.revision);
        setLoaded(true);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [loadVersion]);

  const byKey = useMemo(() => new Map(items.map((s) => [s.key, s])), [items]);

  const effective = useCallback(
    (key: string): string => {
      const pending = draft[key];
      if (pending !== undefined) return pending;
      const s = byKey.get(key);
      if (!s) return "";
      if (s.hasOverride) return s.override;
      if (s.envSet) return s.envValue;
      return s.default;
    },
    [draft, byKey],
  );

  const item = useCallback((key: string) => byKey.get(key), [byKey]);

  const setValue = useCallback(
    (key: string, value: string) => {
      if (byKey.get(key)?.envOnly !== false) return;
      setDraft((prev) => ({ ...prev, [key]: value }));
    },
    [byKey],
  );

  const push = useCallback(
    async (values: Record<string, string | null>): Promise<boolean> => {
      setBusy(true);
      try {
        const res = await adminPutAppSettings(values, revision);
        setItems(res.settings);
        setRevision(res.revision);
        setDraft({});
        return true;
      } catch {
        return false;
      } finally {
        setBusy(false);
      }
    },
    [revision],
  );

  const save = useCallback(async (): Promise<boolean> => {
    const values: Record<string, string | null> = {};
    for (const [k, v] of Object.entries(draft)) values[k] = v === "" ? null : v;
    if (Object.keys(values).length === 0) return true;
    return push(values);
  }, [draft, push]);

  const resetAll = useCallback(async (): Promise<boolean> => {
    const values: Record<string, string | null> = {};
    for (const s of items) if (!s.envOnly && s.hasOverride) values[s.key] = null;
    setDraft({});
    if (Object.keys(values).length === 0) return true;
    return push(values);
  }, [items, push]);

  const discard = useCallback(() => setDraft({}), []);

  return {
    loaded,
    failed,
    busy,
    dirty: Object.keys(draft).length > 0,
    effective,
    item,
    setValue,
    save,
    discard,
    reload,
    resetAll,
  };
}
