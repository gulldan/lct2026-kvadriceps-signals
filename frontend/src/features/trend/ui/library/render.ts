import {
  HalfFloatType,
  PMREMGenerator,
  Vector2,
  WebGLRenderTarget,
  type Object3D,
  type Camera,
  type Scene,
  type WebGLRenderer,
} from "three";
import { EffectComposer } from "three/addons/postprocessing/EffectComposer.js";
import { GTAOPass } from "three/addons/postprocessing/GTAOPass.js";
import { OutputPass } from "three/addons/postprocessing/OutputPass.js";
import { RenderPass } from "three/addons/postprocessing/RenderPass.js";
import { ShaderPass } from "three/addons/postprocessing/ShaderPass.js";
import { UnrealBloomPass } from "three/addons/postprocessing/UnrealBloomPass.js";
import { RoomEnvironment } from "three/addons/environments/RoomEnvironment.js";

/** Прозрачные лучи, ореолы и пыль не должны затенять геометрию за собой. */
class LibraryOcclusion extends GTAOPass {
  private hidden: Object3D[] = [];
  _overrideVisibility() {
    this.scene.traverse((object) => {
      if (object.visible && (object.userData.glow || "isPoints" in object)) {
        object.visible = false;
        this.hidden.push(object);
      }
    });
  }
  _restoreVisibility() {
    this.hidden.forEach((object) => (object.visible = true));
    this.hidden.length = 0;
  }
}

