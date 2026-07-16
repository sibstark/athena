import type { LogLevel } from "./config.js";

const LEVELS: Record<LogLevel, number> = { error: 0, warn: 1, info: 2, debug: 3 };

export interface Logger {
  error(message: string): void;
  warn(message: string): void;
  info(message: string): void;
  debug(message: string): void;
}

export function createLogger(level: LogLevel): Logger {
  const enabled = (candidate: LogLevel): boolean => LEVELS[candidate] <= LEVELS[level];
  return {
    error: (message) => enabled("error") && console.error(message),
    warn: (message) => enabled("warn") && console.warn(message),
    info: (message) => enabled("info") && console.log(message),
    debug: (message) => enabled("debug") && console.debug(message),
  };
}
