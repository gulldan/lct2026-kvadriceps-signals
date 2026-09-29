import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { KbDoc } from "@/shared/types";
import { cn } from "@/shared/lib/cn";
import { sourcePath } from "../sourceApi";

export function SourceVersionLink({
  doc,
  fragment,
  className,
}: {
  doc: KbDoc;
  fragment?: string;
  className?: string;
}) {
  const { t } = useTranslation("document");
  const location = useLocation();
  if (!doc.hasProvenance && !doc.sourceSnapshot) return null;
  return (
    <Link
      className={cn("text-sm text-primary underline-offset-4 hover:underline", className)}
      to={sourcePath(doc.id, doc.sourceSnapshot, fragment, {
        returnTo: location.pathname + location.search + location.hash,
      })}
    >
      {t("source.versionLink")}
    </Link>
  );
}
