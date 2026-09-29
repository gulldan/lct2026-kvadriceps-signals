import { ExternalLink } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useRequest } from "@/shared/lib/useRequest";
import { Button } from "@/shared/ui/Button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/shared/ui/Dialog";
import { getLiveCover } from "../liveApi";

const CACHE_MS = 30 * 60_000;

export function DirectionCover({ topicId }: { topicId: string }) {
  const { t } = useTranslation("trend");
  const [open, setOpen] = useState(false);
  const { data: cover } = useRequest(
    `live-cover:${topicId}`,
    (signal) => getLiveCover(topicId, signal),
    { cacheMs: CACHE_MS },
  );
  if (!cover) return null;
  const figure = cover.figure
    ? t("study.live.cover.figure", { number: cover.figure })
    : t("study.live.cover.figureUnnumbered");
  const paper = t("study.live.cover.paper", { title: cover.paper.title });
  const alt = cover.caption || `${figure} ${paper}`;
  // Рисунки статей рассчитаны на бумагу: прозрачный PNG на тёмной карточке терял бы чёрные линии.
  const frame = "w-full rounded-lg bg-document-paper object-contain ring-1";
  return (
    // Ширина карточки, а не окна: при открытом меню описание рядом с рисунком сжималось в столбик.
    <figure className="w-full space-y-2 @4xl:w-[30rem] @4xl:shrink-0">
      <Button
        variant="ghost"
        size="content"
        className="group w-full p-0"
        aria-label={t("study.live.cover.open")}
        onClick={() => setOpen(true)}
      >
        <img
          src={cover.image}
          alt={alt}
          className={`${frame} h-48 cursor-zoom-in p-2 ring-border-strong transition-shadow group-hover:ring-brand-border @4xl:h-60`}
        />
      </Button>
      <figcaption
        className="line-clamp-2 text-caption text-muted-foreground"
        title={`${figure} ${paper}`}
      >
        {figure}{" "}
        <a
          href={cover.paper.url}
          target="_blank"
          rel="noopener noreferrer"
          className="text-accent-foreground underline underline-offset-4 hover:decoration-2"
        >
          {paper}
        </a>
      </figcaption>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[92dvh] max-w-5xl overflow-y-auto">
          <DialogTitle className="pr-8 leading-snug">
            {figure} {paper}
          </DialogTitle>
          <img src={cover.image} alt={alt} className={`${frame} max-h-[70dvh] p-3 ring-border`} />
          <DialogDescription>{cover.caption}</DialogDescription>
          <Button asChild variant="link" size="inline" className="justify-self-start">
            <a href={cover.paper.url} target="_blank" rel="noopener noreferrer">
              {t("study.live.cover.source")}
              <ExternalLink aria-hidden />
            </a>
          </Button>
        </DialogContent>
      </Dialog>
    </figure>
  );
}