const GRADE = {
  uniforms: { tDiffuse: { value: null } },
  vertexShader: `varying vec2 vUv;
void main() { vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
  fragmentShader: `uniform sampler2D tDiffuse;
varying vec2 vUv;
void main() {
  vec4 color = texture2D(tDiffuse, vUv);
  float luma = dot(color.rgb, vec3(0.2126, 0.7152, 0.0722));
  color.rgb = mix(color.rgb, color.rgb * vec3(0.9, 0.97, 1.1), (1.0 - smoothstep(0.0, 0.45, luma)) * 0.4);
  color.rgb = mix(color.rgb, color.rgb * vec3(1.07, 1.0, 0.88), smoothstep(0.35, 1.3, luma) * 0.35);
  color.rgb = max(mix(vec3(luma), color.rgb, 1.1), 0.0);
  gl_FragColor = color;
}`,
};

// Глубина резкости по буферу глубины затенения: резкость держится вокруг архивариуса,
// ближние стеллажи и дальний край зала мягко размываются, как у игровой камеры.
const FOCUS = {
  uniforms: {
    tDiffuse: { value: null },
    tDepth: { value: null },
    cameraNear: { value: 1 },
    cameraFar: { value: 100 },
    focusDepth: { value: 50 },
    range: { value: 10 },
    radius: { value: 0 },
    resolution: { value: new Vector2(1, 1) },
  },
  vertexShader: `varying vec2 vUv;
void main() { vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
  fragmentShader: `#include <packing>
uniform sampler2D tDiffuse;
uniform sampler2D tDepth;
uniform float cameraNear;
uniform float cameraFar;
uniform float focusDepth;
uniform float range;
uniform float radius;
uniform vec2 resolution;
varying vec2 vUv;
float blurAt(vec2 uv) {
  float depth = -perspectiveDepthToViewZ(texture2D(tDepth, uv).x, cameraNear, cameraFar);
  return smoothstep(0.0, 1.0, abs(depth - focusDepth) / range);
}
void main() {
  vec4 base = texture2D(tDiffuse, vUv);
  float amount = blurAt(vUv) * radius;
  if (amount < 0.3) { gl_FragColor = base; return; }
  vec4 sum = base;
  float weight = 1.0;
  // Золотой угол раскладывает 16 выборок по диску без заметного узора.
  for (int i = 1; i <= 16; i++) {
    float r = sqrt(float(i) / 16.0);
    float a = float(i) * 2.39996;
    vec2 uv = vUv + vec2(cos(a), sin(a)) * r * amount / resolution;
    float w = blurAt(uv);
    sum += texture2D(tDiffuse, uv) * w;
    weight += w;
  }
  gl_FragColor = sum / weight;
}`,
};

/**
 * Кинематографичный кадр: затенение щелей, свечение огня, отражения латуни и цветокоррекция.
 * Холст прозрачный: макет стоит прямо на странице, а свечение добавляет себе непрозрачность.
 * Программный WebGL и слабые видеокарты получают прямой рендер, иначе кадр не успевает за сценой.
 */
export function libraryRenderer(renderer: WebGLRenderer, scene: Scene, camera: Camera) {
  const gl = renderer.getContext();
  const debug = gl.getExtension("WEBGL_debug_renderer_info");
  const device = String(debug ? gl.getParameter(debug.UNMASKED_RENDERER_WEBGL) : "");
  let cinematic = !/swiftshader|llvmpipe|software|basic render/i.test(device);
  if (cinematic) {
    const environment = new PMREMGenerator(renderer);
    const room = new RoomEnvironment();
    scene.environment = environment.fromScene(room, 0.04).texture;
    scene.environmentIntensity = 0.3;
    room.dispose();
    environment.dispose();
  }
  const composer = new EffectComposer(
    renderer,
    new WebGLRenderTarget(1, 1, { type: HalfFloatType, samples: 4 }),
  );
  composer.addPass(new RenderPass(scene, camera));
  const occlusion = new LibraryOcclusion(scene, camera, 1, 1);
  occlusion.updateGtaoMaterial({
    radius: 0.9,
    distanceExponent: 1.4,
    thickness: 2.2,
    scale: 1.25,
    samples: 12,
  });
  occlusion.updatePdMaterial({ radius: 7, samples: 12, rings: 2 });
  occlusion.blendIntensity = 0.95;
  composer.addPass(occlusion);
  const focus = new ShaderPass(FOCUS);
  focus.uniforms.tDepth!.value = occlusion.depthTexture;
  composer.addPass(focus);
  const bloom = new UnrealBloomPass(new Vector2(1, 1), 0.75, 0.5, 2.2);
  composer.addPass(bloom);
  const grade = new ShaderPass(GRADE);
  composer.addPass(grade);
  composer.addPass(new OutputPass());
  let width = 0;
  let height = 0;
  let density = Math.min(window.devicePixelRatio, 1.75);
  const intervals: number[] = [];

  function resize(nextWidth: number, nextHeight: number) {
    width = nextWidth;
    height = nextHeight;
    renderer.setPixelRatio(density);
    renderer.setSize(width, height, false);
    composer.setPixelRatio(density);
    composer.setSize(width, height);
    // Затенение мягкое по природе: половина разрешения неотличима, а стоит вчетверо дешевле.
    occlusion.setSize(Math.ceil((width * density) / 2), Math.ceil((height * density) / 2));
    focus.uniforms.resolution!.value.set(width * density, height * density);
  }
  return {
    get cinematic() {
      return cinematic;
    },
    resize,
    /** Резкость на глубине `depth` вдоль взгляда; `strength` от 0 до 1 растёт с приближением. */
    setFocus(depth: number, near: number, far: number, strength: number) {
      focus.uniforms.focusDepth!.value = depth;
      focus.uniforms.cameraNear!.value = near;
      focus.uniforms.cameraFar!.value = far;
      focus.uniforms.radius!.value = strength * 3.2 * density;
      focus.enabled = strength > 0.02;
    },
    render() {
      if (cinematic) composer.render();
      else renderer.render(scene, camera);
    },
    /** Длинные кадры сначала снижают плотность пикселей, затем отключают эффекты. */
    measure(interval: number) {
      if (!cinematic) return;
      intervals.push(interval);
      if (intervals.length < 45) return;
      const average = intervals.reduce((sum, value) => sum + value, 0) / intervals.length;
      intervals.length = 0;
      if (average < 52) return;
      if (density > 1) density = 1;
      else cinematic = false;
      resize(width, height);
    },
    dispose() {
      occlusion.dispose();
      bloom.dispose();
      grade.dispose();
      focus.dispose();
      composer.dispose();
      scene.environment?.dispose();
    },
  };
}
