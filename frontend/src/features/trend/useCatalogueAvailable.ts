import { useRequest } from "@/shared/lib/useRequest";
import { searchTechnologies } from "./catalogueApi";

let known: boolean | undefined;

/** Ответ запоминается на сессию, чтобы вкладка не мигала. */
export function useCatalogueAvailable(): boolean {
  const { data } = useRequest(known === undefined ? "catalogue-available" : null, (signal) =>
    searchTechnologies({ q: "", all: true, limit: 1 }, signal).then((page) => {
      known = page.reason !== "catalogue_empty";
      return known;
    }),
  );
  return data ?? known ?? true;
}
