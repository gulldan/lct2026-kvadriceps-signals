import { type ComponentType, type ReactNode, useEffect, useState } from "react";

/** Не `React.lazy`: тот показывает заслон и для скачанного чанка, а потом ещё придерживает
 *  содержимое. */
export function loadable<P extends object>(
  load: () => Promise<ComponentType<P>>,
  fallback: ReactNode = null,
) {
  let ready: ComponentType<P> | undefined;
  const warm = () =>
    load().then((component) => {
      ready ??= component;
    });
  function Loadable(props: P) {
    const [, setLoaded] = useState(!!ready);
    useEffect(() => {
      if (ready) return;
      let alive = true;
      void warm().then(() => {
        if (alive) setLoaded(true);
      });
      return () => {
        alive = false;
      };
    }, []);
    const Ready = ready;
    return Ready ? <Ready {...props} /> : fallback;
  }
  return Object.assign(Loadable, { warm });
}
