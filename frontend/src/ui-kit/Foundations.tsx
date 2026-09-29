import { useEffect, useState } from "react";
import { Check, Copy } from "lucide-react";

import { printColors, readColorToken } from "@/shared/design-system/colors";
import { cn } from "@/shared/lib/cn";
import { copyText } from "@/shared/lib/clipboard";
import { toast } from "@/shared/ui/Sonner";
import { i18n } from "@/shared/i18n";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Kicker } from "@/shared/ui/Kicker";
import { Specimen } from "./Specimen";

const anchors = [
  { name: "palette-canvas", label: "Холст", purpose: "Страница и карточка" },
  { name: "palette-graphite", label: "Графит", purpose: "Текст и структура" },
  { name: "palette-blue", label: "Синий", purpose: "Основной: действия и выбор" },
  { name: "palette-lavender", label: "Лавандовый", purpose: "Второй акцент: новые сигналы" },
  { name: "palette-orange", label: "Оранжевый", purpose: "Акцент: внимание и проверка" },
];

const colorGroups = {
  "Поверхности и текст": [
    "background",
    "foreground",
    "card",
    "card-foreground",
    "popover",
    "popover-foreground",
    "secondary",
    "secondary-foreground",
    "muted",
    "muted-foreground",
    "border",
    "input",
  ],
  "Действия и состояния": [
    "primary",
    "primary-foreground",
    "accent",
    "accent-foreground",
    "brand",
    "brand-foreground",
    "brand-wash",
    "brand-border",
    "ring",
    "complement",
    "complement-foreground",
    "complement-wash",
    "destructive",
    "destructive-foreground",
    "ok",
    "ok-wash",
    "warn",
    "warn-wash",
    "risk",
    "risk-wash",
    "mixed",
  ],
  "Навигация и слои": [
    "sidebar",
    "sidebar-foreground",
    "sidebar-primary",
    "sidebar-primary-foreground",
    "sidebar-accent",
    "sidebar-accent-foreground",
    "sidebar-border",
    "sidebar-ring",
    "overlay",
  ],
  "Документы и интеграции": [
    "document-paper",
    "marker",
    "highlight-flash",
    "highlight-outline",
    "highlight-pdf",
    "highlight-pdf-active",
    "integration-word",
    "integration-jira",
  ],
  Графики: [
    "chart-1",
    "chart-2",
    "chart-3",
    "chart-4",
    "chart-5",
    ...Array.from({ length: 5 }, (_, i) => `category-${i + 1}`),
    "category-unassigned",
  ],
};

/** С экрана, а не из исходника: подпись в тёмной теме обязана совпадать с тем, что видно. */
function useTokenValue(name: string) {
  const [value, setValue] = useState("");
  useEffect(() => {
    const read = () => setValue(readColorToken(name, document.documentElement));
    read();
    // Атрибут темы меняют и приложение, и инструменты приёмки, а не только переключатель.
    const observer = new MutationObserver(read);
    observer.observe(document.documentElement, { attributeFilter: ["data-theme"] });
    const media = matchMedia("(prefers-color-scheme: dark)");
    media.addEventListener("change", read);
    return () => {
      observer.disconnect();
      media.removeEventListener("change", read);
    };
  }, [name]);
  return value;
}

function Swatch({ name, label, purpose }: { name: string; label?: string; purpose?: string }) {
  const [copied, setCopied] = useState(false);
  const value = useTokenValue(name);
  return (
    <div
      className={cn(
        "grid min-w-0 gap-x-1 gap-y-2",
        label
          ? "grid-cols-[4.5rem_minmax(0,1fr)_2rem] gap-x-4 gap-y-1 sm:row-span-4 sm:grid-cols-[minmax(0,1fr)_2rem] sm:grid-rows-subgrid sm:gap-x-1 sm:gap-y-2"
          : "row-span-3 grid-cols-[minmax(0,1fr)_2rem] grid-rows-subgrid",
      )}
      data-palette-anchor={label ? name : undefined}
    >
      <div
        className={
          label
            ? "col-start-1 row-span-3 row-start-1 rounded-lg border sm:col-span-2 sm:row-span-1 sm:h-28"
            : "col-span-2 h-14 rounded-md border"
        }
        style={{ backgroundColor: `var(--${name})` }}
      />
      {label ? (
        <span className="col-start-2 row-start-1 self-center text-sm font-medium sm:col-start-1 sm:row-start-2">
          {label}
        </span>
      ) : (
        <code className="row-start-2 self-center truncate text-caption" title={`--${name}`}>
          {name}
        </code>
      )}
      <Button
        variant="ghost"
        size="icon-sm"
        className={
          label
            ? "col-start-3 row-start-1 sm:col-start-2 sm:row-start-2"
            : "col-start-2 row-start-2"
        }
        aria-label={`Копировать --${name}`}
        onClick={async () => {
          const success = await copyText(`var(--${name})`);
          setCopied(success);
          if (!success) toast.error(i18n.t("ui.copyFailed"));
        }}
      >
        {copied ? (
          <Check className="size-3.5" aria-hidden />
        ) : (
          <Copy className="size-3.5" aria-hidden />
        )}
      </Button>
      {purpose && (
        <p className="col-start-2 row-start-2 text-xs leading-relaxed text-muted-foreground sm:col-span-2 sm:col-start-1 sm:row-start-3">
          {purpose}
        </p>
      )}
      <p
        className={cn(
          "font-mono text-caption break-words text-muted-foreground",
          label
            ? "col-start-2 row-start-3 sm:col-span-2 sm:col-start-1 sm:row-start-4"
            : "col-span-2 row-start-3",
        )}
      >
        {value}
      </p>
    </div>
  );
}

