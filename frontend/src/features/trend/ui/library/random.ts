/** Один генератор на сцену: поведение разное между поисками и воспроизводимо при проверке. */
export function libraryRandom(seed = Math.floor(Math.random() * 0x100000000)) {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let value = Math.imul(state ^ (state >>> 15), 1 | state);
    value ^= value + Math.imul(value ^ (value >>> 7), 61 | value);
    return ((value ^ (value >>> 14)) >>> 0) / 0x100000000;
  };
}
