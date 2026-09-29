import type { LibrarySection } from "../../liveSearchProgress";
import { DESK, SHELF_TARGETS, type FloorPoint, type ShelfTarget } from "./layout";
import { libraryRoute, libraryTrip } from "./navigation";

/** Колода полок исчерпывается до перемешивания: дальние крылья не проигрывают коротким путям. */
export function libraryPatrol(random: () => number) {
  let remaining: ShelfTarget[] = [];
  let previous: FloorPoint | undefined;
  return (section: LibrarySection) => {
    const trip = libraryTrip(section, random, previous);
    previous = trip.target;
    if (!remaining.length) {
      remaining = Object.values(SHELF_TARGETS).flat();
      for (let index = remaining.length - 1; index > 0; index--) {
        const other = Math.floor(random() * (index + 1));
        [remaining[index], remaining[other]] = [remaining[other]!, remaining[index]!];
      }
    }
    const candidate = remaining.pop()!;
    const inspection = candidate.point === trip.target ? undefined : candidate;
    const approach = inspection ? libraryRoute(DESK, inspection.point, random) : [];
    const onward = inspection ? libraryRoute(inspection.point, trip.target, random) : [];
    const home = inspection ? libraryRoute(inspection.point, DESK) : [];
    // Не отправляем персонажа в тупик, если план помещения изменился.
    if (!inspection || !approach.length || !onward.length || !home.length)
      return { ...trip, inspection: undefined };
    return { ...trip, inspection: { ...inspection, approach, onward, home } };
  };
}
