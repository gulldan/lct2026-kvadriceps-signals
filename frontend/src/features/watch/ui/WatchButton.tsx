import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Eye } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { toast } from "@/shared/ui/Sonner";
import { addWatch, removeWatch, sameSubject, useWatches, type WatchKind } from "../api";

export function WatchButton({
  kind,
  target,
  title,
}: {
  kind: WatchKind;
  target: string;
  title: string;
}) {
  const { t } = useTranslation("task");
  const { watches, ready, refresh } = useWatches();
  const [busy, setBusy] = useState(false);
  const subject = { kind, target };
  const watching = watches.some((watch) => sameSubject(watch, subject));
  const toggle = async () => {
    setBusy(true);
    try {
      await (watching ? removeWatch(subject) : addWatch({ ...subject, title }));
      toast.success(t(watching ? "watch.removed" : "watch.added"));
      refresh();
    } catch {
      toast.error(t("watch.failed"));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Button variant="outline" disabled={!ready || busy} onClick={() => void toggle()}>
      {watching ? <Check aria-hidden /> : <Eye aria-hidden />}
      {t(watching ? "watch.following" : "watch.follow")}
    </Button>
  );
}
