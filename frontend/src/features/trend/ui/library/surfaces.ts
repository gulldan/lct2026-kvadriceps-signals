import { Mesh, type Material, type Object3D } from "three";

const NOISE = `varying vec3 vLibraryPosition;
varying vec3 vLibraryNormal;
float libraryHash(vec3 p) {
  p = fract(p * 0.3183099 + 0.1);
  p *= 17.0;
  return fract(p.x * p.y * p.z * (p.x + p.y + p.z));
}
float libraryNoise(vec3 x) {
  vec3 i = floor(x);
  vec3 f = fract(x);
  f = f * f * (3.0 - 2.0 * f);
  return mix(
    mix(mix(libraryHash(i), libraryHash(i + vec3(1, 0, 0)), f.x),
        mix(libraryHash(i + vec3(0, 1, 0)), libraryHash(i + vec3(1, 1, 0)), f.x), f.y),
    mix(mix(libraryHash(i + vec3(0, 0, 1)), libraryHash(i + vec3(1, 0, 1)), f.x),
        mix(libraryHash(i + vec3(0, 1, 1)), libraryHash(i + vec3(1, 1, 1)), f.x), f.y),
    f.z);
}`;

// Волокна тянутся вдоль вертикали на стойках и вдоль зала на горизонтальных гранях.
const DETAIL: Record<string, string> = {
  wood: `{
    vec3 stretch = abs(vLibraryNormal.y) > 0.5 ? vec3(0.9, 1.0, 12.0) : vec3(14.0, 0.8, 14.0);
    vec3 q = vLibraryPosition * stretch;
    float figure = libraryNoise(q) * 0.55 + libraryNoise(q * 2.3 + 7.0) * 0.3 + libraryNoise(q * 6.1) * 0.15;
    float rings = sin(figure * 18.0 + libraryNoise(vLibraryPosition * 1.7) * 6.0) * 0.5 + 0.5;
    diffuseColor.rgb *= 0.84 + figure * 0.2 + rings * 0.09;
    roughnessFactor = clamp(roughnessFactor - rings * 0.12, 0.2, 1.0);
  }`,
  plaster: `{
    vec3 q = vLibraryPosition;
    float cloud = libraryNoise(q * 0.7) * 0.55 + libraryNoise(q * 2.9) * 0.3 + libraryNoise(q * 12.0) * 0.15;
    diffuseColor.rgb *= 0.88 + cloud * 0.2;
  }`,
};

/**
 * Процедурная фактура без текстур: координаты берутся из мира, поэтому слитая геометрия
 * тысяч деталей получает непрерывный рисунок без развёртки.
 */
export function librarySurfaces(root: Object3D) {
  const patched = new Set<Material>();
  root.traverse((object) => {
    if (!(object instanceof Mesh)) return;
    for (const material of [object.material].flat() as Material[]) {
      const kind = material.userData.surface as string | undefined;
      const detail = kind && DETAIL[kind];
      if (!detail || patched.has(material)) continue;
      patched.add(material);
      material.customProgramCacheKey = () => `library-surface-${kind}`;
      material.onBeforeCompile = (shader) => {
        shader.vertexShader = shader.vertexShader
          .replace(
            "#include <common>",
            "#include <common>\nvarying vec3 vLibraryPosition;\nvarying vec3 vLibraryNormal;",
          )
          .replace(
            "#include <project_vertex>",
            `#include <project_vertex>
vLibraryPosition = (modelMatrix * vec4(transformed, 1.0)).xyz;
vLibraryNormal = normalize(mat3(modelMatrix) * objectNormal);`,
          );
        shader.fragmentShader = shader.fragmentShader
          .replace("#include <common>", `#include <common>\n${NOISE}`)
          .replace(
            "#include <roughnessmap_fragment>",
            `#include <roughnessmap_fragment>\n${detail}`,
          );
      };
    }
  });
}
