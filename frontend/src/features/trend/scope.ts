const ENTITIES = ["directions", "technologies"] as const;
export type Entity = (typeof ENTITIES)[number];

/** Пороги стадии (§7.1) есть только у этих каналов. */
const CHANNELS = ["science", "patents", "web"] as const;
export type Channel = (typeof CHANNELS)[number];

export const DEFAULT_ENTITY: Entity = "directions";
export const DEFAULT_CHANNEL: Channel = "science";

export function readEntity(params: URLSearchParams): Entity {
  const value = params.get("entity");
  return ENTITIES.includes(value as Entity) ? (value as Entity) : DEFAULT_ENTITY;
}

export function readChannel(params: URLSearchParams): Channel {
  const value = params.get("channel");
  return CHANNELS.includes(value as Channel) ? (value as Channel) : DEFAULT_CHANNEL;
}

/** Курсор принадлежит прежней выдаче и сбрасывается. */
export function withScope(
  params: URLSearchParams,
  next: { entity?: Entity; channel?: Channel },
): URLSearchParams {
  const updated = new URLSearchParams(params);
  if (next.entity) {
    updated.delete("cursor");
    if (next.entity === DEFAULT_ENTITY) updated.delete("entity");
    else updated.set("entity", next.entity);
  }
  if (next.channel) {
    updated.delete("cursor");
    if (next.channel === DEFAULT_CHANNEL) updated.delete("channel");
    else updated.set("channel", next.channel);
  }
  return updated;
}
