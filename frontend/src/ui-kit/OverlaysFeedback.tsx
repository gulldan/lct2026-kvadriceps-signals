import { useState } from "react";
import { Inbox, MoreHorizontal } from "lucide-react";
import { toast } from "@/shared/ui/Sonner";

import { Button } from "@/shared/ui/Button";
import { CardGridSkeleton } from "@/shared/ui/CardGridSkeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/shared/ui/Dialog";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/DropdownMenu";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { InfoHint } from "@/shared/ui/InfoHint";
import { ListSkeleton } from "@/shared/ui/ListSkeleton";
import { Notice } from "@/shared/ui/Notice";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/shared/ui/Sheet";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/Tooltip";
import { SampleGroup, Specimen } from "./Specimen";

export function OverlaysFeedback() {
  const [dialog, setDialog] = useState(false);
  const [sheet, setSheet] = useState<"left" | "right" | "top" | "bottom" | null>(null);
  const [archived, setArchived] = useState(false);
  const [retried, setRetried] = useState(false);
  return (
    <>
      <Specimen
        id="overlays"
        title="Диалоги и меню"
        description="Radix управляет фокусом, Escape, порталом и клавиатурой. Dialog и Sheet используют один цвет затемнения; меню и подсказки — общие токены поверхности."
        code={
          "<Dialog open={open} onOpenChange={setOpen}>\n  <DialogContent><DialogHeader>\n    <DialogTitle>Проверить гипотезу</DialogTitle>\n    <DialogDescription>Пояснение действия.</DialogDescription>\n  </DialogHeader></DialogContent>\n</Dialog>"
        }
      >
        <SampleGroup label="Диалог, меню и подсказки">
          <Button variant="outline" onClick={() => setDialog(true)}>
            Открыть диалог
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline">
                <MoreHorizontal className="size-4" aria-hidden />
                Действия
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuLabel>Источник</DropdownMenuLabel>
              <DropdownMenuItem onSelect={() => toast("Ссылка скопирована — пример уведомления")}>
                Пример действия
              </DropdownMenuItem>
              <DropdownMenuCheckboxItem checked={archived} onCheckedChange={setArchived}>
                Показать архив
              </DropdownMenuCheckboxItem>
              <DropdownMenuItem disabled>Экспорт недоступен</DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={() => setDialog(true)}>
                Удалить источник…
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost">Подсказка</Button>
            </TooltipTrigger>
            <TooltipContent>Пояснение на hover и при фокусе.</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost">Мини-карточка</Button>
            </TooltipTrigger>
            <TooltipContent variant="card" className="w-64 space-y-2">
              <p className="text-sm font-medium">Предпросмотр объекта</p>
              <p className="text-muted-foreground">
                Данные и графики перед открытием подробностей.
              </p>
            </TooltipContent>
          </Tooltip>
          <InfoHint label="Что означает оценка?">
            Оценка рассчитывается по найденным фрагментам и требует проверки экспертом.
          </InfoHint>
        </SampleGroup>
        <SampleGroup label="Направления Sheet">
          {(["left", "right", "top", "bottom"] as const).map((side) => (
            <Button key={side} size="sm" variant="secondary" onClick={() => setSheet(side)}>
              Sheet · {side}
            </Button>
          ))}
        </SampleGroup>
        <Dialog open={dialog} onOpenChange={setDialog}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Проверить гипотезу</DialogTitle>
              <DialogDescription>
                Это образец диалога. Данные приложения не изменяются.
              </DialogDescription>
            </DialogHeader>
            <p className="text-sm">
              Проверьте Tab, Shift+Tab и Escape. После закрытия фокус возвращается к кнопке.
            </p>
            <DialogFooter>
              <Button variant="outline" onClick={() => setDialog(false)}>
                Отмена
              </Button>
              <Button
                onClick={() => {
                  setDialog(false);
                  toast.success("Проверка завершена — пример");
                }}
              >
                Подтвердить
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        <Sheet
          open={sheet !== null}
          onOpenChange={(open) => {
            if (!open) setSheet(null);
          }}
        >
          <SheetContent side={sheet ?? "right"}>
            <SheetHeader>
              <SheetTitle>Источник гипотезы</SheetTitle>
              <SheetDescription>
                Боковая панель сохраняет контекст текущей страницы.
              </SheetDescription>
            </SheetHeader>
            <p className="mt-6 text-sm">Демонстрация поведения фокуса и прокрутки.</p>
          </SheetContent>
        </Sheet>
      </Specimen>
      <Specimen
        id="feedback"
        title="Состояния интерфейса"
        description="Загрузка, отсутствие данных, ошибка и уведомления входят в UI kit. Ошибка даёт понятное действие, а skeleton сохраняет форму будущего содержимого."
        code={
          '<EmptyState icon={Inbox} title="Источников пока нет" action={<Button>Добавить</Button>} />\n<ErrorState message="Не удалось загрузить источники" onRetry={reload} />\n<ListSkeleton rows={3} variant="result" />'
        }
      >
        <div className="grid gap-6 sm:grid-cols-2">
          <SampleGroup label="Пустое состояние">
            <div className="flex min-h-80 w-full items-center">
              <EmptyState
                icon={Inbox}
                title="Источников пока нет"
                description="Добавьте первый документ, чтобы начать проверку."
                action={
                  <Button size="sm" onClick={() => toast("Образец добавления источника")}>
                    Добавить источник
                  </Button>
                }
              />
            </div>
          </SampleGroup>
          <SampleGroup label="Ошибка и повторная попытка">
            <div className="flex min-h-80 w-full items-center">
              {retried ? (
                <output className="w-full text-center text-sm text-ok">
                  Повторная загрузка выполнена — пример.
                </output>
              ) : (
                <ErrorState
                  variant="compact"
                  message="Не удалось загрузить источники."
                  onRetry={() => setRetried(true)}
                />
              )}
            </div>
          </SampleGroup>
        </div>
        <SampleGroup label="Уведомления">
          <Button variant="outline" size="sm" onClick={() => toast.success("Изменения сохранены")}>
            Успех
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => toast.error("Не удалось сохранить. Повторите попытку.")}
          >
            Ошибка
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => toast.warning("Проверьте источники перед публикацией.")}
          >
            Предупреждение
          </Button>
          <Button variant="outline" size="sm" onClick={() => toast("Обработка началась")}>
            Сообщение
          </Button>
        </SampleGroup>
        <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
          <Notice tone="brand">Сначала выберите источники для проверки.</Notice>
          <Notice tone="ok">Источники проверены. Можно продолжать.</Notice>
          <Notice tone="warn">Данных недостаточно для уверенного вывода.</Notice>
          <Notice tone="risk">Не удалось прочитать документ. Загрузите его повторно.</Notice>
          <Notice tone="muted">
            Границы сбора · 4 — это свойства метода, а не ошибки. Приглушённый тон отделяет их от
            отказа источника.
          </Notice>
        </div>
        <SampleGroup label="Загрузка строки">
          <Skeleton className="h-5 w-48" />
        </SampleGroup>
        <div className="grid gap-6 sm:grid-cols-2">
          <ListSkeleton rows={2} variant="simple" />
          <ListSkeleton rows={2} variant="avatar" />
        </div>
        <ListSkeleton rows={2} variant="result" />
        <CardGridSkeleton count={2} />
      </Specimen>
    </>
  );
}
