import { describe, expect, test } from "bun:test";
import { DEFAULT_CHANNEL, DEFAULT_ENTITY, readChannel, readEntity, withScope } from "./scope";

describe("scope в URL", () => {
  test("неизвестное значение читается как значение по умолчанию, а не ломает экран", () => {
    expect(readEntity(new URLSearchParams("entity=nonsense"))).toBe(DEFAULT_ENTITY);
    expect(readChannel(new URLSearchParams("channel=nonsense"))).toBe(DEFAULT_CHANNEL);
    expect(readEntity(new URLSearchParams())).toBe(DEFAULT_ENTITY);
    expect(readChannel(new URLSearchParams())).toBe(DEFAULT_CHANNEL);
  });

  test("переключение сохраняет запрос и корпус и сбрасывает курсор чужой выдачи", () => {
    const before = new URLSearchParams("q=перовскит&scope=a&scope=b&cursor=page2");
    const after = withScope(before, { entity: "technologies" });
    expect(after.get("q")).toBe("перовскит");
    expect(after.getAll("scope")).toEqual(["a", "b"]);
    expect(after.get("entity")).toBe("technologies");
    expect(after.has("cursor")).toBe(false);
  });

  test("значение по умолчанию не пишется в ссылку", () => {
    const params = withScope(new URLSearchParams("entity=technologies&channel=patents"), {
      entity: DEFAULT_ENTITY,
      channel: DEFAULT_CHANNEL,
    });
    expect(params.has("entity")).toBe(false);
    expect(params.has("channel")).toBe(false);
  });

  test("ссылка восстанавливает выбранное состояние", () => {
    const link = withScope(new URLSearchParams("q=rag"), {
      entity: "technologies",
      channel: "patents",
    });
    expect(readEntity(link)).toBe("technologies");
    expect(readChannel(link)).toBe("patents");
  });
});
