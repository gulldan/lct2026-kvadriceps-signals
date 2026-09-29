import { ToggleRow } from "@/shared/ui/Fields";
import { parseBool, type AppSettingsState } from "../model";

export function BoolRow({
  app,
  k,
  label,
  hint,
}: {
  app: AppSettingsState;
  k: string;
  label: string;
  hint?: string;
}) {
  return (
    <ToggleRow
      id={`sys-${k}`}
      label={label}
      hint={hint}
      checked={parseBool(app.effective(k))}
      onCheckedChange={(checked) => app.setValue(k, checked ? "true" : "false")}
    />
  );
}
