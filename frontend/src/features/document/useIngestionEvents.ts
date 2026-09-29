import { useEffect, useRef, useState } from "react";
import { wsURL } from "@/shared/api/client";
import { useAuth } from "@/features/auth";
import type { IngestionEvent } from "./api";

/** Переподключение берёт свежий токен, иначе после истечения сессии сокет бился бы в 401. */
export function useIngestionEvents(onEvent: (ev: IngestionEvent) => void): { live: boolean } {
  const { auth } = useAuth();
  const token = auth?.access_token;
  const [live, setLive] = useState(false);
  const handler = useRef(onEvent);
  handler.current = onEvent;

  useEffect(() => {
    if (!token) return;
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    let stopped = false;

    const retry = () => {
      attempt += 1;
      const delay = Math.min(30_000, 1000 * 2 ** Math.min(attempt - 1, 5));
      timer = setTimeout(() => void connect(), delay);
    };
    const connect = async () => {
      if (stopped) return;
      if (document.hidden) {
        document.addEventListener("visibilitychange", () => void connect(), { once: true });
        return;
      }
      let url: string;
      try {
        url = await wsURL();
      } catch {
        if (!stopped) retry();
        return;
      }
      if (stopped) return;
      const socket = new WebSocket(url);
      ws = socket;
      socket.addEventListener("open", () => {
        attempt = 0;
        setLive(true);
      });
      socket.addEventListener("message", (e: MessageEvent) => {
        let ev: IngestionEvent;
        try {
          ev = JSON.parse(String(e.data)) as IngestionEvent;
        } catch {
          return;
        }
        if (typeof ev?.document_id !== "string" || typeof ev?.status !== "string") return;
        handler.current(ev);
      });
      socket.addEventListener("close", () => {
        setLive(false);
        if (stopped) return;
        retry();
      });
      socket.addEventListener("error", () => {
        socket.close();
      });
    };
    void connect();

    return () => {
      stopped = true;
      clearTimeout(timer);
      ws?.close();
      setLive(false);
    };
  }, [token]);

  return { live };
}
