import { useState } from "react";
import { ArrowRight, Plus, Search, SlidersHorizontal } from "lucide-react";

import { Button } from "@/shared/ui/Button";
import { Badge } from "@/shared/ui/Badge";
import { Chip } from "@/shared/ui/Chip";
import { Switch } from "@/shared/ui/CSwitch";
import { FieldRow, ToggleRow } from "@/shared/ui/Fields";
import { FilterBar } from "@/shared/ui/FilterBar";
import { Input } from "@/shared/ui/Input";
import { Label } from "@/shared/ui/Label";
import { SearchField } from "@/shared/ui/SearchField";
import { Segmented } from "@/shared/ui/Segmented";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { Spinner } from "@/shared/ui/Spinner";
import { Tabs, TabPanel } from "@/shared/ui/Tabs";
import { Textarea } from "@/shared/ui/Textarea";
import { Range } from "@/shared/ui/Range";
import { badgeTones } from "@/shared/design-system/tones";
import { SampleGroup, Specimen } from "./Specimen";

export function Controls() {
  const [clicks, setClicks] = useState(0);
  const [query, setQuery] = useState("");
  const [checked, setChecked] = useState(true);
  const [tab, setTab] = useState("evidence");
  const [view, setView] = useState("list");
  const [format, setFormat] = useState("pdf");
  const [selected, setSelected] = useState(false);
  return (
    <>
      <Specimen
        id="buttons"
        title="Button · Chip"
        description="Основное действие, вторичные действия и компактные фильтры. Кнопки по умолчанию не отправляют форму. Для отправки явно задайте type=submit."
        code={
          '<Button onClick={create}>Создать</Button>\n<Button variant="ghost" size="icon" aria-label="Поиск"><Search /></Button>\n<Button disabled aria-busy="true"><Spinner />Сохранение</Button>'
        }
      >
        <SampleGroup label="Варианты действия">
          {(
            ["default", "secondary", "accent", "outline", "ghost", "link", "destructive"] as const
          ).map((variant) => (
            <Button key={variant} variant={variant} onClick={() => setClicks((n) => n + 1)}>
              {variant}
            </Button>
          ))}
        </SampleGroup>
        <div className="grid gap-6 sm:grid-cols-3">
          <SampleGroup label="Малый · sm">
            <Button size="sm" onClick={() => setClicks((n) => n + 1)}>
              Добавить
            </Button>
            <Button
              size="icon-sm"
              variant="outline"
              aria-label="Параметры"
              onClick={() => setClicks((n) => n + 1)}
            >
              <SlidersHorizontal className="size-4" aria-hidden />
            </Button>
          </SampleGroup>
          <SampleGroup label="Стандартный · default">
            <Button onClick={() => setClicks((n) => n + 1)}>
              <Plus className="size-4" aria-hidden />
              Добавить
            </Button>
            <Button
              size="icon"
              variant="outline"
              aria-label="Поиск"
              onClick={() => setClicks((n) => n + 1)}
            >
              <Search className="size-4" aria-hidden />
            </Button>
          </SampleGroup>
          <SampleGroup label="Крупный · lg">
            <Button size="lg" onClick={() => setClicks((n) => n + 1)}>
              Продолжить
              <ArrowRight className="size-4" aria-hidden />
            </Button>
          </SampleGroup>
        </div>
        <SampleGroup label="Состояния">
          <Button disabled>Недоступно</Button>
          <Button disabled aria-busy="true">
            <Spinner />
            Сохранение
          </Button>
        </SampleGroup>
        <SampleGroup label="Действия в тексте">
          <Button variant="link" size="inline" onClick={() => setClicks((n) => n + 1)}>
            Действие в тексте
          </Button>
          <Button asChild variant="link" size="inline">
            <a href="#forms">К полям ввода</a>
          </Button>
        </SampleGroup>
        <SampleGroup label="Многострочная подпись · content">
          <Button
            variant="ghost"
            size="content"
            onClick={() => setClicks((n) => n + 1)}
            className="max-w-80 text-left leading-relaxed"
          >
            Действие с длинной подписью, которая переносится на следующую строку
          </Button>
        </SampleGroup>
        <SampleGroup label="Фильтр · Chip">
          <Chip aria-pressed={selected} onClick={() => setSelected((value) => !value)}>
            {selected ? "Выбрано: материалы" : "Материалы"}
          </Chip>
        </SampleGroup>
        <output className="text-xs text-muted-foreground">Действий: {clicks}</output>
      </Specimen>
      <Specimen
        id="badges"
        title="Badge"
        description="Статус всегда сопровождается подписью. Семантические цвета означают одно и то же в карточках, таблицах, графиках и уведомлениях."
        code={
          '<Badge variant="ok">Подтверждено</Badge>\n<Badge variant="warn">Нужна проверка</Badge>\n<Badge variant="risk">Опровергнуто</Badge>'
        }
      >
        <div className="flex flex-wrap gap-2">
          {(Object.keys(badgeTones) as (keyof typeof badgeTones)[]).map((tone) => (
            <Badge key={tone} variant={tone}>
              {tone}
            </Badge>
          ))}
        </div>
      </Specimen>
      <Specimen
        id="forms"
        title="Поля и настройки"
        description="Одна рамка, высота, фокус и состояние ошибки для Input и Textarea. Подписи связаны с контролами; подсказка и ошибка доступны скринридеру."
        code={
          '<FieldRow id="name" label="Название">\n  <Input id="name" required />\n</FieldRow>\n<Textarea aria-label="Комментарий" rows={3} />\n<Input aria-invalid aria-describedby="error" />'
        }
      >
        <div className="grid gap-6 sm:grid-cols-2">
          <FieldRow
            id="kit-title"
            label="Название"
            hint="Краткое название поможет найти гипотезу."
            badge={<Badge variant="outline">Обязательное</Badge>}
          >
            <Input
              id="kit-title"
              required
              aria-describedby="kit-title-hint"
              placeholder="Например, термостойкий полимер"
            />
          </FieldRow>
          <FieldRow id="kit-limit" label="Размер выборки" unit="документов">
            <Input id="kit-limit" type="number" min={1} defaultValue={24} />
          </FieldRow>
          <div className="grid content-start gap-2">
            <Label htmlFor="kit-error">Название с ошибкой</Label>
            <Input
              id="kit-error"
              aria-invalid="true"
              aria-describedby="kit-error-message"
              defaultValue=""
            />
            <p id="kit-error-message" className="text-xs text-risk">
              Введите название гипотезы.
            </p>
          </div>
          <FieldRow id="kit-disabled" label="Недоступное поле">
            <Input id="kit-disabled" disabled value="Автоматическая оценка" />
          </FieldRow>
          <FieldRow id="kit-format" label="Формат источника">
            <Select value={format} onValueChange={setFormat}>
              <SelectTrigger id="kit-format">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="pdf">PDF</SelectItem>
                <SelectItem value="docx">Word</SelectItem>
                <SelectItem value="xlsx">Excel</SelectItem>
                <SelectItem value="unavailable" disabled>
                  Недоступный формат
                </SelectItem>
              </SelectContent>
            </Select>
          </FieldRow>
          <FieldRow id="kit-range" label="Вес критерия">
            <Range id="kit-range" min={0} max={100} defaultValue={65} />
          </FieldRow>
          <FieldRow id="kit-compact" label="Компактный поиск" layout="compact">
            <Input id="kit-compact" placeholder="Компактный поиск — подпись доступна скринридеру" />
          </FieldRow>
          <div className="grid gap-2 sm:col-span-2">
            <Label htmlFor="kit-comment">Комментарий эксперта</Label>
            <Textarea
              id="kit-comment"
              rows={3}
              placeholder="Что нужно проверить в следующем эксперименте?"
            />
          </div>
        </div>
        <FieldRow
          id="kit-setting"
          label="Гипотез за один запуск"
          hint="Строка настроек: подпись слева, значение справа."
          layout="row"
          unit="шт."
        >
          <Input
            id="kit-setting"
            type="number"
            min={1}
            max={10}
            defaultValue={5}
            className="w-28"
            aria-describedby="kit-setting-hint"
          />
        </FieldRow>
        <div className="grid gap-3">
          <SearchField
            value={query}
            onChange={setQuery}
            placeholder="Название или ключевое слово"
            ariaLabel="Поиск по образцу"
          />
          <output className="text-xs text-muted-foreground">Запрос: {query || "пусто"}</output>
        </div>
        <ToggleRow
          id="kit-notifications"
          label="Уведомления"
          hint="Сообщать о завершении обработки."
          checked={checked}
          onCheckedChange={setChecked}
        />
        <SampleGroup label="Размеры и состояния Switch">
          <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
            <Switch size="sm" aria-label="Компактный переключатель" defaultChecked />
            Малый
          </span>
          <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
            <Switch aria-label="Выключенный переключатель" disabled />
            Недоступен
          </span>
          <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
            <Switch aria-label="Включённый недоступный переключатель" checked disabled />
            Включён и недоступен
          </span>
        </SampleGroup>
      </Specimen>
      <Specimen
        id="filter-bars"
        title="FilterBar"
        description="Панель задаёт одну высоту для поиска, селекторов, кнопок и сегментов. На узком экране контролы переносятся на следующую строку."
        code={
          '<FilterBar>\n  <SearchField {...search} />\n  <Select>…</Select>\n  <Button variant="outline">Только с индексом</Button>\n</FilterBar>'
        }
      >
        {(["default", "sm"] as const).map((size) => (
          <SampleGroup key={size} label={`Панель фильтров · ${size}`}>
            <FilterBar size={size} className="w-full">
              <SearchField
                value={query}
                onChange={setQuery}
                placeholder="Название или ключевое слово"
                ariaLabel={`Поиск в панели · ${size}`}
                className="min-w-56 flex-1"
              />
              <Select value={format} onValueChange={setFormat}>
                <SelectTrigger className="w-28" aria-label={`Формат в панели · ${size}`}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="pdf">PDF</SelectItem>
                  <SelectItem value="docx">Word</SelectItem>
                  <SelectItem value="xlsx">Excel</SelectItem>
                </SelectContent>
              </Select>
              <Segmented
                aria-label={`Представление в панели · ${size}`}
                value={view}
                onChange={setView}
                options={[
                  { value: "list", label: "Список" },
                  { value: "grid", label: "Карточки" },
                ]}
              />
              <Button
                variant="outline"
                size="sm"
                aria-pressed={selected}
                onClick={() => setSelected((value) => !value)}
              >
                Только с индексом
              </Button>
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={`Параметры панели · ${size}`}
                onClick={() => setClicks((n) => n + 1)}
              >
                <SlidersHorizontal className="size-4" aria-hidden />
              </Button>
            </FilterBar>
          </SampleGroup>
        ))}
      </Specimen>
      <Specimen
        id="navigation"
        title="Tabs · Segmented"
        description="Вкладки переключают разделы и связаны с панелью. Сегменты выбирают значение фильтра. Стрелки работают в обоих компонентах; Home и End — во вкладках."
        code={
          '<Tabs id="detail" tabs={tabs} active={active} onChange={setActive} />\n<TabPanel tabsId="detail" active={active}>{content}</TabPanel>\n<Segmented aria-label="Вид" options={views} value={view} onChange={setView} />'
        }
      >
        <div className="grid gap-4">
          <Tabs
            id="kit-tabs"
            tabs={[
              { id: "evidence", label: "Доказательства", badge: 12 },
              { id: "plan", label: "План проверки" },
              { id: "history", label: "История", badge: 3 },
            ]}
            active={tab}
            onChange={setTab}
          />
          <TabPanel tabsId="kit-tabs" active={tab} className="rounded-lg bg-secondary p-4 text-sm">
            {tab === "evidence"
              ? "12 фрагментов связаны с гипотезой."
              : tab === "plan"
                ? "Определите следующий эксперимент и критерий успеха."
                : "Здесь появятся решения и комментарии экспертов."}
          </TabPanel>
        </div>
        <div className="grid gap-6 sm:grid-cols-2">
          <SampleGroup label="Компактный выбор · sm">
            <Segmented
              size="sm"
              aria-label="Представление образца"
              options={[
                { value: "list", label: "Список" },
                { value: "grid", label: "Карточки" },
                { value: "graph", label: "Граф" },
              ]}
              value={view}
              onChange={setView}
            />
          </SampleGroup>
          <SampleGroup label="Стандартный выбор · md">
            <Segmented
              aria-label="Крупное представление образца"
              size="md"
              options={[
                { value: "list", label: "Список" },
                { value: "grid", label: "Карточки" },
                { value: "graph", label: "Граф" },
              ]}
              value={view}
              onChange={setView}
            />
          </SampleGroup>
        </div>
        <output className="block text-xs text-muted-foreground">Представление: {view}</output>
      </Specimen>
    </>
  );
}
