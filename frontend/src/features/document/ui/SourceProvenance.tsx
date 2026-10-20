import { useTranslation } from "react-i18next";
import type { SourceEvidence } from "../sourceApi";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Notice } from "@/shared/ui/Notice";

export function SourceProvenance({ evidence }: { evidence: SourceEvidence }) {
  const { t } = useTranslation("document");
  const s = evidence.source;
  const unknown = t("source.unknown");
  const date = s.publication;
  const fields = [
    [t("source.identity"), s.native_id],
    [
      t("source.publication"),
      date ? `${date.value} · ${t(`source.precision.${date.precision}`)}` : unknown,
    ],
    [t("source.observed"), s.observed_at],
    [t("source.access"), t(`source.scope.${s.access_scope}`)],
  ];
  if (s.text_status) fields.push([t("source.material"), t(`source.textStatus.${s.text_status}`)]);
  if (s.arxiv)
    fields.push(
      ...(!s.text_status
        ? [
            [
              t("source.material"),
              evidence.mime_type === "application/pdf"
                ? t("source.pdfText")
                : t("source.metadataOnly"),
            ],
          ]
        : []),
      [t("source.firstVersion"), s.arxiv.first_published],
      [
        t("source.contentVersion"),
        `${s.arxiv.base_id}v${s.arxiv.version} · ${s.arxiv.version_updated}`,
      ],
    );
  if (s.github)
    fields.push(
      ["README", s.github.readme_path || t("source.noReadme")],
      [t("source.created"), s.github.created_at],
      [t("source.release"), s.github.released_at ?? unknown],
      [t("source.stars"), s.github.stars === null ? unknown : String(s.github.stars)],
      [t("source.license"), s.github.license || unknown],
      [
        t("source.repositoryState"),
        [
          s.github.archived ? t("source.archived") : t("source.notArchived"),
          s.github.fork
            ? t("source.fork", { root: s.github.root_id ?? unknown })
            : t("source.root"),
        ].join(" · "),
      ],
    );
  if (s.source_kind === "patent")
    fields.push(
      [t("source.priority"), s.priority?.value ?? unknown],
      [t("source.filing"), s.filing?.value ?? unknown],
    );
  const details = [
    [t("source.revision"), s.revision],
    [t("source.snapshot"), evidence.snapshot_id],
    [t("source.originalHash"), `${s.hash_algorithm}: ${s.content_hash}`],
    [t("source.textHash"), `sha256: ${evidence.text_hash}`],
    [t("source.parser"), evidence.parser_revision],
    [t("source.available"), s.available_at ?? unknown],
    [t("source.dateBasis"), date?.basis ?? unknown],
  ];
  if (s.github)
    details.push(
      ["Commit", s.github.commit_sha],
      ["README", s.github.readme_path ?? unknown],
      ["Blob", s.github.blob_sha ?? unknown],
    );
  return (
    <div className="space-y-6">
      {s.arxiv && <Notice tone="brand">{t("source.preprintNotice")}</Notice>}
      {s.github && <Notice tone="brand">{t("source.repositoryNotice")}</Notice>}
      {s.genre && <Notice tone="brand">{t("source.investorNotice")}</Notice>}
      <dl className="grid gap-x-8 gap-y-5 sm:grid-cols-2">
        {fields.map(([label, value]) => (
          <div key={label} className="min-w-0">
            <dt className="text-sm text-muted-foreground">{label}</dt>
            <dd className="mt-1 text-sm wrap-anywhere">{value}</dd>
          </div>
        ))}
      </dl>
      <Disclosure title={t("source.versionDetails")}>
        <dl className="space-y-4">
          {details.map(([label, value]) => (
            <div key={label} className="min-w-0">
              <dt className="text-sm text-muted-foreground">{label}</dt>
              <dd className="mt-1 font-mono text-xs wrap-anywhere">{value}</dd>
            </div>
          ))}
        </dl>
      </Disclosure>
    </div>
  );
}
