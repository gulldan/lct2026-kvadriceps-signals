import {
  ACESFilmicToneMapping,
  AmbientLight,
  Box3,
  CanvasTexture,
  Color,
  DirectionalLight,
  PointLight,
  Group,
  HemisphereLight,
  Mesh,
  MeshBasicMaterial,
  OrthographicCamera,
  PerspectiveCamera,
  PCFShadowMap,
  PlaneGeometry,
  Scene,
  SRGBColorSpace,
  Texture,
  Vector3,
  WebGLRenderer,
  type Material,
} from "three";
import { libraryArchitecture } from "./architecture";
import { libraryEnvironment } from "./environment";
import { libraryArchivist, libraryDeskWork, libraryLadder } from "./archivist";
import { libraryGeometry } from "./geometry";
import { libraryLight, libraryMaterials } from "./materials";
import { libraryRenderer } from "./render";
import { libraryAtmosphere } from "./atmosphere";
import { libraryBeacon } from "./beacon";
import { libraryGait } from "./gait";
import { librarySurfaces } from "./surfaces";
import { libraryMotion, type LibrarySceneState } from "./motion";
import { DESK_BOOK, SHELF_TARGETS } from "./layout";
import { LIBRARY_SECTIONS } from "../../liveSearchProgress";
import { libraryDecor } from "./decor";
import { libraryCamera, libraryDrag } from "./camera";
import { libraryShelfResponse } from "./shelfResponse";

// Узкий объектив, как у изометрических игр с перспективой: глубина без искажения плана.
const FIELD_OF_VIEW = 24;