export function Foundations() {
  return (
    <>
      <Specimen
        id="tokens"
        title="Палитра интерфейса"
        description="Палитра дизайн-системы Газпромбанка: синий ведёт действия и выбор, лавандовый отмечает появляющийся сигнал, оранжевый — внимание и проверку. Фон и текст остаются нейтральными."
        code={
          '// src/shared/design-system/tokens.css\n<Button>Создать гипотезу</Button>\n<Badge variant="ok">Подтверждено</Badge>\n// SVG: fill="var(--brand)"; Canvas: readColorToken("brand", canvas)'
        }
      >
        <div className="grid gap-x-4 gap-y-5 sm:grid-cols-5 sm:gap-y-2">
          {anchors.map((anchor) => (
            <Swatch key={anchor.name} {...anchor} />
          ))}
        </div>
        <p className="max-w-prose text-sm leading-relaxed text-muted-foreground">
          Основной цвет ведёт к действию, комплементарный выделяет появляющиеся сигналы, оранжевый
          просит внимания. Успех отмечен зелёным, ошибка — красным: эти состояния всегда подписаны.
          Цвета данных постоянны при фильтрации; величину объясняют оси и легенда.
        </p>
        <div
          className="flex flex-wrap items-center gap-3 border-y py-5"
          aria-label="Цветовые роли в интерфейсе"
        >
          <Button onClick={() => toast.success("Пример основного действия")}>
            Основное действие
          </Button>
          <Badge variant="complement">Новый сигнал</Badge>
          <Badge variant="warn">Нужна проверка</Badge>
          <Badge variant="ok">Подтверждено</Badge>
          <Badge variant="risk">Ошибка</Badge>
        </div>
        <p className="text-sm font-medium">
          Цвета данных · сплошные заливки, пять различимых оттенков
        </p>
        <div className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-5">
          {[
            "palette-chart-blue",
            "palette-chart-green",
            "palette-chart-orange",
            "palette-chart-violet",
            "palette-chart-rose",
          ].map((name) => (
            <Swatch key={name} name={name} />
          ))}
        </div>
        <details className="border-t pt-4">
          <summary className="cursor-pointer text-sm font-medium">
            Семантические токены и печатная палитра
          </summary>
          <div className="mt-6 space-y-8">
            {Object.entries(colorGroups).map(([title, names]) => (
              <div key={title}>
                <h3 className="mb-3 text-sm font-medium">{title}</h3>
                <div className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3 xl:grid-cols-4">
                  {names.map((name) => (
                    <Swatch key={name} name={name} />
                  ))}
                </div>
              </div>
            ))}
            <div>
              <h3 className="text-sm font-medium">Печатная палитра Word</h3>
              <p className="mt-1 text-xs text-muted-foreground">
                Фиксированные RGB для экспорта, независимо от темы экрана.
              </p>
              <div className="mt-3 flex flex-wrap gap-4">
                {Object.entries(printColors).map(([name, color]) => (
                  <div key={name}>
                    <div
                      className="h-8 w-20 rounded-md border"
                      style={{ backgroundColor: `#${color}` }}
                    />
                    <code className="text-micro">{name}</code>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </details>
      </Specimen>
      <Specimen
        id="typography"
        title="Типографика и ритм"
        description="Одна гарнитура на заголовки и текст, как в дизайн-системе Газпромбанка: на их машинах подхватится Gazprombank Sans, снаружи — Onest. Моноширинный шрифт используется для кода и измерений."
        code={
          '<PageHeader title="Гипотезы" />\n<p className="text-sm text-muted-foreground">Описание</p>\n<span className="font-mono tabular-nums">24 / 38</span>'
        }
      >
        <div className="grid gap-4">
          <p className="font-display max-w-prose text-4xl sm:text-5xl">
            От источника
            <br />к решению.
          </p>
          <p className="max-w-prose text-base leading-relaxed">
            Проверяем гипотезы по базе знаний, сохраняем доказательства и планируем следующий
            эксперимент.
          </p>
        </div>
        <div className="grid gap-4 border-y py-6">
          <p className="text-sm text-muted-foreground">
            Аа Бб Вв · Aa Bb Cc · Исследования / Research / 研究
          </p>
          <p className="font-mono text-2xl tabular-nums">24 / 38 · 0.87 · 12.5%</p>
          <Kicker>Метаданные · обновлено сегодня</Kicker>
        </div>
        <div className="grid gap-6 sm:grid-cols-2">
          <div className="grid content-start gap-4">
            <h3 className="text-xs font-medium text-muted-foreground">Шаг отступов · 4 px</h3>
            <div className="grid grid-cols-6 items-end gap-3">
              {[1, 2, 3, 4, 6, 8].map((step) => (
                <div key={step} className="grid gap-3">
                  <div className="flex h-12 items-end">
                    <div
                      className="rounded-sm bg-brand"
                      style={{ width: `${step * 0.25}rem`, height: `${step * 0.25}rem` }}
                    />
                  </div>
                  <code className="text-xs text-muted-foreground">{step * 4}</code>
                </div>
              ))}
            </div>
          </div>
          <div className="grid content-start gap-4">
            <h3 className="text-xs font-medium text-muted-foreground">Радиусы и тени</h3>
            <div className="grid grid-cols-4 gap-3">
              {["sm", "md", "lg", "xl"].map((size) => (
                <div key={size} className="grid gap-3">
                  <div
                    className="h-12 bg-card"
                    style={{
                      borderRadius: `var(--radius-${size})`,
                      boxShadow: `var(--elevation-${size})`,
                    }}
                  />
                  <code className="text-xs text-muted-foreground">{size}</code>
                </div>
              ))}
            </div>
          </div>
        </div>
      </Specimen>
    </>
  );
}
