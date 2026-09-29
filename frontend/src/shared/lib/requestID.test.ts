import { expect, spyOn, test } from "bun:test";

import { newRequestID } from "./requestID";

test("UUID без готового генератора сохраняет случайные байты и биты версии и варианта", () => {
  const native = Object.getOwnPropertyDescriptor(crypto, "randomUUID");
  Object.defineProperty(crypto, "randomUUID", { configurable: true, value: undefined });
  const random = spyOn(crypto, "getRandomValues").mockImplementation(
    <T extends ArrayBufferView | null>(buffer: T): T => {
      expect(buffer).toBeInstanceOf(Uint8Array);
      expect(buffer?.byteLength).toBe(16);
      if (buffer instanceof Uint8Array)
        buffer.set([0, 1, 15, 16, 127, 128, 255, 254, 255, 255, 170, 85, 66, 0, 8, 240]);
      return buffer;
    },
  );
  try {
    const id = newRequestID();
    expect(id).toMatch(/^[\da-f]{8}-[\da-f]{4}-4[\da-f]{3}-[89ab][\da-f]{3}-[\da-f]{12}$/);
    expect(id).toBe("00010f10-7f80-4ffe-bfff-aa55420008f0");
    expect(random).toHaveBeenCalledTimes(1);
  } finally {
    random.mockRestore();
    if (native) Object.defineProperty(crypto, "randomUUID", native);
    else Reflect.deleteProperty(crypto, "randomUUID");
  }
});