export function createLibraryScene(
  host: HTMLDivElement,
  initial: LibrarySceneState,
  onFailure: () => void,
  onFinished?: () => void,
) {
  const renderer = new WebGLRenderer({
    antialias: true,
    alpha: true,
    powerPreference: "high-performance",
  });
  renderer.outputColorSpace = SRGBColorSpace;
  renderer.toneMapping = ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.08;
  renderer.shadowMap.enabled = true;
  renderer.shadowMap.type = PCFShadowMap;
  const canvas = renderer.domElement;
  canvas.setAttribute("aria-hidden", "true");
  host.appendChild(canvas);
  const scene = new Scene();
  const geometry = libraryGeometry();
  const materials = libraryMaterials(host);
  const light = libraryLight(host);
  const architecture = libraryArchitecture(geometry, materials);
  const decor = libraryDecor(geometry, materials);
  scene.add(architecture, decor.root);
  // Низкое вечернее солнце даёт длинные мягкие тени, прохладное небо — глубину в тени.
  // Заливка приглушена: объём и настроение дают окна, люстры и свет вокруг архивариуса.
  scene.add(new AmbientLight(light.sky, 0.06));
  scene.add(new HemisphereLight(light.sky, materials.wood.color, 0.42));
  const sunlight = new DirectionalLight(light.ember.clone().lerp(materials.paper.color, 0.55), 3.0);
  sunlight.position.set(-27, 33, 16);
  sunlight.castShadow = true;
  sunlight.shadow.mapSize.set(2048, 2048);
  sunlight.shadow.camera.left = -35;
  sunlight.shadow.camera.right = 35;
  sunlight.shadow.camera.top = 35;
  sunlight.shadow.camera.bottom = -35;
  sunlight.shadow.camera.near = 0.5;
  sunlight.shadow.camera.far = 115;
  sunlight.shadow.normalBias = 0.035;
  sunlight.shadow.bias = -0.0003;
  sunlight.shadow.radius = 3;
  scene.add(sunlight);
  const rim = new DirectionalLight(light.sky, 1.1);
  rim.position.set(6, 7, -4);
  scene.add(rim);
  const shadow = document.createElement("canvas");
  shadow.width = shadow.height = 128;
  const context = shadow.getContext("2d")!;
  const gradient = context.createRadialGradient(64, 64, 22, 64, 64, 64);
  gradient.addColorStop(0, new Color().setScalar(1).getStyle());
  gradient.addColorStop(1, "transparent");
  context.fillStyle = gradient;
  context.fillRect(0, 0, 128, 128);
  const shadowTexture = new CanvasTexture(shadow);
  const grounding = new Mesh(
    new PlaneGeometry(62, 52),
    new MeshBasicMaterial({
      color: materials.ink.color,
      alphaMap: shadowTexture,
      transparent: true,
      opacity: 0.2,
      depthWrite: false,
    }),
  );
  grounding.userData.glow = true;
  grounding.rotation.x = -Math.PI / 2;
  grounding.position.y = -1.3;
  scene.add(grounding);

  const actor = libraryArchivist(geometry, materials);
  const ladder = libraryLadder(geometry, materials);
  const work = libraryDeskWork(geometry, materials);
  const environment = libraryEnvironment(geometry, materials);
  const shelfResponse = libraryShelfResponse(geometry, materials);
  scene.add(actor.root, ladder, work.root, work.chair, environment.root, shelfResponse.root);
  const atmosphere = libraryAtmosphere(scene, sunlight.position, light);
  scene.add(atmosphere.root);
  // Настоящие источники в люстрах мерцают вместе с огнём: пятна света живут на полу и полках.
  const chandeliers = (
    (architecture.userData.emitters as { kind: string; position: Vector3 }[] | undefined) ?? []
  )
    .filter(({ kind }) => kind === "chandelier")
    .map(({ position }, index) => {
      const lamp = new PointLight(light.ember, 26, 12, 2);
      lamp.position.set(position.x, position.y + 0.55, position.z);
      scene.add(lamp);
      return { lamp, phase: index * 1.7 };
    });
  // Свет вокруг архивариуса, как у героя в изометрических играх: он всегда виден и тёплый.
  const halo = new PointLight(light.ember, 9, 6.5, 2);
  halo.position.set(0, 2.3, 0.4);
  actor.root.add(halo);
  let flame = 0;
  let models:
    | { dispose: () => void; advance: (dt: number, pose: string, time: number) => void }
    | undefined;
  const beacon = libraryBeacon(light.ember.clone().lerp(materials.brass.color, 0.35));
  const markers = LIBRARY_SECTIONS.map((section) => {
    const group = new Group();
    for (const {
      point: [x, z],
      heading,
    } of SHELF_TARGETS[section]) {
      const marker = new Group();
      marker.position.set(x, 0.1, z);
      marker.rotation.y = heading;
      beacon.attach(marker);
      group.add(marker);
    }
    scene.add(group);
    return group;
  });
  const motion = libraryMotion(actor, ladder, work, initial);
  const gait = libraryGait(actor.root);
  // Базис вида: неподвижная изометрическая ориентация, в её плоскости считаются кадр,
  // слежение и перетаскивание. Рисует перспективная камера с той же ориентацией: узкий угол
  // обзора сохраняет читаемость плана, а перспектива даёт залу глубину и параллакс.
  const basis = new OrthographicCamera(-10, 10, 7, -7, 0.1, 160);
  basis.position.set(44, 55, 44);
  basis.lookAt(0, 1.7, 0);
  basis.updateMatrixWorld();
  const camera = new PerspectiveCamera(FIELD_OF_VIEW, 1.6, 1, 400);
  camera.quaternion.copy(basis.quaternion);
  const viewDirection = new Vector3(0, 0, 1).applyQuaternion(basis.quaternion);
  const bounds = new Box3();
  const vertex = new Vector3();
  architecture.traverse((object) => {
    if (!(object instanceof Mesh)) return;
    const positions = object.geometry.getAttribute("position");
    for (let i = 0; i < positions.count; i++) {
      vertex.fromBufferAttribute(positions, i).applyMatrix4(basis.matrixWorldInverse);
      bounds.expandByPoint(vertex);
    }
  });
  const center = bounds.getCenter(new Vector3());
  const size = bounds.getSize(new Vector3());
  const deskFocus = new Vector3(DESK_BOOK[0], 0.9, DESK_BOOK[1] - 0.15).applyMatrix4(
    basis.matrixWorldInverse,
  );
  let focus = 0;
  const pipeline = libraryRenderer(renderer, scene, camera);
  // Фактура дерева, карта окружения и модели дороги для программного WebGL: там первый кадр
  // компилировал бы шейдеры секундами, поэтому остаются простые материалы.
  if (pipeline.cinematic) librarySurfaces(scene);
  atmosphere.root.visible = pipeline.cinematic;
  // Каждый точечный свет пересобирает шейдеры всех материалов: программному WebGL это не по силам.
  for (const { lamp } of chandeliers) lamp.visible = pipeline.cinematic;
  halo.visible = pipeline.cinematic;
  // Модели догружаются после первого кадра; до тех пор и без видеокарты видны заглушки.
  if (pipeline.cinematic)
    void import("./models")
      .then(({ libraryModels }) => libraryModels(scene, atmosphere))
      .then((loaded) => {
        if (disposed) return loaded.dispose();
        models = loaded;
        canvas.dataset.models = "ready";
        draw();
      })
      .catch(() => {
        canvas.dataset.models = "failed";
      });
  const exploration = libraryCamera();
  let zoom = 1;
  let finishing = initial.activity === "finish";
  let aspect = 1.6;
  let viewHeight = size.y;
  let reportedFinish = false;
  let paused = true;
  let reduced = false;
  let inView = true;
  let disposed = false;
  let contextLost = false;
  let canvasWidth = 0;
  let canvasHeight = 0;
  let request = 0;
  let previous = 0;
  let frame = 0;

  // Точка слежения: архивариус с упреждением по ходу движения, в координатах плоскости камеры.
  const tracked = new Vector3();
  const lastTracked = new Vector3().copy(actor.root.position);
  const lead = new Vector3();
  function followTarget(dt: number) {
    if (dt > 0) {
      const velocity = actor.root.position.clone().sub(lastTracked).divideScalar(dt);
      lead.lerp(velocity.multiplyScalar(0.55), 1 - Math.exp(-dt * 1.8));
    }
    lastTracked.copy(actor.root.position);
    tracked.copy(actor.root.position).add(lead);
    tracked.y += 0.9;
    tracked.applyMatrix4(basis.matrixWorldInverse);
    return [tracked.x - center.x, tracked.y - center.y] as const;
  }
  function draw() {
    if (disposed || contextLost || !canvasWidth || !canvasHeight) return;
    pipeline.render();
    canvas.dataset.frame = String(++frame);
  }
  function frameCamera() {
    const view = exploration.frame(viewHeight, aspect, size.x, size.y);
    const height = view.height + (Math.min(viewHeight, 8.5) - view.height) * focus;
    const x = center.x + view.x + (deskFocus.x - center.x - view.x) * focus;
    const y = center.y + view.y + (deskFocus.y - center.y - view.y) * focus;
    // Точка взгляда лежит в плоскости центра зала; расстояние подобрано так, чтобы на этой
    // глубине в кадр входила та же высота, что у изометрического вида.
    const target = new Vector3(x, y - height * 0.035, center.z).applyMatrix4(basis.matrixWorld);
    const distance = height / 2 / Math.tan((FIELD_OF_VIEW * Math.PI) / 360);
    camera.position.copy(target).addScaledVector(viewDirection, distance);
    camera.aspect = aspect;
    camera.near = Math.max(1, distance - 60);
    camera.far = distance + 60;
    camera.updateProjectionMatrix();
    camera.updateMatrixWorld();
    // Резкость держится на архивариусе и появляется только при приближении.
    const depth = camera.position.distanceTo(actor.root.position) * 0.98;
    pipeline.setFocus(
      depth,
      camera.near,
      camera.far,
      Math.min(1, Math.max(0, exploration.shown - 1.3) / 1.2),
    );
    atmosphere.setScale((canvasHeight * renderer.getPixelRatio()) / height);
  }
  function fit() {
    const width = host.clientWidth;
    const height = host.clientHeight;
    if (!width || !height || disposed) return;
    // Первый кадр сразу получает размер контейнера; уведомление наблюдателя не рисует его повторно.
    if (width === canvasWidth && height === canvasHeight) return;
    canvasWidth = width;
    canvasHeight = height;
    aspect = width / height;
    // Перспектива увеличивает ближний край зала, поэтому общему плану нужен запас.
    viewHeight = Math.max(size.y, size.x / aspect) * 1.16;
    pipeline.resize(width, height);
    frameCamera();
    draw();
  }
  function animate(now: number) {
    request = 0;
    if (paused || reduced || !inView || disposed || contextLost) return;
    const elapsed = now - previous;
    // Допуск в 4 мс: без него дрожание 60-герцовых кадров роняет темп до 20 кадров в секунду.
    if (elapsed >= 1000 / 30 - 4) {
      const dt = Math.min(elapsed / 1000, 0.1);
      gait.restore();
      const pose = motion.advance(dt);
      gait.apply(dt, false);
      models?.advance(dt, pose, flame);
      canvas.dataset.motion = pose;
      canvas.dataset.delivered = String(motion.deliveries);
      environment.advance(dt, actor.root.position);
      decor.advance(dt);
      atmosphere.advance(dt);
      beacon.advance(dt);
      shelfResponse.advance(dt, actor.root.position, pose);
      const closeUp = ["chair", "sit", "open", "settle"].includes(pose);
      focus += ((closeUp ? 1 : 0) - focus) * (1 - Math.exp(-dt * 2.4));
      flame += dt;
      for (const { lamp, phase } of chandeliers)
        lamp.intensity =
          26 *
          (0.9 + Math.sin(flame * 7.1 + phase) * 0.05 + Math.sin(flame * 12.7 + phase * 2) * 0.04);
      halo.intensity = 9 * (0.94 + Math.sin(flame * 5.3) * 0.04);
      exploration.advance(dt);
      exploration.follow(...followTarget(dt), 1 - Math.exp(-dt * 2.2));
      frameCamera();
      pipeline.measure(elapsed);
      previous = now;
      draw();
      if (motion.finished && !reportedFinish) {
        reportedFinish = true;
        onFinished?.();
      }
    }
    request = requestAnimationFrame(animate);
  }
  function schedule() {
    cancelAnimationFrame(request);
    request = 0;
    canvas.dataset.animating = String(!paused && !reduced && inView && !contextLost);
    if (!paused && !reduced && inView && !disposed && !contextLost) {
      previous = performance.now();
      request = requestAnimationFrame(animate);
    }
  }
  const observer = new ResizeObserver(fit);
  observer.observe(host);
  const intersection = new IntersectionObserver(([entry]) => {
    inView = entry?.isIntersecting ?? true;
    schedule();
  });
  intersection.observe(host);
  const loseContext = (event: Event) => {
    event.preventDefault();
    contextLost = true;
    schedule();
    onFailure();
  };
  canvas.addEventListener("webglcontextlost", loseContext);
  function pan(dx: number, dy: number) {
    if (finishing) return;
    exploration.pan(dx, dy, canvasWidth, canvasHeight, viewHeight);
    frameCamera();
    draw();
  }
  const releaseDrag = libraryDrag(canvas, () => zoom > 1 && !finishing && !contextLost, pan);
  function update(state: LibrarySceneState) {
    finishing = state.activity === "finish";
    motion.update(state);
    markers.forEach((marker, index) => {
      marker.visible = LIBRARY_SECTIONS[index] === state.shelf;
    });
    canvas.dataset.consulted = String(state.consulted);
    canvas.dataset.found = String(state.found);
    if (reduced) {
      gait.restore();
      canvas.dataset.motion = motion.advance(0, true);
      gait.apply(0, true);
      environment.advance(0, actor.root.position, true);
      decor.advance(0, true);
      shelfResponse.root.visible = false;
      exploration.follow(...followTarget(0), 1);
      frameCamera();
    }
    draw();
  }
  update(initial);
  fit();
  return {
    update,
    setZoom(value: number) {
      zoom = value;
      // Без анимации вид встаёт сразу, с анимацией камера доезжает сама.
      const instant = paused || reduced || !inView;
      exploration.setZoom(value, instant);
      if (instant) exploration.follow(...followTarget(0), 1);
      canvas.dataset.zoom = String(value);
      frameCamera();
      draw();
    },
    pan,
    setPaused(value: boolean, reduceMotion: boolean) {
      paused = value;
      reduced = reduceMotion;
      if (reduced) {
        gait.restore();
        canvas.dataset.motion = motion.advance(0, true);
        gait.apply(0, true);
        environment.advance(0, actor.root.position, true);
        decor.advance(0, true);
        shelfResponse.root.visible = false;
        exploration.setZoom(zoom, true);
        exploration.follow(...followTarget(0), 1);
        frameCamera();
        draw();
      }
      schedule();
    },
    dispose() {
      disposed = true;
      cancelAnimationFrame(request);
      observer.disconnect();
      intersection.disconnect();
      canvas.removeEventListener("webglcontextlost", loseContext);
      releaseDrag();
      const allMaterials = new Set<Material>(Object.values(materials));
      const allGeometry = new Set<Mesh["geometry"]>();
      scene.traverse((object) => {
        if (!(object instanceof Mesh)) return;
        allGeometry.add(object.geometry);
        (Array.isArray(object.material) ? object.material : [object.material]).forEach((material) =>
          allMaterials.add(material),
        );
      });
      allGeometry.forEach((item) => item.dispose());
      allMaterials.forEach((item) => {
        for (const value of Object.values(item)) if (value instanceof Texture) value.dispose();
        item.dispose();
      });
      geometry.dispose();
      environment.dispose();
      decor.dispose();
      atmosphere.dispose();
      beacon.dispose();
      models?.dispose();
      sunlight.shadow.dispose();
      shadowTexture.dispose();
      pipeline.dispose();
      renderer.dispose();
      renderer.forceContextLoss();
      canvas.remove();
    },
  };
}
