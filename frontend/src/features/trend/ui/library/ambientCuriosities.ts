import type { Group } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import type { AmbientScheduler } from "./ambientScheduler";
import { ambientShelfStories } from "./ambientShelfStories";
import { ambientInstrumentStories } from "./ambientInstrumentStories";
import { ambientPaperStories } from "./ambientPaperStories";

export function ambientCuriosities(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const shelves = ambientShelfStories(g, m, root, scheduler);
  const instruments = ambientInstrumentStories(g, m, root, scheduler);
  const paper = ambientPaperStories(g, m, root, scheduler);
  return {
    advance(still: boolean) {
      shelves.advance(still);
      instruments.advance(still);
      paper.advance(still);
    },
    globeTurn: instruments.globeTurn,
  };
}
