const RINGS = Array.from({ length: 36 }, (_, index) => {
  const radius = 46 + index * 8;
  const start = (-46 + index * 0.55) * (Math.PI / 180);
  const end = (286 + index * 0.55) * (Math.PI / 180);
  return {
    radius,
    path: `M ${360 + radius * Math.cos(start)} ${360 + radius * Math.sin(start)} A ${radius} ${radius} 0 1 1 ${360 + radius * Math.cos(end)} ${360 + radius * Math.sin(end)}`,
  };
});

/** Абстрактный знак сигнала не изображает реальные результаты или ход поиска. */
export function LoginSignal() {
  return (
    <svg
      viewBox="0 0 720 720"
      fill="none"
      className="pointer-events-none absolute top-1/2 left-1/2 w-[min(125%,48rem)] max-w-none -translate-x-1/2 -translate-y-1/2 text-brand"
      aria-hidden="true"
      focusable="false"
    >
      <g stroke="currentColor" strokeWidth="1.2">
        {RINGS.map(({ radius, path }) => (
          <path key={radius} d={path} />
        ))}
      </g>
      <path d="M360 360 563 157" stroke="currentColor" strokeWidth="1.2" />
      <circle cx="360" cy="360" r="5" fill="currentColor" />
      <circle cx="518" cy="202" r="14" className="fill-media-ink" />
      <circle cx="518" cy="202" r="6" className="fill-media-ink-foreground" />
    </svg>
  );
}
