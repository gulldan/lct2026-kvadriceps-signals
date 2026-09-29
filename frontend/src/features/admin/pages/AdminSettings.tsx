import { useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { ScoringWeightsCard } from "@/features/hypothesis";
import { toast } from "@/shared/ui/Sonner";
import { Button } from "@/shared/ui/Button";
import { PageHeader } from "@/shared/ui/PageHeader";
import { PageLayout } from "@/shared/ui/PageLayout";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";
import { Tabs, TabPanel } from "@/shared/ui/Tabs";
import { useAppSettings } from "../model";
import { CostCard } from "../ui/CostCard";
import { GenerationFlagsCard } from "../ui/GenerationFlagsCard";
import { ProviderCard } from "../ui/ProviderCard";
import { RuntimeSettingsCard } from "../ui/RuntimeSettingsCard";

const SETTINGS_TABS = ["model", "generation", "research", "ranking"] as const;
type SettingsTab = (typeof SETTINGS_TABS)[number];

export function AdminSettings() {
  const { t } = useTranslation("admin");
  const [tab, setTab] = useState<SettingsTab>("model");
  const [visited, setVisited] = useState<SettingsTab[]>(["model"]);
  const app = useAppSettings();
  const form = useRef<HTMLFormElement>(null);
  const appTab = tab === "model" || tab === "research";
  const save = async () => {
    if (await app.save()) toast.success(t("system.saved"));
    else toast.error(t("system.saveFailed"));
  };
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!app.busy) void save();
  };
  const changeTab = (id: string) => {
    if (app.busy || (app.dirty && form.current && !form.current.reportValidity())) return false;
    setTab(id as SettingsTab);
    setVisited((tabs) => (tabs.includes(id as SettingsTab) ? tabs : [...tabs, id as SettingsTab]));
    return true;
  };
  return (
    <PageLayout
      onInvalidCapture={(event) => {
        // Нативная проверка формы должна фокусировать поля в свёрнутых разделах.
        let parent = (event.target as HTMLElement).parentElement;
        while (parent) {
          if (parent instanceof HTMLDetailsElement) parent.open = true;
          parent = parent.parentElement;
        }
      }}
    >
      <PageHeader title={t("settings.title")} description={t("settings.description")} />
      <Tabs
        id="settings-tabs"
        className="mt-6"
        active={tab}
        onChange={changeTab}
        tabs={SETTINGS_TABS.map((id) => ({ id, label: t(`settings.tabs.${id}`) }))}
      />
      <TabPanel tabsId="settings-tabs" active={tab}>
        {appTab && app.failed && (
          <ErrorState message={t("system.loadFailed")} onRetry={app.reload} />
        )}
        {appTab && !app.loaded && !app.failed && (
          <div className="flex justify-center py-14">
            <Spinner label={t("loading")} />
          </div>
        )}
        {appTab && app.loaded && (
          <form id="app-settings" ref={form} onSubmit={submit} className="mt-4">
            <fieldset disabled={app.busy} className="min-w-0">
              {tab === "model" && (
                <>
                  <ProviderCard app={app} />
                  <CostCard app={app} />
                </>
              )}
              {tab === "research" && <GenerationFlagsCard app={app} />}
            </fieldset>
          </form>
        )}
        {visited.includes("generation") && (
          <div hidden={tab !== "generation"} className="mt-4">
            <RuntimeSettingsCard />
          </div>
        )}
        {visited.includes("ranking") && (
          <div hidden={tab !== "ranking"} className="mt-4">
            <ScoringWeightsCard />
          </div>
        )}
      </TabPanel>
      {appTab && app.dirty && (
        <div className="sticky bottom-0 z-(--layer-sticky) mt-6 flex flex-wrap items-center justify-between gap-3 border-t bg-background py-4">
          <p className="text-sm text-muted-foreground">{t("system.unsaved")}</p>
          <div className="flex gap-2">
            <Button variant="outline" disabled={app.busy} onClick={app.discard}>
              {t("actions.cancel")}
            </Button>
            <Button type="submit" form="app-settings" disabled={app.busy}>
              {app.busy ? t("actions.saving") : t("actions.save")}
            </Button>
          </div>
        </div>
      )}
    </PageLayout>
  );
}
