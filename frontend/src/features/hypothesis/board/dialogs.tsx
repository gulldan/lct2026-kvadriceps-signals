import { Copy, Download } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "@/shared/ui/Sonner";

import { saveFile } from "@/shared/lib/download";
import { Button } from "@/shared/ui/Button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { RichText } from "@/shared/ui/RichText";
import { ScrollArea } from "@/shared/ui/ScrollArea";
import { copyText } from "@/shared/lib/clipboard";

export function DigestDialog({
  open,
  onOpenChange,
  digest,
  count,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  digest: string;
  count: number;
}) {
  const { t } = useTranslation("hypothesis");
  const { t: tCommon } = useTranslation("common");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("digest.dialogTitle", { n: count })}</DialogTitle>
          <DialogDescription>{t("digest.dialogDescription")}</DialogDescription>
        </DialogHeader>
        <ScrollArea
          aria-label={t("digest.dialogTitle", { n: count })}
          className="max-h-[58vh] rounded-lg border bg-secondary p-4"
        >
          <RichText className="text-sm">{digest}</RichText>
        </ScrollArea>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={async () => {
              if (await copyText(digest)) toast.success(t("digest.copied"));
              else toast.error(tCommon("ui.copyFailed"));
            }}
          >
            <Copy className="size-4" aria-hidden />
            {tCommon("actions.copy")}
          </Button>
          <Button
            onClick={() => saveFile(t("digest.fileName"), digest, "text/markdown;charset=utf-8")}
          >
            <Download className="size-4" aria-hidden />
            {t("digest.downloadMd")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
