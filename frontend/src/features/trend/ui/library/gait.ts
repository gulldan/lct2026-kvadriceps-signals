import { Vector3, type Object3D } from "three";

/**
 * Слой походки поверх маршрута: логика сцены ведёт персонажа по отрезкам с постоянной
 * скоростью, а этот слой добавляет инерцию. Критически задемпфированная пружина даёт разгон,
 * торможение и скруглённые повороты, тело наклоняется вперёд на ходу и кренится в повороте.
 * Позиция маршрута остаётся прежней: пружина отстаёт от неё на доли единицы.
 */
export function libraryGait(root: Object3D) {
  root.rotation.order = "YXZ";
  const shown = root.position.clone();
  const logical = root.position.clone();
  const velocity = new Vector3();
  const offset = new Vector3();
  const step = new Vector3();
  let heading = root.rotation.y;
  let lean = 0;
  let bank = 0;
  return {
    /** Перед шагом логики персонаж возвращается на маршрут: планировщик считает от него. */
    restore() {
      root.position.copy(logical);
    },
    apply(dt: number, still: boolean) {
      logical.copy(root.position);
      const target = logical;
      if (still || dt <= 0) {
        shown.copy(target);
        velocity.set(0, 0, 0);
        lean = bank = 0;
      } else {
        const omega = 9;
        const decay = Math.exp(-omega * dt);
        offset.copy(shown).sub(target);
        step.copy(velocity).addScaledVector(offset, omega).multiplyScalar(dt);
        velocity.addScaledVector(step, -omega).multiplyScalar(decay);
        shown.copy(target).add(offset.add(step).multiplyScalar(decay));
        const speed = Math.hypot(velocity.x, velocity.z);
        const turn = Math.atan2(
          Math.sin(root.rotation.y - heading),
          Math.cos(root.rotation.y - heading),
        );
        const blend = 1 - Math.exp(-dt * 6);
        lean += (Math.min(0.13, speed * 0.075) - lean) * blend;
        bank += (Math.max(-0.14, Math.min(0.14, (-turn / dt) * 0.03)) - bank) * blend;
      }
      heading = root.rotation.y;
      root.position.copy(shown);
      root.rotation.x = lean;
      root.rotation.z = bank;
    },
  };
}
