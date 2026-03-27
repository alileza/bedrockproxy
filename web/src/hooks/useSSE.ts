import { useEffect, useRef, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";

let connected = false;
const listeners = new Set<() => void>();

function subscribe(cb: () => void) {
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
  };
}

function getSnapshot() {
  return connected;
}

function setConnected(value: boolean) {
  if (connected !== value) {
    connected = value;
    listeners.forEach((cb) => cb());
  }
}

export function useWSStatus() {
  return useSyncExternalStore(subscribe, getSnapshot);
}

export function useWS() {
  const queryClient = useQueryClient();
  const retryDelay = useRef(1000);

  useEffect(() => {
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;

    function connect() {
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${proto}//${location.host}/api/ws`);

      ws.onopen = () => {
        retryDelay.current = 1000;
        setConnected(true);
      };

      ws.onmessage = () => {
        queryClient.invalidateQueries();
      };

      ws.onclose = () => {
        setConnected(false);
        timer = setTimeout(() => {
          retryDelay.current = Math.min(retryDelay.current * 2, 30000);
          connect();
        }, retryDelay.current);
      };

      ws.onerror = () => {
        ws?.close();
      };
    }

    connect();

    return () => {
      ws?.close();
      setConnected(false);
      if (timer) clearTimeout(timer);
    };
  }, [queryClient]);
}
