import type { BitrixMessageEvent } from "./types.js";

type RecordValue = Record<string, unknown>;

export class BitrixPayloadError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "BitrixPayloadError";
  }
}

function record(value: unknown): RecordValue | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as RecordValue)
    : undefined;
}

function stringValue(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) return value.trim();
    if (typeof value === "number" && Number.isFinite(value)) return String(value);
  }
  return undefined;
}

function numberValue(...values: unknown[]): number | undefined {
  for (const value of values) {
    const number = typeof value === "number" ? value : Number(value);
    if (Number.isInteger(number) && number >= 0) return number;
  }
  return undefined;
}

function booleanValue(value: unknown): boolean {
  if (typeof value === "boolean") return value;
  if (typeof value === "number") return value !== 0;
  if (typeof value === "string") return ["1", "y", "yes", "true"].includes(value.toLowerCase());
  return false;
}

function firstRecord(...values: unknown[]): RecordValue | undefined {
  return values.map(record).find((value): value is RecordValue => value !== undefined);
}

function firstValue(...values: unknown[]): unknown {
  return values.find((value) => value !== undefined && value !== null);
}

function assignNested(target: RecordValue, key: string, value: string): void {
  const path = [...key.matchAll(/([^\[\]]+)/g)]
    .map((match) => match[1])
    .filter((part): part is string => Boolean(part));
  const last = path.at(-1);
  if (!last) return;

  let current = target;
  for (const part of path.slice(0, -1)) {
    const existing = current[part];
    if (!record(existing)) current[part] = {};
    current = current[part] as RecordValue;
  }
  current[last] = value;
}

async function readBody(request: Request): Promise<RecordValue> {
  const body = await request.text();
  if (new TextEncoder().encode(body).byteLength > 1_048_576) {
    throw new BitrixPayloadError("Request body is too large");
  }

  const contentType = request.headers.get("content-type")?.toLowerCase() ?? "";
  if (contentType.includes("application/x-www-form-urlencoded")) {
    const parsed: RecordValue = {};
    for (const [key, value] of new URLSearchParams(body)) assignNested(parsed, key, value);
    return parsed;
  }

  try {
    const parsed: unknown = JSON.parse(body);
    const result = record(parsed);
    if (!result) throw new Error("JSON root is not an object");
    return result;
  } catch {
    throw new BitrixPayloadError("Invalid webhook payload");
  }
}

export async function parseBitrixPayload(request: Request): Promise<BitrixMessageEvent> {
  const root = await readBody(request);
  const data = firstRecord(root.data, root.DATA) ?? root;
  const auth = firstRecord(root.auth, data.auth, root.AUTH) ?? {};
  const params = firstRecord(data.params, data.PARAMS, root.params, root.PARAMS) ?? {};
  const message = firstRecord(root.message, data.message, root.MESSAGE, data.MESSAGE, params.message, params) ?? {};
  const chat = firstRecord(root.chat, data.chat, root.CHAT, data.CHAT) ?? {};
  const bot = firstRecord(root.bot, data.bot, root.BOT) ?? {};

  const event = stringValue(root.event, data.event, root.EVENT);
  const applicationToken = stringValue(
    auth.application_token,
    auth.applicationToken,
    auth.APPLICATION_TOKEN,
    root.application_token,
  );
  const text = stringValue(message.text, message.message, message.MESSAGE, params.message, params.MESSAGE);
  const authorId = numberValue(message.authorId, message.author_id, message.AUTHOR_ID, params.authorId, params.FROM_USER_ID);
  const dialogId = stringValue(
    chat.dialogId,
    chat.dialog_id,
    chat.DIALOG_ID,
    message.dialogId,
    params.dialogId,
    params.DIALOG_ID,
    root.dialogId,
  );
  const botId = numberValue(bot.id, bot.botId, data.botId, root.botId, params.BOT_ID);

  if (!event || !applicationToken || !text || authorId === undefined || !dialogId || botId === undefined) {
    throw new BitrixPayloadError("Webhook payload does not contain the required Bitrix fields");
  }

  return {
    event,
    applicationToken,
    message: {
      text,
      authorId,
      isSystem: booleanValue(firstValue(message.isSystem, message.is_system, message.system, message.SYSTEM, params.SYSTEM)),
    },
    chat: { dialogId },
    bot: { id: botId },
  };
}
