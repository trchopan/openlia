declare module "node:sqlite" {
  export class DatabaseSync {
    constructor(path: string);
    exec(sql: string): void;
    prepare(sql: string): StatementSync;
    close(): void;
  }

  export class StatementSync {
    run(...parameters: unknown[]): unknown;
    get(...parameters: unknown[]): unknown;
    all(...parameters: unknown[]): unknown[];
  }
}
