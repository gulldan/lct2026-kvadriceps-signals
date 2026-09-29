import { Group, type Vector3 } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import { SHELF_TARGETS } from "./layout";
import type { LibraryPose } from "./poses";

/** Корешок выходит из той полки, у которой стоят руки архивариуса. */
export function libraryShelfResponse(g: LibraryGeometry, m: LibraryMaterials) {
  const root = new Group();
  root.name = "archivist-selected-book";
  const book = new Group();
  root.add(book);
  g.box(book, m.claret, 0, 0, 0, 0.17, 0.49, 0.34);
  g.box(book, m.paper, 0, 0, 0.02, 0.12, 0.44, 0.29);
  for (const y of [-0.17, 0.17]) g.box(book, m.brass, 0, y, -0.176, 0.174, 0.025, 0.012);
  const targets = Object.values(SHELF_TARGETS).flat();
  let time = 0;
  return {
    root,
    advance(dt: number, actor: Vector3, pose: LibraryPose) {
      time += dt;
      const target = targets.find(
        ({ point }) => Math.hypot(point[0] - actor.x, point[1] - actor.z) < 0.65,
      );
      root.visible = !!target && (pose === "browse" || pose === "reach");
      if (!target || !root.visible) return;
      const pull = 0.16 + Math.max(0, Math.sin(time * 2.1)) * 0.22;
      root.position.set(
        target.point[0] + Math.sin(target.heading) * (1.1 - pull),
        actor.y + 1.07,
        target.point[1] + Math.cos(target.heading) * (1.1 - pull),
      );
      root.rotation.y = target.heading;
      book.position.x = pose === "browse" ? Math.sin(time * 0.7) * 0.23 : 0;
    },
  };
}
