import { Database, FileImage, FileSpreadsheet, FileText, Mail, Presentation } from "lucide-react";
import { useTranslation } from "react-i18next";
import { FileType } from "@/shared/types";

const ICONS: Record<FileType, typeof FileText> = {
  pdf: FileText,
  docx: FileText,
  eml: Mail,
  xlsx: FileSpreadsheet,
  pptx: Presentation,
  txt: FileText,
  image: FileImage,
  db: Database,
  other: FileText,
};

export function FileTypeBadge({ type }: { type: FileType }) {
  const { t } = useTranslation();
  const Icon = ICONS[type];
  return (
    <span className="inline-flex items-center gap-1 rounded-sm bg-secondary px-1.5 py-0.5 text-caption font-semibold text-muted-foreground">
      <Icon className="size-3" aria-hidden />
      {t(`fileTypes.${type}`)}
    </span>
  );
}
