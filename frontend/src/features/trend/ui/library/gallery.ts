import { Group, type Vector3 } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import type { AmbientScheduler } from "./ambientScheduler";
import { ambientStory, storyEnvelope } from "./ambientStory";

export function libraryGallery(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const portrait = new Group();
  portrait.name = "curiosity-portrait";
  portrait.position.set(7.25, 2.9, -18.14);
  root.add(portrait);
  const fixed = new Group();
  g.box(fixed, m.wood, 0, 0, 0, 1.62, 2.23, 0.12);
  g.box(fixed, m.brass, 0, 0, 0.07, 1.5, 2.11, 0.045);
  g.box(fixed, m.navy, 0, 0, 0.105, 1.38, 1.99, 0.025);
  for (let cloud = 0; cloud < 4; cloud++) {
    g.ball(
      fixed,
      m.paper,
      -0.44 + cloud * 0.29,
      0.68 + Math.sin(cloud * 2) * 0.12,
      0.14,
      0.17,
      0.044,
      0.018,
    );
  }
  g.box(fixed, m.sage, 0, -0.73, 0.14, 1.36, 0.49, 0.02);
  g.ball(fixed, m.ink, 0, -0.5, 0.16, 0.37, 0.43, 0.035);
  g.box(fixed, m.paper, 0, -0.19, 0.195, 0.14, 0.28, 0.023);
  g.box(fixed, m.claret, 0, -0.24, 0.218, 0.044, 0.3, 0.018);
  g.ball(fixed, m.floorAlt, 0, 0.17, 0.2, 0.19, 0.26, 0.02);
  g.ball(fixed, m.ink, 0, 0.42, 0.2, 0.2, 0.145, 0.026);
  g.box(fixed, m.ink, 0, 0.36, 0.232, 0.56, 0.052, 0.04);
  portrait.add(batchArchitecture(fixed));
  const apple = new Group();
  apple.position.set(0, 0.17, 0.29);
  portrait.add(apple);
  g.ball(apple, m.sage, -0.065, 0, 0, 0.136, 0.145, 0.07);
  g.ball(apple, m.sage, 0.065, 0, 0, 0.136, 0.145, 0.07);
  g.rod(apple, m.wood, [0, 0.12, 0], [0.035, 0.23, 0], 0.013);
  const leaf = g.ball(apple, m.sage, 0.083, 0.19, 0, 0.08, 0.023, 0.019);
  leaf.rotation.z = 0.3;
  const story = ambientStory(scheduler, "portrait", portrait, (p, variant, quiet) => {
    const peek = quiet ? 0 : storyEnvelope(p, 0.28, 0.68);
    apple.position.x = peek * (variant % 2 ? 0.38 : -0.38);
    apple.position.y = 0.17 + peek * Math.sin(p * Math.PI * 2) * 0.07;
    apple.rotation.z = peek * 0.17;
    portrait.userData.phase = p < 0.28 ? "reveal" : p < 0.68 ? "watch" : "conceal";
  });
  const square = new Group();
  square.position.set(-22.79, 2.9, 13.02);
  square.rotation.y = Math.PI / 2;
  const squareParts = new Group();
  g.box(squareParts, m.wood, 0, 0, 0, 1.23, 1.36, 0.11);
  g.box(squareParts, m.brass, 0, 0, 0.067, 1.13, 1.26, 0.045);
  g.box(squareParts, m.paper, 0, 0, 0.1, 1.03, 1.16, 0.025);
  const black = g.box(squareParts, m.ink, 0.015, 0.05, 0.121, 0.74, 0.77, 0.019);
  black.rotation.z = -0.028;
  square.add(batchArchitecture(squareParts));
  root.add(square);

  const clock = new Group();
  clock.name = "melting-clock";
  clock.position.set(20.05, 4.5, -17.76);
  root.add(clock);
  const clockFace = new Group();
  clockFace.rotation.x = -Math.PI / 3;
  clock.add(clockFace);
  g.ball(clockFace, m.brass, 0, 0, 0, 0.55, 0.65, 0.034);
  g.ball(clockFace, m.paper, 0, 0, 0.033, 0.51, 0.61, 0.017);
  for (let hour = 0; hour < 12; hour++) {
    const angle = (hour * Math.PI) / 6;
    g.ball(clockFace, m.wood, Math.sin(angle) * 0.42, Math.cos(angle) * 0.5, 0.057, 0.018);
  }
  const hands = new Group();
  clockFace.add(hands);
  g.rod(hands, m.wood, [0, 0, 0.067], [0, 0.38, 0.067], 0.013);
  g.rod(hands, m.wood, [0, 0, 0.067], [0.21, -0.05, 0.067], 0.018);
  g.ball(clock, m.brass, 0, -0.38, 0.3, 0.31, 0.36, 0.043);
  g.ball(clock, m.paper, 0, -0.39, 0.341, 0.27, 0.32, 0.016);
  const droplet = g.ball(clock, m.brass, 0.04, -0.79, 0.33, 0.055, 0.09, 0.035);
  return {
    advance(still: boolean, actor: Vector3) {
      story.advance(still);
      const time = scheduler.time();
      hands.rotation.z = still ? 0 : -time * 0.045;
      const close = Math.max(0, 1 - Math.hypot(actor.x - 20.05, actor.z + 17.76) / 5);
      droplet.scale.y = 0.09 * (1 + (still ? 0 : Math.sin(time * 0.65) * 0.22 + close * 0.4));
    },
  };
}
