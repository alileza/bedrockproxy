import { useEffect, useRef, useState } from "react";

/**
 * Returns a key that changes every time `value` changes (after initial render).
 * Use this as a React key or dependency to trigger CSS animations on update.
 */
export function useHighlightKey(value: unknown): number {
  const [key, setKey] = useState(0);
  const prev = useRef(value);
  const isFirst = useRef(true);

  useEffect(() => {
    if (isFirst.current) {
      isFirst.current = false;
      prev.current = value;
      return;
    }
    if (prev.current !== value) {
      prev.current = value;
      setKey((k) => k + 1);
    }
  }, [value]);

  return key;
}
