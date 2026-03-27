import { useEffect, useRef, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";

let connected = false;
const listeners = new Set<() => void>();

function subscribe(cb: () => void) {
  listeners.add(cb);
  return () => listeners.delete(cb);
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

export function useSSEStatus() {
  return useSyncExternalStore(subscribe, getSnapshot);
}

export function useSSE() {
  const queryClient = useQueryClient();
  const retryDelay = useRef(1000);

  useEffect(() => {
    let es: EventSource | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;

    function connect() {
      es = new EventSource("/api/events");

      es.onopen = () => {
        retryDelay.current = 1000;
        setConnected(true);
      };

      es.onmessage = () => {
        queryClient.invalidateQueries();
      };

      es.onerror = () => {
        setConnected(false);
        es?.close();
        timer = setTimeout(() => {
          retryDelay.current = Math.min(retryDelay.current * 2, 30000);
          connect();
        }, retryDelay.current);
      };
    }

    connect();

    return () => {
      es?.close();
      setConnected(false);
      if (timer) clearTimeout(timer);
    };
  }, [queryClient]);
}
