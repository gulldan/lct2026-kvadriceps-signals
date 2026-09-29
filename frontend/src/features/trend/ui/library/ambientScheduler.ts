export type AmbientKind =
  | "wind"
  | "mouse"
  | "moth"
  | "drawer"
  | "scroll"
  | "feather"
  | "dust"
  | "tea"
  | "sort"
  | "letter"
  | "cart"
  | "lamp"
  | "reader"
  | "pneumatic"
  | "cat"
  | "chime"
  | "domino"
  | "owl"
  | "bookmark"
  | "planetarium"
  | "cuckoo"
  | "globe"
  | "origami"
  | "beetle"
  | "rose"
  | "penguin"
  | "newton"
  | "portrait";
export type AmbientRandom = () => number;
type AmbientEvent = {
  serial: number;
  start: number;
  duration: number;
  variant: number;
  strength: number;
};

const EPISODES: Record<
  AmbientKind,
  {
    duration: readonly [number, number];
    rest: readonly [number, number];
    first: number;
    resource?: "visitor" | "clerk" | "binder" | "clock" | "exhibit";
    manual?: boolean;
  }
> = {
  wind: { duration: [15, 22], rest: [19, 44], first: 0 },
  mouse: { duration: [90, 150], rest: [28, 56], first: 5, resource: "visitor", manual: true },
  moth: { duration: [15, 26], rest: [15, 34], first: 2, resource: "visitor", manual: true },
  drawer: { duration: [5, 8], rest: [24, 57], first: 4 },
  scroll: { duration: [7, 11], rest: [26, 48], first: 3 },
  feather: { duration: [8, 14], rest: [22, 51], first: 1 },
  dust: { duration: [6, 10], rest: [17, 39], first: 1 },
  tea: { duration: [10, 18], rest: [23, 46], first: 0 },
  sort: { duration: [8, 13], rest: [18, 35], first: 0, resource: "clerk", manual: true },
  letter: { duration: [12, 20], rest: [20, 39], first: 3, resource: "clerk", manual: true },
  cart: { duration: [12, 18], rest: [26, 46], first: 1, resource: "binder", manual: true },
  lamp: { duration: [7, 12], rest: [24, 43], first: 5, resource: "binder", manual: true },
  reader: { duration: [15, 25], rest: [16, 37], first: 1 },
  pneumatic: { duration: [12, 18], rest: [23, 41], first: 4, resource: "clerk", manual: true },
  cat: { duration: [9, 16], rest: [19, 36], first: 2 },
  chime: { duration: [4, 7], rest: [31, 62], first: 7, resource: "clock" },
  domino: { duration: [12, 17], rest: [58, 103], first: 16 },
  owl: { duration: [15, 23], rest: [68, 124], first: 24 },
  bookmark: { duration: [15, 21], rest: [71, 131], first: 31 },
  planetarium: { duration: [14, 20], rest: [58, 105], first: 11 },
  cuckoo: { duration: [9, 14], rest: [88, 151], first: 43, resource: "clock" },
  globe: { duration: [12, 19], rest: [51, 96], first: 21 },
  origami: { duration: [22, 31], rest: [79, 143], first: 34 },
  beetle: { duration: [18, 26], rest: [83, 147], first: 48 },
  rose: { duration: [14, 20], rest: [37, 67], first: 8, resource: "exhibit" },
  penguin: { duration: [19, 25], rest: [42, 79], first: 3, resource: "exhibit" },
  newton: { duration: [12, 19], rest: [31, 56], first: 0, resource: "exhibit" },
  portrait: { duration: [10, 16], rest: [55, 95], first: 13, resource: "exhibit" },
};
const KINDS = Object.keys(EPISODES) as AmbientKind[];

/** Случайность выбирается при смене эпизода; кадры только исполняют выбранную сценку. */
export function ambientScheduler(random: AmbientRandom) {
  const events = {} as Record<AmbientKind, AmbientEvent | undefined>;
  const available = {} as Record<AmbientKind, number>;
  const lastVariant = {} as Record<AmbientKind, number>;
  let elapsed = 0;
  let next = 0.6 + random() * 1.2;
  let serial = 0;
  let last: AmbientKind | undefined;
  for (const kind of KINDS) {
    available[kind] = EPISODES[kind].first + random() * 4;
    lastVariant[kind] = -1;
  }
  available.wind = 0;

  const range = (pair: readonly [number, number]) => pair[0] + random() * (pair[1] - pair[0]);
  function finish(kind: AmbientKind) {
    if (!events[kind]) return;
    events[kind] = undefined;
    available[kind] = elapsed + range(EPISODES[kind].rest);
  }
  function advance(dt: number) {
    elapsed += dt;
    for (const kind of KINDS) {
      const event = events[kind];
      if (event && !EPISODES[kind].manual && elapsed >= event.start + event.duration) finish(kind);
    }
    if (elapsed < next) return;
    const running = KINDS.filter((kind) => events[kind]);
    const choices =
      running.length >= 3
        ? []
        : KINDS.filter((kind) => {
            const resource = EPISODES[kind].resource;
            return (
              !events[kind] &&
              kind !== last &&
              available[kind] <= elapsed &&
              (!resource ||
                !running.some((activeKind) => EPISODES[activeKind].resource === resource))
            );
          });
    if (!choices.length) {
      next = elapsed + 0.8;
      return;
    }
    const kind = choices[Math.floor(random() * choices.length)]!;
    let variant = Math.floor(random() * 4);
    if (variant === lastVariant[kind]) variant = (variant + 1 + Math.floor(random() * 3)) % 4;
    events[kind] = {
      serial: ++serial,
      start: elapsed,
      duration: range(EPISODES[kind].duration),
      variant,
      strength: 0.68 + random() * 0.5,
    };
    lastVariant[kind] = variant;
    last = kind;
    next = elapsed + 3.8 + random() * 5.1;
  }
  function wind(x: number, z: number) {
    const event = events.wind;
    if (!event) return 0;
    const sourceZ = event.variant % 2 ? -0.5 : -6.8;
    const delay = Math.hypot(x + 22.65, z - sourceZ) * (0.13 + event.variant * 0.017);
    const age = elapsed - event.start - delay;
    const span = event.duration - 8;
    if (age <= 0 || age >= span) return 0;
    const envelope = Math.sin((age / span) * Math.PI) ** 2;
    const ripple =
      event.variant < 2
        ? 0.86 + Math.sin(age * 1.6 + event.variant) * 0.14
        : 0.74 + Math.sin(age * 2.7) * 0.26;
    return envelope * ripple * event.strength;
  }
  return { advance, finish, wind, event: (kind: AmbientKind) => events[kind], time: () => elapsed };
}
export type AmbientScheduler = ReturnType<typeof ambientScheduler>;
